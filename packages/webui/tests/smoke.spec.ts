import { expect, test } from '@playwright/test'

test.beforeEach(async ({ page }) => {
  await page.route('**/api/admin/**', route => {
    const path = new URL(route.request().url()).pathname
    const data = /\/usage\/(trend|by-model|by-model-daily)$/.test(path) ? []
      : path.endsWith('/usage/costs') ? { amount: '0.120000000001', attempts: 2, requests: 2, unpriced: 1, models: [], sources: [], keys: [], trend: [] }
      : path.endsWith('/usage/cost-attempts') ? { items: [{ requestId: 'unpriced', attempt: 1, startedMs: 1, modelId: 'demo', sourceId: 'local', status: 'missing_usage', amount: null, reasons: ['provider did not report usage'], usage: {}, price: {} }] }
      : { items: [], total: 0, seq: 1 }
    return route.fulfill({ json: { ok: true, data } })
  })
})

test('opens without login and exposes five sections', async ({ page }) => {
  await page.goto('/#/tokens')
  await expect(page.getByRole('heading', { name: '调用 Key', exact: true })).toBeVisible()
  await expect(page.getByRole('navigation', { name: '主导航' }).getByRole('link')).toHaveText(['总览', '模型', '调用 Key', '用量费用', '设置'])
  await expect(page.getByRole('button', { name: '新增 Key' }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: '退出登录' })).toHaveCount(0)
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
