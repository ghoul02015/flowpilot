import { useMemo, useState } from 'react'
import { ExternalLink, Play, Plus, RefreshCw, Search, Settings2, Square, Trash2 } from 'lucide-react'
import { EmptyState, PageHeader, Panel, StatusBadge } from '@/components/common'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { Instance } from '@/types'

interface Props {
  instances: Instance[]
  gpuNames: string[]
  realMode: boolean
  onCreate: () => void
  onAction: (id: string, action: 'start' | 'stop' | 'restart') => void
  onDelete: (id: string) => void
}

export function Instances({ instances, gpuNames, realMode, onCreate, onAction, onDelete }: Props) {
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState('all')
  const [detailId, setDetailId] = useState<string | null>(null)
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [confirmName, setConfirmName] = useState('')
  const filtered = useMemo(() => instances.filter((item) => item.name.toLowerCase().includes(query.toLowerCase()) && (status === 'all' || item.status === status)), [instances, query, status])
  const detail = instances.find((item) => item.id === detailId)
  const deleting = instances.find((item) => item.id === deleteId)

  function closeDelete() {
    setDeleteId(null)
    setConfirmName('')
  }

  return <>
    <PageHeader eyebrow="COMFYUI" title="实例管理" description="创建独立 ComfyUI 容器，管理 GPU、端口、运行状态和持久化数据。" action={<button type="button" className="btn-primary" onClick={onCreate}><Plus className="size-4" />新建实例</button>} />
    <Panel>
      <div className="flex flex-col gap-3 border-b border-slate-200 p-4 sm:flex-row sm:items-center">
        <label className="relative flex-1"><span className="sr-only">搜索实例</span><Search className="absolute top-3.5 left-3 size-4 text-slate-400" /><input className="field-input pl-9" placeholder="搜索实例名称" value={query} onChange={(event) => setQuery(event.target.value)} /></label>
        <select className="field-input sm:w-44" aria-label="按状态筛选" value={status} onChange={(event) => setStatus(event.target.value)}><option value="all">全部状态</option><option value="running">运行中</option><option value="stopped">已停止</option><option value="error">异常</option></select>
      </div>
      <div className="overflow-x-auto"><table className="data-table min-w-[1120px]"><thead><tr><th>实例名称</th><th>状态</th><th>资源</th><th>访问端口</th><th>制图任务</th><th>操作</th></tr></thead><tbody>
        {filtered.map((item) => { const busy = ['creating', 'starting', 'stopping', 'restarting'].includes(item.status); return <tr key={item.id}>
          <td><button type="button" className="cursor-pointer text-left font-medium text-slate-900 hover:text-teal-700" onClick={() => setDetailId(item.id)}>{item.name}</button><div className="mt-1 text-xs text-slate-500">{item.template} · {item.version}</div></td>
          <td><StatusBadge status={item.status} /></td><td>{item.gpu >= 0 ? `GPU ${item.gpu}` : '不分配'}<div className="mt-1 text-xs text-slate-500">{item.gpu >= 0 ? gpuNames[item.gpu] ?? 'GPU' : '仅 CPU 运行'}</div></td><td className="font-mono text-xs">{item.port}</td>
          <td>{item.status === 'running'
            ? item.comfy
              ? item.comfy.reachable
                ? <span data-testid={`comfy-queue-${item.name}`} className="text-sm text-slate-800">{item.comfy.queueRunning > 0 || item.comfy.queuePending > 0 ? <>运行 <strong className="text-teal-700">{item.comfy.queueRunning}</strong> · 排队 <strong>{item.comfy.queuePending}</strong></> : <span className="text-slate-500">空闲</span>}</span>
                : <span className="text-xs text-slate-400">服务启动中…</span>
              : <span className="text-xs text-slate-400">探测中…</span>
            : <span className="text-xs text-slate-400">—</span>}</td>
          <td><div className="flex items-center gap-1">
            {item.status === 'running' ? <><button type="button" className="icon-button" aria-label={`停止 ${item.name}`} title="停止" disabled={busy} onClick={() => onAction(item.id, 'stop')}><Square className="size-4" /></button><button type="button" className="icon-button" aria-label={`重启 ${item.name}`} title="重启" disabled={busy} onClick={() => onAction(item.id, 'restart')}><RefreshCw className="size-4" /></button></> : <button type="button" className="icon-button" aria-label={`启动 ${item.name}`} title="启动" disabled={busy} onClick={() => onAction(item.id, 'start')}><Play className="size-4" /></button>}
            {realMode && item.status === 'running' && <button type="button" className="icon-button text-teal-700 hover:bg-teal-50" aria-label={`打开 ${item.name} ComfyUI`} title="打开 ComfyUI 应用" onClick={() => window.open(`http://${window.location.hostname}:${item.port}`, '_blank', 'noopener')}><ExternalLink className="size-4" /></button>}
            <button type="button" className="icon-button" aria-label={`查看 ${item.name} 详情`} title="实例详情" onClick={() => setDetailId(item.id)}><Settings2 className="size-4" /></button>
            <button type="button" className="icon-button text-rose-600 hover:bg-rose-50" aria-label={`删除 ${item.name}`} title="删除" disabled={busy} onClick={() => setDeleteId(item.id)}><Trash2 className="size-4" /></button>
          </div></td>
        </tr>})}
      </tbody></table></div>
      {filtered.length === 0 && <EmptyState title={instances.length === 0 ? '暂无 ComfyUI 实例' : '没有匹配的实例'} description={instances.length === 0 ? '点击右上角「新建实例」开始部署。' : '请调整搜索词或状态筛选。'} />}
    </Panel>

    <Dialog open={Boolean(detail)} onOpenChange={(open) => !open && setDetailId(null)}>
      <DialogContent className="sm:max-w-xl">{detail && <>
        <DialogHeader><DialogTitle>{detail.name}</DialogTitle><DialogDescription>{detail.template} · {detail.version}</DialogDescription></DialogHeader>
        <div className="flex items-center justify-between rounded-lg bg-slate-100 px-4 py-3"><StatusBadge status={detail.status} /><span className="text-xs text-slate-500">{detail.uptime}</span></div>
        <dl className="grid grid-cols-2 gap-4 text-sm">{[
          ['GPU', detail.gpu >= 0 ? `GPU ${detail.gpu} · ${gpuNames[detail.gpu] ?? 'GPU'}` : '不分配 GPU'],
          ['访问端口', String(detail.port)],
          ['制图任务', detail.status !== 'running' ? '实例未运行' : detail.comfy ? (detail.comfy.reachable ? `运行 ${detail.comfy.queueRunning} · 排队 ${detail.comfy.queuePending}${detail.comfy.comfyuiVersion ? ` · ComfyUI ${detail.comfy.comfyuiVersion}` : ''}` : '服务启动中…') : '探测中…'],
          ['创建时间', detail.createdAt],
          ['模型仓库挂载', detail.modelsTarget ? `共享仓库 → ${detail.modelsTarget}（只读）` : '共享仓库（默认挂载）'],
          ['导出资产路径', detail.outputsTarget ? `共享 outputs → ${detail.outputsTarget}` : '共享 outputs 目录'],
          ['容器名称', `flowpilot-comfyui-${detail.name}`],
        ].map(([term, value]) => <div key={term} className={term.includes('目录') || term.includes('容器') ? 'col-span-2' : ''}><dt className="text-xs text-slate-500">{term}</dt><dd className="mt-1 break-all font-medium text-slate-800">{value}</dd></div>)}</dl>
        <label className="flex min-h-12 items-center justify-between rounded-lg border border-slate-200 px-4"><span className="text-sm font-medium text-slate-700">开机自动启动（创建时指定，由容器重启策略执行）</span><input type="checkbox" className="size-4 accent-teal-700" checked={detail.autoStart} disabled readOnly /></label>
        <DialogFooter><button type="button" className="btn-secondary" disabled={detail.status !== 'running'} onClick={() => window.open(`http://${window.location.hostname}:${detail.port}`, '_blank', 'noopener')}><ExternalLink className="size-4" />打开 ComfyUI</button><button type="button" className="btn-primary" onClick={() => setDetailId(null)}>完成</button></DialogFooter>
      </>}</DialogContent>
    </Dialog>

    <Dialog open={Boolean(deleting)} onOpenChange={(open) => !open && closeDelete()}>
      <DialogContent>{deleting && <>
        <DialogHeader><DialogTitle>删除实例</DialogTitle><DialogDescription>此操作将删除真实 Docker 容器；共享模型目录始终保留。</DialogDescription></DialogHeader>
        <div className="rounded-lg border border-rose-200 bg-rose-50 p-3 text-sm leading-6 text-rose-800">请输入实例名称 <strong>{deleting.name}</strong> 以确认删除。</div>
        <label className="field-label">实例名称<input className="field-input" value={confirmName} onChange={(event) => setConfirmName(event.target.value)} autoComplete="off" /></label>
        <DialogFooter><button type="button" className="btn-secondary" onClick={closeDelete}>取消</button><button type="button" className="btn-danger" disabled={confirmName !== deleting.name} onClick={() => { onDelete(deleting.id); closeDelete() }}><Trash2 className="size-4" />确认删除</button></DialogFooter>
      </>}</DialogContent>
    </Dialog>
  </>
}
