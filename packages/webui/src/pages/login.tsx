import { BrandMark } from '@/components/brand-mark'
import { ThemeToggle } from '@/components/theme-toggle'
import { PasswordForm } from '@/components/password-form'
import type { PanelSession } from '@/lib/api'

export function LoginPage({ session }: { session: PanelSession }) {
  return <main className="flex min-h-dvh items-center justify-center bg-background px-5 py-12">
    <div className="absolute right-5 top-5"><ThemeToggle /></div>
    <div className="w-full max-w-sm space-y-8">
      <BrandMark size="login" />
      <div className="space-y-2 text-center">
        <h1 className="text-2xl font-semibold tracking-tight">{session.setupRequired ? '设置管理密码' : '登录'}</h1>
        <p className="text-sm text-muted-foreground">{session.setupRequired ? '首次使用，为管理面板设置密码。' : '输入管理密码以继续。'}</p>
      </div>
      {session.setupRequired && !session.setupAllowed
        ? <p role="status" className="text-sm text-muted-foreground">请在运行 Columba 的本机完成首次设置。</p>
        : <PasswordForm mode={session.setupRequired ? 'setup' : 'login'} />}
    </div>
  </main>
}
