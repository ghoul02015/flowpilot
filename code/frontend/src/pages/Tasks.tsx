import { useEffect, useMemo, useState } from 'react'
import { Ban, FileText, Loader2, RotateCcw } from 'lucide-react'
import { getUnifiedTasks, type UnifiedTaskView } from '@/api'
import { EmptyState, PageHeader, Panel, ProgressBar } from '@/components/common'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

interface Props {
  onAction: (id: string, action: 'cancel' | 'retry') => void
}

// 统一任务状态徽章（0.0.9.9 协议）。
const statusMeta: Record<string, { label: string; className: string }> = {
  queued: { label: '排队中', className: 'border-slate-200 bg-slate-50 text-slate-600' },
  running: { label: '运行中', className: 'border-teal-200 bg-teal-50 text-teal-700' },
  paused: { label: '已暂停', className: 'border-amber-200 bg-amber-50 text-amber-700' },
  succeeded: { label: '已完成', className: 'border-emerald-200 bg-emerald-50 text-emerald-700' },
  failed: { label: '失败', className: 'border-rose-200 bg-rose-50 text-rose-700' },
  cancelled: { label: '已取消', className: 'border-slate-200 bg-slate-50 text-slate-500' },
  partial: { label: '部分完成', className: 'border-orange-200 bg-orange-50 text-orange-700' },
}

const typeMeta: Record<string, string> = {
  download: '模型下载',
  import: '模型导入',
  'image-pull': '镜像拉取',
  'image-build': '镜像构建',
  migration: '数据迁移',
  backup: '平台备份',
}

// Tasks 统一任务中心（0.0.9.9）：聚合下载/导入、镜像构建/拉取、数据迁移任务。
export function Tasks({ onAction }: Props) {
  const [tasks, setTasks] = useState<UnifiedTaskView[] | null>(null)
  const [status, setStatus] = useState('all')
  const [type, setType] = useState('all')
  const [logId, setLogId] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const controller = new AbortController()
    async function poll() {
      try {
        const page = await getUnifiedTasks()
        setTasks(page.tasks)
        setLoading(false)
      } catch {
        if (!controller.signal.aborted) setTasks(null)
      }
    }
    void poll()
    const timer = window.setInterval(() => void poll(), 3000)
    return () => {
      controller.abort()
      window.clearInterval(timer)
    }
  }, [])

  const filtered = useMemo(() => (tasks ?? []).filter((task) => (status === 'all' || task.status === status) && (type === 'all' || task.type === type)), [tasks, status, type])
  const logTask = (tasks ?? []).find((task) => task.id === logId)

  return <>
    <PageHeader eyebrow="TASK CENTER" title="任务中心" description="统一跟踪模型下载、导入、镜像构建/拉取与数据迁移任务，失败原因清晰可查。" />
    <Panel>
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 p-4">
        <div className="text-sm text-slate-600">{loading ? '加载中…' : `共 ${filtered.length} 个任务`}</div>
        <div className="flex gap-2">
          <select className="field-input sm:w-40" aria-label="按任务类型筛选" value={type} onChange={(event) => setType(event.target.value)} data-testid="task-type-filter">
            <option value="all">全部类型</option>
            {Object.entries(typeMeta).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
          </select>
          <select className="field-input sm:w-36" aria-label="按任务状态筛选" value={status} onChange={(event) => setStatus(event.target.value)}>
            <option value="all">全部状态</option>
            {Object.entries(statusMeta).map(([key, item]) => <option key={key} value={key}>{item.label}</option>)}
          </select>
        </div>
      </div>
      <div className="divide-y divide-slate-100" data-testid="unified-task-list">{filtered.map((task) => { const meta = statusMeta[task.status] ?? { label: task.status, className: 'border-slate-200 bg-slate-50 text-slate-500' }; return <div key={task.id} className="grid gap-4 px-5 py-5 lg:grid-cols-[1.3fr_1fr_auto] lg:items-center">
        <div><div className="flex flex-wrap items-center gap-2"><span className="font-medium text-slate-900">{task.name}</span><span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${meta.className}`}>{meta.label}</span><span className="rounded bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-600" data-testid={`task-type-badge-${task.type}`}>{typeMeta[task.type] ?? task.type}</span></div><div className="mt-2 text-xs text-slate-500">{task.objectRef && <>对象 {task.objectRef} · </>}{task.createdAt}</div><p className="mt-2 text-sm leading-6 text-slate-600">{task.error || task.message}</p></div>
        <div className="flex items-center gap-3">
          {task.progress >= 0
            ? <><ProgressBar value={task.progress} tone={task.status === 'failed' ? 'slate' : 'teal'} /><span className="w-10 text-right text-xs font-medium text-slate-600">{task.progress}%</span></>
            : <div className="flex flex-1 items-center gap-2 text-xs text-slate-500"><Loader2 className="size-3.5 animate-spin" />进度不可估计</div>}
        </div>
        <div className="flex items-center gap-1">
          <button type="button" className="btn-quiet" onClick={() => setLogId(task.id)}><FileText className="size-4" />详情</button>
          {task.cancelable && task.id.startsWith('dl-') && ['running', 'paused'].includes(task.status) && <button type="button" className="icon-button" title="取消任务" aria-label={`取消 ${task.name}`} onClick={() => onAction(task.id, 'cancel')}><Ban className="size-4" /></button>}
          {task.id.startsWith('dl-') && ['failed', 'cancelled'].includes(task.status) && <button type="button" className="btn-quiet" onClick={() => onAction(task.id, 'retry')}><RotateCcw className="size-4" />重试</button>}
        </div>
      </div> })}{!loading && filtered.length === 0 && <EmptyState title="当前筛选下没有任务" description="切换类型或状态筛选查看其他任务。" />}{tasks === null && !loading && <EmptyState title="任务接口不可用" description="请确认后端已连接。" />}</div>
    </Panel>
    <Dialog open={Boolean(logTask)} onOpenChange={(open) => !open && setLogId(null)}><DialogContent className="sm:max-w-2xl">{logTask && <><DialogHeader><DialogTitle>任务详情</DialogTitle><DialogDescription>{logTask.name} · {logTask.id}</DialogDescription></DialogHeader><dl className="grid gap-3 rounded-lg border border-slate-200 p-4 text-sm">{[['任务类型', typeMeta[logTask.type] ?? logTask.type], ['关联对象', logTask.objectRef || '—'], ['当前状态', (statusMeta[logTask.status] ?? { label: logTask.status }).label], ['当前进度', logTask.progress >= 0 ? `${logTask.progress}%` : '不可估计'], ['创建时间', logTask.createdAt], ['最新信息', logTask.message], ...(logTask.error ? [['失败原因', logTask.error]] : [])].map(([term, value]) => <div key={term} className="grid grid-cols-[88px_1fr] gap-3"><dt className="text-slate-500">{term}</dt><dd className="break-all font-medium text-slate-800">{value}</dd></div>)}</dl><p className="text-xs leading-5 text-slate-500">统一任务协议聚合各子系统实时状态；数据迁移任务全程持久化可追溯。</p><DialogFooter><button type="button" className="btn-primary" onClick={() => setLogId(null)}>关闭</button></DialogFooter></>}</DialogContent></Dialog>
  </>
}
