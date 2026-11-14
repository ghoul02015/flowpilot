// 任务详情页：字段、进度、失败原因、镜像日志与允许的操作。
// 独立路由可直接刷新与分享；只轮询当前任务，不复制列表轮询。
import { useEffect, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router'
import { Ban, ChevronLeft, Loader2, Pause, Play, RotateCcw, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { cancelImageTask, deleteUnifiedTask, downloadActionApi, getImageTaskLogs, getUnifiedTask, type UnifiedTaskView } from '@/api'
import { EmptyState, PageHeader, Panel, PanelHeader, ProgressBar } from '@/components/common'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { documentTitle, pagePaths } from '@/lib/routes'
import { formatTaskTime, isActiveStatus, isDownloadType, statusMeta, typeMeta } from '@/lib/task-meta'
import { usePolling } from '@/hooks/use-polling'

export function TaskDetail({ id }: { id: string }) {
  const navigate = useNavigate()
  const location = useLocation()
  const from = typeof (location.state as { from?: unknown } | null)?.from === 'string' && String((location.state as { from?: string }).from).startsWith('/tasks') ? String((location.state as { from?: string }).from) : pagePaths.tasks
  const [task, setTask] = useState<UnifiedTaskView | null>(null)
  const [missing, setMissing] = useState(false)
  const [loading, setLoading] = useState(true)
  const [logs, setLogs] = useState<string[]>([])
  const [logsLoading, setLogsLoading] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [confirmBusy, setConfirmBusy] = useState(false)
  const active = task ? isActiveStatus(task.status) : false

  useEffect(() => {
    document.title = documentTitle(undefined, task ? task.name : '任务详情')
  }, [task])

  // 活跃任务 2 秒、终态 15 秒；页面级轮询随路由卸载停止。
  const poll = usePolling(async (signal) => {
    try {
      const result = await getUnifiedTask(id)
      if (signal.aborted) return
      setTask(result)
      setMissing(false)
    } catch (error) {
      if (signal.aborted) return
      if (error instanceof Error && /不存在|404|not found/i.test(error.message)) setMissing(true)
    } finally {
      setLoading(false)
    }
  }, { interval: 2000, idleInterval: 15000, isActive: () => active })

  useEffect(() => {
    if (!task || !task.type.startsWith('image-')) {
      setLogs([])
      return
    }
    let cancelled = false
    setLogsLoading(true)
    getImageTaskLogs(task.id)
      .then((result) => { if (!cancelled) setLogs(result) })
      .catch(() => { if (!cancelled) setLogs([]) })
      .finally(() => { if (!cancelled) setLogsLoading(false) })
    return () => { cancelled = true }
  }, [task])

  async function runAction(action: 'pause' | 'resume' | 'cancel' | 'retry') {
    if (!task) return
    try {
      if (task.id.startsWith('dl-')) {
        await downloadActionApi(task.id.slice(3), action)
      } else if (task.id.startsWith('img-') && action === 'cancel') {
        await cancelImageTask(task.id.slice(4))
      } else {
        throw new Error('该任务类型暂不支持此操作')
      }
      toast.success(action === 'cancel' ? '任务已停止' : action === 'retry' ? '任务已重试' : action === 'pause' ? '任务已暂停' : '任务已继续', { description: task.name })
      await poll.refresh()
    } catch (error) {
      toast.error('任务操作失败', { description: error instanceof Error ? error.message : '请稍后重试' })
      await poll.refresh()
    }
  }

  async function removeTask() {
    if (!task) return
    setConfirmBusy(true)
    try {
      await deleteUnifiedTask(task.id)
      toast.success('任务记录已删除', { description: task.name })
      navigate(pagePaths.tasks)
    } catch (error) {
      toast.error('删除任务记录失败', { description: error instanceof Error ? error.message : '请稍后重试' })
    } finally {
      setConfirmBusy(false)
      setConfirmOpen(false)
    }
  }

  if (missing) {
    return <>
      <PageHeader eyebrow="TASK" title="任务不存在" description="该任务记录可能已被清理，或地址不正确。" />
      <Panel><EmptyState title="没有找到任务记录" description="返回任务中心可查看当前任务与历史记录。" />
        <div className="flex justify-center pb-8"><Link to={pagePaths.tasks} className="btn-secondary"><ChevronLeft className="size-4" />返回任务中心</Link></div>
      </Panel>
    </>
  }

  if (loading && !task) {
    return <div className="flex min-h-[320px] items-center justify-center gap-2 text-sm text-slate-500"><Loader2 className="size-4 animate-spin" />正在读取任务…</div>
  }

  if (!task) return null

  const download = isDownloadType(task.type)
  const meta = statusMeta[task.status] ?? { label: task.status, className: 'border-slate-200 bg-slate-50 text-slate-500' }
  const failure = task.error || (['failed', 'cancelled'].includes(task.status) ? task.message : '')
  const fields = ([
    ['当前状态', meta.label],
    ['任务类型', typeMeta[task.type] ?? task.type],
    ['关联对象', task.objectRef || '—'],
    ['进度', task.progress >= 0 ? `${task.progress}%` : '不可估计'],
    ['创建时间', formatTaskTime(task.createdAt)],
    ['开始时间', formatTaskTime(task.startedAt)],
    ['结束时间', formatTaskTime(task.finishedAt)],
    ['最新信息', task.message || '—'],
    // 失败原因已单独高亮时，不再重复同一段文本。
  ] as [string, string][]).filter(([term, value]) => !(term === '最新信息' && failure && value === failure))

  return <>
    <PageHeader
      eyebrow="TASK DETAIL"
      title={task.name}
      description={`${typeMeta[task.type] ?? task.type} · ${task.id}`}
      action={<div className="flex flex-wrap gap-2">
        <Link to={from} className="btn-secondary"><ChevronLeft className="size-4" />返回任务中心</Link>
        {download && task.status === 'running' && <button type="button" className="btn-secondary" onClick={() => void runAction('pause')}><Pause className="size-4" />暂停</button>}
        {download && task.status === 'paused' && <button type="button" className="btn-secondary" onClick={() => void runAction('resume')}><Play className="size-4" />继续</button>}
        {download && ['failed', 'cancelled'].includes(task.status) && <button type="button" className="btn-secondary" onClick={() => void runAction('retry')}><RotateCcw className="size-4" />重试</button>}
        {isActiveStatus(task.status) && <button type="button" className="btn-secondary text-rose-600" onClick={() => void runAction('cancel')}><Ban className="size-4" />{download ? '取消' : '停止'}</button>}
        {!isActiveStatus(task.status) && <button type="button" className="btn-danger" onClick={() => setConfirmOpen(true)}><Trash2 className="size-4" />删除记录</button>}
      </div>}
    />

    <div className={task.type.startsWith('image-') ? 'grid gap-5 lg:grid-cols-3' : 'grid gap-5'}>
      <Panel className={task.type.startsWith('image-') ? 'lg:col-span-2' : undefined}>
        <PanelHeader title="任务信息" description="字段来自统一任务视图，进度与状态随轮询更新" action={<span className={`inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-medium ${meta.className}`}>{meta.label}</span>} />
        <dl className="grid gap-4 p-5 text-sm sm:grid-cols-2">
          {fields.map(([term, value]) => <div key={term} className={term === '最新信息' ? 'sm:col-span-2' : ''}>
            <dt className="text-xs text-slate-500">{term}</dt>
            <dd className="mt-1 break-all font-medium text-slate-800">{value}</dd>
          </div>)}
        </dl>
        {task.progress >= 0 && <div className="border-t border-slate-200 p-5">
          <div className="mb-2 flex items-center justify-between text-xs text-slate-500"><span>进度</span><span>{task.progress}%</span></div>
          <ProgressBar value={task.progress} />
        </div>}
        {failure && <div className="border-t border-rose-200 bg-rose-50 p-5 text-sm leading-6 break-words text-rose-800"><strong className="mr-1">失败原因</strong>{failure}</div>}
      </Panel>

      {task.type.startsWith('image-') && <Panel>
        <PanelHeader title="运行日志" description="镜像构建／拉取尾部日志" />
        <div className="p-4">
          {logsLoading
            ? <div className="rounded-lg bg-slate-950 p-4 text-xs text-slate-300">正在读取日志…</div>
            : <pre className="max-h-[420px] overflow-auto rounded-lg bg-slate-950 p-4 font-mono text-xs leading-5 whitespace-pre-wrap text-slate-100">{logs.length > 0 ? logs.join('\n') : '暂无日志；任务可能仍在排队或记录已清理。'}</pre>}
        </div>
      </Panel>}
    </div>

    <Dialog open={confirmOpen} onOpenChange={(open) => { if (!open && !confirmBusy) setConfirmOpen(false) }}>
      <DialogContent>
        <DialogHeader><DialogTitle>删除任务记录</DialogTitle><DialogDescription>将删除“{task.name}”的历史记录，不会删除模型、镜像、备份文件或实例数据。</DialogDescription></DialogHeader>
        <DialogFooter>
          <button type="button" className="btn-secondary" disabled={confirmBusy} onClick={() => setConfirmOpen(false)}>取消</button>
          <button type="button" className="btn-danger" disabled={confirmBusy} onClick={() => void removeTask()}>{confirmBusy ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}{confirmBusy ? '处理中' : '删除记录'}</button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </>
}
