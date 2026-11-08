import { useEffect, useMemo, useState } from 'react'
import { Check, ChevronDown, ChevronRight, CircleAlert, CloudDownload, Download, HardDrive, Link2, LoaderCircle, RefreshCw, Search, Store } from 'lucide-react'
import { toast } from 'sonner'
import { formatBytes, getModelCatalog, getOnlineRepoFiles, getSourcesStatus, refreshModelCatalog, searchOnlineModels, type CatalogEntry, type ModelCatalog, type OnlineFile, type OnlineRepo, type SourceStatus } from '@/api'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { AddModelInput } from '@/types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (input: AddModelInput) => void
  // 模型市场：选中目录条目直接下载（useFallback=true 时走 HF 镜像备选地址）。
  onMarketDownload: (entry: CatalogEntry, useFallback: boolean) => void
  // 在线源：选中仓库权重文件直接下载（地址与类别由后端推断）。
  onOnlineDownload: (file: OnlineFile) => void
  // 共享仓库与进行中任务已占用的目标路径（category/fileName），用于标注已入库。
  existingPaths: string[]
}

type SourceTab = 'market' | 'url' | 'import'
type MarketSource = 'curated' | 'modelscope' | 'hf'

const typeFilters = ['全部类型', 'Checkpoints', 'LoRA', 'VAE', 'ControlNet', 'Text Encoders', 'Diffusion Models', 'Upscale Models', 'Embeddings']

// categoryLabels 将入库类别（ComfyUI 目录约定）映射为展示类型。
const categoryLabels: Record<string, string> = {
  checkpoints: 'Checkpoints',
  loras: 'LoRA',
  vae: 'VAE',
  controlnet: 'ControlNet',
  embeddings: 'Embeddings',
  upscale_models: 'Upscale Models',
  text_encoders: 'Text Encoders',
  diffusion_models: 'Diffusion Models',
  model_patches: 'Model Patches',
  clip_vision: 'CLIP Vision',
  other: '其他',
}

function categoryLabel(category: string): string {
  return categoryLabels[category] ?? category
}

function formatDownloads(count: number): string {
  if (count >= 1e8) return `${(count / 1e8).toFixed(1)} 亿`
  if (count >= 1e4) return `${(count / 1e4).toFixed(1)} 万`
  return `${count}`
}

// 源状态点：绿＝可达（显示延迟），红＝不可达，灰＝尚未探测。
function SourceDot({ status }: { status?: SourceStatus }) {
  if (!status) return <span className="inline-block size-2 rounded-full bg-slate-300" title="正在探测连接状态…" />
  return <span
    className={`inline-block size-2 rounded-full ${status.reachable ? 'bg-emerald-500' : 'bg-rose-500'}`}
    title={status.reachable ? `${status.home} 连接正常 · ${status.latencyMs}ms` : `${status.home} 当前不可达`} />
}

function MarketPanel({ onMarketDownload, onOnlineDownload, existingPaths }: { onMarketDownload: Props['onMarketDownload']; onOnlineDownload: Props['onOnlineDownload']; existingPaths: string[] }) {
  const [source, setSource] = useState<MarketSource>('curated')
  const [statuses, setStatuses] = useState<SourceStatus[]>([])
  const [catalog, setCatalog] = useState<ModelCatalog | null>(null)
  const [loadError, setLoadError] = useState('')
  const [query, setQuery] = useState('')
  const [type, setType] = useState('全部类型')
  const [refreshing, setRefreshing] = useState(false)
  // 在线源状态
  const [searching, setSearching] = useState(false)
  const [results, setResults] = useState<OnlineRepo[] | null>(null)
  const [searchError, setSearchError] = useState('')
  const [expandedRepo, setExpandedRepo] = useState<string | null>(null)
  const [filesByRepo, setFilesByRepo] = useState<Record<string, OnlineFile[] | 'loading' | 'error'>>({})
  const occupied = useMemo(() => new Set(existingPaths), [existingPaths])

  useEffect(() => {
    let cancelled = false
    getModelCatalog()
      .then((result) => { if (!cancelled) setCatalog(result) })
      .catch((error: unknown) => { if (!cancelled) setLoadError(error instanceof Error ? error.message : '模型目录加载失败') })
    getSourcesStatus()
      .then((result) => { if (!cancelled) setStatuses(result) })
      .catch(() => { /* 状态探测失败不阻塞市场使用 */ })
    return () => { cancelled = true }
  }, [])

  function statusOf(id: string): SourceStatus | undefined {
    return statuses.find((item) => item.id === id)
  }

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

  function search() {
    const keyword = query.trim()
    if (!keyword || searching) return
    setSearching(true)
    setSearchError('')
    setExpandedRepo(null)
    searchOnlineModels(source, keyword)
      .then((repos) => setResults(repos))
      .catch((error: unknown) => {
        setResults(null)
        setSearchError(error instanceof Error ? error.message : '源站搜索失败')
      })
      .finally(() => setSearching(false))
  }

  function toggleRepo(repo: OnlineRepo) {
    if (expandedRepo === repo.repo) {
      setExpandedRepo(null)
      return
    }
    setExpandedRepo(repo.repo)
    if (filesByRepo[repo.repo]) return
    setFilesByRepo((previous) => ({ ...previous, [repo.repo]: 'loading' }))
    getOnlineRepoFiles(repo.source, repo.repo)
      .then((files) => setFilesByRepo((previous) => ({ ...previous, [repo.repo]: files })))
      .catch(() => setFilesByRepo((previous) => ({ ...previous, [repo.repo]: 'error' })))
  }

  const filtered = useMemo(() => (catalog?.entries ?? []).filter((entry) => {
    const matchType = type === '全部类型' || entry.typeLabel === type
    const keyword = query.trim().toLowerCase()
    const matchQuery = !keyword || entry.name.toLowerCase().includes(keyword) || entry.fileName.toLowerCase().includes(keyword) || entry.description.toLowerCase().includes(keyword)
    return matchType && matchQuery
  }), [catalog, query, type])

  const sources: { key: MarketSource; label: string; statusId?: string }[] = [
    { key: 'curated', label: '推荐目录' },
    { key: 'modelscope', label: 'ModelScope', statusId: 'modelscope' },
    { key: 'hf', label: 'HF 镜像', statusId: 'hf' },
  ]

  return <div className="min-w-0">
    {/* 源选择：推荐目录（内置＋在线刷新）／ModelScope／HF 镜像，后两者实时显示连接状态 */}
    <div className="mb-3 grid grid-cols-3 gap-2 rounded-lg bg-slate-100 p-1">
      {sources.map(({ key, label, statusId }) => <button
        type="button"
        key={key}
        data-testid={`source-tab-${key}`}
        className={`flex min-h-9 cursor-pointer items-center justify-center gap-1.5 rounded-md text-xs font-medium transition-colors ${source === key ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-600 hover:text-slate-900'}`}
        onClick={() => { setSource(key); setQuery(''); setResults(null); setSearchError(''); setExpandedRepo(null) }}>
        {statusId && <SourceDot status={statusOf(statusId)} />}{label}
      </button>)}
    </div>

    <div className="mb-3 flex flex-col gap-2 sm:flex-row">
      <label className="relative flex-1"><span className="sr-only">搜索模型</span><Search className="absolute top-3.5 left-3 size-4 text-slate-400" /><input
        className="field-input pl-9"
        placeholder={source === 'curated' ? '搜索模型名称、文件或描述' : `在 ${source === 'modelscope' ? 'ModelScope' : 'HF 镜像'} 搜索模型仓库，回车确认`}
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        onKeyDown={(event) => { if (event.key === 'Enter' && source !== 'curated') search() }} /></label>
      <select className="field-input sm:w-44" aria-label="按模型类型筛选" value={type} onChange={(event) => setType(event.target.value)}>{typeFilters.map((item) => <option key={item}>{item}</option>)}</select>
      {source !== 'curated' && <button type="button" className="btn-primary shrink-0 px-4 text-xs" disabled={searching || !query.trim()} onClick={search} aria-label="在线搜索">{searching ? <LoaderCircle className="size-3.5 animate-spin" /> : <Search className="size-3.5" />}搜索</button>}
    </div>

    {source === 'curated' && (loadError
      ? <div className="flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900"><CircleAlert className="mt-0.5 size-5 shrink-0" />模型目录加载失败：{loadError}。可切换到在线源搜索或「URL 下载」。</div>
      : !catalog
        ? <div className="flex items-center justify-center gap-2 py-16 text-sm text-slate-500"><LoaderCircle className="size-4 animate-spin" />正在加载模型目录…</div>
        : <>
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
                    {/* 显示名与 ComfyUI 官方模板引用的文件名保持一致 */}
                    <p className="mt-1 truncate font-mono text-[11px] text-slate-500" title={`${entry.repo} · ${entry.filePath}`}>{entry.fileName}</p>
                    <p className="mt-1 text-xs leading-5 text-slate-600">{entry.description}</p>
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
            {filtered.length === 0 && <p className="rounded-lg border border-dashed border-slate-300 p-6 text-center text-sm text-slate-500">没有匹配的模型。可切换到 ModelScope／HF 镜像在线搜索，或调整筛选条件。</p>}
          </div>
          <p className="mt-3 text-[11px] leading-4 text-slate-400">推荐目录主源为 ModelScope（国内直连），文件名与 ComfyUI 官方模板引用一致；标注「需授权」的 HF 备选需先接受许可。找不到的模型可切到在线源搜索全站。</p>
        </>)}

    {source !== 'curated' && <div>
      {searchError && <div className="flex items-start gap-3 rounded-lg border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800"><CircleAlert className="mt-0.5 size-4 shrink-0" />{searchError}</div>}
      {!results && !searchError && !searching && <p className="rounded-lg border border-dashed border-slate-300 p-6 text-center text-sm text-slate-500">输入模型名（如 MiniMax-H3、Wan2.1、FLUX）回车，实时搜索 {source === 'modelscope' ? 'ModelScope' : 'HF 镜像'} 全站仓库。</p>}
      {searching && <div className="flex items-center justify-center gap-2 py-12 text-sm text-slate-500"><LoaderCircle className="size-4 animate-spin" />正在搜索 {source === 'modelscope' ? 'ModelScope' : 'HF 镜像'}…</div>}
      {results && !searching && <div className="max-h-80 space-y-2 overflow-y-auto pr-1">
        {results.length === 0 && <p className="rounded-lg border border-dashed border-slate-300 p-6 text-center text-sm text-slate-500">未找到相关仓库。源站分词对长词更严格，可尝试更短的关键词（如把「MiniMax-H3」换成「MiniMax」），或切换到另一个源再试。</p>}
        {results.map((repo) => {
          const expanded = expandedRepo === repo.repo
          const files = filesByRepo[repo.repo]
          const visibleFiles = Array.isArray(files) ? files.filter((file) => type === '全部类型' || categoryLabel(file.category) === type) : []
          return <div key={repo.repo} className="rounded-lg border border-slate-200" data-testid={`online-repo-${repo.repo}`}>
            <button type="button" className="flex w-full cursor-pointer items-center gap-2 p-3 text-left hover:bg-slate-50" onClick={() => toggleRepo(repo)} aria-expanded={expanded}>
              {expanded ? <ChevronDown className="size-4 shrink-0 text-slate-400" /> : <ChevronRight className="size-4 shrink-0 text-slate-400" />}
              <span className="min-w-0 flex-1">
                <span className="block truncate font-mono text-xs font-semibold text-slate-900">{repo.repo}</span>
                {repo.chineseName && <span className="mt-0.5 block truncate text-[11px] text-slate-500">{repo.chineseName}</span>}
              </span>
              <span className="shrink-0 text-[11px] text-slate-500">下载 {formatDownloads(repo.downloads)}{repo.updatedAt ? ` · 更新 ${repo.updatedAt}` : ''}</span>
            </button>
            {expanded && <div className="border-t border-slate-100 p-2">
              {files === 'loading' && <div className="flex items-center justify-center gap-2 py-4 text-xs text-slate-500"><LoaderCircle className="size-3.5 animate-spin" />正在读取仓库文件…</div>}
              {files === 'error' && <p className="py-3 text-center text-xs text-rose-600">文件列表读取失败，请稍后重试。</p>}
              {Array.isArray(files) && files.length === 0 && <p className="py-3 text-center text-xs text-slate-500">仓库中没有可下载的权重文件（.safetensors 等）。</p>}
              {Array.isArray(files) && visibleFiles.map((file) => {
                const taken = occupied.has(`${file.category}/${file.name}`)
                return <div key={file.path} className="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-slate-50">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-mono text-[11px] text-slate-800" title={file.path}>{file.name}</span>
                    <span className="mt-0.5 flex items-center gap-2 text-[10px] text-slate-500"><span className="rounded-full bg-slate-100 px-1.5 py-px">{categoryLabel(file.category)}</span>{formatBytes(file.sizeBytes)}</span>
                  </span>
                  {taken
                    ? <span className="inline-flex shrink-0 items-center gap-1 text-[11px] text-emerald-700"><Check className="size-3" />已入库</span>
                    : <button type="button" className="btn-primary shrink-0 px-2.5 py-1 text-[11px]" aria-label={`下载 ${file.name}`} onClick={() => onOnlineDownload(file)}><CloudDownload className="size-3" />下载</button>}
                </div>
              })}
              {Array.isArray(files) && files.length > 0 && visibleFiles.length === 0 && <p className="py-3 text-center text-xs text-slate-500">该仓库没有「{type}」类型的文件。</p>}
            </div>}
          </div>
        })}
      </div>}
      <p className="mt-3 text-[11px] leading-4 text-slate-400">搜索结果与文件大小实时来自源站，源站更新即所见，平台无需等待版本更新；类别按 ComfyUI 目录约定自动推断入库。优先选择 Comfy-Org 等官方打包仓库。</p>
    </div>}
  </div>
}

export function AddModelDialog({ open, onOpenChange, onSubmit, onMarketDownload, onOnlineDownload, existingPaths }: Props) {
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
        {tab === 'market' && <MarketPanel onMarketDownload={onMarketDownload} onOnlineDownload={onOnlineDownload} existingPaths={existingPaths} />}
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
