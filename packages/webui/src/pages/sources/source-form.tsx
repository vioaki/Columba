import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { SecretInput } from '@/components/secret-input'
import { Switch } from '@/components/ui/switch'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog'
import { useToast } from '@/components/ui/use-toast'
import { api } from '@/lib/api'
import type { ModelSource, Platform, SourceAPIKey, SourceKeyStrategy } from '@/lib/types'

const protocols: [Platform, string][] = [['openai', 'Chat Completions'], ['responses', 'Responses'], ['claude', 'Anthropic Messages'], ['gemini', 'Gemini']]
const emptySource = (): ModelSource => ({ id: '', name: '', baseUrl: '', platform: 'openai', enabled: true, autoFetchModels: true, apiKeys: [], keyStrategy: 'round-robin' })

export function SourceFormDialog({ open, onOpenChange, source, onSaved }: { open: boolean; onOpenChange: (v: boolean) => void; source: ModelSource | null; onSaved: () => void }) {
  const toast = useToast()
  const [form, setForm] = useState<ModelSource>(emptySource)
  const [manual, setManual] = useState('')
  const [saving, setSaving] = useState(false)
  useEffect(() => { if (open) { setForm(source ? { ...source, apiKeys: source.apiKeys?.length ? source.apiKeys.map(k => ({ ...k })) : source.apiKey ? [{ value: source.apiKey }] : [] } : emptySource()); setManual((source?.manualModels ?? []).map(m => m.id).join('\n')) } }, [open, source])
  const patch = (update: Partial<ModelSource>) => setForm(f => ({ ...f, ...update }))
  const keyPatch = (index: number, update: Partial<SourceAPIKey>) => setForm(f => ({ ...f, apiKeys: f.apiKeys?.map((k, i) => i === index ? { ...k, ...update } : k) }))
  async function save(event: React.FormEvent) {
    event.preventDefault()
    if (!form.name.trim() || !form.baseUrl.trim()) { toast.error('请填写渠道名称和地址'); return }
    if (form.apiKeys?.some(k => !k.value.trim())) { toast.error('请填写 Key 或移除空行'); return }
    setSaving(true)
    try {
      const ids = [...new Set(manual.split('\n').map(s => s.trim()).filter(Boolean))]
      const body: ModelSource = { ...form, name: form.name.trim(), baseUrl: form.baseUrl.trim(), apiKey: '', manualModels: ids.map(id => ({ ...source?.manualModels?.find(m => m.id === id), id, name: id, type: 'llm', available: true })) }
      if (source) await api.updateSource(source.id, body); else await api.createSource(body)
      toast.success('渠道已保存'); onSaved(); onOpenChange(false)
    } catch (e) { toast.error('保存失败', (e as Error).message) } finally { setSaving(false) }
  }
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto"><form onSubmit={save} className="space-y-5">
    <DialogHeader><DialogTitle>{source ? '编辑渠道' : '新增渠道'}</DialogTitle><DialogDescription>原生协议与模型 ID 保持不变。渠道和模型均启用后即可调用。</DialogDescription></DialogHeader>
    <label className="block space-y-2"><span>渠道名称</span><Input required value={form.name} onChange={e => patch({ name: e.target.value })} /></label>
    <label className="block space-y-2"><span>上游地址</span><Input required type="url" placeholder="https://api.example.com" value={form.baseUrl} onChange={e => patch({ baseUrl: e.target.value })} /></label>
    <label className="block space-y-2"><span>原生协议</span><select className="w-full rounded-md border bg-background p-2" value={form.platform} onChange={e => patch({ platform: e.target.value as Platform })}>{!protocols.some(([p]) => p === form.platform) && <option value={form.platform}>请选择原生协议</option>}{protocols.map(([p, label]) => <option key={p} value={p}>{label}</option>)}</select></label>
    <div className="flex flex-wrap gap-6"><label className="flex items-center gap-2"><Switch checked={form.enabled} onCheckedChange={enabled => patch({ enabled })} />启用渠道</label><label className="flex items-center gap-2"><Switch checked={form.autoFetchModels} onCheckedChange={autoFetchModels => patch({ autoFetchModels })} />自动拉取模型</label></div>
    <label className="block space-y-2"><span>{form.autoFetchModels ? '额外手动模型（每行一个原始 ID）' : '手动模型（每行一个原始 ID）'}</span><textarea className="min-h-24 w-full rounded-md border bg-background p-2 font-mono text-sm" value={manual} onChange={e => setManual(e.target.value)} /></label>
    <fieldset className="space-y-3"><legend>渠道 Key</legend><p className="text-xs text-muted-foreground">无鉴权渠道可以不填。停用的 Key 不参与调用；自动拉取结果按各 Key 保存。</p>
      {(form.apiKeys ?? []).map((key, index) => <div key={index} className="space-y-2 rounded-md border p-3">
        <div className="flex gap-2"><SecretInput aria-label={`渠道 Key ${index + 1}`} value={key.value} onChange={e => keyPatch(index, { value: e.target.value })} /><Button type="button" variant="outline" onClick={() => patch({ apiKeys: form.apiKeys?.filter((_, i) => i !== index) })}>移除</Button></div>
        <Input aria-label={`Key ${index + 1} 备注`} placeholder="备注" value={key.note ?? ''} onChange={e => keyPatch(index, { note: e.target.value })} />
        <label className="flex items-center gap-2"><Switch checked={!key.disabled} onCheckedChange={v => keyPatch(index, { disabled: !v })} />启用 Key {index + 1}</label>
        {!form.autoFetchModels && <label className="block text-xs">允许模型（逗号分隔，留空允许全部）<Input value={key.allowedModels?.join(',') ?? ''} onChange={e => keyPatch(index, { allowedModels: e.target.value.trim() ? e.target.value.split(',').map(s => s.trim()).filter(Boolean) : null })} /></label>}
      </div>)}
      <Button type="button" variant="outline" onClick={() => patch({ apiKeys: [...form.apiKeys ?? [], { value: '' }] })}>添加 Key</Button>
      <label className="block space-y-2"><span>Key 选择</span><select className="w-full rounded-md border bg-background p-2" value={form.keyStrategy} onChange={e => patch({ keyStrategy: e.target.value as SourceKeyStrategy })}><option value="round-robin">轮询</option><option value="priority">列表顺序</option><option value="random">随机</option></select></label>
    </fieldset>
    <details><summary className="cursor-pointer">高级连接设置</summary><label className="mt-3 block space-y-2"><span>模型列表地址（留空使用上游地址）</span><Input type="url" value={form.fetchBaseUrl ?? ''} onChange={e => patch({ fetchBaseUrl: e.target.value })} /></label></details>
    <DialogFooter><Button type="button" variant="outline" onClick={() => onOpenChange(false)}>取消</Button><Button type="submit" variant="primary" disabled={saving}>{saving ? '保存中…' : '保存渠道'}</Button></DialogFooter>
  </form></DialogContent></Dialog>
}
