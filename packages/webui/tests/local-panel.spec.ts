import { expect, test } from '@playwright/test'

test('opens the local panel without a token and sends no panel credential', async ({ page }) => {
  const headers: Record<string, string>[] = []
  await page.route('**/api/admin/**', async route => {
    headers.push(route.request().headers())
    await route.fulfill({ json: { ok: true, data: { items: [], total: 0, seq: 1 } } })
  })
  await page.goto('/#/logs')
  await expect(page.getByRole('heading', { name: '系统日志', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '退出登录' })).toHaveCount(0)
  expect(headers.length).toBeGreaterThan(0)
  expect(headers.every(header => !header.authorization)).toBe(true)
  expect(await page.evaluate(() => localStorage.getItem('columba-webui.panel-token'))).toBeNull()
})
