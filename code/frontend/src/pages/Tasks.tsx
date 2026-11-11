import { useEffect, useMemo, useState } from 'react'
import { Ban, CheckCircle2, ChevronLeft, ChevronRight, FileText, Loader2, Pause, Play, RefreshCw, RotateCcw, Search, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { cancelImageTask, cleanupFinishedTasks, deleteUnifiedTask, downloadActionApi, getImageTaskLogs, getUnifiedTasks, type UnifiedTaskView } from '@/api'
import { EmptyState, PageHeader, Panel, ProgressBar } from '@/components/common'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

const PAGE_SIZE = 20

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

interface ConfirmRequest {
  title: string
  description: string
  confirmLabel: string
  action: () => Promise<void>
}

function isActive(status: string): boolean {
  return ['queued', 'running', 'paused'].includes(status)
}

function isDownloadType(type: string): boolean {
  return type === 'download' || type === 'import'
}

function formatTime(value?: string): string {
  if (!value) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString('zh-CN', { hour12: false })
}

function StatusBadge({ status }: { status: string }) {
  const meta = statusMeta[status] ?? { label: status, className: 'border-slate-200 bg-slate-50 text-slate-500' }
  return <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${meta.className}`}>{meta.label}</span>
}

function TaskProgress({ task }: { task: UnifiedTaskView }) {
  if (task.status === 'succeeded') {
    return <span className="inline-flex items-center gap-1.5 text-xs font-medium text-emerald-700"><CheckCircle2 className="size-4" />已完成</span>
  }
  if (task.status === 'partial') {
    return <span className="text-xs font-medium text-orange-700">完成，校验有告警</span>
  }
  if (task.status === 'failed' || task.status === 'cancelled') {
    return <span className="text-xs text-slate-400">—</span>
  }
  if (task.status === 'queued') {
    return <span className="text-xs text-slate-500">等待执行</span>
  }
  if (task.progress < 0) {
    return <span className="text-xs text-slate-500">{task.status === 'paused' ? '已暂停' : '进度不可估计'}</span>
  }
  return <div className="flex w-full items-center gap-2"><ProgressBar value={task.progress} /><span className="w-10 shrink-0 text-right text-xs font-medium text-slate-600">{task.progress}%</span></div>
}

function TaskActions({ task, onAction, onDelete, onDetail }: {
  task: UnifiedTaskView
  onAction: (task: UnifiedTaskView, action: 'pause' | 'resume' | 'cancel' | 'retry') => void
  onDelete: (task: UnifiedTaskView) => void
  onDetail: (task: UnifiedTaskView) => void
}) {
  const active = isActive(task.status)
  const download = isDownloadType(task.type)
  return <div className="flex flex-wrap items-center justify-end gap-0.5">
    <button type="button" className="btn-quiet px-2 text-xs" onClick={() => onDetail(task)}><FileText className="size-3.5" />详情</button>
    {download && task.status === 'running' && <button type="button" className="btn-quiet px-2 text-xs" onClick={() => onAction(task, 'pause')}><Pause className="size-3.5" />暂停</button>}
    {download && task.status === 'paused' && <button type="button" className="btn-quiet px-2 text-xs" onClick={() => onAction(task, 'resume')}><Play className="size-3.5" />继续</button>}
    {download && ['failed', 'cancelled'].includes(task.status) && <button type="button" className="btn-quiet px-2 text-xs" onClick={() => onAction(task, 'retry')}><RotateCcw className="size-3.5" />重试</button>}
    {active && <button type="button" className="btn-quiet px-2 text-xs text-rose-600 hover:bg-rose-50" onClick={() => onAction(task, 'cancel')}><Ban className="size-3.5" />{download ? '取消' : '停止'}</button>}
    {!active && <button type="button" className="btn-quiet px-2 text-xs text-rose-600 hover:bg-rose-50" onClick={() => onDelete(task)}><Trash2 className="size-3.5" />删除记录</button>}
  </div>
}

// Tasks 统一任务中心：活跃任务优先，终态任务保留可审计记录，可按类型/状态/关键词筛选。
export function Tasks() {
  const [tasks, setTasks] = useState<UnifiedTaskView[] | null>(null)
  const [total, setTotal] = useState(0)
  const [status, setStatus] = useState('all')
  const [type, setType] = useState('all')
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(0)
  const [loading, setLoading] = useState(true)
  const [refreshKey, setRefreshKey] = useState(0)
  const [detailTask, setDetailTask] = useState<UnifiedTaskView | null>(null)
  const [detailLogs, setDetailLogs] = useState<string[]>([])
  const [logsLoading, setLogsLoading] = useState(false)
  const [confirmRequest, setConfirmRequest] = useState<ConfirmRequest | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)

  // 搜索防抖，避免每输入一个字都请求后端。
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setQuery(search.trim())
      setPage(0)
    }, 300)
    return () => window.clearTimeout(timer)
  }, [search])

  useEffect(() => {
    const controller = new AbortController()
    let cancelled = false
    async function load(initial: boolean) {
      if (initial) setLoading(true)
      try {
        const result = await getUnifiedTasks({
          type: type === 'all' ? undefined : type,
          status: status === 'all' ? undefined : status,
          q: query || undefined,
          limit: PAGE_SIZE,
          offset: page * PAGE_SIZE,
        })
        if (cancelled) return
        setTasks(result.tasks)
        setTotal(result.total)
      } catch {
        if (!cancelled && initial) setTasks(null)
      } finally {
        if (!cancelled && initial) setLoading(false)
      }
    }
    void load(true)
    const timer = window.setInterval(() => void load(false), 5000)
    return () => {
      cancelled = true
      controller.abort()
      window.clearInterval(timer)
    }
  }, [type, status, query, page, refreshKey])

  useEffect(() => {
    if (!detailTask || !detailTask.type.startsWith('image-')) {
      setDetailLogs([])
      return
    }
    let cancelled = false
    setLogsLoading(true)
    getImageTaskLogs(detailTask.id)
      .then((logs) => { if (!cancelled) setDetailLogs(logs) })
      .catch(() => { if (!cancelled) setDetailLogs([]) })
      .finally(() => { if (!cancelled) setLogsLoading(false) })
    return () => { cancelled = true }
  }, [detailTask])

  const activeCount = useMemo(() => (tasks ?? []).filter((task) => isActive(task.status)).length, [tasks])
  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE))

  function refresh() {
    setRefreshKey((value) => value + 1)
  }

  function resetFilters() {
    setType('all')
    setStatus('all')
    setSearch('')
    setPage(0)
  }

  async function runAction(task: UnifiedTaskView, action: 'pause' | 'resume' | 'cancel' | 'retry') {
    try {
      if (task.id.startsWith('dl-')) {
        await downloadActionApi(task.id.slice(3), action)
      } else if (task.id.startsWith('img-') && action === 'cancel') {
        await cancelImageTask(task.id.slice(4))
      } else {
        throw new Error('该任务类型暂不支持此操作')
      }
      toast.success(action === 'cancel' ? '任务已停止' : action === 'retry' ? '任务已重试' : action === 'pause' ? '任务已暂停' : '任务已继续', { description: task.name })
      refresh()
    } catch (error) {
      toast.error('任务操作失败', { description: error instanceof Error ? error.message : '请稍后重试' })
      refresh()
    }
  }

  function requestDelete(task: UnifiedTaskView) {
    setConfirmRequest({
      title: '删除任务记录',
      description: `将删除“${task.name}”的历史记录，不会删除模型、镜像、备份文件或实例数据。`,
      confirmLabel: '删除记录',
      action: async () => {
        try {
          await deleteUnifiedTask(task.id)
          toast.success('任务记录已删除', { description: task.name })
          if (detailTask?.id === task.id) setDetailTask(null)
          refresh()
        } catch (error) {
          toast.error('删除任务记录失败', { description: error instanceof Error ? error.message : '请稍后重试' })
        }
      },
    })
  }

  function requestCleanup() {
    setConfirmRequest({
      title: '清理已结束任务记录',
      description: `将删除当前筛选类型下全部已结束任务记录（${type === 'all' ? '全部类型' : typeMeta[type] ?? type}），不会删除任何业务产物；排队、运行和暂停任务不会被删除。`,
      confirmLabel: '清理记录',
      action: async () => {
        try {
          const result = await cleanupFinishedTasks(type === 'all' ? undefined : type)
          toast.success('任务记录已清理', { description: `删除 ${result.deleted} 条${result.failed > 0 ? `，${result.failed} 条未能删除` : ''}` })
          setPage(0)
          refresh()
        } catch (error) {
          toast.error('清理任务记录失败', { description: error instanceof Error ? error.message : '请稍后重试' })
        }
      },
    })
  }

  async function executeConfirm() {
    if (!confirmRequest) return
    setConfirmBusy(true)
    try {
      await confirmRequest.action()
      setConfirmRequest(null)
    } finally {
      setConfirmBusy(false)
    }
  }

  function openDetail(task: UnifiedTaskView) {
    setDetailTask(task)
  }

  return <>
    <PageHeader eyebrow="TASK CENTER" title="任务中心" description="统一跟踪模型下载、导入、镜像构建/拉取、数据迁移与平台备份；活跃任务优先展示，历史记录可按需清理。" />
    <Panel>
      <div className="flex flex-col gap-3 border-b border-slate-200 p-4 xl:flex-row xl:items-center xl:justify-between">
        <div className="flex items-center gap-3 text-sm text-slate-600">
          <span>{loading ? '加载中…' : `共 ${total} 个任务`}</span>
          {activeCount > 0 && <span className="inline-flex items-center gap-1 rounded-full bg-teal-50 px-2 py-0.5 text-xs font-medium text-teal-700"><Loader2 className="size-3 animate-spin" />{activeCount} 个进行中</span>}
          <button type="button" className="btn-quiet px-1.5 text-xs" title="刷新任务" aria-label="刷新任务" onClick={refresh}><RefreshCw className={`size-3.5 ${loading ? 'animate-spin' : ''}`} /></button>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <label className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-slate-400" />
            <input className="field-input h-9 w-52 pl-8 text-sm" aria-label="搜索任务" placeholder="搜索任务或关联对象" value={search} onChange={(event) => setSearch(event.target.value)} />
          </label>
          <select className="field-input h-9 w-36 text-sm" aria-label="按任务类型筛选" value={type} onChange={(event) => { setType(event.target.value); setPage(0) }} data-testid="task-type-filter">
            <option value="all">全部类型</option>
            {Object.entries(typeMeta).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
          </select>
          <select className="field-input h-9 w-32 text-sm" aria-label="按任务状态筛选" value={status} onChange={(event) => { setStatus(event.target.value); setPage(0) }}>
            <option value="all">全部状态</option>
            {Object.entries(statusMeta).map(([key, item]) => <option key={key} value={key}>{item.label}</option>)}
          </select>
          {(search || type !== 'all' || status !== 'all') && <button type="button" className="btn-quiet h-9 px-2 text-xs" onClick={resetFilters}>重置筛选</button>}
          <button type="button" className="btn-secondary h-9 px-3 text-xs" onClick={requestCleanup}><Trash2 className="size-3.5" />清理已结束</button>
        </div>
      </div>

      <div data-testid="unified-task-list">
        {tasks !== null && tasks.length > 0 && <>
          <div className="hidden overflow-x-auto lg:block">
            <table className="w-full min-w-[920px] table-fixed text-sm">
              <colgroup><col /><col className="w-[190px]" /><col className="w-[150px]" /><col className="w-[232px]" /></colgroup>
              <thead><tr className="border-b border-slate-200 bg-slate-50/70 text-left text-xs font-medium text-slate-500">
                <th className="px-4 py-2.5">任务</th><th className="px-3 py-2.5">进度</th><th className="px-3 py-2.5">创建时间</th><th className="px-4 py-2.5 text-right">操作</th>
              </tr></thead>
              <tbody className="divide-y divide-slate-100">
                {tasks.map((task) => <tr key={task.id} className="align-middle hover:bg-slate-50/70">
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap items-center gap-2"><span className="max-w-[380px] truncate font-medium text-slate-900" title={task.name}>{task.name}</span><StatusBadge status={task.status} /><span className="rounded bg-slate-100 px-1.5 py-0.5 text-[11px] font-medium text-slate-600" data-testid={`task-type-badge-${task.type}`}>{typeMeta[task.type] ?? task.type}</span></div>
                    <div className="mt-1 flex min-w-0 items-center gap-1.5 text-xs text-slate-500">{task.objectRef && <><span className="max-w-[280px] truncate" title={task.objectRef}>{task.objectRef}</span><span>·</span></>}<span className="truncate" title={task.message}>{task.error || task.message || '—'}</span></div>
                  </td>
                  <td className="px-3 py-3"><TaskProgress task={task} /></td>
                  <td className="px-3 py-3 text-xs text-slate-500">{formatTime(task.createdAt)}</td>
                  <td className="px-4 py-3"><TaskActions task={task} onAction={(item, action) => void runAction(item, action)} onDelete={requestDelete} onDetail={openDetail} /></td>
                </tr>)}
              </tbody>
            </table>
          </div>
          <div className="divide-y divide-slate-100 lg:hidden">
            {tasks.map((task) => <div key={task.id} className="p-4">
              <div className="flex flex-wrap items-center gap-2"><span className="min-w-0 flex-1 truncate font-medium text-slate-900">{task.name}</span><StatusBadge status={task.status} /><span className="rounded bg-slate-100 px-1.5 py-0.5 text-[11px] font-medium text-slate-600" data-testid={`task-type-badge-${task.type}`}>{typeMeta[task.type] ?? task.type}</span></div>
              <div className="mt-1.5 text-xs text-slate-500">{task.objectRef && <>对象 {task.objectRef} · </>}{formatTime(task.createdAt)}</div>
              <p className={`mt-2 text-sm leading-5 ${task.error ? 'text-rose-700' : 'text-slate-600'}`}>{task.error || task.message || '—'}</p>
              <div className="mt-2"><TaskProgress task={task} /></div>
              <div className="mt-2 border-t border-slate-100 pt-2"><TaskActions task={task} onAction={(item, action) => void runAction(item, action)} onDelete={requestDelete} onDetail={openDetail} /></div>
            </div>)}
          </div>
        </>}
        {!loading && tasks !== null && tasks.length === 0 && <EmptyState title={query || type !== 'all' || status !== 'all' ? '当前筛选下没有任务' : '暂无任务记录'} description={query || type !== 'all' || status !== 'all' ? '调整关键词、类型或状态筛选。' : '模型下载、镜像构建、数据迁移和备份任务会在这里留下记录。'} />}
        {tasks === null && !loading && <EmptyState title="任务接口不可用" description="请确认管理服务已连接。" />}
      </div>

      {total > PAGE_SIZE && <div className="flex items-center justify-between border-t border-slate-200 px-4 py-3 text-xs text-slate-500">
        <span>第 {page + 1} / {pageCount} 页 · 每页 {PAGE_SIZE} 条</span>
        <div className="flex gap-1">
          <button type="button" className="btn-quiet px-2 text-xs" disabled={page === 0} onClick={() => setPage((value) => Math.max(0, value - 1))}><ChevronLeft className="size-3.5" />上一页</button>
          <button type="button" className="btn-quiet px-2 text-xs" disabled={page + 1 >= pageCount} onClick={() => setPage((value) => value + 1)}>下一页<ChevronRight className="size-3.5" /></button>
        </div>
      </div>}
    </Panel>

    <Dialog open={Boolean(detailTask)} onOpenChange={(open) => !open && setDetailTask(null)}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-3xl">
        {detailTask && <>
          <DialogHeader>
            <DialogTitle>{detailTask.name}</DialogTitle>
            <DialogDescription>{typeMeta[detailTask.type] ?? detailTask.type} · {detailTask.id}</DialogDescription>
          </DialogHeader>
          <dl className="grid gap-3 rounded-lg border border-slate-200 p-4 text-sm">
            {[
              ['当前状态', statusMeta[detailTask.status]?.label ?? detailTask.status],
              ['关联对象', detailTask.objectRef || '—'],
              ['进度', detailTask.progress >= 0 ? `${detailTask.progress}%` : '不可估计'],
              ['创建时间', formatTime(detailTask.createdAt)],
              ['开始时间', formatTime(detailTask.startedAt)],
              ['结束时间', formatTime(detailTask.finishedAt)],
              ['最新信息', detailTask.message || '—'],
            ].map(([term, value]) => <div key={term} className="grid grid-cols-[88px_1fr] gap-3"><dt className="text-slate-500">{term}</dt><dd className="break-all font-medium text-slate-800">{value}</dd></div>)}
          </dl>
          {detailTask.error && <div className="mt-3 rounded-lg border border-rose-200 bg-rose-50 p-3 text-sm leading-6 text-rose-800"><strong className="mr-1">失败原因</strong>{detailTask.error}</div>}
          {detailTask.type.startsWith('image-') && <div className="mt-4">
            <div className="mb-2 text-sm font-medium text-slate-700">运行日志</div>
            {logsLoading ? <div className="rounded-lg bg-slate-950 p-4 text-xs text-slate-300">正在读取日志…</div> : <pre className="max-h-72 overflow-auto rounded-lg bg-slate-950 p-4 font-mono text-xs leading-5 whitespace-pre-wrap text-slate-100">{detailLogs.length > 0 ? detailLogs.join('\n') : '暂无日志；任务可能仍在排队或记录已清理。'}</pre>}
          </div>}
          <DialogFooter><button type="button" className="btn-primary" onClick={() => setDetailTask(null)}>关闭</button></DialogFooter>
        </>}
      </DialogContent>
    </Dialog>

    <Dialog open={Boolean(confirmRequest)} onOpenChange={(open) => { if (!open && !confirmBusy) setConfirmRequest(null) }}>
      <DialogContent>
        {confirmRequest && <>
          <DialogHeader><DialogTitle>{confirmRequest.title}</DialogTitle><DialogDescription>{confirmRequest.description}</DialogDescription></DialogHeader>
          <DialogFooter>
            <button type="button" className="btn-secondary" disabled={confirmBusy} onClick={() => setConfirmRequest(null)}>取消</button>
            <button type="button" className="btn-danger" disabled={confirmBusy} onClick={() => void executeConfirm()}>{confirmBusy ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}{confirmBusy ? '处理中' : confirmRequest.confirmLabel}</button>
          </DialogFooter>
        </>}
      </DialogContent>
    </Dialog>
  </>
}
