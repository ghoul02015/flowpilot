import { useCallback, useEffect, useMemo, useState } from 'react'
import { Ban, CloudDownload, Cpu, Download, FileText, Hammer, Layers, LoaderCircle, RefreshCw, ScrollText, Tag, Trash2, Upload, XCircle } from 'lucide-react'
import { toast } from 'sonner'
import { buildScriptURL, cancelImageTask, deleteImageTask, deleteLocalImageApi, exportLocalImageURL, formatBytes, getComfyUIRefs, getImageCatalog, getImageDiskUsage, getImageTasks, getLocalImages, imageBuildSummary, importLocalImageApi, isComfyImage, pruneImageBuildCache, startImageBuild, startImagePull, tagLocalImageApi, type ComfyRef, type ImageBuildSpec, type ImageDiskUsage, type ImageRecipe, type ImageTask, type LocalImage } from '@/api'
import { EmptyState, Panel, PanelHeader } from '@/components/common'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

interface ImageTagRow {
  tag: string
  image?: LocalImage
  task?: ImageTask
  virtual?: boolean
}

interface ImageTagGroup {
  key: string
  tags: string[]
  image?: LocalImage
  task?: ImageTask
  virtual?: boolean
}

interface ConfirmRequest {
  title: string
  description: string
  confirmLabel: string
  danger?: boolean
  action: () => Promise<void>
}

// groupTagRows 按镜像 ID 合并标签，避免同一镜像的多个别名看起来像多份镜像。
function groupTagRows(rows: ImageTagRow[]): ImageTagGroup[] {
  const groups = new Map<string, ImageTagGroup>()
  for (const row of rows) {
    const key = row.image?.Id ? `image:${row.image.Id}` : `task:${row.task?.id ?? row.tag}`
    const existing = groups.get(key)
    if (existing) {
      if (!existing.tags.includes(row.tag)) existing.tags.push(row.tag)
      if (!existing.task && row.task) existing.task = row.task
      continue
    }
    groups.set(key, { key, tags: [row.tag], image: row.image, task: row.task, virtual: row.virtual })
  }
  return [...groups.values()]
}

const taskStateText: Record<string, string> = { queued: '排队中', running: '进行中', done: '已完成', error: '失败', cancelled: '已取消', interrupted: '已中断' }

const platformOptions = [
  { value: 'host', label: '宿主机架构', description: '在当前机器本机架构构建，适合快速验证。' },
  { value: 'linux/arm64', label: 'linux/arm64（aarch64）', description: '适合 DGX Spark / ARM 集群节点分发。' },
  { value: 'linux/amd64', label: 'linux/amd64（x86_64）', description: '适合通用 x86_64 GPU 服务器。' },
  { value: 'linux/arm/v7', label: 'linux/arm/v7', description: '预留给边缘 ARMv7 节点，需宿主机支持跨架构构建。' },
]

function isActiveTask(task: ImageTask): boolean {
  return task.state === 'queued' || task.state === 'running'
}

function TaskStateBadge({ state }: { state: string }) {
  const tone = state === 'done' ? 'bg-emerald-50 text-emerald-700' : state === 'running' ? 'bg-teal-50 text-teal-700' : state === 'queued' ? 'bg-amber-50 text-amber-700' : state === 'error' ? 'bg-rose-50 text-rose-700' : 'bg-slate-100 text-slate-600'
  return <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium ${tone}`}>{state === 'running' && <LoaderCircle className="size-3 animate-spin" />}{taskStateText[state] ?? state}</span>
}

export function ImageManager() {
  const [images, setImages] = useState<LocalImage[]>([])
  const [recipes, setRecipes] = useState<ImageRecipe[]>([])
  const [refs, setRefs] = useState<ComfyRef[]>([{ name: 'master', type: 'branch' }])
  const [tasks, setTasks] = useState<ImageTask[]>([])
  const [diskUsage, setDiskUsage] = useState<ImageDiskUsage | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [pruning, setPruning] = useState(false)
  const [pullImage, setPullImage] = useState('')
  const [recipeId, setRecipeId] = useState('nvidia-cu130')
  const [imageName, setImageName] = useState('flowpilot/comfyui:cu130')
  const [ref, setRef] = useState('master')
  const [pip, setPip] = useState('tsinghua')
  const [manager, setManager] = useState(true)
  const [platform, setPlatform] = useState('host')
  const [buildOpen, setBuildOpen] = useState(false)
  const [pullOpen, setPullOpen] = useState(false)
  const [logsTaskId, setLogsTaskId] = useState<string | null>(null)
  // 打开日志时的任务快照：成功任务入库后记录会被自动清理，
  // 没有快照会让正在查看的日志弹窗突然关闭。
  const [logsSnapshot, setLogsSnapshot] = useState<ImageTask | null>(null)
  // 标签管理：同一镜像 ID 可保留多个标签，不复制镜像层。
  const [tagOpen, setTagOpen] = useState(false)
  const [tagImage, setTagImage] = useState<LocalImage | null>(null)
  const [tagList, setTagList] = useState<string[]>([])
  const [tagSource, setTagSource] = useState('')
  const [tagTarget, setTagTarget] = useState('')
  const [tagMode, setTagMode] = useState<'alias' | 'rename'>('alias')
  const [tagError, setTagError] = useState('')
  const [tagBusy, setTagBusy] = useState(false)
  // 统一产品确认框，替代浏览器原生 confirm。
  const [confirmRequest, setConfirmRequest] = useState<ConfirmRequest | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)

  const refreshTasks = useCallback(() => {
    getImageTasks().then(setTasks).catch(() => {})
  }, [])

  const refresh = useCallback(() => {
    setLoading(true)
    Promise.all([
      getLocalImages().then(setImages),
      getImageCatalog().then((list) => {
        setRecipes(list)
        const selected = list.find((item) => item.id === recipeId) ?? list[0]
        if (selected) {
          setRecipeId(selected.id)
          setImageName((current) => current || selected.image)
        }
      }),
      getComfyUIRefs().then(setRefs).catch(() => setRefs([{ name: 'master', type: 'branch' }])),
      getImageTasks().then(setTasks).catch(() => {}),
      getImageDiskUsage().then(setDiskUsage).catch(() => {}),
    ]).catch((error: unknown) => toast.error('镜像信息读取失败', { description: error instanceof Error ? error.message : '请确认后端已连接 Docker。' })).finally(() => setLoading(false))
  }, [recipeId])

  useEffect(() => { refresh() }, [refresh])

  // 有排队／进行中任务时每 2 秒轮询任务列表；全部收口后刷新一次镜像清单。
  const activeCount = tasks.filter(isActiveTask).length
  useEffect(() => {
    if (activeCount === 0) return
    const timer = window.setInterval(() => {
      getImageTasks().then((next) => {
        setTasks(next)
        if (!next.some(isActiveTask)) refresh()
      }).catch(() => {})
    }, 2000)
    return () => window.clearInterval(timer)
  }, [activeCount, refresh])

  const activeBuildByImage = useMemo(() => {
    const map = new Map<string, ImageTask>()
    for (const task of tasks) {
      if (task.kind === 'build' && isActiveTask(task)) map.set(task.image, task)
    }
    return map
  }, [tasks])

  const tagRows = useMemo<ImageTagRow[]>(() => images.flatMap((image) => image.RepoTags.map((tag) => ({ tag, image }))).filter((item) => item.tag && item.tag !== '<none>:<none>'), [images])
  const comfyRowsBase = tagRows.filter((item) => isComfyImage(item.tag, item.image))
  const otherRows = tagRows.filter((item) => !isComfyImage(item.tag, item.image))
  const virtualRows: ImageTagRow[] = [...activeBuildByImage.values()].filter((task) => !comfyRowsBase.some((item) => item.tag === task.image)).map((task) => ({ tag: task.image, task, virtual: true }))
  const comfyRows = comfyRowsBase.map((row) => ({ ...row, task: activeBuildByImage.get(row.tag) }))
  const comfyRowsWithVirtual = [...virtualRows, ...comfyRows]
  const selectedRecipe = recipes.find((item) => item.id === recipeId)
  const selectedPlatform = platformOptions.find((item) => item.value === platform) ?? platformOptions[0]
  const logsTask = tasks.find((task) => task.id === logsTaskId) ?? (logsSnapshot?.id === logsTaskId ? logsSnapshot : null)

  function openLogs(task: ImageTask) {
    setLogsSnapshot(task)
    setLogsTaskId(task.id)
  }

  function closeLogs() {
    setLogsTaskId(null)
    setLogsSnapshot(null)
  }

  const currentBuildSpec = useMemo<ImageBuildSpec>(() => ({ variant: recipeId, image: imageName.trim() || selectedRecipe?.image || 'flowpilot/comfyui:local', ref: ref.trim() || 'master', pip, manager: manager ? '1' : '0', platform }), [imageName, manager, pip, platform, recipeId, ref, selectedRecipe?.image])

  function onRecipeChange(next: string) {
    setRecipeId(next)
    const recipe = recipes.find((item) => item.id === next)
    if (recipe) setImageName(recipe.image)
  }

  function openTagManager(group: ImageTagGroup) {
    if (!group.image) return
    setTagImage(group.image)
    setTagList(group.tags)
    setTagSource(group.tags[0] ?? '')
    setTagTarget('')
    setTagMode('alias')
    setTagError('')
    setTagOpen(true)
  }

  async function submitTag() {
    const target = tagTarget.trim()
    if (!tagSource) {
      setTagError('请选择源标签')
      return
    }
    if (!target) {
      setTagError('请输入新的镜像标签')
      return
    }
    if (target === tagSource) {
      setTagError('新标签与源标签相同')
      return
    }
    setTagBusy(true)
    setTagError('')
    try {
      const result = await tagLocalImageApi(tagSource, target, tagMode)
      toast.success(result.action === 'rename' ? '镜像标签已重命名' : '镜像别名已添加', {
        description: result.action === 'rename' ? `${result.source} → ${result.target}` : `${result.source} 与 ${result.target} 指向同一镜像，不重复占用磁盘`,
      })
      setTagOpen(false)
      refresh()
    } catch (error) {
      setTagError(error instanceof Error ? error.message : '标签操作失败，请稍后重试。')
    } finally {
      setTagBusy(false)
    }
  }

  function askConfirm(request: ConfirmRequest) {
    setConfirmRequest(request)
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

  function requestRemoveTag(tag: string, remaining: number) {
    setTagOpen(false)
    askConfirm({
      title: remaining > 1 ? '删除镜像标签' : '删除镜像',
      description: remaining > 1
        ? `将删除标签 ${tag}。该镜像仍由其他标签保留，不会删除镜像数据。`
        : `将删除最后一个标签 ${tag} 及对应镜像数据。此操作不可撤销。`,
      confirmLabel: remaining > 1 ? '删除标签' : '删除镜像',
      danger: true,
      action: async () => {
        setBusy(true)
        try {
          await deleteLocalImageApi(tag)
          toast.success(remaining > 1 ? '镜像标签已删除' : '镜像已删除', { description: tag })
          refresh()
        } catch (error) {
          toast.error('删除镜像失败', { description: error instanceof Error ? error.message : '请确认没有实例正在使用该镜像。' })
        } finally {
          setBusy(false)
        }
      },
    })
  }

  async function importImage(file?: File) {
    if (!file) return
    setBusy(true)
    try {
      await importLocalImageApi(file)
      toast.success('镜像导入完成', { description: file.name })
      refresh()
    } catch (error) {
      toast.error('镜像导入失败', { description: error instanceof Error ? error.message : '请确认文件为 docker save 导出的 tar。' })
    } finally {
      setBusy(false)
    }
  }

  async function startPull() {
    if (!pullImage.trim()) {
      toast.error('请输入镜像名')
      return
    }
    try {
      const task = await startImagePull(pullImage.trim())
      setPullOpen(false)
      setPullImage('')
      toast.success(task.state === 'queued' ? '拉取任务已排队' : '已开始拉取镜像', { description: task.image })
      refreshTasks()
    } catch (error) {
      toast.error('启动拉取失败', { description: error instanceof Error ? error.message : '请稍后重试。' })
    }
  }

  async function startBuild() {
    if (!currentBuildSpec.image.trim()) {
      toast.error('请输入构建后的镜像名')
      return
    }
    try {
      const task = await startImageBuild(currentBuildSpec)
      setBuildOpen(false)
      openLogs(task)
      toast.success(task.state === 'queued' ? '构建任务已排队' : '已开始构建镜像', { description: task.image })
      refreshTasks()
    } catch (error) {
      toast.error('启动构建失败', { description: error instanceof Error ? error.message : '请稍后重试。' })
    }
  }

  function stopTask(task: ImageTask) {
    const label = task.kind === 'build' ? '构建' : '拉取'
    askConfirm({
      title: `停止${label}任务`,
      description: `确认停止${label}任务 ${task.image}？已生成的中间结果会按 Docker 规则保留或清理。`,
      confirmLabel: '停止任务',
      danger: true,
      action: async () => {
        try {
          await cancelImageTask(task.id)
          toast.success(`${label}任务已停止`, { description: task.image })
          refreshTasks()
        } catch (error) {
          toast.error(`停止${label}任务失败`, { description: error instanceof Error ? error.message : '请稍后重试。' })
        }
      },
    })
  }

  function removeTask(task: ImageTask) {
    askConfirm({
      title: '删除任务记录',
      description: `将删除 ${task.image} 的任务记录，不会删除镜像或构建产物。`,
      confirmLabel: '删除记录',
      danger: true,
      action: async () => {
        try {
          await deleteImageTask(task.id)
          toast.success('任务记录已删除', { description: task.image })
          refreshTasks()
        } catch (error) {
          toast.error('删除任务记录失败', { description: error instanceof Error ? error.message : '请稍后重试。' })
        }
      },
    })
  }

  function pruneCache() {
    askConfirm({
      title: '清理 Docker 构建缓存',
      description: '将清理全部构建缓存，下次构建会重新下载依赖，耗时会明显增加。镜像本身不受影响。',
      confirmLabel: '清理构建缓存',
      danger: true,
      action: async () => {
        setPruning(true)
        try {
          const reclaimed = await pruneImageBuildCache()
          toast.success('构建缓存已清理', { description: `回收空间 ${formatBytes(reclaimed)}` })
          getImageDiskUsage().then(setDiskUsage).catch(() => {})
        } catch (error) {
          toast.error('清理构建缓存失败', { description: error instanceof Error ? error.message : '请稍后重试。' })
        } finally {
          setPruning(false)
        }
      },
    })
  }

  function renderImageTable(title: string, description: string, rows: ImageTagRow[], emptyTitle: string) {
    const groups = groupTagRows(rows)
    return <Panel>
      <PanelHeader title={title} description={description} action={<button type="button" className="btn-secondary" onClick={refresh} disabled={loading}><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />刷新</button>} />
      <div className="overflow-x-auto"><table className="data-table min-w-[1080px]"><thead><tr><th>镜像标签</th><th>镜像简述参数</th><th>构建进度</th><th>镜像 ID</th><th>大小</th><th>创建时间</th><th>操作</th></tr></thead><tbody>
        {groups.map((group) => {
          const task = group.task
          const active = task ? isActiveTask(task) : false
          const primaryTag = group.tags[0] ?? ''
          const summary = task?.summary?.length ? task.summary : imageBuildSummary(group.image)
          return <tr key={group.key}>
            <td>
              <div className="flex flex-wrap items-center gap-1.5">
                {group.tags.map((tag) => <span key={tag} className="rounded-md bg-slate-100 px-2 py-1 font-mono text-xs text-slate-800">{tag}</span>)}
                {group.virtual && task && <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${task.state === 'queued' ? 'bg-amber-100 text-amber-800' : 'bg-teal-100 text-teal-800'}`}>{task.state === 'queued' ? '排队中' : '构建中'}</span>}
              </div>
              {group.tags.length > 1 && <div className="mt-1.5 text-[11px] text-slate-500">同一镜像 · {group.tags.length} 个标签，不重复占用磁盘</div>}
            </td>
            <td>{summary.length > 0 ? <div className="flex max-w-[320px] flex-wrap gap-1">{summary.slice(0, 4).map((item) => <span key={`${primaryTag}-${item.label}`} className="rounded-full bg-slate-100 px-2 py-1 text-[11px] text-slate-600">{item.label}：{item.value}</span>)}</div> : <span className="text-xs text-slate-400">未记录</span>}</td>
            <td>{task && active ? <div className="min-w-28"><div className="h-2 overflow-hidden rounded-full bg-slate-100"><div className="h-full bg-teal-600" style={{ width: `${Math.max(1, Math.min(100, task.progress || 1))}%` }} /></div><span className="mt-1 block text-xs text-slate-500">{task.state === 'queued' ? '排队等待中' : `${task.progress || 1}% · 进行中`}</span></div> : <span className="text-xs text-slate-400">—</span>}</td>
            <td className="font-mono text-xs text-slate-500">{group.image?.Id.slice(0, 20) ?? '等待 Docker 生成'}</td>
            <td>{group.image ? formatBytes(group.image.Size) : '—'}</td>
            <td>{group.image ? new Date(group.image.Created * 1000).toLocaleString('zh-CN', { hour12: false }) : '—'}</td>
            <td><div className="flex items-center gap-1">
              {task && task.logs.length > 0 && <button type="button" className="icon-button" title="构建日志" onClick={() => openLogs(task)}><ScrollText className="size-4" /></button>}
              {task && active && <button type="button" className="icon-button text-rose-600 hover:bg-rose-50" title="停止构建" aria-label={`停止构建 ${primaryTag}`} onClick={() => stopTask(task)}><Ban className="size-4" /></button>}
              {group.image && <button type="button" className="icon-button" title="管理标签" aria-label={`管理标签 ${primaryTag}`} disabled={busy} onClick={() => openTagManager(group)}><Tag className="size-4" /></button>}
              {group.image && <button type="button" className="icon-button" title="导出 tar" aria-label={`导出 ${primaryTag}`} onClick={() => window.open(exportLocalImageURL(primaryTag), '_blank', 'noopener')}><Download className="size-4" /></button>}
              {group.image && <button type="button" className="icon-button text-rose-600 hover:bg-rose-50" title={group.tags.length > 1 ? '删除当前标签' : '删除镜像'} aria-label={`删除 ${primaryTag}`} disabled={busy} onClick={() => requestRemoveTag(primaryTag, group.tags.length)}><Trash2 className="size-4" /></button>}
            </div></td>
          </tr>
        })}
      </tbody></table></div>
      {rows.length === 0 && <EmptyState title={emptyTitle} description="可通过导入 tar、在线拉取或手动构建来准备镜像。" />}
    </Panel>
  }

  // 构建与拉取任务面板：所有未自动清理的记录在此可见，支持停止与删除。
  const taskPanel = tasks.length > 0 && <Panel>
    <PanelHeader title="构建与拉取任务" description="最多 2 个构建 + 1 个拉取并行，超出自动排队；成功入库的记录会自动清理，失败/取消的记录可手动删除。" />
    <div className="divide-y divide-slate-100">{tasks.slice(0, 10).map((task) => <div key={task.id} className="flex flex-col gap-2 px-5 py-4 sm:flex-row sm:items-center sm:gap-4">
      <span className={`flex size-8 shrink-0 items-center justify-center rounded-lg ${task.kind === 'build' ? 'bg-teal-50 text-teal-700' : 'bg-sky-50 text-sky-700'}`}>{task.kind === 'build' ? <Hammer className="size-4" /> : <CloudDownload className="size-4" />}</span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2"><span className="truncate font-mono text-xs font-semibold text-slate-900">{task.image}</span><TaskStateBadge state={task.state} /></div>
        <div className="mt-1 truncate text-xs text-slate-500" title={task.message}>{task.message} · {new Date(task.createdAt).toLocaleString('zh-CN', { hour12: false })}</div>
        {task.kind === 'build' && task.state === 'running' && <div className="mt-2 flex max-w-xs items-center gap-2"><div className="h-1.5 flex-1 overflow-hidden rounded-full bg-slate-100"><div className="h-full bg-teal-600" style={{ width: `${Math.max(1, Math.min(100, task.progress || 1))}%` }} /></div><span className="text-xs text-slate-500">{task.progress || 1}%</span></div>}
      </div>
      <div className="flex shrink-0 items-center gap-1">
        {task.logs.length > 0 && <button type="button" className="btn-quiet px-2 text-xs" onClick={() => openLogs(task)}><ScrollText className="size-3.5" />日志</button>}
        {isActiveTask(task) && <button type="button" className="btn-quiet px-2 text-xs text-rose-600 hover:bg-rose-50" onClick={() => void stopTask(task)}><Ban className="size-3.5" />停止</button>}
        {!isActiveTask(task) && <button type="button" className="icon-button size-9 text-slate-400 hover:text-rose-600" title="删除任务记录" aria-label={`删除任务记录 ${task.image}`} onClick={() => void removeTask(task)}><XCircle className="size-4" /></button>}
      </div>
    </div>)}</div>
  </Panel>

  const dialogs = <>
    <Dialog open={buildOpen} onOpenChange={setBuildOpen}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Hammer className="size-5 text-teal-700" />一键构建 ComfyUI 镜像</DialogTitle>
          <DialogDescription>选择 CPU 架构、GPU/PyTorch 配方、ComfyUI 版本和依赖源；最多 2 个构建并行，超出自动排队，任务可在清单中随时停止。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid gap-4 sm:grid-cols-2"><label className="field-label">CPU 芯片架构<select className="field-input" value={platform} onChange={(event) => setPlatform(event.target.value)}>{platformOptions.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select></label><label className="field-label">GPU / PyTorch 配方<select className="field-input" value={recipeId} onChange={(event) => onRecipeChange(event.target.value)}>{recipes.map((recipe) => <option key={recipe.id} value={recipe.id}>{recipe.label}</option>)}</select></label></div>
          <div className="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm leading-6 text-slate-600"><p><Cpu className="mr-1 inline size-4" />{selectedPlatform.description}</p>{selectedRecipe && <p><Layers className="mr-1 inline size-4" />{selectedRecipe.description}；建议镜像名 <span className="font-mono">{selectedRecipe.image}</span>。</p>}</div>
          <div className="grid gap-4 sm:grid-cols-2"><label className="field-label">镜像名<input className="field-input font-mono text-sm" value={imageName} onChange={(event) => setImageName(event.target.value)} placeholder="flowpilot/comfyui:cu130" /></label><label className="field-label">ComfyUI 版本（在线选择或手动输入）<input className="field-input font-mono text-sm" list="comfyui-refs" value={ref} onChange={(event) => setRef(event.target.value)} placeholder="master 或标签" /><datalist id="comfyui-refs">{refs.map((item) => <option key={`${item.type}-${item.name}`} value={item.name}>{item.name} · {item.type === 'branch' ? '分支' : '标签'}</option>)}</datalist></label></div>
          <div className="grid gap-4 sm:grid-cols-2"><label className="field-label">pip 源<select className="field-input" value={pip} onChange={(event) => setPip(event.target.value)}><option value="tsinghua">清华镜像</option><option value="aliyun">阿里云镜像</option><option value="pypi">PyPI 官方</option></select></label></div>
          <label className="flex min-h-11 cursor-pointer items-center gap-2 text-sm text-slate-700"><input type="checkbox" className="size-4 accent-teal-700" checked={manager} onChange={(event) => setManager(event.target.checked)} />内置 ComfyUI-Manager</label>
          <div className="rounded-lg border border-slate-200 p-3 text-xs leading-6 text-slate-500"><p className="font-semibold text-slate-700">参数简述</p><p>CPU 架构：{selectedPlatform.label}</p><p>GPU 配方：{selectedRecipe?.label ?? recipeId}</p><p>ComfyUI 版本：{currentBuildSpec.ref}</p><p>pip 源：{pip}；Manager：{manager ? '内置' : '不内置'}</p></div>
        </div>
        <DialogFooter>
          <a className="btn-secondary" href={buildScriptURL(currentBuildSpec)} download="build-comfyui-image.sh"><FileText className="size-4" />下载同参数构建脚本</a>
          <button type="button" className="btn-primary" onClick={() => void startBuild()}><Hammer className="size-4" />开始构建</button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog open={pullOpen} onOpenChange={setPullOpen}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><CloudDownload className="size-5 text-teal-700" />拉取在线容器镜像</DialogTitle>
          <DialogDescription>输入 Docker Hub、私有仓库或镜像代理中的完整镜像名；拉取任务进入任务列表，可随时停止。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4"><label className="field-label">在线镜像名<input className="field-input font-mono text-sm" value={pullImage} onChange={(event) => setPullImage(event.target.value)} placeholder="ghcr.io/xxx/comfyui:latest" /></label><p className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs leading-5 text-slate-500">建议拉取标签中包含 comfyui 的镜像；非 ComfyUI 镜像会进入「其他机器镜像」，不会出现在新建实例候选中。</p></div>
        <DialogFooter><button type="button" className="btn-primary" onClick={() => void startPull()}><CloudDownload className="size-4" />开始拉取</button></DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog open={Boolean(logsTask)} onOpenChange={(open) => !open && closeLogs()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader><DialogTitle className="flex items-center gap-2"><ScrollText className="size-5 text-teal-700" />{logsTask?.kind === 'pull' ? '拉取日志' : '构建日志'}</DialogTitle><DialogDescription>{logsTask ? `${logsTask.image} · ${taskStateText[logsTask.state] ?? logsTask.state}${logsTask.kind === 'build' ? ` · ${logsTask.progress || 0}%` : ''}` : '暂无任务'}</DialogDescription></DialogHeader>
        <pre className="max-h-[65dvh] overflow-auto rounded-lg bg-slate-950 p-4 font-mono text-xs leading-5 whitespace-pre-wrap text-slate-100">{logsTask?.logs?.length ? logsTask.logs.join('\n') : '等待任务开始。'}</pre>
      </DialogContent>
    </Dialog>

    <Dialog open={tagOpen} onOpenChange={(open) => { if (!open && !tagBusy) setTagOpen(false) }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Tag className="size-5 text-teal-700" />编辑镜像标签</DialogTitle>
          <DialogDescription>多个标签指向同一镜像，不会复制镜像层或重复占用磁盘。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs leading-5 text-slate-600">
            <div className="flex flex-wrap items-center justify-between gap-2"><span>镜像 ID</span><span className="font-mono text-slate-800">{tagImage?.Id.slice(0, 24)}</span></div>
            <div className="mt-1 flex flex-wrap items-center justify-between gap-2"><span>镜像大小</span><span>{tagImage ? formatBytes(tagImage.Size) : '—'}（多标签共用）</span></div>
            <div className="mt-3 flex flex-wrap gap-1.5">{tagList.map((tag) => <span key={tag} className="inline-flex items-center gap-1 rounded-md border border-slate-200 bg-white py-1 pr-1 pl-2 font-mono text-[11px] text-slate-700">{tag}<button type="button" className="rounded p-0.5 text-slate-400 hover:bg-rose-50 hover:text-rose-600" title={`删除标签 ${tag}`} aria-label={`删除标签 ${tag}`} onClick={() => requestRemoveTag(tag, tagList.length)}><Trash2 className="size-3" /></button></span>)}</div>
          </div>
          <label className="field-label">源标签<select className="field-input font-mono text-sm" value={tagSource} onChange={(event) => setTagSource(event.target.value)}>{tagList.map((tag) => <option key={tag} value={tag}>{tag}</option>)}</select></label>
          <label className="field-label">新标签<input className="field-input font-mono text-sm" value={tagTarget} onChange={(event) => setTagTarget(event.target.value)} placeholder="flowpilot/comfyui:cu130-new" autoComplete="off" /></label>
          <fieldset className="grid gap-2">
            <legend className="field-label">操作方式</legend>
            <label className={`flex cursor-pointer items-start gap-3 rounded-lg border p-3 text-sm ${tagMode === 'alias' ? 'border-teal-300 bg-teal-50/60' : 'border-slate-200'}`}><input type="radio" name="tag-mode" className="mt-1 accent-teal-700" checked={tagMode === 'alias'} onChange={() => setTagMode('alias')} /><span><strong className="block text-slate-800">添加别名（推荐）</strong><span className="mt-1 block text-xs leading-5 text-slate-500">保留源标签，新标签与源标签指向同一镜像。</span></span></label>
            <label className={`flex cursor-pointer items-start gap-3 rounded-lg border p-3 text-sm ${tagMode === 'rename' ? 'border-teal-300 bg-teal-50/60' : 'border-slate-200'}`}><input type="radio" name="tag-mode" className="mt-1 accent-teal-700" checked={tagMode === 'rename'} onChange={() => setTagMode('rename')} /><span><strong className="block text-slate-800">重命名（移除源标签）</strong><span className="mt-1 block text-xs leading-5 text-slate-500">源标签被实例使用时会被拒绝；请先添加别名或调整实例镜像。</span></span></label>
          </fieldset>
          {tagError && <p role="alert" className="rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700">{tagError}</p>}
        </div>
        <DialogFooter>
          <button type="button" className="btn-secondary" disabled={tagBusy} onClick={() => setTagOpen(false)}>取消</button>
          <button type="button" className="btn-primary" disabled={tagBusy || Boolean(tagImage && tagList.length === 0)} onClick={() => void submitTag()}>{tagBusy ? <LoaderCircle className="size-4 animate-spin" /> : <Tag className="size-4" />}{tagBusy ? '处理中' : tagMode === 'rename' ? '确认重命名' : '添加别名'}</button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog open={Boolean(confirmRequest)} onOpenChange={(open) => { if (!open && !confirmBusy) setConfirmRequest(null) }}>
      <DialogContent>
        {confirmRequest && <>
          <DialogHeader>
            <DialogTitle>{confirmRequest.title}</DialogTitle>
            <DialogDescription>{confirmRequest.description}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <button type="button" className="btn-secondary" disabled={confirmBusy} onClick={() => setConfirmRequest(null)}>取消</button>
            <button type="button" className={confirmRequest.danger ? 'btn-danger' : 'btn-primary'} disabled={confirmBusy} onClick={() => void executeConfirm()}>{confirmBusy ? <LoaderCircle className="size-4 animate-spin" /> : <Trash2 className="size-4" />}{confirmBusy ? '处理中' : confirmRequest.confirmLabel}</button>
          </DialogFooter>
        </>}
      </DialogContent>
    </Dialog>
  </>

  // 镜像中心单视图：入口卡片 + 镜像清单 + 任务列表 + 磁盘占用。
  return <div className="grid gap-5">
    <Panel>
      <PanelHeader title="镜像中心" description="构建、拉取、导入与管理本机镜像；任务支持并行与排队，可在任务列表中查看进度、停止或删除记录。" action={<div className="flex flex-wrap gap-2"><label className={`btn-secondary ${busy ? 'pointer-events-none opacity-60' : ''}`}><Upload className="size-4" />导入 tar<input type="file" accept=".tar,application/x-tar" className="sr-only" disabled={busy} onChange={(event) => void importImage(event.target.files?.[0])} /></label><button type="button" className="btn-secondary" onClick={refresh} disabled={loading}><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />刷新</button></div>} />
      <div className="grid gap-4 p-5 md:grid-cols-2">
        <button type="button" className="rounded-xl border border-slate-200 p-5 text-left transition-colors hover:border-teal-300 hover:bg-teal-50" onClick={() => setBuildOpen(true)}><Hammer className="mb-3 size-6 text-teal-700" /><span className="block font-semibold text-slate-900">一键构建 ComfyUI 镜像</span><span className="mt-2 block text-sm leading-6 text-slate-500">选择 CPU 架构、GPU 配方、ComfyUI 版本、pip 源和 Manager 开关，并可下载同参数构建脚本。</span></button>
        <button type="button" className="rounded-xl border border-slate-200 p-5 text-left transition-colors hover:border-teal-300 hover:bg-teal-50" onClick={() => setPullOpen(true)}><CloudDownload className="mb-3 size-6 text-teal-700" /><span className="block font-semibold text-slate-900">拉取在线容器镜像</span><span className="mt-2 block text-sm leading-6 text-slate-500">输入远程镜像名后台拉取；拉取完成后按 ComfyUI/其他镜像自动归类。</span></button>
      </div>
      {diskUsage && <div className="flex flex-col gap-2 border-t border-slate-200 px-5 py-3 text-xs text-slate-600 sm:flex-row sm:items-center sm:justify-between">
        <span>磁盘占用：镜像 {formatBytes(diskUsage.imagesBytes)}（{diskUsage.imagesCount} 个） · 构建缓存 {formatBytes(diskUsage.buildCacheBytes)}</span>
        <button type="button" className="btn-quiet px-2 text-xs" disabled={pruning} onClick={() => void pruneCache()}>{pruning ? <LoaderCircle className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}清理构建缓存</button>
      </div>}
    </Panel>
    {renderImageTable('ComfyUI 镜像', '仅此分类会出现在新建实例的镜像选择框中；构建任务会以虚拟行进入清单，展示百分比、参数简述、日志与停止入口。', comfyRowsWithVirtual, '暂无 ComfyUI 镜像')}
    {renderImageTable('机器其他镜像', '用于环境排查和维护，不会作为 ComfyUI 实例候选。', otherRows, '暂无其他镜像')}
    {taskPanel}
    {dialogs}
  </div>
}
