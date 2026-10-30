import assert from 'node:assert/strict'
import { spawn, execFileSync } from 'node:child_process'
import { createHash, randomBytes } from 'node:crypto'
import { createWriteStream } from 'node:fs'
import { mkdtemp, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'node:net'
import { createServer as createHTTPServer } from 'node:http'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const platforms = { win32: 'windows', linux: 'linux', darwin: 'darwin' }
const architectures = { x64: 'amd64', arm64: 'arm64' }
const platform = platforms[process.platform]
const architecture = architectures[process.arch]
assert(platform && architecture, 'No standalone target for this host')
const executable = `columba-${platform}-${architecture}${platform === 'windows' ? '.exe' : ''}`
const binary = join(root, 'dist', 'standalone', executable)
const binaryBytes = await readFile(binary)
const expectedCommit = execFileSync('git', ['rev-parse', '--short', 'HEAD'], { cwd: root, encoding: 'utf8', windowsHide: true }).trim()
const directory = await mkdtemp(join(tmpdir(), 'columba-standalone-smoke-'))
const token = randomBytes(24).toString('hex')
const startupTimeoutMillis = 30000
const requestTimeoutMillis = 2000

const listener = createServer()
await new Promise((resolve, reject) => {
  listener.once('error', reject)
  listener.listen(0, '127.0.0.1', resolve)
})
const port = listener.address().port
await new Promise((resolve, reject) => listener.close(error => error ? reject(error) : resolve()))
const baseURL = `http://127.0.0.1:${port}`
const config = join(directory, 'config.json')
await writeFile(config, JSON.stringify({
  host: '127.0.0.1', port, databasePath: join(directory, 'gateway.sqlite3'),
  openBrowserOnStart: false, logLifecycleVersion: 1, outbound: { deniedIpRanges: [] },
  modelCatalog: { enabled: false, syncIntervalMinutes: 0 },
}), { mode: 0o600 })
const log = createWriteStream(join(directory, 'server.log'))
const child = spawn(binary, ['-config', config], { cwd: directory, stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true })
child.stdout.pipe(log, { end: false })
child.stderr.pipe(log, { end: false })
const completion = new Promise((resolve, reject) => {
  child.once('error', reject)
  child.once('close', (code, signal) => resolve({ code, signal }))
})
completion.catch(() => {})

async function readJSON(path, isAdmin = false) {
  const response = await fetch(`${baseURL}${path}`, {
    signal: AbortSignal.timeout(requestTimeoutMillis),
    headers: {},
  })
  assert.equal(response.status, 200, path)
  const result = await response.json()
  if (!isAdmin) return result
  assert.equal(result.ok, true, path)
  return result.data
}

try {
  let health
  const deadline = Date.now() + startupTimeoutMillis
  while (Date.now() < deadline) {
    assert(child.exitCode === null && child.signalCode === null, 'Binary exited before readiness; inspect server.log')
    try {
      health = await readJSON('/health')
      break
    } catch { // New databases may still be migrating while the listener starts.
      await delay(250)
    }
  }
  assert(health, 'Standalone readiness timed out')
  assert.equal(health.database, true)
  assert.equal(health.status, 'ok')
  assert.equal(health.commit, expectedCommit)
  for (const path of ['/api/admin/protocols/schema', '/api/admin/model-groups', '/api/admin/agent/status']) {
    const response = await fetch(baseURL + path)
    await response.arrayBuffer()
    assert.equal(response.status, 404, path)
  }
  const malicious = await fetch(baseURL + '/api/admin/api-tokens', { headers: { Origin: 'https://example.com' } })
  assert.equal(malicious.status, 403)
  await malicious.arrayBuffer()
  const unauthorized = await fetch(baseURL + '/v1/models')
  assert.equal(unauthorized.status, 401)
  await unauthorized.arrayBuffer()
  const admin = async (path, method, body) => {
    const response = await fetch(baseURL + '/api/admin' + path, {
      method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
      signal: AbortSignal.timeout(requestTimeoutMillis),
    })
    assert.equal(response.status, 200, path + ': ' + await response.text())
  }
  await admin('/api-tokens', 'POST', { name: 'smoke', token, enabled: true })
  const wire = '{"id":"synthetic","choices":[],"unknown":{"preserved":true},"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}'
  const requestBody = '{"model":"synthetic-model","messages":[],"unknown":1.00}'
  const upstream = createHTTPServer(async (request, response) => {
    let body = ''
    for await (const chunk of request) body += chunk
    assert.equal(body, requestBody)
    assert.equal(request.headers.authorization, 'Bearer synthetic-upstream')
    response.writeHead(200, { 'Content-Type': 'application/json' })
    response.end(wire)
  })
  await new Promise(resolve => upstream.listen(0, '127.0.0.1', resolve))
  try {
    await admin('/model-sources', 'POST', {
      id: 'smoke', name: 'smoke', platform: 'openai', enabled: true, autoFetchModels: false,
      baseUrl: `http://127.0.0.1:${upstream.address().port}`, apiKey: 'synthetic-upstream',
      manualModels: [{ id: 'synthetic-model', name: 'synthetic-model', enabled: true, available: true }],
    })
    await admin('/model-prices', 'PUT', { sourceId: 'smoke', modelId: 'synthetic-model', override: { input: '1', output: '2' } })
    const models = await fetch(baseURL + '/v1/models', { headers: { Authorization: `Bearer ${token}` } })
    assert.deepEqual((await models.json()).data.map(item => item.id), ['synthetic-model'])
    const response = await fetch(baseURL + '/v1/chat/completions', { method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: requestBody })
    assert.equal(response.status, 200)
    assert.equal(await response.text(), wire)
    let costs
    for (let i = 0; i < 40; i++) {
      costs = await readJSON('/api/admin/usage/costs', true)
      if (costs.amount === '0.000020000000' && costs.unpriced === 0) break
      await delay(50)
    }
    assert.equal(costs.amount, '0.000020000000')
    assert.equal(costs.unpriced, 0)
    await admin('/usage/reset', 'POST', {})
    assert.equal((await readJSON('/api/admin/usage/costs', true)).amount, costs.amount)
  } finally { await new Promise(resolve => upstream.close(resolve)) }
  const ui = await fetch(`${baseURL}/ui/`, { signal: AbortSignal.timeout(requestTimeoutMillis) })
  assert.equal(ui.status, 200)
  const html = await ui.text()
  const script = html.match(/<script[^>]+src="([^"]+)"/)?.[1]
  assert(script, 'Embedded UI script missing')
  const asset = await fetch(new URL(script, ui.url), { signal: AbortSignal.timeout(requestTimeoutMillis) })
  assert.equal(asset.status, 200)
  assert((await asset.arrayBuffer()).byteLength > 0)
  const evidence = {
    executable, sha256: createHash('sha256').update(binaryBytes).digest('hex'),
    platform, architecture, health, nativePassthrough: true, persistentCosts: true, localPanel: true,
    embeddedUI: true, passed: true, timestamp: new Date().toISOString(),
  }
  await writeFile(join(directory, 'evidence.json'), JSON.stringify(evidence, null, 2))
  console.log(JSON.stringify(evidence, null, 2))
} finally {
  // Only this isolated child is owned by the smoke runner.
  if (child.exitCode === null && child.signalCode === null) child.kill('SIGTERM')
  await completion
  await new Promise(resolve => log.end(resolve))
  console.log(`Isolated standalone smoke data retained at ${directory}`)
}
