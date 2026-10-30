import { useState } from 'react'
import useSWR from 'swr'
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { request } from '@/lib/api'
import { useMinuteTick } from '@/lib/hooks'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
interface Total { name: string; amount: string; attempts: number; unpriced: number }
interface Summary extends Total { requests: number; models: Total[]; sources: Total[]; keys: Total[]; trend: Total[] }
interface Attempt { requestId: string; attempt: number; startedMs: number; modelId: string; sourceId: string; sourceName: string; keyName: string; httpStatus: number; amount: string | null; status: string; reasons?: string[]; usage: unknown; price: unknown }
const statusLabels: Record<string, string> = { complete: '可计价', partial: '部分可计价', missing_price: '价格缺失', missing_usage: '用量缺失', historical_unpriced: '历史未计价', pending: '待结算 / 中断未结算' }
function money(value: string) { return value.includes('.') ? value.replace(/0+$/, '').replace(/\.$/, '') : value }
export function BillingPanel() {
  const minuteTick = useMinuteTick()
  const now = new Date(minuteTick * 60_000)
  const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  // 未手动修改的日期随本地零点更新；空字符串表示清除日期限制。
  const [customFilter, setFilter] = useState({ from: null as string | null, to: null as string | null, model: '', source: '', key: '', request: '' })
  const filter = { ...customFilter, from: customFilter.from ?? today, to: customFilter.to ?? today }
  const [offset, setOffset] = useState(0)
  const end = filter.to ? new Date(`${filter.to}T00:00:00`) : undefined
  if (end) end.setDate(end.getDate() + 1)
  const query = { ...filter, from: filter.from ? new Date(`${filter.from}T00:00:00`).getTime() : undefined, to: end?.getTime() }
  const summary = useSWR(['costs', query], () => request<Summary>('/usage/costs', { query }), { refreshInterval: 10000 })
  const attempts = useSWR(['cost-attempts', query, offset], () => request<{ items: Attempt[] }>('/usage/cost-attempts', { query: { ...query, offset } }), { refreshInterval: 10000 })
  const update = (changes: Partial<typeof filter>) => { setFilter({ ...customFilter, ...changes }); setOffset(0) }
  const error = summary.error || attempts.error
  return <div className="space-y-6">
    <p className="text-sm text-muted-foreground">USD 参考估算，仅覆盖 Token 与缓存费用。工具、图片、音频及按秒收费未覆盖；缺价或未报告用量不会按免费处理。清理调用日志保留账目。</p>
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
      {(['from', 'to', 'model', 'source', 'key'] as const).map((field, i) => <label key={field} className="space-y-1 text-sm"><span>{['开始日期', '结束日期', '模型 ID', '渠道 ID', '调用 Key'][i]}</span><Input aria-label={`费用${['开始日期', '结束日期', '模型 ID', '渠道 ID', '调用 Key'][i]}`} type={i < 2 ? 'date' : 'text'} value={filter[field]} onChange={e => update({ [field]: e.target.value })} /></label>)}
    </div>
    {filter.request && <p className="break-all text-sm">请求：{filter.request}<Button variant="ghost" onClick={() => update({ request: '' })}>清除请求筛选</Button></p>}
    {error && <div role="alert">{error.message}<Button variant="outline" onClick={() => { void summary.mutate(); void attempts.mutate() }}>重试</Button></div>}
    {summary.isLoading && <p role="status">正在加载费用…</p>}
    {summary.data && <>
      <div className="grid gap-3 sm:grid-cols-3">{[
        ['已计价金额（USD）', `$${money(summary.data.amount)}`], ['请求 / 上游尝试', `${summary.data.requests} / ${summary.data.attempts}`], ['未完整计价的尝试', String(summary.data.unpriced)],
      ].map(([label, value]) => <div key={label} className="rounded-lg border bg-card p-5"><p className="text-sm text-muted-foreground">{label}</p><p className="mt-2 text-2xl font-semibold tabular-nums">{value}</p></div>)}</div>
      {summary.data.unpriced > 0 && <p className="text-sm">当前金额仅包含已知部分，{summary.data.unpriced} 次尝试未完整计价。</p>}
      <section className="rounded-lg border p-4" aria-label="费用趋势"><h2 className="mb-4 font-medium">每日已计价金额 · USD · UTC</h2>{summary.data.trend.length === 0 ? <p className="text-sm text-muted-foreground">暂无账目</p> : <div className="h-56"><ResponsiveContainer width="100%" height="100%"><AreaChart data={summary.data.trend.map(v => ({ ...v, chartAmount: Number(v.amount) }))}><XAxis dataKey="name" tick={{ fontSize: 11 }} /><YAxis tick={{ fontSize: 11 }} /><Tooltip formatter={(_, __, item) => [`$${money(item.payload.amount)}`, '已计价金额']} /><Area dataKey="chartAmount" stroke="currentColor" fill="currentColor" fillOpacity={0.12} isAnimationActive={false} /></AreaChart></ResponsiveContainer></div>}</section>
      <div className="grid gap-4 lg:grid-cols-3">{([['模型', summary.data.models], ['渠道', summary.data.sources], ['调用 Key', summary.data.keys]] as const).map(([label, rows]) => <section key={label} className="min-w-0 overflow-auto rounded-lg border p-4"><h2 className="mb-3 font-medium">按{label}统计</h2><table className="w-full text-left text-xs"><thead><tr><th scope="col" className="py-2">{label}</th><th scope="col">USD</th><th scope="col">未完整计价</th></tr></thead><tbody>{rows.map(row => <tr key={row.name} className="border-t"><td className="max-w-40 break-all py-2 pr-2">{row.name || '未知'}</td><td className="pr-2 tabular-nums">{money(row.amount)}</td><td>{row.unpriced}</td></tr>)}</tbody></table></section>)}</div>
    </>}
    <section className="space-y-3"><h2 className="font-medium">上游尝试账目</h2><p className="text-xs text-muted-foreground">点击请求 ID 可汇总该请求的全部尝试。展开详情可查看当时的用量、价格快照与不完整原因。</p>{attempts.data?.items.map(a => <details key={`${a.requestId}/${a.attempt}`} className="rounded-lg border p-4"><summary className="cursor-pointer text-sm"><span className="mr-3 tabular-nums">{new Date(a.startedMs).toLocaleString()}</span><span className="break-all">{a.modelId || '未知模型'} · 尝试 {a.attempt || '历史'}</span><span className="ml-3 font-medium">{a.amount === null ? '金额未知' : `$${money(a.amount)}`}</span><span className="ml-2">{statusLabels[a.status] ?? a.status}</span></summary><div className="mt-3 space-y-2 text-sm"><Button variant="ghost" className="max-w-full whitespace-normal break-all text-left" onClick={() => update({ request: a.requestId })}>{a.requestId}</Button><p className="break-all">渠道 {a.sourceName || a.sourceId} · Key {a.keyName || '未知'} · HTTP {a.httpStatus || '未返回'}</p>{a.reasons?.map((reason, i) => <p key={i}>{reason}</p>)}<pre className="max-h-80 overflow-auto rounded border p-3 text-xs">{JSON.stringify({ usage: a.usage, price: a.price }, null, 2)}</pre></div></details>)}{attempts.data?.items.length === 0 && <p className="text-sm text-muted-foreground">暂无账目</p>}<div className="flex items-center gap-3"><Button variant="outline" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - 100))}>上一页</Button><span className="text-sm">第 {offset / 100 + 1} 页</span><Button variant="outline" disabled={(attempts.data?.items.length ?? 0) < 100} onClick={() => setOffset(offset + 100)}>下一页</Button></div></section>
  </div>
}
