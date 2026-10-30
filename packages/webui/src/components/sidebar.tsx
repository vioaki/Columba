import { NavLink } from 'react-router-dom'
import { Activity, BarChart3, Database, KeyRound, LogOut, Settings } from 'lucide-react'
import { useState } from 'react'
import { api } from '@/lib/api'
import { Button } from './ui/button'
import { useToast } from './ui/use-toast'
import { cn } from '@/lib/utils'
import { ThemeToggle } from './theme-toggle'
import { BrandMark } from './brand-mark'
const items = [
 { to: '/overview', label: '总览', icon: Activity },
 { to: '/sources', label: '模型', icon: Database },
 { to: '/tokens', label: '调用 Key', icon: KeyRound },
 { to: '/usage', label: '用量费用', icon: BarChart3 },
 { to: '/settings', label: '设置', icon: Settings },
]
export function Sidebar({ onNavigate }: { onNavigate?: () => void }) {
 const [leaving, setLeaving] = useState(false)
 const toast = useToast()
 async function logout() {
  setLeaving(true)
  try { await api.panelLogout(); window.location.reload() }
  catch { toast.error('退出失败，请重试'); setLeaving(false) }
 }
 return <div className="flex h-full flex-col gap-8 bg-sidebar py-6"><BrandMark className="px-5" /><nav aria-label="主导航" className="flex flex-1 flex-col gap-2 overflow-y-auto px-3">{items.map(({ to, label, icon: Icon }) => <NavLink key={to} to={to} onClick={onNavigate} className={({ isActive }) => cn('flex min-h-11 items-center gap-3 rounded-lg px-3 text-sm', isActive ? 'bg-foreground font-medium text-background' : 'text-muted-foreground hover:bg-muted hover:text-foreground')}><Icon aria-hidden className="h-[18px] w-[18px]" /><span>{label}</span></NavLink>)}</nav><div className="mx-5 flex items-center justify-between border-t pt-3"><Button variant="ghost" disabled={leaving} onClick={() => void logout()}><LogOut aria-hidden />退出登录</Button><ThemeToggle /></div></div>
}
