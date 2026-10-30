import { Suspense, lazy, useEffect } from 'react'
import { HashRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AppLayout } from './components/app-layout'
import useSWR from 'swr'
import { api, SESSION_EXPIRED } from './lib/api'
import { LoginPage } from './pages/login'
import { Button } from './components/ui/button'

// 页面按路由拆包：recharts 等重组件只随用到它的页面下载，
// 登录页与首屏外壳保持轻量。
const OverviewPage = lazy(() => import('./pages/overview').then((m) => ({ default: m.OverviewPage })))
const SourcesPage = lazy(() => import('./pages/sources').then((m) => ({ default: m.SourcesPage })))
const TokensPage = lazy(() => import('./pages/tokens').then((m) => ({ default: m.TokensPage })))
const SettingsPage = lazy(() => import('./pages/settings').then(m => ({ default: m.SettingsPage })))
const UsagePage = lazy(() => import('./pages/usage').then((m) => ({ default: m.UsagePage })))

/** 登录前/外壳外加载页面的占位：全屏居中。 */
function BootFallback() {
  return (
    <div className="flex min-h-screen items-center justify-center" aria-busy="true">
      <span className="skeleton h-10 w-10 rounded-full" />
    </div>
  )
}


/**
 * 登录后只预热高概率下一跳（总览之后常见的调用日志 / 模型源），避开图表
 * chunk。空闲时调度，省流或 2g 网络跳过。
 */
function usePreloadRoutes(enabled: boolean) {
  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    const run = () => {
      if (cancelled) return
      const conn = (navigator as Navigator & { connection?: { saveData?: boolean; effectiveType?: string } }).connection
      if (conn?.saveData || conn?.effectiveType === 'slow-2g' || conn?.effectiveType === '2g') return
      void import('./pages/usage-logs')
      void import('./pages/sources')
    }
    const ric = window.requestIdleCallback
    if (typeof ric === 'function') {
      const id = ric(run, { timeout: 4000 })
      return () => {
        cancelled = true
        window.cancelIdleCallback(id)
      }
    }
    const id = window.setTimeout(run, 2000)
    return () => {
      cancelled = true
      window.clearTimeout(id)
    }
  }, [enabled])
}

export function App() {
  const { data: session, error, mutate } = useSWR('panel-session', api.panelSession, { shouldRetryOnError: false })
  usePreloadRoutes(session?.authenticated === true)
  useEffect(() => {
    // Reload clears all private SWR data and in-flight requests after logout/expiry.
    let reloading = false
    const expired = () => {
      if (!reloading) { reloading = true; window.location.reload() }
    }
    window.addEventListener(SESSION_EXPIRED, expired)
    return () => window.removeEventListener(SESSION_EXPIRED, expired)
  }, [])

  if (error) return <main className="flex min-h-dvh flex-col items-center justify-center gap-4 px-5">
    <p role="alert">无法连接管理面板，请确认服务正在运行。</p>
    <Button onClick={() => void mutate()}>重试</Button>
  </main>
  if (!session) return <BootFallback />
  if (!session.authenticated) return <LoginPage session={session} />

  return (
    <HashRouter>
      <Suspense fallback={<BootFallback />}>
          <Routes>
            <Route element={<AppLayout />}>
              <Route path="/overview" element={<OverviewPage />} />
              <Route path="/sources" element={<SourcesPage />} />
              <Route path="/tokens" element={<TokensPage />} />
              <Route path="/settings" element={<SettingsPage />} />
              <Route path="/usage" element={<UsagePage />} />
              <Route path="/usage-logs" element={<Navigate to="/usage?tab=calls" replace />} />
              <Route path="/logs" element={<Navigate to="/settings?tab=logs" replace />} />
              <Route path="/runtime" element={<Navigate to="/settings?tab=runtime" replace />} />
              <Route path="/diagnostics" element={<Navigate to="/settings?tab=diagnostics" replace />} />
            </Route>
            <Route path="*" element={<Navigate to="/overview" replace />} />
          </Routes>
      </Suspense>
    </HashRouter>
  )
}
