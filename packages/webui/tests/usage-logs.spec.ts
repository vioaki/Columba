import { expect, test } from '@playwright/test'
import type { UsageLogDetail } from '../src/lib/types'

const emptyBody = { content: '', truncated: false }
const details: UsageLogDetail[] = [
  {
    requestId: 'missing', startedAt: '2026-09-28T12:10:00Z', endedAt: '2026-09-28T12:10:01Z',
    keyName: 'test-key', keyHash: '', requestedModelGroup: 'gpt-6-luna', groupName: '', modelName: '',
    platform: '', inputFormat: 'openai_responses', stream: true, statusCode: 404,
    error: 'Model not found', errorKind: 'model_not_found', firstByteMs: 0, durationMs: 5, usage: {}, retryCount: 0,
    incomingBody: emptyBody, outgoingBody: emptyBody, providerResponse: emptyBody, downstreamResponse: emptyBody,
  },
]
const base = { ...details[0], sourceId: 'deepseek', statusCode: 200, error: '', errorKind: '', firstByteMs: 250, durationMs: 1600 }
details.push(
  { ...base, requestId: 'same', requestedModelGroup: 'deepseek-flash', modelName: 'deepseek-flash', groupName: 'deepseek-flash', platform: 'deepseek', sourceFormat: 'openai', targetFormat: 'chat_completions' },
  { ...base, requestId: 'mapped', requestedModelGroup: 'long-routing-group-name-that-needs-truncation-'.repeat(3), modelName: 'actual-model', platform: 'openai', sourceFormat: 'openai_responses', targetFormat: 'openai' },
  { ...base, requestId: 'agent', keyName: 'AI 助手', requestedModelGroup: 'deepseek-flash', modelName: 'deepseek-flash', inputFormat: '', platform: 'deepseek', targetFormat: 'chat_completions', relayMode: 'agent-assist',
    upstreamRevision: 'diagnostic-revision', cacheSynthesis: true,
    systemStructure: { before: [{ path: '/content/0', kind: 'message', metadata: ['attributes'], cacheCount: 0 }], after: [{ path: '/content/0', kind: 'message', metadata: ['attributes'], cacheCount: 0 }] },
    incomingBody: { ...emptyBody, content: '{"Messages":[{"role":"user","content":"hello"}]}' },
    outgoingBody: { ...emptyBody, content: '{"model":"deepseek-flash","stream":true}' },
    providerResponse: { ...emptyBody, content: '[{"choices":[{"delta":{"content":"hello"}}]}]' },
    downstreamResponse: { ...emptyBody, content: '{"result":{"Text":"hello"}}' },
  },
  { ...base, requestId: 'cancelled', requestedModelGroup: 'chat', modelName: 'deepseek-flash', statusCode: 499, errorKind: 'client_canceled', error: 'context canceled', sourceFormat: 'openai', targetFormat: 'openai' },
  { ...base, requestId: 'gemini', requestedModelGroup: 'chat', modelName: 'gemini-flash', platform: 'gemini', sourceFormat: 'openai', relayMode: 'transform' },
)

test('normalized cache creation is an input component and absent counters stay unknown', async ({ page }) => {
  const scenarios = [
    { usage: { inputTokens: 180, cacheHitTokens: 30, cacheCreationTokens: 50, outputTokens: 200 }, total: '380', remainder: '未命中输入 100', creation: '缓存创建 50' },
    { usage: { inputTokens: 0, cacheHitTokens: 0, cacheCreationTokens: 0, outputTokens: 0 }, total: '0', remainder: '未命中输入 0', creation: '缓存创建 0' },
    { usage: { inputTokens: 100, outputTokens: 200 }, total: '300', remainder: '其余输入 100', creation: '缓存创建 —' },
    { usage: { outputTokens: 200 }, total: '—', remainder: '其余输入 —', creation: '缓存创建 —' },
  ]
  for (const scenario of scenarios) {
    await page.route('**/api/admin/usage/logs/gemini', (route) => route.fulfill({ json: { ok: true, data: { ...details[5], usage: scenario.usage } } }))
    await page.getByRole('button', { name: '查看请求 gemini 详情', exact: true }).click()
    const usage = page.getByLabel('Token 用量', { exact: true })
    await expect(usage).toContainText(`合计 ${scenario.total}`)
    await expect(usage).toContainText(scenario.remainder)
    await expect(usage).toContainText(scenario.creation)
    await page.getByRole('dialog', { name: '调用详情' }).getByRole('button', { name: '关闭', exact: true }).last().click()
  }
})



test('delivery evidence separates HTTP from logical errors and does not infer who canceled', async ({ page }) => {
  const record: UsageLogDetail = {
    ...details[5], statusCode: 502, upstreamHttpStatus: 200,
    error: 'synthetic delivery failure', errorKind: 'downstream_transport',
    delivery: { httpStatus: 200, outcome: 'failed', phase: 'response_finalize', cause: 'downstream_transport', error: 'synthetic final write error' },
  }
  await page.route('**/api/admin/usage/logs/gemini', (route) => route.fulfill({ json: { ok: true, data: record } }))
  await page.getByRole('button', { name: '查看请求 gemini 详情', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '调用详情' })
  await expect(dialog.getByTestId('upstream-http-status')).toHaveText('200')
  await expect(dialog.getByTestId('logical-status')).toHaveText('502')
  await expect(dialog.getByTestId('downstream-http-status')).toHaveText('200')
  await expect(dialog.getByTestId('delivery-outcome')).toHaveText('交付失败')
  await expect(dialog.getByText('结束响应（response_finalize）')).toBeVisible()
  const downloaded = page.waitForEvent('download')
  await dialog.getByRole('button', { name: '导出记录' }).click()
  const stream = await (await downloaded).createReadStream()
  let exported = ''
  for await (const chunk of stream!) exported += chunk.toString()
  expect(JSON.parse(exported)).toMatchObject({ statusCode: 502, upstreamHttpStatus: 200, delivery: record.delivery })
  await dialog.getByRole('button', { name: '关闭', exact: true }).last().click()

  await page.route('**/api/admin/usage/logs/gemini', (route) => route.fulfill({ json: { ok: true, data: {
    ...record, statusCode: 503, errorKind: 'request_canceled',
    delivery: { ...record.delivery, cause: 'request_canceled' },
  } } }))
  await page.getByRole('button', { name: '查看请求 gemini 详情', exact: true }).click()
  await expect(dialog.getByTestId('logical-status')).toHaveText('503')
  await expect(dialog.getByText('客户端取消', { exact: false })).toHaveCount(0)
})

test('an aborted response shows the server panic cause without rewriting committed HTTP status', async ({ page }) => {
  await page.route('**/api/admin/usage/logs/gemini', (route) => route.fulfill({ json: { ok: true, data: {
    ...details[5], statusCode: 500, upstreamHttpStatus: 200,
    error: 'response aborted by request panic', errorKind: 'server_error',
    delivery: { httpStatus: 200, outcome: 'failed', phase: 'response_abort', cause: 'server_panic', error: 'response aborted by request panic' },
  } } }))
  await page.getByRole('button', { name: '查看请求 gemini 详情', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '调用详情' })
  await expect(dialog.getByTestId('logical-status')).toHaveText('500')
  await expect(dialog.getByTestId('downstream-http-status')).toHaveText('200')
  await expect(dialog.getByTestId('delivery-outcome')).toHaveText('交付失败')
  await expect(dialog.getByText('中止响应（response_abort）', { exact: true })).toBeVisible()
  await expect(dialog.getByText('服务处理异常（server_panic）', { exact: true })).toBeVisible()
})


test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('columba-webui.theme', 'light')
  })
  await page.route('**/api/admin/**', async (route) => {
    const url = new URL(route.request().url())
    let data: unknown = { items: [], total: 0 }
    if (url.pathname.endsWith('/seq')) data = { seq: 1 }
    else if (url.pathname.endsWith('/protocols/enabled')) data = { items: [
      { id: 'openai-chat-completions', name: 'Openai Chat Completions' },
      { id: 'openai-responses', name: 'Openai Responses' },
      { id: 'anthropic-messages', name: 'Anthropic Messages' },
      { id: 'google-generate-content', name: 'Google Generate-content' },
      { id: 'team-relay', name: 'Team Gateway' },
    ] }
    else if (url.pathname.endsWith('/model-sources')) data = { items: [{ id: 'deepseek', name: 'DeepSeek', enabled: true }] }
    else if (url.pathname.endsWith('/usage/logs')) {
      const keys = url.searchParams.getAll('keyName')
      const items = details.filter((d) => !keys.length || keys.includes(d.keyName)).map((d) => ({ ...d, inputTokens: 100, outputTokens: 20 }))
      data = { total: items.length, items }
    } else if (url.pathname.includes('/usage/logs/')) data = details.find((d) => url.pathname.endsWith(`/${d.requestId}`))
    await route.fulfill({ json: { ok: true, data } })
  })
  await page.goto('/#/usage-logs')
})






for (const viewport of [{ width: 1440, height: 800 }, { width: 1440, height: 500 }, { width: 375, height: 812 }]) {
  test(`opening log details preserves page and sidebar position at ${viewport.width}x${viewport.height}`, async ({ page }) => {
    await page.setViewportSize(viewport)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    const rows = Array.from({ length: 20 }, (_, index) => ({ ...details[3], requestId: `row-${index}`, keyName: 'test-key', relayMode: 'transform', sourceFormat: 'openai', targetFormat: 'gemini', platform: 'gemini' }))
    await page.route('**/api/admin/usage/logs?**', (route) => route.fulfill({ json: { ok: true, data: { items: rows, total: rows.length } } }))
    await page.route('**/api/admin/usage/logs/row-*', (route) => route.fulfill({ json: { ok: true, data: rows.find((row) => route.request().url().endsWith(`/${row.requestId}`)) } }))
    await page.reload()
    const row = page.getByRole('button', { name: '查看请求 row-18 详情', exact: true })
    await row.scrollIntoViewIfNeeded()
    const scrollY = await page.evaluate(() => window.scrollY)
    expect(scrollY).toBeGreaterThan(500)
    const sidebar = page.locator('aside').first()
    const sidebarBox = await sidebar.boundingBox()
    const nav = sidebar.locator('nav')
    const navScroll = await nav.evaluate((el) => { el.scrollTop = el.scrollHeight; return el.scrollTop })
    await row.click()
    const sheet = page.getByRole('dialog', { name: '调用详情' })
    await expect(sheet.getByText('deepseek-flash · gemini', { exact: true })).toBeVisible()
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(scrollY)
    await expect.poll(() => sidebar.boundingBox()).toEqual(sidebarBox)
    await expect.poll(() => nav.evaluate((el) => el.scrollTop)).toBe(navScroll)
    await page.mouse.move(10, viewport.height / 2)
    await page.mouse.wheel(0, -400)
    await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    expect(await page.evaluate(() => window.scrollY)).toBe(scrollY)
    expect(await sidebar.boundingBox()).toEqual(sidebarBox)
    for (const segment of ['incoming', 'outgoing', 'provider', 'downstream']) {
      await expect(sheet.locator(`#chain-trigger-${segment}`)).toHaveAttribute('aria-expanded', 'false')
    }
    await sheet.locator('#chain-trigger-incoming').click()
    await expect(sheet.locator('#chain-trigger-incoming')).toHaveAttribute('aria-expanded', 'true')
    await page.keyboard.press('Escape')
    await expect(sheet).not.toBeVisible()
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(scrollY)
    await expect.poll(() => sidebar.boundingBox()).toEqual(sidebarBox)
    await expect.poll(() => nav.evaluate((el) => el.scrollTop)).toBe(navScroll)
    await row.press('Enter')
    await expect(sheet.locator('#chain-trigger-incoming')).toHaveAttribute('aria-expanded', 'false')
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(scrollY)
    await expect.poll(() => sidebar.boundingBox()).toEqual(sidebarBox)
    await expect.poll(() => nav.evaluate((el) => el.scrollTop)).toBe(navScroll)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}

test('large bodies render only when expanded and preserve JSON and SSE contents', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const messages = Array.from({ length: 6000 }, (_, index) => ({ role: 'user', content: `message ${index} <script>sample</script>` }))
  const request = { model: 'test-model', messages }
  const jsonBody = { ...emptyBody, content: JSON.stringify(request) }
  const sseBody = { ...emptyBody, content: messages.map((message) => `data: ${JSON.stringify({ choices: [{ delta: message }] })}\n\n`).join('') + 'data: [DONE]\n\n' }
  await page.route('**/api/admin/usage/logs/agent', (route) => route.fulfill({ json: { ok: true, data: {
    ...details[3], incomingBody: jsonBody, outgoingBody: jsonBody, providerResponse: sseBody, downstreamResponse: sseBody,
  } } }))
  await page.getByRole('button', { name: '查看请求 agent 详情', exact: true }).click()
  const sheet = page.getByRole('dialog', { name: '调用详情' })
  await expect(sheet.locator('#chain-trigger-downstream')).toBeVisible()
  await expect(sheet.locator('[id^="chain-body-"] pre')).toHaveCount(0)
  await sheet.locator('#chain-trigger-incoming').click()
  await expect(sheet.locator('[id^="chain-body-"] pre')).toHaveCount(1)
  expect(JSON.parse((await sheet.locator('#chain-body-incoming pre').textContent())!)).toEqual(request)
  await expect(sheet.locator('#chain-body-incoming script')).toHaveCount(0)
  await sheet.locator('#chain-trigger-provider').click()
  await expect(sheet.locator('[id^="chain-body-"] pre')).toHaveCount(2)
  await expect(sheet.locator('#chain-body-provider pre')).toContainText('message 5999 <script>sample</script>')
  await expect(sheet.locator('#chain-body-provider pre')).toContainText('[DONE]')
  await sheet.locator('#chain-trigger-incoming').click()
  await expect(sheet.locator('#chain-body-incoming pre')).toHaveCount(0)
  await expect(sheet.locator('#chain-body-provider pre')).toBeVisible()
})
