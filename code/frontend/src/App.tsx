// 认证入口：setup → login → ready；就绪后交给路由表渲染。
import { useEffect } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router'
import { Toaster } from 'sonner'
import { LoginPage } from '@/components/LoginPage'
import { SetupPage } from '@/components/SetupPage'
import { safeNextPath } from '@/lib/routes'
import { AppRoutes } from '@/router'
import { AppDataProvider, useAppData } from '@/state/app-data'

function AuthGate() {
  const { authState, backendVersion, markLoggedIn } = useAppData()
  const navigate = useNavigate()
  const location = useLocation()

  // 登录成功回到原始目标（仅限站内安全路径），否则进入看板。
  useEffect(() => {
    if (authState !== 'ready') return
    if (location.pathname !== '/login' && location.pathname !== '/setup') return
    const next = safeNextPath(new URLSearchParams(location.search).get('next'))
    navigate(next ?? '/dashboard', { replace: true })
  }, [authState, location.pathname, location.search, navigate])

  if (authState === 'checking') {
    return <div className="flex min-h-dvh items-center justify-center bg-slate-100 text-sm text-slate-500">正在检查登录状态…</div>
  }

  if (authState === 'setup') {
    return <div className="min-h-dvh">
      <Toaster position="top-right" richColors closeButton />
      <SetupPage onReady={() => markLoggedIn('admin')} />
    </div>
  }

  if (authState === 'login') {
    // 未登录访问站内页面时，记录目标地址后跳登录；登录成功再回跳。
    if (location.pathname !== '/login') {
      const target = `${location.pathname}${location.search}`
      return <Navigate to={`/login?next=${encodeURIComponent(target)}`} replace />
    }
    return <div className="min-h-dvh">
      <Toaster position="top-right" richColors closeButton />
      <LoginPage version={backendVersion} onLoggedIn={() => markLoggedIn()} />
    </div>
  }

  return <AppRoutes />
}

export default function App() {
  return <AppDataProvider><AuthGate /></AppDataProvider>
}
