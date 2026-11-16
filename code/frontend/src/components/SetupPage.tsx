import { useEffect, useState } from 'react'
import logoFull from '@/assets/logo-hl.png'
import { KeyRound, Loader2, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'
import { completeSetup, login } from '@/api'

interface Props {
  onReady: () => void
}

// SetupPage 平台初始化：一次性令牌＋管理员密码，完成后自动登录。
export function SetupPage({ onReady }: Props) {
  useEffect(() => { document.title = '初始化 · 慧浪智算 · FlowPilot' }, [])
  const [token, setToken] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [submitting, setSubmitting] = useState(false)

  async function submit() {
    if (!token.trim()) {
      toast.error('请输入初始化令牌', { description: '令牌在服务器数据目录 init-token 文件中（0600 权限）。' })
      return
    }
    if (password !== confirm) {
      toast.error('两次输入的密码不一致')
      return
    }
    if (password.length < 8 || !/[a-zA-Z]/.test(password) || !/\d/.test(password)) {
      toast.error('密码不满足强度要求', { description: '至少 8 位，且同时包含字母与数字。' })
      return
    }
    setSubmitting(true)
    try {
      await completeSetup(token.trim(), password)
      await login('admin', password)
      toast.success('初始化完成', { description: '管理员账户已创建，已自动登录。' })
      onReady()
    } catch (error) {
      toast.error('初始化失败', { description: error instanceof Error ? error.message : '未知错误' })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-100 p-6" data-testid="setup-page">
      <div className="grid w-full max-w-4xl overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm lg:grid-cols-2">
        <div className="hidden flex-col justify-between bg-teal-800 p-10 text-white lg:flex">
          <div>
            <div className="inline-flex rounded-xl bg-white px-3 py-2">
              <img src={logoFull} alt="慧浪智算 · FlowPilot" className="h-9 w-auto" />
            </div>
            <div className="mt-3 text-sm text-teal-100">ComfyUI 一体化部署与管理平台</div>
          </div>
          <div className="space-y-4 text-sm leading-6 text-teal-50">
            <div className="flex items-start gap-3"><ShieldCheck className="mt-0.5 size-5 shrink-0" /><div>首次使用需要初始化管理员账户，完成后初始化令牌立即失效。</div></div>
            <div className="flex items-start gap-3"><KeyRound className="mt-0.5 size-5 shrink-0" /><div>初始化令牌位于服务器数据目录的 <code className="rounded bg-teal-900/60 px-1">init-token</code> 文件（权限 0600）。</div></div>
            <div className="flex items-start gap-3"><ShieldCheck className="mt-0.5 size-5 shrink-0" /><div>管理台建议仅在受控管理网开放；本平台默认不对公网暴露。</div></div>
          </div>
          <div className="text-xs text-teal-200">FlowPilot · 安全初始化</div>
        </div>
        <div className="p-10">
          <h1 className="text-xl font-semibold text-slate-900">平台初始化</h1>
          <p className="mt-2 text-sm text-slate-500">设置管理员密码，开启安全登录。</p>
          <div className="mt-8 space-y-5">
            <div>
              <label htmlFor="setup-token" className="text-sm font-medium text-slate-700">初始化令牌</label>
              <input
                id="setup-token"
                type="password"
                autoComplete="off"
                className="mt-1.5 w-full rounded-lg border border-slate-300 px-3 py-2.5 text-sm outline-none focus:border-teal-600 focus:ring-2 focus:ring-teal-100"
                placeholder="输入服务器上的 init-token 文件内容"
                value={token}
                onChange={(event) => setToken(event.target.value)}
              />
            </div>
            <div>
              <label htmlFor="setup-password" className="text-sm font-medium text-slate-700">管理员密码</label>
              <input
                id="setup-password"
                type="password"
                autoComplete="new-password"
                className="mt-1.5 w-full rounded-lg border border-slate-300 px-3 py-2.5 text-sm outline-none focus:border-teal-600 focus:ring-2 focus:ring-teal-100"
                placeholder="至少 8 位，包含字母与数字"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
              />
            </div>
            <div>
              <label htmlFor="setup-confirm" className="text-sm font-medium text-slate-700">确认密码</label>
              <input
                id="setup-confirm"
                type="password"
                autoComplete="new-password"
                className="mt-1.5 w-full rounded-lg border border-slate-300 px-3 py-2.5 text-sm outline-none focus:border-teal-600 focus:ring-2 focus:ring-teal-100"
                placeholder="再次输入密码"
                value={confirm}
                onChange={(event) => setConfirm(event.target.value)}
              />
            </div>
            <button
              type="button"
              className="btn-primary w-full justify-center"
              disabled={submitting}
              onClick={submit}
              data-testid="setup-submit"
            >
              {submitting ? <Loader2 className="size-4 animate-spin" /> : <ShieldCheck className="size-4" />}
              {submitting ? '正在初始化' : '完成初始化'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
