import { useEffect, useMemo, useState } from 'react'
import { Check, CircleAlert, CloudDownload, Download, HardDrive, Link2, LoaderCircle, RefreshCw, Search, Store } from 'lucide-react'
import { toast } from 'sonner'
import { formatBytes, getModelCatalog, refreshModelCatalog, type CatalogEntry, type ModelCatalog } from '@/api'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { AddModelInput } from '@/types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (input: AddModelInput) => void
  // 模型市场：选中目录条目直接下载（useFallback=true 时走 HF 镜像备选地址）。
  onMarketDownload: (entry: CatalogEntry, useFallback: boolean) => void
  // 共享仓库与进行中任务已占用的目标路径（category/fileName），用于标注已入库。
  existingPaths: string[]
}

type SourceTab = 'market' | 'url' | 'import'

const typeFilters = ['全部类型', 'Checkpoints', 'LoRA', 'VAE', 'ControlNet', 'Text Encoders', 'Diffusion Models', 'Upscale Models']

function MarketPanel({ onMarketDownload, existingPaths }: { onMarketDownload: Props['onMarketDownload']; existingPaths: string[] }) {
  const [catalog, setCatalog] = useState<ModelCatalog | null>(null)
  const [loadError, setLoadError] = useState('')
  const [query, setQuery] = useState('')
  const [type, setType] = useState('全部类型')
  const [refreshing, setRefreshing] = useState(false)
  const occupied = useMemo(() => new Set(existingPaths), [existingPaths])

  useEffect(() => {
    let cancelled = false
    getModelCatalog()
      .then((result) => {
        if (!cancelled) setCatalog(result)
      })
      .catch((error: unknown) => {
        if (!cancelled) setLoadError(error instanceof Error ? error.message : '模型目录加载失败')
      })
    return () => {
      cancelled = true
    }
  }, [])

  function refresh() {
    setRefreshing(true)
    refreshModelCatalog()
      .then((result) => {
        setCatalog(result)
        const unreachable = result.entries.filter((entry) => !entry.reachable).length
        toast.success('模型目录已更新', { description: unreachable > 0 ? `已同步 ModelScope 最新信息，${unreachable} 个条目暂不可达。` : '全部条目已同步 ModelScope 最新信息。' })
      })
      .catch((error: unknown) => {
        toast.error('目录更新失败', { description: error instanceof Error ? error.message : 'ModelScope 连接失败，已保留现有目录。' })
      })
      .finally(() => setRefreshing(false))
  }

  const filtered = useMemo(() => (catalog?.entries ?? []).filter((entry) => {
    const matchType = type === '全部类型' || entry.typeLabel === type
    const keyword = query.trim().toLowerCase()
    const matchQuery = !keyword || entry.name.toLowerCase().includes(keyword) || entry.fileName.toLowerCase().includes(keyword) || entry.description.toLowerCase().includes(keyword)
    return matchType && matchQuery
  }), [catalog, query, type])

  if (loadError) {
    return <div className="flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900"><CircleAlert className="mt-0.5 size-5 shrink-0" />模型目录加载失败：{loadError}。可切换到「URL 下载」手动添加。</div>
  }
  if (!catalog) {
    return <div className="flex items-center justify-center gap-2 py-16 text-sm text-slate-500"><LoaderCircle className="size-4 animate-spin" />正在加载模型目录…</div>
  }
  return <div>
    <div className="mb-3 flex flex-col gap-2 sm:flex-row">
      <label className="relative flex-1"><span className="sr-only">搜索模型</span><Search className="absolute top-3.5 left-3 size-4 text-slate-400" /><input className="field-input pl-9" placeholder="搜索模型名称、文件或描述" value={query} onChange={(event) => setQuery(event.target.value)} /></label>
      <select className="field-input sm:w-44" aria-label="按模型类型筛选" value={type} onChange={(event) => setType(event.target.value)}>{typeFilters.map((item) => <option key={item}>{item}</option>)}</select>
    </div>
    <div className="mb-3 flex items-center justify-between text-xs text-slate-500">
      <span>{catalog.source === 'cache' && catalog.refreshedAt ? `目录已同步 · ${new Date(catalog.refreshedAt).toLocaleString('zh-CN', { hour12: false })}` : '内置目录 · 未在线同步'} · 共 {filtered.length} 个模型</span>
      <button type="button" className="btn-quiet text-xs" disabled={refreshing} onClick={refresh}><RefreshCw className={`size-3.5 ${refreshing ? 'animate-spin' : ''}`} />{refreshing ? '正在同步 ModelScope…' : '更新目录'}</button>
    </div>
    <div className="max-h-80 space-y-2 overflow-y-auto pr-1">
      {filtered.map((entry) => {
        const taken = occupied.has(`${entry.category}/${entry.fileName}`) || (entry.category === 'other' && occupied.has(entry.fileName))
        return <div key={entry.id} className="rounded-lg border border-slate-200 p-3" data-testid={`market-entry-${entry.id}`}>
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2"><span className="text-sm font-semibold text-slate-900">{entry.name}</span><span className="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] text-slate-600">{entry.typeLabel}</span><span className="text-xs text-slate-500">{formatBytes(entry.sizeBytes)}</span>{!entry.reachable && <span className="rounded-full bg-amber-100 px-2 py-0.5 text-[11px] text-amber-800">暂不可达</span>}</div>
              <p className="mt-1 text-xs leading-5 text-slate-600">{entry.description}</p>
              <p className="mt-1 truncate font-mono text-[11px] text-slate-400" title={entry.fileName}>{entry.fileName}</p>
            </div>
            <div className="flex shrink-0 flex-col items-end gap-1.5">
              {taken
                ? <span className="inline-flex items-center gap-1 rounded-md bg-emerald-50 px-2.5 py-1.5 text-xs font-medium text-emerald-700"><Check className="size-3.5" />已入库</span>
                : <button type="button" className="btn-primary px-3 py-1.5 text-xs" aria-label={`下载 ${entry.name}`} onClick={() => onMarketDownload(entry, false)}><CloudDownload className="size-3.5" />下载</button>}
              {!taken && entry.fallbackUrl && <button type="button" className="text-[11px] text-slate-500 underline decoration-dotted hover:text-teal-700" aria-label={`使用 HF 镜像下载 ${entry.name}`} onClick={() => onMarketDownload(entry, true)}>HF 镜像{entry.fallbackGated ? '（需授权）' : ''}</button>}
            </div>
          </div>
        </div>
      })}
      {filtered.length === 0 && <p className="rounded-lg border border-dashed border-slate-300 p-6 text-center text-sm text-slate-500">没有匹配的模型，请调整搜索词或类型筛选。</p>}
    </div>
    <p className="mt-3 text-[11px] leading-4 text-slate-400">主源为 ModelScope（国内直连）；HF 镜像为备选，标注「需授权」的模型需先在 Hugging Face 接受许可。下载由平台任务引擎执行，支持断点续传与 SHA256 校验。</p>
  </div>
}

export function AddModelDialog({ open, onOpenChange, onSubmit, onMarketDownload, existingPaths }: Props) {
  const [tab, setTab] = useState<SourceTab>('market')
  const [name, setName] = useState('')
  const [type, setType] = useState('Checkpoints')
  const [source, setSource] = useState('')
  const [error, setError] = useState('')

  function changeTab(value: SourceTab) {
    setTab(value)
    setSource('')
    setError('')
  }

  function submit() {
    // 文件名可留空，由下载地址或源路径自动推导。
    if (tab === 'url' && !/^https?:\/\//.test(source)) {
      setError('请输入以 http:// 或 https:// 开头的下载地址。')
      return
    }
    if (tab === 'import' && !source.startsWith('/data/import/')) {
      setError('仅允许从 /data/import/ 目录导入，请先将文件放入宿主机安装目录下的 import 文件夹。')
      return
    }
    onSubmit({ name: name.trim(), type, source, sourceType: tab === 'url' ? 'URL 下载' : '本地导入' })
    onOpenChange(false)
  }

  const tabs: { key: SourceTab; label: string; icon: typeof Store }[] = [
    { key: 'market', label: '模型市场', icon: Store },
    { key: 'url', label: 'URL 下载', icon: Link2 },
    { key: 'import', label: '本地导入', icon: HardDrive },
  ]

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader><DialogTitle>添加模型</DialogTitle><DialogDescription>从模型市场选择主流模型直接下载，或通过 URL／本地文件添加；校验完成后写入全局共享仓库。</DialogDescription></DialogHeader>
        <div className="grid grid-cols-3 gap-2 rounded-lg bg-slate-100 p-1">
          {tabs.map(({ key, label, icon: Icon }) => <button type="button" key={key} className={`min-h-11 cursor-pointer rounded-md text-sm font-medium transition-colors ${tab === key ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-600 hover:text-slate-900'}`} onClick={() => changeTab(key)}><Icon className="mr-1.5 inline size-4" />{label}</button>)}
        </div>
        {tab === 'market' && <MarketPanel onMarketDownload={onMarketDownload} existingPaths={existingPaths} />}
        {tab !== 'market' && <div className="grid gap-4 py-2">
          <label className="field-label">模型名称<span className="ml-1 font-normal text-slate-400">（留空则从地址推导）</span><input className="field-input" value={name} onChange={(event) => setName(event.target.value)} autoComplete="off" /></label>
          <label className="field-label">模型类型<select className="field-input" value={type} onChange={(event) => setType(event.target.value)}>{['Checkpoints', 'LoRA', 'VAE', 'ControlNet', 'Embeddings', 'Upscale Models', '其他'].map((item) => <option key={item}>{item}</option>)}</select></label>
          <label className="field-label">{tab === 'url' ? '下载地址' : '服务器文件路径'}<input className="field-input font-mono text-xs" value={source} onChange={(event) => setSource(event.target.value)} placeholder={tab === 'url' ? 'https://…/model.safetensors' : '/data/import/model.safetensors'} autoComplete="off" /></label>
          <div className="rounded-lg bg-slate-100 p-3 text-xs leading-5 text-slate-600">{tab === 'url'
            ? '将从该地址真实下载并写入共享仓库，支持暂停、断点续传与失败重试；任务进度在模型中心与任务中心实时可见。'
            : '将从 /data/import 目录（宿主机安装目录下的 import 文件夹）复制文件并写入共享仓库，支持暂停与校验；任务进度在模型中心与任务中心实时可见。'}</div>
          {error && <p role="alert" className="rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700">{error}</p>}
        </div>}
        <DialogFooter>{tab !== 'market' && <><button type="button" className="btn-secondary" onClick={() => onOpenChange(false)}>取消</button><button type="button" className="btn-primary" onClick={submit}><Download className="size-4" />{tab === 'url' ? '开始下载' : '开始导入'}</button></>}</DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
