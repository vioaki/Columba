import { expect, test } from '@playwright/test'
test('five local sections use neutral light/dark colors and fit small windows', async ({ page }, testInfo) => {
 await page.route('**/api/admin/**', route => route.fulfill({ json: { ok: true, data: { items: [], total: 0, seq: 1 } } }))
 await page.goto('/#/tokens')
 await expect(page.getByRole('navigation', { name: '主导航' }).getByRole('link')).toHaveText(['总览', '模型', '调用 Key', '用量费用', '设置'])
 for (const dark of [false, true]) {
  await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
  const colors = await page.evaluate(() => { const s = getComputedStyle(document.body); return [s.backgroundColor, s.color] })
  for (const color of colors) { const values = color.match(/\d+/g)!.map(Number); expect(values[0]).toBe(values[1]); expect(values[1]).toBe(values[2]) }
  expect(await page.evaluate(() => getComputedStyle(document.body, '::before').backgroundImage)).toBe('none')
  await expect(page.getByRole('button', { name: '新增 Key' }).first()).toHaveCSS('background-color', dark ? 'rgb(235, 235, 235)' : 'rgb(31, 31, 31)')
  await page.screenshot({ path: testInfo.outputPath(dark ? 'dark.png' : 'light.png'), fullPage: true })
 }
 await page.setViewportSize({ width: 375, height: 812 })
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
 await page.getByRole('button', { name: '打开导航菜单' }).click()
 await expect(page.getByRole('dialog').getByRole('link')).toHaveCount(5)
 await page.keyboard.press('Escape')
 await expect(page.getByRole('button', { name: '打开导航菜单' })).toBeFocused()
})
