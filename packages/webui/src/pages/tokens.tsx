import { useEffect, useState } from 'react'
import { Pencil, Trash2 } from 'lucide-react'
import { PageHeader } from '@/components/page-header'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { AsyncState } from '@/components/ui/states'
import { SecretInput } from '@/components/secret-input'
import { CopyButton } from '@/components/copy-button'
import { RevealCopyButton } from '@/components/reveal-copy-button'
import { useConfirm } from '@/components/ui/confirm-dialog'
import { useToast } from '@/components/ui/use-toast'
import { useTokens, revalidate } from '@/lib/hooks'
import { api } from '@/lib/api'
import { useApiAction } from '@/lib/use-api-action'
import { useEntityFormDialog } from '@/lib/use-entity-form-dialog'
import { formatDateTime } from '@/lib/utils'
import type { ApiToken } from '@/lib/types'

export function TokensPage() {
  const { confirm, dialog } = useConfirm()
  const { data, isLoading, error, mutate } = useTokens()
  const form = useEntityFormDialog<ApiToken | null>(null)
  const editing = form.item
  const formOpen = form.open
  const { openCreate, openEdit } = form
  const { run, isBusy } = useApiAction()
  const inferenceTokens = data

  const toggleToken = (token: ApiToken) =>
    run(token.name, async () => {
      // 只提交启停所需字段，绝不回传列表里的 token——那是脱敏值（abcd...wxyz），
      // 整体 PUT 会绕过「留空即不变」把真实密钥覆盖成掩码（密钥永久损坏）。
      await api.updateToken(token.name, {
        name: token.name,
        enabled: !token.enabled,
      })
    }, {
      success: { title: token.enabled ? '已停用 API Key' : '已启用 API Key', description: token.name },
      refresh: [mutate, () => revalidate.usage()],
    })

  async function handleDelete(token: ApiToken) {
    const okToDelete = await confirm({
      title: `删除 API Key「${token.name}」？`,
      description: '使用该 Key 的客户端将立即失去访问权限，且无法恢复。',
      confirmText: '删除',
    })
    if (!okToDelete) return
    await run(token.name, async () => {
      await api.deleteToken(token.name)
    }, {
      success: { title: '已删除 API Key' },
      refresh: [mutate],
      errorTitle: '删除失败',
    })
  }

  return (
    <>

      <div className="relative z-[1] space-y-6">
        <PageHeader
          title="调用 Key"          actions={
            <Button variant="primary" onClick={openCreate}>新增 Key</Button>
          }
        />

        <AsyncState
          isLoading={isLoading}
          error={error}
          data={inferenceTokens}
          onRetry={() => mutate()}
          loadingColumns={5}
          emptyTitle="暂无任何调用 Key"
          emptyAction={
            <Button variant="primary" className="h-9 px-5" onClick={openCreate}>新增 Key</Button>
          }
        >
          {(tokens) => (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <TableHeader className="bg-secondary/20">
                  <TableRow className="border-b border-border/60 hover:bg-transparent">
                    <TableHead className="py-3.5 pl-4">名称</TableHead>
                    <TableHead className="py-3.5">Key 凭证</TableHead>
                    <TableHead className="py-3.5">创建时间</TableHead>
                    <TableHead className="py-3.5 text-center">状态</TableHead>
                    <TableHead className="py-3.5 pr-4 text-right font-semibold text-2xs uppercase tracking-wider text-muted-foreground">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody className="divide-y divide-border/30">
                {tokens.map((token) => (
                  <TableRow key={token.name} className="border-b-0">
                    <TableCell className="py-3.5 pl-4 font-medium text-foreground">{token.name}</TableCell>
                    <TableCell className="py-3.5 font-mono text-xs text-muted-foreground">
                      <span className="flex items-center gap-1.5">
                        <RevealCopyButton name={token.name} maskedToken={token.token || '••••'} />
                      </span>
                    </TableCell>
                    <TableCell className="py-3.5 whitespace-nowrap font-mono text-xs text-muted-foreground">
                      {token.createdAt ? formatDateTime(token.createdAt) : '—'}
                    </TableCell>
                    <TableCell className="py-3.5 text-center">
                      <Switch
                        checked={token.enabled}
                        disabled={isBusy(token.name)}
                        onCheckedChange={() => toggleToken(token)}
                        aria-label={`${token.enabled ? '停用' : '启用'} ${token.name}`}
                      />
                    </TableCell>
                    <TableCell className="py-3.5 pr-4 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="iconSm"
                          title="编辑"
                          onClick={() => openEdit(token)}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button variant="danger" size="iconSm" title="删除" onClick={() => handleDelete(token)}>
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </table>
          </div>
        )}
      </AsyncState>

      <TokenFormDialog open={formOpen} onOpenChange={form.setOpen} token={editing} onSaved={() => mutate()} />
      {dialog}
    </div>
    </>
  )
}

function TokenFormDialog({
  open,
  onOpenChange,
  token,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  token: ApiToken | null
  onSaved: () => void
}) {
  const toast = useToast()
  const isEdit = !!token
  const [name, setName] = useState('')
  const [secret, setSecret] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [saving, setSaving] = useState(false)

  // 打开时初始化一次表单。deps 刻意不含 groups：对话框开着时任何
  // model-groups 重验证（别的页面触发全局 mutate 等）都会重置表单，
  // 清掉用户输到一半的 secret。悬空组过滤在初始化时刻快照即可。
  useEffect(() => {
    if (open) {
      setName(token?.name ?? '')
      setSecret('')
      setEnabled(token?.enabled ?? true)
    }
  }, [open, token])

  async function handleSave() {
    if (!name.trim()) {
      toast.error('请填写名称')
      return
    }
    if (!isEdit && !secret.trim()) {
      toast.error('请填写 Key 明文')
      return
    }
    setSaving(true)
    try {
      const payload: ApiToken = { name: name.trim(), enabled }
      if (secret.trim()) payload.token = secret.trim()
      if (isEdit && token) await api.updateToken(token.name, payload)
      else await api.createToken(payload)
      // 保存后立即清空明文输入框
      setSecret('')
      onSaved()
      toast.success(isEdit ? 'API Key 已更新' : 'API Key 已创建')
      onOpenChange(false)
    } catch (err) {
      toast.error('保存失败', (err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>
            {isEdit ? '编辑调用 Key' : '新增调用 Key'}
          </DialogTitle>
          <DialogDescription>
            明文 Key 仅在此处录入，保存后不再展示完整值。
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="space-y-2">
            <Label required>名称</Label>
            <Input
              value={name}
              placeholder="default"
              disabled={isEdit}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label required={!isEdit}>Key 明文</Label>
            <div className="flex items-center gap-2">
              <SecretInput
                className="flex-1"
                value={secret}
                placeholder={isEdit ? '留空则保持原 Key' : 'client-key'}
                onChange={(e) => setSecret(e.target.value)}
              />
              {secret.trim() && <CopyButton value={secret.trim()} title="复制" />}
            </div>
          </div>
          <label className="flex items-center gap-3">
            <Switch checked={enabled} onCheckedChange={setEnabled} />
            <span className="text-sm font-medium">启用</span>
          </label>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            取消
          </Button>
          <Button variant="primary" onClick={handleSave} disabled={saving}>
            {saving ? '保存中…' : '保存'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
