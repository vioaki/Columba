import { expect, test } from '@playwright/test'
test('shows known costs, unknown attempts, request attribution and filters', async ({ page }) => {
  const queries: string[] = []
  await page.route('**/api/admin/**', async route => {
    const u = new URL(route.request().url()); queries.push(u.search)
    let data: unknown = { items: [], total: 0, seq: 1 }
    if (/\/usage\/(trend|by-model|by-model-daily)$/.test(u.pathname)) data = []
    if (u.pathname.endsWith('/usage/costs')) data = { amount: '0.120000000001', attempts: 3, requests: 2, unpriced: 1, models: [{ name: 'm', amount: '0.120000000001', attempts: 3, unpriced: 1 }], sources: [], keys: [], trend: [] }
    if (u.pathname.endsWith('/usage/cost-attempts')) data = { items: [{ requestId: 'r1', attempt: 1, startedMs: Date.now(), modelId: 'm', sourceId: 'channel', keyName: 'client', httpStatus: 500, amount: null, status: 'missing_usage', reasons: ['provider did not report usage'], usage: {}, price: {} }] }
    await route.fulfill({ json: { ok: true, data } })
  })
  await page.goto('/#/usage')
  await expect(page.getByRole('heading', { name: '用量费用', exact: true })).toBeVisible()
  await expect(page.getByText('$0.120000000001', { exact: true })).toBeVisible()
  await expect(page.getByText('金额未知', { exact: true })).toBeVisible()
  await page.getByLabel('费用模型 ID').fill('org/model')
  await expect.poll(() => queries.some(v => v.includes('model=org%2Fmodel'))).toBe(true)
  await page.locator('summary').click()
  await page.getByRole('button', { name: 'r1', exact: true }).click()
  await expect.poll(() => queries.some(v => v.includes('request=r1'))).toBe(true)
  await page.getByRole('tab', { name: '费用', exact: true }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('tab', { name: 'Token 统计' })).toBeFocused()
})
