import { useEffect, useMemo, useState } from 'react'
import { ExternalLink, Link2, Play, Plus, RefreshCw, ScrollText, Search, Settings2, Square, Trash2 } from 'lucide-react'
import { getInstanceEvents, getInstanceHealth, type InstanceEventEntry, type InstanceHealthView, type ModelReport } from '@/api'
import { EmptyState, PageHeader, Panel, StatusBadge } from '@/components/common'
import { InstanceLogsDialog } from '@/components/InstanceLogsDialog'
import { ModelLinksDialog } from '@/components/ModelLinksDialog'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { Instance } from '@/types'

interface Props {
  instances: Instance[]
  gpuNames: string[]
  realMode: boolean
  busyIds: string[]
  /**
   * 首页跳转带来的目标实例名：作为本页的「初始定位」直接打开详情并高亮。
   * 父级以 token 拼进 key，每次带定位跳转都会重新挂载本组件，初始定位因此自然生效。
   */
  initialFocus?: string
  /** 共享仓库当前扫描结果（模型关联弹窗的候选来源）。 */
  models?: ModelReport | null
  onCreate: () => void
  onAction: (id: string, action: 'start' | 'stop' | 'restart') => void
  onDelete: (id: string, purge?: boolean) => void
}

type InstanceTab = 'instances'

export function Instances({ instances, gpuNames, realMode, busyIds, initialFocus, models, onCreate, onAction, onDelete }: Props) {
  // 初始定位（仅首次渲染生效）：命中则直接打开详情弹窗并高亮该行。
  const initialId = initialFocus ? instances.find((item) => item.name === initialFocus)?.id ?? null : null
  const [tab, setTab] = useState<InstanceTab>('instances')
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState('all')
  const [detailId, setDetailId] = useState<string | null>(initialId)
  // 应用健康与事件（0.0.9.8）：详情弹窗打开时拉取，5 秒轮询健康。
  const [detailHealth, setDetailHealth] = useState<InstanceHealthView | null>(null)
  const [detailEvents, setDetailEvents] = useState<InstanceEventEntry[]>([])
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [confirmName, setConfirmName] = useState('')
  const [purgeMode, setPurgeMode] = useState(false)
  const [logsInstance, setLogsInstance] = useState<{ name: string; status: string } | null>(null)
  const [linksInstance, setLinksInstance] = useState<{ name: string; status: string } | null>(null)
  const [highlightId, setHighlightId] = useState<string | null>(initialId)
  const filtered = useMemo(() => instances.filter((item) => item.name.toLowerCase().includes(query.toLowerCase()) && (status === 'all' || item.status === status)), [instances, query, status])
  const detail = instances.find((item) => item.id === detailId)
  const detailName = detail?.name
  const detailStatus = detail?.status
  useEffect(() => {
    const name: string = detailName ?? ''
    if (!name) return
    let cancelled = false
    const loadAll = async (targetName: string): Promise<void> => {
      try {
        const healthResult = await getInstanceHealth(targetName)
        if (!cancelled) setDetailHealth(healthResult.health)
      } catch { /* 健康接口失败保持上次状态 */ }
      try {
        const eventResult = await getInstanceEvents(targetName, 20)
        if (!cancelled) setDetailEvents(eventResult.events)
      } catch { /* 忽略 */ }
    }
    void loadAll(name)
    const timer = window.setInterval(() => void loadAll(name), 5000)
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [detailName, detailStatus])
  const deleting = instances.find((item) => item.id === deleteId)

  // 跳转高亮 3 秒后自动消退；定时器回调属异步更新，不产生级联渲染。
  useEffect(() => {
    if (!highlightId) return
    const timer = window.setTimeout(() => setHighlightId(null), 3000)
    return () => window.clearTimeout(timer)
  }, [highlightId])

  function closeDelete() {
    setDeleteId(null)
    setConfirmName('')
  }

  const tabs: { key: InstanceTab; label: string }[] = [{ key: 'instances', label: '实例列表' }]

  return <>
    <PageHeader eyebrow="COMFYUI" title="实例管理" description="创建独立 ComfyUI 容器，统一管理本机镜像、手动构建、在线拉取与实例生命周期。" action={tab === 'instances' ? <button type="button" className="btn-primary" onClick={onCreate}><Plus className="size-4" />新建实例</button> : undefined} />
    <div className="mb-5 flex gap-1 overflow-x-auto rounded-lg border border-slate-200 bg-white p-1" role="tablist" aria-label="实例管理子菜单">{tabs.map((item) => <button type="button" role="tab" aria-selected={tab === item.key} key={item.key} className={`min-h-11 flex-1 cursor-pointer rounded-md px-4 text-sm font-medium whitespace-nowrap transition-colors ${tab === item.key ? 'bg-teal-700 text-white' : 'text-slate-600 hover:bg-slate-100'}`} onClick={() => setTab(item.key)}>{item.label}</button>)}</div>
    {tab === 'instances' && <Panel>
      <div className="flex flex-col gap-3 border-b border-slate-200 p-4 sm:flex-row sm:items-center">
        <label className="relative flex-1"><span className="sr-only">搜索实例</span><Search className="absolute top-3.5 left-3 size-4 text-slate-400" /><input className="field-input pl-9" placeholder="搜索实例名称" value={query} onChange={(event) => setQuery(event.target.value)} /></label>
        <select className="field-input sm:w-44" aria-label="按状态筛选" value={status} onChange={(event) => setStatus(event.target.value)}><option value="all">全部状态</option><option value="running">运行中</option><option value="stopped">已停止</option><option value="error">异常</option></select>
      </div>
      <div className="overflow-x-auto"><table className="data-table min-w-[1080px]"><thead><tr><th>实例名称</th><th>容器状态</th><th>应用健康</th><th>资源</th><th>访问端口</th><th>ComfyUI 队列</th><th>操作</th></tr></thead><tbody>
        {filtered.map((item) => { const busy = busyIds.includes(item.id); return <tr key={item.id} className={highlightId === item.id ? 'bg-teal-50 transition-colors' : undefined} data-testid={`instance-row-${item.name}`}>
          <td><button type="button" className="cursor-pointer text-left font-medium text-slate-900 hover:text-teal-700" onClick={() => setDetailId(item.id)}>{item.name}</button><div className="mt-1 text-xs text-slate-500">{item.template} · {item.version}</div></td>
          <td><StatusBadge status={item.status} /></td><td><AppHealthBadge status={item.status} reachable={item.comfy?.reachable} /></td><td>{item.gpu >= 0 ? `GPU ${item.gpu}` : '不分配'}<div className="mt-1 max-w-48 truncate text-xs text-slate-500" title={item.gpu >= 0 ? gpuNames[item.gpu] ?? 'GPU' : undefined}>{item.gpu >= 0 ? gpuNames[item.gpu] ?? 'GPU' : '仅 CPU 运行'}</div></td><td className="font-mono text-xs">{item.port}</td>
          <td>{item.status === 'running'
            ? item.comfy
              ? item.comfy.reachable
                ? <span data-testid={`comfy-queue-${item.name}`} className="text-sm text-slate-800">{item.comfy.queueRunning > 0 || item.comfy.queuePending > 0 ? <>运行 <strong className="text-teal-700">{item.comfy.queueRunning}</strong> · 排队 <strong>{item.comfy.queuePending}</strong></> : <span className="text-slate-500">空闲</span>}</span>
                : <span className="text-xs text-slate-400">服务启动中…</span>
              : <span className="text-xs text-slate-400">探测中…</span>
            : <span className="text-xs text-slate-400">—</span>}</td>
          <td><div className="flex items-center gap-1">
            {item.status === 'running' ? <><button type="button" className="icon-button" aria-label={`停止 ${item.name}`} title="停止" disabled={busy} onClick={() => onAction(item.id, 'stop')}><Square className="size-4" /></button><button type="button" className="icon-button" aria-label={`重启 ${item.name}`} title="重启" disabled={busy} onClick={() => onAction(item.id, 'restart')}><RefreshCw className={`size-4 ${busy ? 'animate-spin' : ''}`} /></button></> : <button type="button" className="icon-button" aria-label={`启动 ${item.name}`} title="启动" disabled={busy} onClick={() => onAction(item.id, 'start')}><Play className="size-4" /></button>}
            {realMode && item.status === 'running' && <button type="button" className="icon-button text-teal-700 hover:bg-teal-50" aria-label={`打开 ${item.name} ComfyUI`} title="打开 ComfyUI 应用" onClick={() => window.open(`http://${window.location.hostname}:${item.port}`, '_blank', 'noopener')}><ExternalLink className="size-4" /></button>}
            <button type="button" className="icon-button" aria-label={`查看 ${item.name} 日志`} title="实例日志（实时流）" onClick={() => setLogsInstance({ name: item.name, status: item.status })}><ScrollText className="size-4" /></button>
            <button type="button" className="icon-button" aria-label={`关联 ${item.name} 模型`} title="模型关联" disabled={!realMode} onClick={() => setLinksInstance({ name: item.name, status: item.status })}><Link2 className="size-4" /></button>
            <button type="button" className="icon-button" aria-label={`查看 ${item.name} 详情`} title="实例详情" onClick={() => setDetailId(item.id)}><Settings2 className="size-4" /></button>
            <span className="mx-0.5 h-5 w-px bg-slate-200" aria-hidden="true" />
            <button type="button" className="icon-button text-rose-600 hover:bg-rose-50" aria-label={`删除 ${item.name}`} title="删除" disabled={busy} onClick={() => setDeleteId(item.id)}><Trash2 className="size-4" /></button>
          </div></td>
        </tr>})}
      </tbody></table></div>
      {filtered.length === 0 && <EmptyState title={instances.length === 0 ? '暂无 ComfyUI 实例' : '没有匹配的实例'} description={instances.length === 0 ? '点击右上角「新建实例」开始部署。' : '请调整搜索词或状态筛选。'} />}
    </Panel>}

    <Dialog open={Boolean(detail)} onOpenChange={(open) => !open && setDetailId(null)}>
      <DialogContent className="sm:max-w-xl">{detail && <>
        <DialogHeader><DialogTitle>{detail.name}</DialogTitle><DialogDescription>{detail.template} · {detail.version}</DialogDescription></DialogHeader>
        <div className="rounded-lg bg-slate-100 px-4 py-3">
          <div className="flex items-center justify-between"><div className="flex items-center gap-2"><span className="text-xs text-slate-500">容器</span><StatusBadge status={detail.status} /></div><span className="text-xs text-slate-500">{detail.uptime}</span></div>
          <div className="mt-2 flex items-center justify-between" data-testid="detail-app-health"><span className="text-xs text-slate-500">应用</span><div className="flex items-center gap-2"><AppHealthBadge status={detail.status} app={detailHealth?.app} reachable={detail.comfy?.reachable} />{detailHealth?.lastCheckAt && <span className="text-xs text-slate-400">检查于 {new Date(detailHealth.lastCheckAt).toLocaleTimeString('zh-CN', { hour12: false })}</span>}</div></div>
        </div>
        {detailEvents.length > 0 && <div className="max-h-40 overflow-y-auto rounded-lg border border-slate-200 p-3" data-testid="detail-events">
          <div className="text-xs font-medium text-slate-500">最近事件</div>
          <ol className="mt-2 space-y-1.5">
            {detailEvents.slice(0, 8).map((event) => <li key={event.id} className="flex items-center gap-2 text-xs">
              <span className="font-mono text-slate-400">{new Date(event.ts).toLocaleTimeString('zh-CN', { hour12: false })}</span>
              <span className="rounded bg-slate-100 px-1.5 py-0.5 font-medium text-slate-600">{event.kind}</span>
              {event.detail && <span className="truncate text-slate-500" title={event.detail}>{event.detail}</span>}
            </li>)}
          </ol>
        </div>}
        <dl className="grid grid-cols-2 gap-4 text-sm">{[
          ['GPU', detail.gpu >= 0 ? `GPU ${detail.gpu} · ${gpuNames[detail.gpu] ?? 'GPU'}` : '不分配 GPU'],
          ['访问端口', String(detail.port)],
          ['ComfyUI 队列', detail.status !== 'running' ? '实例未运行' : detail.comfy ? (detail.comfy.reachable ? `运行 ${detail.comfy.queueRunning} · 排队 ${detail.comfy.queuePending}${detail.comfy.comfyuiVersion ? ` · ComfyUI ${detail.comfy.comfyuiVersion}` : ''}` : '服务启动中…') : '探测中…'],
          ['创建时间', detail.createdAt],
          ['模型仓库挂载', detail.modelsTarget ? `共享仓库 → ${detail.modelsTarget}（只读）` : '共享仓库（默认挂载）'],
          ['导出资产路径', detail.outputsTarget ? `共享 outputs → ${detail.outputsTarget}` : '共享 outputs 目录'],
          ['工作流挂载', detail.workflowsTarget ? `共享 workflows → ${detail.workflowsTarget}` : '共享 workflows 目录'],
          ['容器名称', `flowpilot-comfyui-${detail.name}`],
        ].map(([term, value]) => <div key={term} className={['模型仓库挂载', '导出资产路径', '工作流挂载', '容器名称'].includes(term) ? 'col-span-2' : ''}><dt className="text-xs text-slate-500">{term}</dt><dd className="mt-1 break-all font-medium text-slate-800">{value}</dd></div>)}</dl>
        <div className="flex min-h-12 items-center justify-between rounded-lg border border-slate-200 px-4"><span className="text-sm font-medium text-slate-700">开机自动启动（创建时指定，由容器重启策略执行）</span><span className={`rounded-full px-2.5 py-1 text-xs font-medium ${detail.autoStart ? 'bg-teal-50 text-teal-700' : 'bg-slate-100 text-slate-600'}`}>{detail.autoStart ? '已启用' : '未启用'}</span></div>
        <DialogFooter><button type="button" className="btn-secondary" disabled={detail.status !== 'running'} onClick={() => window.open(`http://${window.location.hostname}:${detail.port}`, '_blank', 'noopener')}><ExternalLink className="size-4" />打开 ComfyUI</button><button type="button" className="btn-primary" onClick={() => setDetailId(null)}>完成</button></DialogFooter>
      </>}</DialogContent>
    </Dialog>

    <Dialog open={Boolean(deleting)} onOpenChange={(open) => { if (!open) closeDelete(); setPurgeMode(false) }}>
      <DialogContent>{deleting && <>
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
        <div className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">请输入实例名称 <strong>{deleting.name}</strong> 以确认{purgeMode ? '彻底删除' : '删除'}。</div>
        <label className="field-label">实例名称<input className="field-input" value={confirmName} onChange={(event) => setConfirmName(event.target.value)} autoComplete="off" /></label>
        <DialogFooter><button type="button" className="btn-secondary" onClick={() => { closeDelete(); setPurgeMode(false) }}>取消</button><button type="button" className="btn-danger" disabled={confirmName !== deleting.name} onClick={() => { onDelete(deleting.id, purgeMode); closeDelete(); setPurgeMode(false) }}><Trash2 className="size-4" />{purgeMode ? '彻底删除' : '确认删除'}</button></DialogFooter>
      </>}</DialogContent>
    </Dialog>

    {/* 日志与关联弹窗按实例名重挂载：切换实例时内部状态（日志缓冲、勾选集）自然归零，无需 effect 内重置。 */}
    <InstanceLogsDialog key={`logs-${logsInstance?.name ?? 'idle'}`} instance={logsInstance} onClose={() => setLogsInstance(null)} />
    <ModelLinksDialog key={`links-${linksInstance?.name ?? 'idle'}`} instance={linksInstance} models={models ?? null} onClose={() => setLinksInstance(null)} />
  </>
}

// AppHealthBadge 应用层健康徽章（0.0.9.8）。
// app 为后端检查器权威状态；缺失时按容器状态与页面探测推导。
function AppHealthBadge({ status, app, reachable }: { status: string; app?: string; reachable?: boolean }) {
  const state = app ?? (status !== 'running' ? 'not_running' : reachable === undefined ? 'starting' : reachable ? 'ready' : 'degraded')
  const meta: Record<string, { label: string; className: string }> = {
    ready: { label: '就绪', className: 'border-emerald-200 bg-emerald-50 text-emerald-700' },
    starting: { label: '启动中', className: 'border-amber-200 bg-amber-50 text-amber-700' },
    degraded: { label: '降级', className: 'border-orange-200 bg-orange-50 text-orange-700' },
    failed: { label: '失败', className: 'border-rose-200 bg-rose-50 text-rose-700' },
    not_running: { label: '未运行', className: 'border-slate-200 bg-slate-50 text-slate-500' },
    unknown: { label: '未知', className: 'border-slate-200 bg-slate-50 text-slate-500' },
  }
  const item = meta[state] ?? meta.unknown
  return <span data-testid={`app-health-${state}`} className={`inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-medium ${item.className}`}>{item.label}</span>
}
