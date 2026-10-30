import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Sheet, SheetBody, SheetContent, SheetHeader, SheetSectionTitle, SheetTitle } from '@/components/ui/sheet'
import { api } from '@/lib/api'
import type { UsageLogDetail } from '@/lib/types'
import { downloadJSON, formatDateTime, formatDuration } from '@/lib/utils'
import { ChainBodies } from './log-detail/body-sections'
import { CallOutcome } from './log-detail/call-outcome'
import { UsageTokens } from './log-detail/usage-tokens'

export function LogDetailSheet({ id, onClose }: { id: string | null; onClose: () => void }) {
  const [detail, setDetail] = useState<UsageLogDetail | null>(null)
  const [error, setError] = useState('')
  useEffect(() => { setDetail(null); setError(''); if (!id) return; let active = true; void api.usageLogDetail(id).then(value => { if (active) setDetail(value) }).catch(e => { if (active) setError(e.message) }); return () => { active = false } }, [id])
  return <Sheet open={!!id} onOpenChange={open => { if (!open) onClose() }}><SheetContent className="w-[min(640px,94vw)]"><SheetHeader><SheetTitle>调用详情</SheetTitle></SheetHeader><SheetBody>
    {error && <p role="alert">{error}</p>}{!detail && !error && <p role="status">加载中…</p>}
    {detail && <div className="space-y-5"><p className="break-all font-mono text-xs">{detail.requestId}</p><p>{formatDateTime(detail.startedAt)} · {formatDuration(detail.durationMs)}</p><p>{detail.modelId || detail.modelName} · {detail.platform}</p><p>渠道：{detail.sourceId || '—'} · 调用 Key：{detail.keyName || '—'}</p>{detail.error && <p role="alert">{detail.error}</p>}<CallOutcome detail={detail} />
      <SheetSectionTitle>上游报告用量</SheetSectionTitle><UsageTokens usage={detail.usage} />
      {detail.requestWarnings?.map((warning, i) => <p key={i} className="text-sm text-muted-foreground">{warning}</p>)}
      <ChainBodies detail={detail} /><Button variant="outline" onClick={() => downloadJSON(`columba-${detail.requestId}.json`, detail)}>导出记录</Button>
    </div>}
  </SheetBody></SheetContent></Sheet>
}
