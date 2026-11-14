import type { ReactNode } from 'react'
import { ArrowUpRight, CheckCircle2, CircleAlert, Clock3, LoaderCircle, PauseCircle } from 'lucide-react'
import type { InstanceStatus, ModelStatus, TaskStatus } from '@/types'

export function PageHeader({ eyebrow, title, description, action }: { eyebrow?: string; title: string; description: string; action?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
      <div>
        {eyebrow && <div className="mb-1 text-xs font-semibold tracking-[0.14em] text-teal-700 uppercase">{eyebrow}</div>}
        <h1 className="text-2xl font-bold tracking-tight text-slate-900 md:text-[28px]">{title}</h1>
        <p className="mt-2 max-w-3xl text-sm leading-6 break-words text-slate-600">{description}</p>
      </div>
      {action && <div className="shrink-0 self-start sm:self-auto">{action}</div>}
    </div>
  )
}

export function Panel({ children, className = '', ...rest }: { children: ReactNode; className?: string } & Record<string, unknown>) {
  return <section className={`min-w-0 rounded-xl border border-slate-200 bg-[oklch(0.995_0.003_220)] shadow-[0_1px_2px_rgba(15,23,42,0.03)] ${className}`} {...rest}>{children}</section>
}

export function PanelHeader({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  return (
    <div className="flex min-h-16 flex-col gap-3 border-b border-slate-200 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h2 className="text-base font-semibold text-slate-900">{title}</h2>
        {description && <p className="mt-1 text-xs leading-5 text-slate-500">{description}</p>}
      </div>
      {action && <div className="shrink-0 self-start sm:self-auto">{action}</div>}
    </div>
  )
}

const statusText: Record<InstanceStatus | ModelStatus | TaskStatus, string> = {
  creating: '创建中', starting: '启动中', running: '运行中', stopping: '停止中', stopped: '已停止', restarting: '重启中', error: '异常',
  waiting: '等待中', downloading: '下载中', paused: '已暂停', ready: '就绪', failed: '失败', cancelled: '已取消', completed: '已完成',
}

export function StatusBadge({ status }: { status: InstanceStatus | ModelStatus | TaskStatus }) {
  const success = ['running', 'ready', 'completed'].includes(status)
  const warning = ['creating', 'starting', 'stopping', 'restarting', 'waiting', 'downloading'].includes(status)
  const danger = ['error', 'failed'].includes(status)
  const paused = ['paused', 'stopped', 'cancelled'].includes(status)
  const Icon = success ? CheckCircle2 : warning ? LoaderCircle : danger ? CircleAlert : paused ? PauseCircle : Clock3
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium whitespace-nowrap ${success ? 'bg-emerald-50 text-emerald-700' : warning ? 'bg-teal-50 text-teal-700' : danger ? 'bg-rose-50 text-rose-700' : 'bg-slate-100 text-slate-600'}`}>
      <Icon className={`size-3.5 ${['creating', 'starting', 'stopping', 'restarting', 'downloading'].includes(status) ? 'animate-spin' : ''}`} />
      {statusText[status]}
    </span>
  )
}

export function ProgressBar({ value, tone = 'teal' }: { value: number; tone?: 'teal' | 'slate' }) {
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-100" role="progressbar" aria-valuenow={value} aria-valuemin={0} aria-valuemax={100}>
      <div className={`h-full rounded-full transition-[width] duration-200 ${tone === 'teal' ? 'bg-teal-600' : 'bg-slate-400'}`} style={{ width: `${Math.max(0, Math.min(100, value))}%` }} />
    </div>
  )
}

export function TextButton({ children, onClick }: { children: ReactNode; onClick: () => void }) {
  return <button type="button" onClick={onClick} className="inline-flex min-h-10 cursor-pointer items-center gap-1 text-sm font-medium text-teal-700 transition-colors hover:text-teal-900 focus-visible:outline-teal-600">{children}<ArrowUpRight className="size-4" /></button>
}

export function EmptyState({ title, description }: { title: string; description: string }) {
  return <div className="px-5 py-14 text-center"><div className="font-medium text-slate-800">{title}</div><p className="mt-2 text-sm text-slate-500">{description}</p></div>
}
