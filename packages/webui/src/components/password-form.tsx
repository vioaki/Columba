import { useId, useState, type FormEvent } from 'react'
import { api } from '@/lib/api'
import { Button } from './ui/button'
import { Label } from './ui/label'
import { SecretInput } from './secret-input'

export function PasswordForm({ mode }: { mode: 'login' | 'setup' | 'change' }) {
  const id = useId()
  const [current, setCurrent] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const creating = mode !== 'login'

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (busy) return
    setError('')
    setSaved(false)
    if (creating && password !== confirmation) {
      setError('两次输入的密码不一致')
      return
    }
    setBusy(true)
    try {
      if (mode === 'change') {
        await api.panelChangePassword(current, password)
        setCurrent('')
        setPassword('')
        setConfirmation('')
        setSaved(true)
      } else {
        await api.panelLogin(password)
        window.location.reload()
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '请求失败，请重试')
    } finally {
      setBusy(false)
    }
  }

  return <form onSubmit={submit} className="space-y-5" aria-busy={busy}>
    {mode === 'change' && <div className="space-y-2">
      <Label htmlFor={`${id}-current`}>当前密码</Label>
      <SecretInput id={`${id}-current`} autoComplete="current-password" value={current} onChange={event => setCurrent(event.target.value)} required disabled={busy} />
    </div>}
    <div className="space-y-2">
      <Label htmlFor={`${id}-password`}>{creating ? '新密码' : '管理密码'}</Label>
      <SecretInput id={`${id}-password`} autoComplete={creating ? 'new-password' : 'current-password'} value={password} onChange={event => setPassword(event.target.value)} minLength={creating ? 12 : 1} maxLength={256} required disabled={busy} aria-describedby={creating ? `${id}-hint` : undefined} />
      {creating && <p id={`${id}-hint`} className="text-xs text-muted-foreground">至少 12 个字符，建议使用长密码或密码短语。</p>}
    </div>
    {creating && <div className="space-y-2">
      <Label htmlFor={`${id}-confirm`}>确认新密码</Label>
      <SecretInput id={`${id}-confirm`} autoComplete="new-password" value={confirmation} onChange={event => setConfirmation(event.target.value)} required disabled={busy} />
    </div>}
    {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    {saved && <p role="status" className="text-sm text-muted-foreground">密码已更新，其他会话已退出。</p>}
    <Button type="submit" variant="primary" size="lg" className="w-full" disabled={busy}>
      {busy ? '请稍候…' : mode === 'setup' ? '设置密码并进入' : mode === 'change' ? '更新密码' : '登录'}
    </Button>
  </form>
}
