import { expect, test } from '@playwright/test'

test.beforeEach(async ({ page }) => {
  await page.route('**/api/auth/session', route => route.fulfill({ json: { ok: true, data: { authenticated: true, setupRequired: false, setupAllowed: true } } }))
  await page.route('**/api/admin/**', route => {
    const path = new URL(route.request().url()).pathname
    const data = /\/usage\/(trend|by-model|by-model-daily)$/.test(path) ? []
      : path.endsWith('/health') ? { memory: { alloc: 0, sys: 0, numGC: 0 } }
      : path.endsWith('/usage/stats') ? { requests: 0, success: 0, failed: 0, inputTokens: 0, outputTokens: 0, totalTokens: 0, cacheHitTokens: 0, cacheHitRate: 0 }
      : path.endsWith('/usage/pulse') ? { points: [], window: { requests: 0, totalTokens: 0, avgDurationMs: 0, p95DurationMs: 0 } }
      : path.endsWith('/usage/costs') ? { amount: '0.120000000001', attempts: 2, requests: 2, unpriced: 1, models: [], sources: [], keys: [], trend: [] }
      : path.endsWith('/usage/cost-attempts') ? { items: [{ requestId: 'unpriced', attempt: 1, startedMs: 1, modelId: 'demo', sourceId: 'local', status: 'missing_usage', amount: null, reasons: ['provider did not report usage'], usage: {}, price: {} }] }
      : { items: [], total: 0, seq: 1 }
    return route.fulfill({ json: { ok: true, data } })
  })
})

test('opens the authenticated panel with five sections', async ({ page }) => {
  await page.goto('/#/tokens')
  await expect(page.getByRole('heading', { name: '调用 Key', exact: true })).toBeVisible()
  await expect(page.getByRole('navigation', { name: '主导航' }).getByRole('link')).toHaveText(['总览', '模型', '调用 Key', '用量费用', '设置'])
  await expect(page.getByRole('button', { name: '新增 Key' }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()
})

test('sets a password, logs out, rejects a wrong password and returns to login on expiry', async ({ page }) => {
  let authenticated = false
  let setupRequired = true
  await page.route('**/api/auth/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/login')) {
      expect(route.request().headers()['x-columba-panel']).toBe('1')
      if (route.request().postDataJSON().password !== 'test-password-123') {
        return route.fulfill({ status: 400, json: { ok: false, error: { code: 'invalid_credentials', message: '密码不正确' } } })
      }
      authenticated = true
      setupRequired = false
    }
    if (path.endsWith('/logout')) authenticated = false
    return route.fulfill({ json: { ok: true, data: { authenticated, setupRequired, setupAllowed: true } } })
  })
  await page.goto('/#/tokens')
  await expect(page.getByRole('heading', { name: '设置管理密码' })).toBeVisible()
  await page.getByLabel('新密码', { exact: true }).fill('test-password-123')
  await page.getByLabel('确认新密码', { exact: true }).fill('test-password-123')
  await page.getByRole('button', { name: '设置密码并进入' }).click()
  await expect(page.getByRole('heading', { name: '调用 Key', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '退出登录' }).click()
  await expect(page.getByRole('heading', { name: '登录', exact: true })).toBeVisible()
  await page.getByLabel('管理密码').fill('wrong-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('密码不正确')
  await page.getByLabel('管理密码').fill('test-password-123')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: '调用 Key', exact: true })).toBeVisible()
  expect(await page.evaluate(() => Object.keys(localStorage).some(key => /token|password|session/.test(key)))).toBe(false)
  authenticated = false
  await page.route('**/api/admin/usage/seq', route => route.fulfill({ status: 401, json: { ok: false } }))
  await expect(page.getByRole('heading', { name: '登录', exact: true })).toBeVisible()
})

test('distinguishes unknown costs and applies model filters', async ({ page }) => {
  await page.goto('/#/usage')
  await expect(page.getByText('$0.120000000001', { exact: true })).toBeVisible()
  await expect(page.getByText('金额未知', { exact: true })).toBeVisible()
  const filtered = page.waitForRequest(request => {
    const url = new URL(request.url())
    return url.pathname.endsWith('/usage/costs') && url.searchParams.get('model') === 'org/model'
  })
  await page.getByLabel('费用模型 ID').fill('org/model')
  await filtered
  await page.getByRole('tab', { name: '费用', exact: true }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('tab', { name: 'Token 统计' })).toBeFocused()
})

test('supports neutral themes and keyboard navigation in small windows', async ({ page }) => {
  await page.goto('/#/tokens')
  const addKey = page.getByRole('button', { name: '新增 Key' }).first()
  for (const dark of [false, true]) {
    await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
    await expect(addKey).toHaveCSS('background-color', dark ? 'rgb(235, 235, 235)' : 'rgb(31, 31, 31)')
  }
  await page.setViewportSize({ width: 375, height: 812 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  const menu = page.getByRole('button', { name: '打开导航菜单' })
  await menu.click()
  const drawer = page.getByRole('dialog', { name: '导航菜单', exact: true })
  await expect(drawer.getByRole('link')).toHaveCount(5)
  await page.keyboard.press('Tab')
  expect(await drawer.evaluate(element => element.contains(document.activeElement))).toBe(true)
  await page.keyboard.press('Escape')
  await expect(menu).toBeFocused()
})

test.describe('daily usage defaults', () => {
  test.use({ timezoneId: 'Asia/Shanghai' })

  test('opens daily views at local midnight and keeps historical ranges selectable', async ({ page }) => {
    await page.clock.install({ time: new Date('2026-10-30T23:59:00+08:00') })
    await page.addInitScript(() => localStorage.setItem('columba-webui.overview-kpi-range', 'all'))
    const popular = page.waitForRequest('**/usage/by-model?*')
    await page.goto('/#/overview')
    const kpi = page.getByRole('group', { name: '核心用量时间窗', exact: true })
    await expect(kpi.getByRole('button')).toHaveText(['今日', '累计'])
    await expect(kpi.getByRole('button', { name: '今日', exact: true })).toHaveAttribute('aria-pressed', 'true')
    expect(new URL((await popular).url()).searchParams.get('from')).toBe('2026-10-29T16:00:00.000Z')
    await kpi.getByRole('button', { name: '累计', exact: true }).click()
    await expect(page.getByText('累计请求总数', { exact: true })).toBeVisible()
    for (const [tab, endpoint] of [['calls', 'logs'], ['tokens', 'stats']]) {
      await page.goto(`/#/usage?tab=${tab}`)
      const request = page.waitForRequest(`**/usage/${endpoint}?*`)
      await page.reload()
      await expect(page.getByRole('group', { name: '时间窗', exact: true }).getByRole('button', { name: '今日', exact: true })).toHaveAttribute('aria-pressed', 'true')
      const query = new URL((await request).url()).searchParams
      expect(query.get('from')).toBe('2026-10-29T16:00:00.000Z')
      expect(query.get('to')).toBe('2026-10-30T16:00:00.000Z')
    }
    const tomorrow = page.waitForRequest(request => {
      const url = new URL(request.url())
      return url.pathname.endsWith('/usage/stats') && url.searchParams.get('from') === '2026-10-30T16:00:00.000Z'
    })
    await page.clock.fastForward(60_000)
    await tomorrow
    const allTime = page.waitForRequest(request => new URL(request.url()).pathname.endsWith('/usage/stats') && !new URL(request.url()).searchParams.has('from'))
    await page.getByRole('group', { name: '时间窗', exact: true }).getByRole('button', { name: '全部', exact: true }).click()
    await allTime
  })

  test('advances default billing dates at midnight and allows unbounded history', async ({ page }) => {
    await page.clock.install({ time: new Date('2026-10-30T23:59:00+08:00') })
    const first = page.waitForRequest('**/usage/costs?*')
    await page.goto('/#/usage')
    const query = new URL((await first).url()).searchParams
    expect(Number(query.get('from'))).toBe(Date.parse('2026-10-29T16:00:00Z'))
    expect(Number(query.get('to'))).toBe(Date.parse('2026-10-30T16:00:00Z'))
    await page.getByLabel('费用模型 ID').fill('demo')
    await page.clock.fastForward(60_000)
    await expect(page.getByLabel('费用开始日期')).toHaveValue('2026-10-31')
    await expect(page.getByLabel('费用结束日期')).toHaveValue('2026-10-31')
    const history = page.waitForRequest(request => {
      const url = new URL(request.url())
      return url.pathname.endsWith('/usage/costs') && !url.searchParams.has('from') && !url.searchParams.has('to')
    })
    await page.getByLabel('费用开始日期').fill('')
    await page.getByLabel('费用结束日期').fill('')
    await history
  })
})

test.describe('billing calendar days', () => {
  test.use({ timezoneId: 'America/New_York' })

  test('includes the whole local day across daylight saving changes', async ({ page }) => {
    await page.clock.install({ time: new Date('2026-11-01T12:00:00-05:00') })
    await page.goto('/#/usage')
    for (const [date, from, to] of [
      ['2026-11-01', '2026-11-01T04:00:00Z', '2026-11-02T05:00:00Z'],
      ['2026-03-08', '2026-03-08T05:00:00Z', '2026-03-09T04:00:00Z'],
    ]) {
      const query = page.waitForRequest(request => {
        const url = new URL(request.url())
        return url.pathname.endsWith('/usage/costs') && url.searchParams.get('model') === date
      })
      await page.getByLabel('费用开始日期').fill(date)
      await page.getByLabel('费用结束日期').fill(date)
      await page.getByLabel('费用模型 ID').fill(date)
      const params = new URL((await query).url()).searchParams
      expect(Number(params.get('from'))).toBe(Date.parse(from))
      expect(Number(params.get('to'))).toBe(Date.parse(to))
    }
  })
})
