import { useMemo, useState } from 'react'
import { ChevronDown, Download, HardDriveDownload, Pause, Play, Plus, Search, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import type { CatalogEntry, OnlineFile } from '@/api'
import { EmptyState, PageHeader, Panel, ProgressBar, StatusBadge } from '@/components/common'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { ModelItem } from '@/types'
import ModelBundles from '@/components/ModelBundles'
import { MarketPanel } from '@/components/AddModelDialog'

type ModelTabKey = 'local' | 'online' | 'bundles'

interface Props {
  models: ModelItem[]
  realMode: boolean
  modelsRoot?: string
  totalBytes?: number
  /** 网络存储（NAS）扫描状态；undefined 表示未配置。 */
  network?: { root: string; available: boolean }
  /** 运行中实例名称列表，删除保护提示用。 */
  runningInstanceNames: string[]
  /** 当前页签来自 URL（?tab=local|online|bundles）。 */
  tab: ModelTabKey
  onTabChange: (tab: ModelTabKey) => void
  onAdd: () => void
  onPause: (id: string) => void
  onDelete: (id: string) => void
  onLocalize: (id: string) => void
  onBatchDelete: (ids: string[]) => void
  onBatchLocalize: (ids: string[]) => void
  /** 在线获取页签的下载入口，与添加模型弹窗共用同一业务动作。 */
  onMarketDownload: (entry: CatalogEntry, useFallback: boolean) => void
  onOnlineDownload: (file: OnlineFile) => void
  existingPaths: string[]
}

// 僵尸模型阈值：超过 30 天未更新且大于 1 GB 的本地模型标记为「疑似闲置」。
const STALE_DAYS = 30
const STALE_MIN_BYTES = 1024 ** 3

// 类别展示顺序：常用类型在前，其余字典序追加。
const categoryOrder = ['Checkpoints', 'LoRA', 'VAE', 'Text Encoders', 'Diffusion Models', 'CLIP Vision', 'Model Patches', 'ControlNet', 'Upscale Models', 'Embeddings', '其他']

function parseSize(size: string): number {
  const match = /^([\d.]+)\s*(B|KB|MB|GB|TB)$/.exec(size)
  if (!match) return 0
  const scale = { B: 1, KB: 1024, MB: 1024 ** 2, GB: 1024 ** 3, TB: 1024 ** 4 }[match[2] as 'B' | 'KB' | 'MB' | 'GB' | 'TB']
  return Number(match[1]) * scale
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

function isStale(item: ModelItem): boolean {
  if (item.origin !== 'local' || !item.modifiedRaw) return false
  const modified = Date.parse(item.modifiedRaw)
  if (Number.isNaN(modified)) return false
  return parseSize(item.size) > STALE_MIN_BYTES && modified < Date.now() - STALE_DAYS * 86400_000
}

function ModelRow({ item, duplicateCount, selected, onToggleSelect, onPause, onDelete, onLocalize }: { item: ModelItem; duplicateCount: number; selected: boolean; onToggleSelect: (id: string) => void; onPause: Props['onPause']; onDelete: (id: string) => void; onLocalize: Props['onLocalize'] }) {
  const selectable = Boolean(item.origin)
  const stale = isStale(item)
  return <tr>
    <td><div className="flex items-center gap-3">
      {selectable
        ? <input type="checkbox" className="size-4 shrink-0 accent-teal-600" aria-label={`选择 ${item.name}`} checked={selected} onChange={() => onToggleSelect(item.id)} />
        : <span className="size-4 shrink-0" />}
      <Download className="size-4 shrink-0 text-slate-400" />
      <div><div className="max-w-72 truncate font-medium text-slate-800" title={item.rawPath ?? item.name}>{item.name}</div><div className="mt-1 text-xs text-slate-500">{item.createdAt}</div>
        {(stale || duplicateCount > 1) && <div className="mt-1 flex gap-1">
          {stale && <span className="inline-flex items-center rounded-full bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700" title={`超过 ${STALE_DAYS} 天未更新且大于 1 GB，建议评估清理`}>疑似闲置</span>}
          {duplicateCount > 1 && <span className="inline-flex items-center rounded-full bg-rose-50 px-2 py-0.5 text-[11px] font-medium text-rose-700" title={`存在 ${duplicateCount} 个同名同大小的模型文件`}>重复×{duplicateCount}</span>}
        </div>}
      </div>
    </div></td>
    <td>{item.size}</td>
    <td><div className="w-40"><StatusBadge status={item.status} />{['downloading', 'paused'].includes(item.status) && <div className="mt-2 flex items-center gap-2"><ProgressBar value={item.progress} /><span className="text-xs text-slate-500">{item.progress}%</span></div>}</div></td>
    <td>{item.origin === 'network'
      ? <span className="inline-flex items-center rounded-full bg-sky-50 px-2 py-0.5 text-[11px] font-medium text-sky-700">网络存储 · 只读</span>
      : item.origin === 'local'
        ? <span className="inline-flex items-center rounded-full bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-600">本地仓库</span>
        : <span className="text-xs text-slate-500">{item.source}</span>}</td>
    <td><div className="flex gap-1">
      {['downloading', 'paused'].includes(item.status) && <button type="button" className="icon-button" title={item.status === 'downloading' ? '暂停下载' : '继续下载'} aria-label={item.status === 'downloading' ? `暂停 ${item.name}` : `继续 ${item.name}`} onClick={() => onPause(item.id)}>{item.status === 'downloading' ? <Pause className="size-4" /> : <Play className="size-4" />}</button>}
      {item.origin === 'network' && <button type="button" className="icon-button text-teal-700 hover:bg-teal-50" title="本地化：复制到本地仓库，实例加载走本机高速盘" aria-label={`本地化 ${item.name}`} onClick={() => onLocalize(item.id)}><HardDriveDownload className="size-4" /></button>}
      {item.origin !== 'network' && <button type="button" className="icon-button text-rose-600 hover:bg-rose-50" title="删除模型" aria-label={`删除 ${item.name}`} onClick={() => onDelete(item.id)}><Trash2 className="size-4" /></button>}
    </div></td>
  </tr>
}

export function Models({ models, realMode, modelsRoot, totalBytes, network, runningInstanceNames, tab, onTabChange, onAdd, onPause, onDelete, onLocalize, onBatchDelete, onBatchLocalize, onMarketDownload, onOnlineDownload, existingPaths }: Props) {
  const [query, setQuery] = useState('')
  const [type, setType] = useState('all')
  const [sort, setSort] = useState<'default' | 'size' | 'time'>('default')
  const [onlyStale, setOnlyStale] = useState(false)
  const [onlyDuplicate, setOnlyDuplicate] = useState(false)
  const [collapsed, setCollapsed] = useState<string[]>([])
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [batchDeleteOpen, setBatchDeleteOpen] = useState(false)

  // 重复检测：按「名称 + 大小」分组，同名同大小多于 1 个即视为重复。
  const duplicateCounts = useMemo(() => {
    const counts = new Map<string, number>()
    for (const item of models) {
      if (!item.origin || item.status !== 'ready') continue
      const key = `${item.name}|${item.size}`
      counts.set(key, (counts.get(key) ?? 0) + 1)
    }
    return counts
  }, [models])
  const duplicateOf = (item: ModelItem) => item.origin && item.status === 'ready' ? duplicateCounts.get(`${item.name}|${item.size}`) ?? 0 : 0

  const filtered = useMemo(() => {
    const dupOf = (item: ModelItem) => item.origin && item.status === 'ready' ? duplicateCounts.get(`${item.name}|${item.size}`) ?? 0 : 0
    return models.filter((item) =>
      item.name.toLowerCase().includes(query.toLowerCase())
      && (type === 'all' || item.type === type)
      && (!onlyStale || isStale(item))
      && (!onlyDuplicate || dupOf(item) > 1)
    )
  }, [models, query, type, onlyStale, onlyDuplicate, duplicateCounts])
  const deleting = models.find((item) => item.id === deleteId)

  // 分组：进行中下载任务归入「进行中任务」组置顶，仓库文件按类别分组。
  const groups = useMemo(() => {
    const active = filtered.filter((item) => ['downloading', 'paused', 'failed', 'cancelled', 'waiting'].includes(item.status))
    const ready = filtered.filter((item) => item.status === 'ready')
    const sorter = (a: ModelItem, b: ModelItem) => sort === 'size' ? parseSize(b.size) - parseSize(a.size) : sort === 'time' ? b.createdAt.localeCompare(a.createdAt) : a.name.localeCompare(b.name)
    const byType = new Map<string, ModelItem[]>()
    for (const item of ready) {
      const list = byType.get(item.type) ?? []
      list.push(item)
      byType.set(item.type, list)
    }
    const orderedTypes = [...categoryOrder.filter((item) => byType.has(item)), ...[...byType.keys()].filter((item) => !categoryOrder.includes(item)).sort()]
    const sections = orderedTypes.map((name) => {
      const items = byType.get(name)!.sort(sorter)
      return { name, items, bytes: items.reduce((sum, item) => sum + parseSize(item.size), 0) }
    })
    if (active.length > 0) sections.unshift({ name: '进行中的任务', items: active, bytes: 0 })
    return sections
  }, [filtered, sort])

  const localFiles = models.filter((item) => item.origin === 'local')
  const networkFiles = models.filter((item) => item.origin === 'network')
  const networkBytes = networkFiles.reduce((sum, item) => sum + parseSize(item.size), 0)

  function requestDelete(id: string) {
    if (runningInstanceNames.length > 0) {
      toast.error('当前无法删除模型', { description: `保护策略：请先停止运行中的实例（${runningInstanceNames.join('、')}）。` })
      return
    }
    setDeleteId(id)
  }

  function toggleGroup(name: string) {
    setCollapsed((previous) => previous.includes(name) ? previous.filter((item) => item !== name) : [...previous, name])
  }

  // 勾选状态派生修剪：仓库刷新后已消失的 id 自动失效，不进入批量范围。
  const selectedIds = selected.filter((id) => models.some((item) => item.id === id))

  function toggleSelect(id: string) {
    setSelected((previous) => previous.includes(id) ? previous.filter((item) => item !== id) : [...previous, id])
  }

  const selectedItems = models.filter((item) => selectedIds.includes(item.id))
  const selectedLocal = selectedItems.filter((item) => item.origin === 'local').map((item) => item.id)
  const selectedNetwork = selectedItems.filter((item) => item.origin === 'network').map((item) => item.id)

  function requestBatchDelete() {
    if (runningInstanceNames.length > 0) {
      toast.error('当前无法删除模型', { description: `保护策略：请先停止运行中的实例（${runningInstanceNames.join('、')}）。` })
      return
    }
    if (selectedLocal.length === 0) {
      toast.info('未选择本地模型', { description: '批量删除仅支持本地仓库文件；网络存储为只读。' })
      return
    }
    setBatchDeleteOpen(true)
  }

  const stats = realMode
    ? [
      { label: '本地仓库', value: `${localFiles.length} 个 · ${formatTotal(totalBytes ?? 0)}`, note: modelsRoot ?? '' },
      { label: '网络存储（NAS）', value: network ? (network.available ? `${networkFiles.length} 个 · ${formatTotal(networkBytes)}` : '已配置 · 暂不可达') : '未配置', note: network ? network.root : '系统管理 → 网络存储 中配置' },
      { label: '模型类别', value: `${groups.filter((group) => group.name !== '进行中的任务').length} 类`, note: '按 ComfyUI 目录约定分组' },
      { label: '数据来源', value: '实时扫描', note: '60 秒缓存 · 写后即时刷新' },
    ]
    : [
      { label: '本地仓库', value: '—', note: '仓库接口不可用' },
      { label: '网络存储（NAS）', value: '—', note: '等待后端连接' },
      { label: '模型类别', value: '—', note: '—' },
      { label: '数据来源', value: '离线', note: '恢复后自动刷新' },
    ]

  return <>
    <PageHeader eyebrow="MODEL STORE" title="模型中心" description="本地仓库负责文件与下载，在线获取对接 ModelScope／HF 镜像，组合套装按场景整组部署和验收。" action={tab === 'local' ? <button type="button" className="btn-primary" onClick={onAdd}><Plus className="size-4" />添加模型</button> : undefined} />
    <div className="mb-4 flex gap-1 overflow-x-auto rounded-lg bg-slate-100 p-1 text-sm" role="tablist" aria-label="模型中心页签">
      {([['local', '本地仓库'], ['online', '在线获取'], ['bundles', '组合套装']] as [ModelTabKey, string][]).map(([key, label]) => <button key={key} type="button" role="tab" aria-selected={tab === key} aria-controls={`model-panel-${key}`} className={`min-h-9 rounded-md px-4 py-1.5 font-medium whitespace-nowrap transition ${tab === key ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500 hover:text-slate-700'}`} onClick={() => onTabChange(key)}>{label}</button>)}
    </div>
    {tab === 'online' && <div id="model-panel-online" role="tabpanel" aria-label="在线获取">
      <Panel className="p-5"><MarketPanel onMarketDownload={onMarketDownload} onOnlineDownload={onOnlineDownload} existingPaths={existingPaths} /></Panel>
    </div>}
    {tab === 'bundles' && <div id="model-panel-bundles" role="tabpanel" aria-label="组合套装"><ModelBundles /></div>}
    {tab === 'local' && <div id="model-panel-local" role="tabpanel" aria-label="本地仓库">
    <div className="mb-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {stats.map((item) => <Panel key={item.label} className="p-5"><div className="text-sm text-slate-500">{item.label}</div><div className="mt-2 text-xl font-bold text-slate-900">{item.value}</div><div className="mt-1 truncate text-xs text-slate-500" title={item.note}>{item.note}</div></Panel>)}
    </div>
    <div className="mb-4 flex flex-col gap-3 sm:flex-row">
      <label className="relative flex-1"><span className="sr-only">搜索模型</span><Search className="absolute top-3.5 left-3 size-4 text-slate-400" /><input className="field-input pl-9" placeholder="搜索模型文件" value={query} onChange={(event) => setQuery(event.target.value)} /></label>
      <select className="field-input sm:w-52" aria-label="按模型类型筛选" value={type} onChange={(event) => setType(event.target.value)}><option value="all">全部类型</option>{[...categoryOrder].map((item) => <option key={item}>{item}</option>)}</select>
      <select className="field-input sm:w-40" aria-label="排序方式" value={sort} onChange={(event) => setSort(event.target.value as typeof sort)}><option value="default">默认排序</option><option value="size">按大小（大到小）</option><option value="time">按时间（新到旧）</option></select>
      <button type="button" className={`rounded-lg border px-3 text-sm transition ${onlyStale ? 'border-amber-300 bg-amber-50 font-medium text-amber-700' : 'border-slate-200 text-slate-500 hover:border-slate-300'}`} aria-pressed={onlyStale} onClick={() => setOnlyStale((value) => !value)}>仅看疑似闲置</button>
      <button type="button" className={`rounded-lg border px-3 text-sm transition ${onlyDuplicate ? 'border-rose-300 bg-rose-50 font-medium text-rose-700' : 'border-slate-200 text-slate-500 hover:border-slate-300'}`} aria-pressed={onlyDuplicate} onClick={() => setOnlyDuplicate((value) => !value)}>仅看重复</button>
    </div>
    {selectedIds.length > 0 && <div className="mb-4 flex flex-wrap items-center gap-3 rounded-xl border border-teal-200 bg-teal-50/60 px-4 py-3">
      <span className="text-sm font-medium text-teal-800">已选 {selectedIds.length} 个</span>
      <button type="button" className="btn-secondary text-sm" disabled={selectedNetwork.length === 0} onClick={() => { onBatchLocalize(selectedNetwork); setSelected([]) }}>批量本地化{selectedNetwork.length > 0 ? `（${selectedNetwork.length}）` : '（仅网络存储）'}</button>
      <button type="button" className="btn-danger text-sm" disabled={selectedLocal.length === 0} onClick={requestBatchDelete}>批量删除{selectedLocal.length > 0 ? `（${selectedLocal.length}）` : '（仅本地仓库）'}</button>
      <button type="button" className="text-sm text-slate-500 underline-offset-2 hover:underline" onClick={() => setSelected([])}>取消选择</button>
    </div>}
    <div className="grid gap-4">
      {groups.map((group) => {
        const isCollapsed = collapsed.includes(group.name)
        return <Panel key={group.name}>
          <button type="button" className="flex w-full cursor-pointer items-center gap-2 px-5 py-4 text-left" onClick={() => toggleGroup(group.name)} aria-expanded={!isCollapsed}>
            <ChevronDown className={`size-4 text-slate-400 transition-transform ${isCollapsed ? '-rotate-90' : ''}`} />
            <span className="font-semibold text-slate-900">{group.name}</span>
            <span className="text-xs text-slate-500">{group.items.length} 个{group.bytes > 0 ? ` · ${formatTotal(group.bytes)}` : ''}</span>
          </button>
          {!isCollapsed && <div className="overflow-x-auto border-t border-slate-100"><table className="data-table min-w-[980px]"><thead><tr><th>模型名称</th><th>大小</th><th>状态／进度</th><th>来源</th><th>操作</th></tr></thead><tbody>
            {group.items.map((item) => <ModelRow key={item.id} item={item} duplicateCount={duplicateOf(item)} selected={selectedIds.includes(item.id)} onToggleSelect={toggleSelect} onPause={onPause} onDelete={requestDelete} onLocalize={onLocalize} />)}
          </tbody></table></div>}
        </Panel>
      })}
      {groups.length === 0 && <Panel><EmptyState title="没有匹配的模型" description="请调整搜索词或模型类型筛选。" /></Panel>}
    </div>
    <Dialog open={Boolean(deleting)} onOpenChange={(open) => !open && setDeleteId(null)}><DialogContent>{deleting && <><DialogHeader><DialogTitle>{deleting.id.startsWith('dl-') ? '移除下载任务' : '删除模型文件'}</DialogTitle><DialogDescription>{deleting.id.startsWith('dl-') ? `将取消并移除 ${deleting.name} 的下载任务，已下载的临时分片会一并清理。` : `将从本地共享仓库物理删除 ${deleting.name}，实例工作流和输出不受影响。`}</DialogDescription></DialogHeader><div className="rounded-lg border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800">{deleting.id.startsWith('dl-') ? '任务记录移除后不可恢复，已入库的模型文件不受影响。' : `此操作不可撤销，将永久删除本地仓库中的文件 ${deleting.rawPath ?? deleting.id}。`}</div><DialogFooter><button type="button" className="btn-secondary" onClick={() => setDeleteId(null)}>取消</button><button type="button" className="btn-danger" onClick={() => { onDelete(deleting.id); setDeleteId(null) }}><Trash2 className="size-4" />确认删除</button></DialogFooter></>}</DialogContent></Dialog>
    <Dialog open={batchDeleteOpen} onOpenChange={setBatchDeleteOpen}><DialogContent><DialogHeader><DialogTitle>批量删除模型文件</DialogTitle><DialogDescription>将从本地共享仓库物理删除选中的 {selectedLocal.length} 个模型文件，实例工作流和输出不受影响。</DialogDescription></DialogHeader><div className="rounded-lg border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800">此操作不可撤销；网络存储（只读）和下载任务不在批量删除范围内，已自动排除。</div><DialogFooter><button type="button" className="btn-secondary" onClick={() => setBatchDeleteOpen(false)}>取消</button><button type="button" className="btn-danger" onClick={() => { onBatchDelete(selectedLocal); setSelected([]); setBatchDeleteOpen(false) }}><Trash2 className="size-4" />确认删除 {selectedLocal.length} 个</button></DialogFooter></DialogContent></Dialog>
    </div>}
  </>
}
