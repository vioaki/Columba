import { useMemo, useState } from 'react'
import useSWR from 'swr'
import { PageHeader } from '@/components/page-header'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog'
import { useConfirm } from '@/components/ui/confirm-dialog'
import { useToast } from '@/components/ui/use-toast'
import { api } from '@/lib/api'
import { useModels, useSources, revalidate, POLL } from '@/lib/hooks'
import type { Model, ModelSource, ModelRouting } from '@/lib/types'
import { SourceFormDialog } from './sources/source-form'

export function SourcesPage() {
  const toast = useToast()
  const { confirm, dialog } = useConfirm()
  const sources = useSources(POLL.SOURCE_FAST)
  const models = useModels(POLL.SOURCE_FAST)
  const routing = useSWR('model-routing', api.modelRouting)
  const [sourceOpen, setSourceOpen] = useState(false)
  const [source, setSource] = useState<ModelSource | null>(null)
  const [selected, setSelected] = useState<{ policy: ModelRouting; models: Model[] } | null>(null)
  const [search, setSearch] = useState('')
  const [busy, setBusy] = useState(false)
  const grouped = useMemo(() => {
    const map = new Map<string, Model[]>()
    for (const m of models.data ?? []) { const key = JSON.stringify([m.id, m.platform]); map.set(key, [...map.get(key) ?? [], m]) }
    return [...map.values()].filter(ms => ms[0].id.toLowerCase().includes(search.toLowerCase()))
  }, [models.data, search])
  async function refresh() { await Promise.all([sources.mutate(), models.mutate(), routing.mutate()]); void revalidate.usage() }
  async function action(fn: () => Promise<unknown>) { setBusy(true); try { await fn(); await refresh() } catch (e) { toast.error('操作失败', (e as Error).message) } finally { setBusy(false) } }
  function editSchedule(ms: Model[]) { const first = ms[0]; const policy = routing.data?.policies.find(p => p.modelId === first.id && p.protocol === first.platform) ?? { modelId: first.id, protocol: first.platform, strategy: 'round-robin', maxRetries: 3, retryInterval: 1000, channels: {} }; setSelected({ models: ms, policy: { ...policy, channels: { ...policy.channels } } }) }
  const error = sources.error || models.error || routing.error
  return <div className="space-y-8">
    <PageHeader title="模型" actions={<Button variant="primary" onClick={() => { setSource(null); setSourceOpen(true) }}>新增渠道</Button>} />
    <p className="text-sm text-muted-foreground">启用即暴露。同一模型 ID 与协议自动聚合，客户端直接使用原始模型 ID。</p>
    {error && <div role="alert">{error.message}<Button variant="outline" onClick={() => void refresh()}>重试</Button></div>}
    <Input aria-label="搜索模型" placeholder="搜索原始模型 ID" value={search} onChange={e => setSearch(e.target.value)} />
    <section aria-label="聚合模型" className="space-y-3">
      {models.isLoading && <p role="status">正在加载模型…</p>}
      {!models.isLoading && grouped.length === 0 && <p className="rounded-lg border p-6 text-muted-foreground">暂无模型。添加渠道后拉取模型，或在渠道中手动添加。</p>}
      {grouped.map(ms => { const first = ms[0]; const count = ms.filter(m => m.enabled && sources.data?.find(s => s.id === m.sourceId)?.enabled).length; return <details key={JSON.stringify([first.id, first.platform])} className="rounded-lg border bg-card p-4">
        <summary className="cursor-pointer break-all font-medium">{first.id}<span className="ml-3 text-xs text-muted-foreground">{first.platform} · {count} 个启用渠道</span></summary>
        <div className="mt-4 space-y-3">{ms.map(m => <div key={m.sourceId} className="flex flex-wrap items-center justify-between gap-3 border-t pt-3"><span>{m.sourceName || m.sourceId}</span><label className="flex items-center gap-2 text-sm"><Switch disabled={busy} checked={m.enabled} onCheckedChange={enabled => void action(() => api.updateModel(m.sourceId!, m.id, { enabled }))} />{m.enabled ? '已启用' : '已停用'}</label></div>)}
          <Button variant="outline" onClick={() => editSchedule(ms)}>高级调度</Button>
        </div>
      </details> })}
    </section>
    <section aria-label="渠道" className="space-y-3"><h2 className="text-lg font-semibold">渠道</h2>{sources.data?.map(s => <div key={s.id} className="flex flex-wrap items-center justify-between gap-4 rounded-lg border p-4">
      <div className="min-w-0"><h3 className="font-medium">{s.name}</h3><p className="break-all text-xs text-muted-foreground">{s.platform} · {s.baseUrl}</p>{s.refreshState?.refreshing && <p role="status" className="text-xs">正在拉取模型…</p>}{s.refreshState?.lastError && <p role="alert" className="text-sm">{s.refreshState.lastError}</p>}</div>
      <div className="flex flex-wrap items-center gap-2"><Switch aria-label={`启用渠道 ${s.name}`} checked={s.enabled} disabled={busy} onCheckedChange={enabled => void action(() => api.setSourceEnabled(s.id, enabled))} />{s.autoFetchModels && <Button variant="outline" disabled={busy || s.refreshState?.refreshing} onClick={() => void action(() => api.fetchSource(s.id))}>拉取模型</Button>}<Button variant="outline" onClick={() => { setSource(s); setSourceOpen(true) }}>编辑</Button><Button variant="ghost" onClick={async () => { if (await confirm({ title: `删除渠道「${s.name}」？`, description: '该渠道及其模型将停止服务，历史账目保留。', confirmText: '删除' })) void action(() => api.deleteSource(s.id)) }}>删除</Button></div>
    </div>)}</section>
    <SourceFormDialog open={sourceOpen} onOpenChange={setSourceOpen} source={source} onSaved={() => void refresh()} />
    <Dialog open={!!selected} onOpenChange={v => { if (!v) setSelected(null) }}><DialogContent className="max-h-[90vh] overflow-y-auto"><DialogHeader><DialogTitle>高级调度</DialogTitle><DialogDescription>{selected?.policy.modelId} · {selected?.policy.protocol}</DialogDescription></DialogHeader>{selected && <form className="space-y-4" onSubmit={e => { e.preventDefault(); void action(async () => { await api.saveModelRouting(selected.policy); setSelected(null) }) }}>
      <label className="block space-y-2"><span>渠道策略</span><select className="w-full rounded-md border bg-background p-2" value={selected.policy.strategy} onChange={e => setSelected({ ...selected, policy: { ...selected.policy, strategy: e.target.value as ModelRouting['strategy'] } })}><option value="round-robin">等权轮询</option><option value="priority">优先级（数值小优先）</option><option value="weighted">加权轮询</option></select></label>
      {(['maxRetries', 'retryInterval'] as const).map(field => <label key={field} className="block space-y-2"><span>{field === 'maxRetries' ? '额外重试次数' : '重试间隔（毫秒）'}</span><Input type="number" required min={0} max={field === 'maxRetries' ? 100 : 60000} value={selected.policy[field]} onChange={e => setSelected({ ...selected, policy: { ...selected.policy, [field]: Number(e.target.value) } })} /></label>)}
      {selected.policy.strategy !== 'round-robin' && selected.models.map(m => { const id = m.sourceId!; const field = selected.policy.strategy === 'priority' ? 'priority' : 'weight'; return <label key={id} className="block space-y-2"><span>{m.sourceName || id} · {field === 'priority' ? '优先级' : '权重'}</span><Input type="number" required min={field === 'priority' ? 0 : 1} max={field === 'priority' ? 100000 : 1000} value={selected.policy.channels[id]?.[field] ?? (field === 'priority' ? 0 : 1)} onChange={e => setSelected({ ...selected, policy: { ...selected.policy, channels: { ...selected.policy.channels, [id]: { ...(selected.policy.channels[id] ?? {priority: 0, weight: 1}), [field]: Number(e.target.value) } } } })} /></label> })}
      <DialogFooter><Button type="submit" variant="primary" disabled={busy}>保存调度</Button></DialogFooter>
    </form>}</DialogContent></Dialog>{dialog}
  </div>
}
