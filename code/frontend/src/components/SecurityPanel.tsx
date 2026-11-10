import { useEffect, useState } from 'react'
import { Clock, KeyRound, LogOut, MonitorSmartphone, RefreshCw, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'
import {
  changePassword,
  listAuthSessions,
  revokeAuthSession,
  type AuthSession,
} from '@/api'
import { Panel, PanelHeader } from '@/components/common'

function formatTime(value: string): string {
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString('zh-CN', { hour12: false })
}

// SecurityPanel 安全与会话：当前账户、修改密码、活跃会话管理。
export function SecurityPanel({ onLoggedOut }: { onLoggedOut: () => void }) {
  const [sessions, setSessions] = useState<AuthSession[]>([])
  const [loading, setLoading] = useState(false)
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirmValue, setConfirmValue] = useState('')
  const [changing, setChanging] = useState(false)

  function reload() {
    setLoading(true)
    listAuthSessions()
      .then((result) => setSessions(result.sessions))
      .catch((reason: unknown) => toast.error('会话列表读取失败', { description: reason instanceof Error ? reason.message : '未知错误' }))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    reload()
  }, [])

  async function submitChange() {
    if (next !== confirmValue) {
      toast.error('两次输入的新密码不一致')
      return
    }
    setChanging(true)
    try {
      await changePassword(current, next)
      toast.success('密码已修改', { description: '其他会话已全部撤销，当前会话保持登录。' })
      setCurrent('')
      setNext('')
      setConfirmValue('')
      reload()
    } catch (error) {
      toast.error('修改失败', { description: error instanceof Error ? error.message : '未知错误' })
    } finally {
      setChanging(false)
    }
  }

  async function revoke(id: string) {
    try {
      await revokeAuthSession(id.replace('…', ''))
      toast.success('会话已撤销')
      reload()
    } catch (error) {
      toast.error('撤销失败', { description: error instanceof Error ? error.message : '未知错误' })
    }
  }

  return (
    <div className="space-y-5" data-testid="security-panel">
      <Panel>
        <PanelHeader title="修改密码" description="管理员密码 · Argon2id 哈希存储" />
        <div className="grid gap-4 p-5 sm:max-w-lg">
          <div>
            <label htmlFor="current-password" className="text-sm font-medium text-slate-700">当前密码</label>
            <input id="current-password" type="password" autoComplete="current-password" className="mt-1.5 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm outline-none focus:border-teal-600" value={current} onChange={(event) => setCurrent(event.target.value)} />
          </div>
          <div>
            <label htmlFor="new-password" className="text-sm font-medium text-slate-700">新密码</label>
            <input id="new-password" type="password" autoComplete="new-password" className="mt-1.5 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm outline-none focus:border-teal-600" placeholder="至少 8 位，包含字母与数字" value={next} onChange={(event) => setNext(event.target.value)} />
          </div>
          <div>
            <label htmlFor="confirm-new-password" className="text-sm font-medium text-slate-700">确认新密码</label>
            <input id="confirm-new-password" type="password" autoComplete="new-password" className="mt-1.5 w-full rounded-lg border border-slate-300 px-3 py-2 text-sm outline-none focus:border-teal-600" value={confirmValue} onChange={(event) => setConfirmValue(event.target.value)} />
          </div>
          <button type="button" className="btn-primary w-fit" disabled={changing} onClick={submitChange} data-testid="change-password-submit">
            <KeyRound className="size-4" />{changing ? '提交中' : '修改密码'}
          </button>
          <p className="text-xs leading-5 text-slate-500">修改成功后，除当前会话外的全部登录会话将被撤销。</p>
        </div>
      </Panel>

      <Panel>
        <PanelHeader
          title="活跃会话"
          description="闲置 30 分钟或 7 天绝对过期后自动失效"
          action={<div className="flex gap-2">
            <button type="button" className="btn-secondary" disabled={loading} onClick={reload}><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />刷新</button>
            <button type="button" className="btn-secondary" onClick={onLoggedOut}><LogOut className="size-4" />退出登录</button>
          </div>}
        />
        <div className="overflow-x-auto" data-testid="auth-sessions-table">
          <table className="data-table">
            <thead>
              <tr><th>会话</th><th>最近活动</th><th>创建时间</th><th>闲置到期</th><th>客户端</th><th>操作</th></tr>
            </thead>
            <tbody>
              {sessions.map((item) => (
                <tr key={item.id} data-testid={`auth-session-${item.current ? 'current' : 'other'}`}>
                  <td><span className="font-mono text-xs">{item.id}</span>{item.current && <span className="ml-2 inline-flex rounded-full bg-emerald-50 px-2 py-0.5 text-xs font-medium text-emerald-700">当前</span>}</td>
                  <td className="text-slate-600">{formatTime(item.lastSeenAt)}</td>
                  <td className="text-slate-600">{formatTime(item.createdAt)}</td>
                  <td className="text-slate-600"><Clock className="mr-1 inline size-3.5" />{formatTime(item.idleExpiresAt)}</td>
                  <td className="max-w-56 truncate text-slate-500" title={item.userAgent}><MonitorSmartphone className="mr-1 inline size-3.5" />{item.userAgent || '未知'}</td>
                  <td>{item.current ? <span className="text-xs text-slate-400">—</span> : <button type="button" className="text-sm font-medium text-rose-600 hover:underline" onClick={() => revoke(item.id)}>撤销</button>}</td>
                </tr>
              ))}
              {sessions.length === 0 && <tr><td colSpan={6} className="text-center text-slate-400">暂无活跃会话</td></tr>}
            </tbody>
          </table>
        </div>
        <div className="border-t border-slate-200 px-5 py-4 text-xs leading-5 text-slate-500">
          <ShieldCheck className="mr-1 inline size-3.5 text-emerald-600" />Cookie 为 HttpOnly＋SameSite=Lax；写操作需 CSRF 令牌校验；登录连续失败 5 次临时锁定 15 分钟。
        </div>
      </Panel>
    </div>
  )
}
