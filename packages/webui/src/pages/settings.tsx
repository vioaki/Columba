import { useSearchParams } from 'react-router-dom'
import * as Tabs from '@radix-ui/react-tabs'
import { PageHeader } from '@/components/page-header'
import { RuntimeConfigPage } from './runtime-config'
import { SystemLogsPage } from './system-logs'
import { DiagnosticsPage } from './diagnostics'
import { PasswordForm } from '@/components/password-form'
export function SettingsPage() {
 const [params, setParams] = useSearchParams()
 return <div className="space-y-6"><PageHeader title="设置" /><Tabs.Root value={params.get('tab') ?? 'runtime'} onValueChange={tab => setParams({ tab })}><Tabs.List aria-label="设置分类" className="mb-5 flex flex-wrap gap-2 border-b">{[['runtime', '运行配置'], ['security', '登录安全'], ['logs', '系统日志'], ['diagnostics', '诊断']].map(([value, label]) => <Tabs.Trigger key={value} value={value} className="rounded-t px-4 py-3 text-sm data-[state=active]:border-b-2 data-[state=active]:border-foreground">{label}</Tabs.Trigger>)}</Tabs.List><Tabs.Content value="runtime"><RuntimeConfigPage /></Tabs.Content><Tabs.Content value="security"><section className="max-w-sm space-y-6"><h2 className="text-lg font-medium">修改管理密码</h2><PasswordForm mode="change" /></section></Tabs.Content><Tabs.Content value="logs"><SystemLogsPage /></Tabs.Content><Tabs.Content value="diagnostics"><DiagnosticsPage /></Tabs.Content></Tabs.Root></div>
}
