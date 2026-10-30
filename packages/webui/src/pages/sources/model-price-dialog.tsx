import { useState } from 'react'
import useSWR from 'swr'
import { request } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog'

const fields = { input: '输入', output: '输出', reasoning: '推理', cache_read: '缓存读取', cache_write: '缓存写入 / 5 分钟', cache_write_1h: '缓存写入 / 1 小时' } as const
type Rates = Partial<Record<keyof typeof fields, string>>
type Prices = Rates & { tiers?: (Rates & { tier: { type: 'context'; size: number } })[] }
interface PriceConfig { sourceId: string; modelId: string; provider: string; catalogModel: string; override: Prices }
interface PriceResult { config: PriceConfig; snapshot: { prices: Prices; matched: boolean; updatedAt: string } }
export function ModelPriceDialog({ sourceId, modelId, onClose }: { sourceId: string; modelId: string; onClose: () => void }) {
  const result = useSWR(['model-prices', sourceId, modelId], () => request<PriceResult>('/model-prices', { query: { sourceId, modelId } }))
  const [draft, setDraft] = useState<PriceConfig | null>(null)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const config = draft ?? result.data?.config
  const update = (changes: Partial<PriceConfig>) => { if (config) setDraft({ ...config, ...changes }) }
  const rateInputs = (rates: Rates, change: (rates: Rates) => void, prefix: string) => <div className="grid grid-cols-2 gap-3">{Object.entries(fields).map(([key, label]) => <label key={key} className="space-y-1 text-sm"><span>{label}</span><Input aria-label={`${prefix}${label}`} type="number" min="0" step="any" placeholder="未填写" value={rates[key as keyof Rates] ?? ''} onChange={e => change({ ...rates, [key]: e.target.value || undefined })} /></label>)}</div>
  return <Dialog open onOpenChange={open => { if (!open) onClose() }}><DialogContent><DialogHeader><DialogTitle>模型价格</DialogTitle><DialogDescription>{modelId} · {sourceId} · USD / 百万 Token</DialogDescription></DialogHeader>
    <p className="text-sm text-muted-foreground">按供应商与模型 ID 精确匹配。手填值优先；空白表示继承目录，0 表示明确免费。工具、图片、音频和按秒收费不在估算范围内。</p>
    {result.error && <p role="alert">{result.error.message}</p>}
    {!config && !result.error && <p role="status">正在加载价格…</p>}
    {config && <form className="space-y-4" onSubmit={async e => { e.preventDefault(); setSaving(true); setError(''); try { await request('/model-prices', { method: 'PUT', body: config }); await result.mutate(); setDraft(null) } catch (e) { setError((e as Error).message) } finally { setSaving(false) } }}>
      <label className="block space-y-1 text-sm"><span>目录供应商 ID</span><Input value={config.provider} placeholder="例如 openai" onChange={e => update({ provider: e.target.value })} /></label>
      <label className="block space-y-1 text-sm"><span>目录模型 ID</span><Input value={config.catalogModel} placeholder="models.dev 原始模型 ID" onChange={e => update({ catalogModel: e.target.value })} /></label>
      <p className="text-xs text-muted-foreground">{result.data?.snapshot.matched ? '已精确匹配目录' : '未匹配目录；缺少的价格不会按免费处理'} · 目录更新时间：{result.data?.snapshot.updatedAt ? new Date(result.data.snapshot.updatedAt).toLocaleString() : '暂无'}</p>
      <h3 className="font-medium">手动覆盖</h3>{rateInputs(config.override, override => update({ override: { ...config.override, ...override } }), '覆盖')}
      <details className="space-y-3"><summary className="cursor-pointer text-sm">上下文阶梯覆盖</summary><p className="text-xs text-muted-foreground">超过阈值时整次请求使用该档价格。新增后将替换目录阶梯，按阈值从小到大填写。</p>{config.override.tiers?.map((tier, i) => <div key={i} className="space-y-3 rounded border p-3"><label className="block text-sm">输入上下文阈值<Input aria-label={`阶梯 ${i + 1} 阈值`} type="number" min={1} required value={tier.tier.size} onChange={e => update({ override: { ...config.override, tiers: config.override.tiers!.map((v, j) => j === i ? { ...v, tier: { type: 'context', size: Number(e.target.value) } } : v) } })} /></label>{rateInputs(tier, rates => update({ override: { ...config.override, tiers: config.override.tiers!.map((v, j) => j === i ? { ...rates, tier: v.tier } : v) } }), `阶梯 ${i + 1} `)}<Button variant="ghost" type="button" onClick={() => update({ override: { ...config.override, tiers: config.override.tiers!.filter((_, j) => i !== j) } })}>删除阶梯</Button></div>)}<div className="flex gap-2"><Button type="button" variant="outline" onClick={() => update({ override: { ...config.override, tiers: [...config.override.tiers ?? [], { tier: { type: 'context', size: 200000 } }] } })}>新增阶梯</Button><Button type="button" variant="ghost" onClick={() => update({ override: { ...config.override, tiers: undefined } })}>继承目录阶梯</Button></div></details>
      <details><summary className="cursor-pointer text-sm">已保存的生效价格</summary><pre className="overflow-auto rounded border p-3 text-xs">{JSON.stringify(result.data?.snapshot.prices, null, 2)}</pre></details>
      {error && <p role="alert">{error}</p>}<DialogFooter><Button type="submit" variant="primary" disabled={saving}>{saving ? '保存中…' : '保存价格'}</Button></DialogFooter>
    </form>}
  </DialogContent></Dialog>
}
