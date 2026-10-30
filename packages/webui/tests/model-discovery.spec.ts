import { expect, test } from '@playwright/test'
test('channel editor preserves multiple keys and adds manual models; exact price mapping survives save', async ({ page }) => {
 let source = { id: 'c', name: 'Channel', baseUrl: 'https://upstream.example', platform: 'openai', enabled: true, autoFetchModels: true, manualModels: [{ id: 'm', name: 'm' }], apiKeys: [{ value: 'key-a', fetchedModels: ['m'] }, { value: 'key-b', fetchedModels: ['m'] }], keyStrategy: 'round-robin' }
 let price = { sourceId: 'c', modelId: 'm', provider: '', catalogModel: '', override: {} }
 await page.route('**/api/admin/**', async route => {
  const path = new URL(route.request().url()).pathname
  let data: unknown = { items: [], policies: [], seq: 1 }
  if (path.endsWith('/model-sources/c') && route.request().method() === 'PUT') { source = route.request().postDataJSON(); data = source }
  if (path.endsWith('/model-sources')) data = { items: [source] }
  if (path.endsWith('/models')) data = { items: [{ id: 'm', name: 'm', sourceId: 'c', sourceName: 'Channel', platform: 'openai', enabled: true }] }
  if (path.endsWith('/model-prices')) { if (route.request().method() === 'PUT') price = route.request().postDataJSON(); data = { config: price, snapshot: { matched: false, updatedAt: '2026-10-30T00:00:00Z', prices: price.override } } }
  await route.fulfill({ json: { ok: true, data } })
 })
 await page.goto('/#/sources')
 await page.getByRole('button', { name: '编辑', exact: true }).click()
 const dialog = page.getByRole('dialog', { name: '编辑渠道' })
 await dialog.getByLabel('额外手动模型（每行一个原始 ID）').fill('m\nnew-model')
 await dialog.getByRole('button', { name: '保存渠道' }).click()
 await expect(dialog).not.toBeVisible()
 expect(source.apiKeys.map(k => k.value)).toEqual(['key-a', 'key-b'])
 expect(source.manualModels.map(m => m.id)).toEqual(['m', 'new-model'])
 await page.locator('summary').filter({ hasText: 'm' }).click()
 await page.getByRole('button', { name: '价格', exact: true }).click()
 await page.getByLabel('目录供应商 ID').fill('vendor')
 await page.getByLabel('目录模型 ID').fill('Exact/ID')
 await page.getByLabel('覆盖输入', { exact: true }).fill('0')
 await page.getByRole('button', { name: '保存价格' }).click()
 await expect.poll(() => price.provider).toBe('vendor')
 expect(price.catalogModel).toBe('Exact/ID')
 expect(price.override).toMatchObject({ input: '0' })
})
