import { useEffect, useState } from 'react'
import logoFull from '@/assets/logo-hl.png'
import { Loader2, LogIn } from 'lucide-react'
import { toast } from 'sonner'
import { login } from '@/api'

interface Props {
  version: string
  onLoggedIn: () => void
}

// LoginPage 管理员登录页。
export function LoginPage({ version, onLoggedIn }: Props) {
  useEffect(() => { document.title = '登录 · 慧浪智算 · FlowPilot' }, [])
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)

  async function submit() {
    setSubmitting(true)
    try {
      await login(username.trim(), password)
      toast.success('登录成功')
      onLoggedIn()
    } catch (error) {
      toast.error('登录失败', { description: error instanceof Error ? error.message : '未知错误' })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-100 p-6" data-testid="login-page">
      <div className="w-full max-w-md rounded-2xl border border-slate-200 bg-white p-10 shadow-sm">
        <div>
          <img src={logoFull} alt="慧浪智算 · FlowPilot" className="h-20 w-auto max-w-full" />
          <div className="mt-3 text-xs text-slate-500">ComfyUI 一体化管理平台 · v{version}</div>
        </div>
        <div className="mt-8 space-y-5">
          <div>
            <label htmlFor="login-username" className="text-sm font-medium text-slate-700">用户名</label>
            <input
              id="login-username"
              autoComplete="username"
              className="mt-1.5 w-full rounded-lg border border-slate-300 px-3 py-2.5 text-sm outline-none focus:border-teal-600 focus:ring-2 focus:ring-teal-100"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
            />
          </div>
          <div>
            <label htmlFor="login-password" className="text-sm font-medium text-slate-700">密码</label>
            <input
              id="login-password"
              type="password"
              autoComplete="current-password"
              className="mt-1.5 w-full rounded-lg border border-slate-300 px-3 py-2.5 text-sm outline-none focus:border-teal-600 focus:ring-2 focus:ring-teal-100"
              placeholder="输入管理员密码"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              onKeyDown={(event) => { if (event.key === 'Enter' && !submitting) void submit() }}
            />
          </div>
          <button type="button" className="btn-primary w-full justify-center" disabled={submitting} onClick={submit} data-testid="login-submit">
            {submitting ? <Loader2 className="size-4 animate-spin" /> : <LogIn className="size-4" />}
            {submitting ? '正在登录' : '登录'}
          </button>
        </div>
        <p className="mt-6 text-center text-xs leading-5 text-slate-400">管理台建议仅在受控管理网开放 · 连续失败 5 次将临时锁定 15 分钟</p>
      </div>
    </div>
  )
}
