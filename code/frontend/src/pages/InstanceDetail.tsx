// 实例详情页：概览、应用健康、最近事件与生命周期操作。
// 数据来自实例列表轮询与详情专用健康/事件轮询，页面关闭即停止详情轮询。
import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { ExternalLink, Link2, LoaderCircle, Play, RefreshCw, ScrollText, Settings2, Square, Trash2 } from 'lucide-react'
import { getInstanceEvents, getInstanceHealth, type InstanceEventEntry, type InstanceHealthView } from '@/api'
import { EmptyState, PageHeader, Panel, PanelHeader, StatusBadge } from '@/components/common'
import { InstanceLogsDialog } from '@/components/InstanceLogsDialog'
import { ModelLinksDialog } from '@/components/ModelLinksDialog'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { documentTitle, pagePaths } from '@/lib/routes'
import { usePolling } from '@/hooks/use-polling'
import { useAppData } from '@/state/app-data'

export function InstanceDetail({ name }: { name: string }) {
  const { displayInstances, realInstances, realModels, gpuNames, busyIds, instanceAction, deleteInstance } = useAppData()
  const navigate = useNavigate()
  const detail = displayInstances.find((item) => item.name === name)
  const [health, setHealth] = useState<InstanceHealthView | null>(null)
  const [events, setEvents] = useState<InstanceEventEntry[]>([])
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [confirmName, setConfirmName] = useState('')
  const [purgeMode, setPurgeMode] = useState(false)
  const [logsOpen, setLogsOpen] = useState(false)
  const [linksOpen, setLinksOpen] = useState(false)
  const loading = realInstances === null

  useEffect(() => {
    document.title = documentTitle(undefined, detail ? detail.name : '实例详情')
  }, [detail])

  // 详情健康 5 秒、事件 15 秒；路由离开后自动停止。
  usePolling(async (signal) => {
    if (!detail) return
    try {
      const result = await getInstanceHealth(name, signal)
      setHealth(result.health)
    } catch {
      // 健康接口失败保持上次状态
    }
  }, { enabled: Boolean(detail), interval: 5000 })

  usePolling(async (signal) => {
    if (!detail) return
    try {
      const result = await getInstanceEvents(name, 20, signal)
      setEvents(result.events)
    } catch {
      // 事件读取失败保持上次结果
    }
  }, { enabled: Boolean(detail), interval: 15000 })

  function confirmDelete() {
    if (!detail) return
    deleteInstance(detail.id, purgeMode)
    setDeleteOpen(false)
    setPurgeMode(false)
    navigate(pagePaths.instances)
  }

  if (!detail) {
    return <>
      <PageHeader eyebrow="COMFYUI" title={loading ? '正在读取实例…' : '实例不存在'} description={loading ? '等待实例列表返回。' : `未找到名为 ${name} 的实例，可能已被删除或重命名。`} />
      <Panel><EmptyState title={loading ? '加载中' : '实例不存在'} description={loading ? '请稍候，列表返回后会自动展示详情。' : '返回实例列表确认当前可用实例。'} />
        <div className="flex justify-center gap-2 pb-8">
          <Link to={pagePaths.instances} className="btn-secondary"><Settings2 className="size-4" />返回实例列表</Link>
        </div>
      </Panel>
    </>
  }

  const busy = busyIds.includes(detail.id)
  const appState = health?.app ?? (detail.status !== 'running' ? 'not_running' : detail.comfy?.reachable === undefined ? 'starting' : detail.comfy.reachable ? 'ready' : 'degraded')
  const appMeta: Record<string, { label: string; className: string }> = {
    ready: { label: '就绪', className: 'border-emerald-200 bg-emerald-50 text-emerald-700' },
    starting: { label: '启动中', className: 'border-amber-200 bg-amber-50 text-amber-700' },
    degraded: { label: '降级', className: 'border-orange-200 bg-orange-50 text-orange-700' },
    failed: { label: '失败', className: 'border-rose-200 bg-rose-50 text-rose-700' },
    not_running: { label: '未运行', className: 'border-slate-200 bg-slate-50 text-slate-500' },
    unknown: { label: '未知', className: 'border-slate-200 bg-slate-50 text-slate-500' },
  }
  const appItem = appMeta[appState] ?? appMeta.unknown

  const facts: [string, string][] = [
    ['GPU', detail.gpu >= 0 ? `GPU ${detail.gpu} · ${gpuNames[detail.gpu] ?? 'GPU'}` : '不分配 GPU'],
    ['访问端口', String(detail.port)],
    ['ComfyUI 队列', detail.status !== 'running' ? '实例未运行' : detail.comfy ? (detail.comfy.reachable ? `运行 ${detail.comfy.queueRunning} · 排队 ${detail.comfy.queuePending}${detail.comfy.comfyuiVersion ? ` · ComfyUI ${detail.comfy.comfyuiVersion}` : ''}` : '服务启动中…') : '探测中…'],
    ['创建时间', detail.createdAt],
    ['模型仓库挂载', detail.modelsTarget ? `共享仓库 → ${detail.modelsTarget}（只读）` : '共享仓库（默认挂载）'],
    ['导出资产路径', detail.outputsTarget ? `共享 outputs → ${detail.outputsTarget}` : '共享 outputs 目录'],
    ['工作流挂载', detail.workflowsTarget ? `共享 workflows → ${detail.workflowsTarget}` : '共享 workflows 目录'],
    ['容器名称', `flowpilot-comfyui-${detail.name}`],
  ]

  return <>
    <PageHeader
      eyebrow="COMFYUI INSTANCE"
      title={detail.name}
      description={`${detail.template} · ${detail.version} · ${detail.uptime}`}
      action={<div className="flex flex-wrap gap-2">
        <button type="button" className="btn-secondary" onClick={() => navigate(pagePaths.instances)}>返回列表</button>
        {detail.status === 'running'
          ? <><button type="button" className="btn-secondary" disabled={busy} onClick={() => instanceAction(detail.id, 'stop')}><Square className="size-4" />停止</button>
            <button type="button" className="btn-secondary" disabled={busy} onClick={() => instanceAction(detail.id, 'restart')}><RefreshCw className={`size-4 ${busy ? 'animate-spin' : ''}`} />重启</button></>
          : <button type="button" className="btn-primary" disabled={busy} onClick={() => instanceAction(detail.id, 'start')}><Play className="size-4" />启动</button>}
      </div>}
    />

    <div className="grid gap-5 lg:grid-cols-3">
      <Panel className="lg:col-span-2">
        <PanelHeader title="运行概览" description={`最近检查 ${health?.lastCheckAt ? new Date(health.lastCheckAt).toLocaleTimeString('zh-CN', { hour12: false }) : '—'}`} />
        <div className="grid gap-px bg-slate-200 sm:grid-cols-2">
          <div className="bg-white p-5">
            <div className="text-xs text-slate-500">容器状态</div>
            <div className="mt-2"><StatusBadge status={detail.status} /></div>
            <div className="mt-2 text-xs text-slate-500">{detail.uptime}</div>
          </div>
          <div className="bg-white p-5" data-testid="detail-app-health">
            <div className="text-xs text-slate-500">应用健康</div>
            <div className="mt-2"><span className={`inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-medium ${appItem.className}`}>{appItem.label}</span></div>
            <div className="mt-2 text-xs text-slate-500">{health?.lastCheckAt ? `检查于 ${new Date(health.lastCheckAt).toLocaleTimeString('zh-CN', { hour12: false })}` : '等待首次探测'}</div>
          </div>
        </div>
        <dl className="grid gap-4 border-t border-slate-200 p-5 text-sm sm:grid-cols-2">
          {facts.map(([term, value]) => <div key={term} className={['模型仓库挂载', '导出资产路径', '工作流挂载', '容器名称'].includes(term) ? 'sm:col-span-2' : ''}>
            <dt className="text-xs text-slate-500">{term}</dt>
            <dd className="mt-1 break-all font-medium text-slate-800">{value}</dd>
          </div>)}
        </dl>
        <div className="flex flex-wrap items-center gap-2 border-t border-slate-200 p-5">
          <button type="button" className="btn-secondary" disabled={detail.status !== 'running'} onClick={() => window.open(`http://${window.location.hostname}:${detail.port}`, '_blank', 'noopener')}><ExternalLink className="size-4" />打开 ComfyUI</button>
          <button type="button" className="btn-quiet" onClick={() => setLogsOpen(true)}><ScrollText className="size-4" />实例日志</button>
          <button type="button" className="btn-quiet" disabled={realInstances === null} onClick={() => setLinksOpen(true)}><Link2 className="size-4" />模型关联</button>
          <button type="button" className="btn-quiet ml-auto text-rose-600 hover:bg-rose-50" onClick={() => { setDeleteOpen(true); setConfirmName(''); setPurgeMode(false) }}><Trash2 className="size-4" />删除实例</button>
        </div>
      </Panel>

      <Panel>
        <PanelHeader title="最近事件" description="实例启停、健康探测与配置变更记录" />
        <div className="max-h-[520px] overflow-y-auto p-4" data-testid="detail-events">
          {events.length === 0
            ? <p className="py-8 text-center text-sm text-slate-500">暂无事件记录。</p>
            : <ol className="space-y-2">
              {events.slice(0, 20).map((event) => <li key={event.id} className="flex flex-wrap items-center gap-2 text-xs">
                <span className="font-mono text-slate-400">{new Date(event.ts).toLocaleTimeString('zh-CN', { hour12: false })}</span>
                <span className="rounded bg-slate-100 px-1.5 py-0.5 font-medium text-slate-600">{event.kind}</span>
                {event.detail && <span className="min-w-0 flex-1 truncate text-slate-500" title={event.detail}>{event.detail}</span>}
              </li>)}
            </ol>}
        </div>
      </Panel>
    </div>

    <Dialog open={deleteOpen} onOpenChange={(open) => { if (!open) { setDeleteOpen(false); setPurgeMode(false) } }}>
      <DialogContent>
        <DialogHeader><DialogTitle>删除实例</DialogTitle><DialogDescription>共享模型目录始终保留；实例数据的去留由下方选项决定。</DialogDescription></DialogHeader>
        <div className="grid gap-2">
          <button type="button" onClick={() => setPurgeMode(false)} className={`rounded-lg border p-3 text-left text-sm transition-colors ${!purgeMode ? 'border-teal-600 bg-teal-50' : 'border-slate-200 hover:border-slate-300'}`} data-testid="delete-keep-data">
            <div className="font-medium text-slate-900">删除实例，保留数据（推荐）</div>
            <div className="mt-0.5 text-slate-500">删除容器与模型链接；输出、工作流等实例数据保留，重建同名实例时自动沿用。</div>
          </button>
          <button type="button" onClick={() => setPurgeMode(true)} className={`rounded-lg border p-3 text-left text-sm transition-colors ${purgeMode ? 'border-rose-500 bg-rose-50' : 'border-slate-200 hover:border-slate-300'}`} data-testid="delete-purge-data">
            <div className="font-medium text-slate-900">彻底删除实例及数据</div>
            <div className="mt-0.5 text-slate-500">删除容器与全部实例数据（输出、工作流、用户数据），不可恢复。</div>
          </button>
        </div>
        <div className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">请输入实例名称 <strong>{detail.name}</strong> 以确认{purgeMode ? '彻底删除' : '删除'}。</div>
        <label className="field-label">实例名称<input className="field-input" value={confirmName} onChange={(event) => setConfirmName(event.target.value)} autoComplete="off" /></label>
        <DialogFooter>
          <button type="button" className="btn-secondary" onClick={() => { setDeleteOpen(false); setPurgeMode(false) }}>取消</button>
          <button type="button" className="btn-danger" disabled={confirmName !== detail.name} onClick={confirmDelete}><Trash2 className="size-4" />{purgeMode ? '彻底删除' : '确认删除'}</button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <InstanceLogsDialog key={`logs-${detail.name}`} instance={logsOpen ? { name: detail.name, status: detail.status } : null} onClose={() => setLogsOpen(false)} />
    <ModelLinksDialog key={`links-${detail.name}`} instance={linksOpen ? { name: detail.name, status: detail.status } : null} models={realModels} onClose={() => setLinksOpen(false)} />
    {busy && <div className="fixed bottom-6 left-1/2 z-30 flex -translate-x-1/2 items-center gap-2 rounded-full bg-slate-900 px-4 py-2 text-xs text-white shadow-lg"><LoaderCircle className="size-3.5 animate-spin" />正在执行实例操作…</div>}
  </>
}
