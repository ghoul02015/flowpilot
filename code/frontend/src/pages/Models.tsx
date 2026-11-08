import { useMemo, useState } from 'react'
import { Download, Pause, Play, Plus, Search, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { EmptyState, PageHeader, Panel, ProgressBar, StatusBadge } from '@/components/common'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { ModelItem } from '@/types'

interface Props {
  models: ModelItem[]
  realMode: boolean
  modelsRoot?: string
  totalBytes?: number
  hasRunningInstances: boolean
  onAdd: () => void
  onPause: (id: string) => void
  onDelete: (id: string) => void
}

function formatTotal(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 'B'
  for (const next of units) {
    if (value < 1024) break
    value /= 1024
    unit = next
  }
  return `${value >= 100 ? value.toFixed(0) : value.toFixed(1)} ${unit}`
}

export function Models({ models, realMode, modelsRoot, totalBytes, hasRunningInstances, onAdd, onPause, onDelete }: Props) {
  const [query, setQuery] = useState('')
  const [type, setType] = useState('all')
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const filtered = useMemo(() => models.filter((item) => item.name.toLowerCase().includes(query.toLowerCase()) && (type === 'all' || item.type === type)), [models, query, type])
  const deleting = models.find((item) => item.id === deleteId)

  function requestDelete(id: string) {
    if (hasRunningInstances) {
      toast.error('当前无法删除模型', { description: 'MVP 采用保护策略，请先停止所有运行中的 ComfyUI 实例。' })
      return
    }
    setDeleteId(id)
  }

  return <>
    <PageHeader eyebrow="MODEL STORE" title="模型中心" description="统一管理全局共享模型，支持 URL 下载、本地导入、断点续传和校验。" action={<button type="button" className="btn-primary" onClick={onAdd}><Plus className="size-4" />添加模型</button>} />
    <div className="mb-5 grid gap-4 sm:grid-cols-3">
      {(realMode
        ? [{ label: '模型文件', value: `${models.length} 个`, note: '共享仓库实时扫描' }, { label: '仓库占用', value: formatTotal(totalBytes ?? 0), note: modelsRoot ?? '' }, { label: '数据来源', value: '实时扫描', note: '每 3 秒自动刷新' }]
        : [{ label: '模型文件', value: '—', note: '仓库接口不可用' }, { label: '仓库占用', value: '—', note: '等待后端连接' }, { label: '数据来源', value: '离线', note: '恢复后自动刷新' }]
      ).map((item) => <Panel key={item.label} className="p-5"><div className="text-sm text-slate-500">{item.label}</div><div className="mt-2 text-2xl font-bold text-slate-900">{item.value}</div><div className="mt-1 truncate text-xs text-slate-500" title={item.note}>{item.note}</div></Panel>)}
    </div>
    <Panel>
      <div className="flex flex-col gap-3 border-b border-slate-200 p-4 sm:flex-row"><label className="relative flex-1"><span className="sr-only">搜索模型</span><Search className="absolute top-3.5 left-3 size-4 text-slate-400" /><input className="field-input pl-9" placeholder="搜索模型文件" value={query} onChange={(event) => setQuery(event.target.value)} /></label><select className="field-input sm:w-52" aria-label="按模型类型筛选" value={type} onChange={(event) => setType(event.target.value)}><option value="all">全部类型</option>{['Checkpoints', 'LoRA', 'VAE', 'ControlNet', 'Embeddings', 'Upscale Models', '其他'].map((item) => <option key={item}>{item}</option>)}</select></div>
      <div className="overflow-x-auto"><table className="data-table min-w-[980px]"><thead><tr><th>模型名称</th><th>类型</th><th>大小</th><th>状态／进度</th><th>来源</th><th>操作</th></tr></thead><tbody>{filtered.map((item) => <tr key={item.id}>
        <td><div className="flex items-center gap-3"><Download className="size-4 shrink-0 text-slate-400" /><div><div className="max-w-72 truncate font-medium text-slate-800" title={item.name}>{item.name}</div><div className="mt-1 text-xs text-slate-500">{item.createdAt}</div></div></div></td>
        <td>{item.type}</td><td>{item.size}</td><td><div className="w-40"><StatusBadge status={item.status} />{['downloading', 'paused'].includes(item.status) && <div className="mt-2 flex items-center gap-2"><ProgressBar value={item.progress} /><span className="text-xs text-slate-500">{item.progress}%</span></div>}</div></td><td className="text-xs text-slate-500">{item.source}</td>
        <td><div className="flex gap-1">{['downloading', 'paused'].includes(item.status) && <button type="button" className="icon-button" title={item.status === 'downloading' ? '暂停下载' : '继续下载'} aria-label={item.status === 'downloading' ? `暂停 ${item.name}` : `继续 ${item.name}`} onClick={() => onPause(item.id)}>{item.status === 'downloading' ? <Pause className="size-4" /> : <Play className="size-4" />}</button>}<button type="button" className="icon-button text-rose-600 hover:bg-rose-50" title="删除模型" aria-label={`删除 ${item.name}`} onClick={() => requestDelete(item.id)}><Trash2 className="size-4" /></button></div></td>
      </tr>)}</tbody></table></div>{filtered.length === 0 && <EmptyState title="没有匹配的模型" description="请调整搜索词或模型类型。" />}
    </Panel>
    <Dialog open={Boolean(deleting)} onOpenChange={(open) => !open && setDeleteId(null)}><DialogContent>{deleting && <><DialogHeader><DialogTitle>{deleting.id.startsWith('dl-') ? '移除下载任务' : '删除模型文件'}</DialogTitle><DialogDescription>{deleting.id.startsWith('dl-') ? `将取消并移除 ${deleting.name} 的下载任务，已下载的临时分片会一并清理。` : `将从全局共享仓库物理删除 ${deleting.name}，实例工作流和输出不受影响。`}</DialogDescription></DialogHeader><div className="rounded-lg border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800">{deleting.id.startsWith('dl-') ? '任务记录移除后不可恢复，已入库的模型文件不受影响。' : `此操作不可撤销，将真实删除共享仓库中的文件 ${deleting.id}。`}</div><DialogFooter><button type="button" className="btn-secondary" onClick={() => setDeleteId(null)}>取消</button><button type="button" className="btn-danger" onClick={() => { onDelete(deleting.id); setDeleteId(null) }}><Trash2 className="size-4" />确认删除</button></DialogFooter></>}</DialogContent></Dialog>
  </>
}
