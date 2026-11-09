import { useCallback, useEffect, useMemo, useState } from 'react'
import { Ban, CloudDownload, Cpu, Download, FileText, Hammer, Layers, LoaderCircle, RefreshCw, ScrollText, Tag, Trash2, Upload, XCircle } from 'lucide-react'
import { toast } from 'sonner'
import { buildScriptURL, cancelImageTask, deleteImageTask, deleteLocalImageApi, exportLocalImageURL, formatBytes, getComfyUIRefs, getImageCatalog, getImageDiskUsage, getImageTasks, getLocalImages, imageBuildSummary, importLocalImageApi, isComfyImage, pruneImageBuildCache, startImageBuild, startImagePull, tagLocalImageApi, type ComfyRef, type ImageBuildSpec, type ImageDiskUsage, type ImageRecipe, type ImageTask, type LocalImage } from '@/api'
import { EmptyState, Panel, PanelHeader } from '@/components/common'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

interface Props {
  mode: 'manage' | 'build'
}

interface ImageTagRow {
  tag: string
  image?: LocalImage
  task?: ImageTask
  virtual?: boolean
}

const platformOptions = [
  { value: 'host', label: '宿主机架构', description: '在当前机器本机架构构建，适合快速验证。' },
  { value: 'linux/arm64', label: 'linux/arm64（aarch64）', description: '适合 DGX Spark / ARM 集群节点分发。' },
  { value: 'linux/amd64', label: 'linux/amd64（x86_64）', description: '适合通用 x86_64 GPU 服务器。' },
  { value: 'linux/arm/v7', label: 'linux/arm/v7', description: '预留给边缘 ARMv7 节点，需宿主机支持跨架构构建。' },
]

const taskStateText: Record<string, string> = { queued: '排队中', running: '进行中', done: '已完成', error: '失败', cancelled: '已取消', interrupted: '已中断' }

function isActiveTask(task: ImageTask): boolean {
  return task.state === 'queued' || task.state === 'running'
}

function TaskStateBadge({ state }: { state: string }) {
  const tone = state === 'done' ? 'bg-emerald-50 text-emerald-700' : state === 'running' ? 'bg-teal-50 text-teal-700' : state === 'queued' ? 'bg-amber-50 text-amber-700' : state === 'error' ? 'bg-rose-50 text-rose-700' : 'bg-slate-100 text-slate-600'
  return <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium ${tone}`}>{state === 'running' && <LoaderCircle className="size-3 animate-spin" />}{taskStateText[state] ?? state}</span>
}

export function ImageManager({ mode }: Props) {
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
  const logsTask = tasks.find((task) => task.id === logsTaskId)

  const currentBuildSpec = useMemo<ImageBuildSpec>(() => ({ variant: recipeId, image: imageName.trim() || selectedRecipe?.image || 'flowpilot/comfyui:local', ref: ref.trim() || 'master', pip, manager: manager ? '1' : '0', platform }), [imageName, manager, pip, platform, recipeId, ref, selectedRecipe?.image])

  function onRecipeChange(next: string) {
    setRecipeId(next)
    const recipe = recipes.find((item) => item.id === next)
    if (recipe) setImageName(recipe.image)
  }

  async function retag(source: string) {
    const target = window.prompt('请输入新的镜像标签，例如 flowpilot/comfyui:cu130-test', source)
    if (!target || target === source) return
    setBusy(true)
    try {
      await tagLocalImageApi(source, target)
      toast.success('镜像标签已更新', { description: `${source} → ${target}` })
      refresh()
    } catch (error) {
      toast.error('修改镜像标签失败', { description: error instanceof Error ? error.message : '请稍后重试。' })
    } finally {
      setBusy(false)
    }
  }

  async function remove(tag: string) {
    if (!window.confirm(`确认删除本机镜像标签 ${tag}？如果镜像被容器使用，Docker 会拒绝删除。`)) return
    setBusy(true)
    try {
      await deleteLocalImageApi(tag)
      toast.success('镜像已删除', { description: tag })
      refresh()
    } catch (error) {
      toast.error('删除镜像失败', { description: error instanceof Error ? error.message : '请确认没有实例正在使用该镜像。' })
    } finally {
      setBusy(false)
    }
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
      setLogsTaskId(task.id)
      toast.success(task.state === 'queued' ? '构建任务已排队' : '已开始构建镜像', { description: task.image })
      refreshTasks()
    } catch (error) {
      toast.error('启动构建失败', { description: error instanceof Error ? error.message : '请稍后重试。' })
    }
  }

  async function stopTask(task: ImageTask) {
    const label = task.kind === 'build' ? '构建' : '拉取'
    if (!window.confirm(`确认停止${label}任务 ${task.image}？`)) return
    try {
      await cancelImageTask(task.id)
      toast.success(`${label}任务已停止`, { description: task.image })
      refreshTasks()
    } catch (error) {
      toast.error(`停止${label}任务失败`, { description: error instanceof Error ? error.message : '请稍后重试。' })
    }
  }

  async function removeTask(task: ImageTask) {
    try {
      await deleteImageTask(task.id)
      toast.success('任务记录已删除', { description: task.image })
      refreshTasks()
    } catch (error) {
      toast.error('删除任务记录失败', { description: error instanceof Error ? error.message : '请稍后重试。' })
    }
  }

  async function pruneCache() {
    if (!window.confirm('确认清理全部 Docker 构建缓存？下次构建将从头下载依赖，耗时会明显增加。')) return
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
  }

  function renderImageTable(title: string, description: string, rows: ImageTagRow[], emptyTitle: string) {
    return <Panel>
      <PanelHeader title={title} description={description} action={<button type="button" className="btn-secondary" onClick={refresh} disabled={loading}><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />刷新</button>} />
      <div className="overflow-x-auto"><table className="data-table min-w-[1080px]"><thead><tr><th>镜像标签</th><th>镜像简述参数</th><th>构建进度</th><th>镜像 ID</th><th>大小</th><th>创建时间</th><th>操作</th></tr></thead><tbody>
        {rows.map((row) => {
          const task = row.task
          const active = task ? isActiveTask(task) : false
          const summary = task?.summary?.length ? task.summary : imageBuildSummary(row.image)
          return <tr key={`${row.image?.Id ?? task?.id ?? 'building'}-${row.tag}`}>
            <td className="font-mono text-xs">{row.tag}{row.virtual && task && <span className={`ml-2 rounded-full px-2 py-0.5 text-[11px] font-medium ${task.state === 'queued' ? 'bg-amber-100 text-amber-800' : 'bg-teal-100 text-teal-800'}`}>{task.state === 'queued' ? '排队中' : '构建中'}</span>}</td>
            <td>{summary.length > 0 ? <div className="flex max-w-[320px] flex-wrap gap-1">{summary.slice(0, 4).map((item) => <span key={`${row.tag}-${item.label}`} className="rounded-full bg-slate-100 px-2 py-1 text-[11px] text-slate-600">{item.label}：{item.value}</span>)}</div> : <span className="text-xs text-slate-400">未记录</span>}</td>
            <td>{task && active ? <div className="min-w-28"><div className="h-2 overflow-hidden rounded-full bg-slate-100"><div className="h-full bg-teal-600" style={{ width: `${Math.max(1, Math.min(100, task.progress || 1))}%` }} /></div><span className="mt-1 block text-xs text-slate-500">{task.state === 'queued' ? '排队等待中' : `${task.progress || 1}% · 进行中`}</span></div> : <span className="text-xs text-slate-400">—</span>}</td>
            <td className="font-mono text-xs text-slate-500">{row.image?.Id.slice(0, 20) ?? '等待 Docker 生成'}</td>
            <td>{row.image ? formatBytes(row.image.Size) : '—'}</td>
            <td>{row.image ? new Date(row.image.Created * 1000).toLocaleString('zh-CN', { hour12: false }) : '—'}</td>
            <td><div className="flex items-center gap-1">
              {task && task.logs.length > 0 && <button type="button" className="icon-button" title="构建日志" onClick={() => setLogsTaskId(task.id)}><ScrollText className="size-4" /></button>}
              {task && active && <button type="button" className="icon-button text-rose-600 hover:bg-rose-50" title="停止构建" aria-label={`停止构建 ${row.tag}`} onClick={() => void stopTask(task)}><Ban className="size-4" /></button>}
              {row.image && <button type="button" className="icon-button" title="改标签" disabled={busy} onClick={() => void retag(row.tag)}><Tag className="size-4" /></button>}
              {row.image && <button type="button" className="icon-button" title="导出 tar" onClick={() => window.open(exportLocalImageURL(row.tag), '_blank', 'noopener')}><Download className="size-4" /></button>}
              {row.image && <button type="button" className="icon-button text-rose-600 hover:bg-rose-50" title="删除镜像" disabled={busy} onClick={() => void remove(row.tag)}><Trash2 className="size-4" /></button>}
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
        {task.logs.length > 0 && <button type="button" className="btn-quiet px-2 text-xs" onClick={() => setLogsTaskId(task.id)}><ScrollText className="size-3.5" />日志</button>}
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
          <div className="grid gap-4 sm:grid-cols-2"><label className="field-label">镜像名<input className="field-input font-mono text-sm" value={imageName} onChange={(event) => setImageName(event.target.value)} placeholder="flowpilot/comfyui:cu130" /></label><label className="field-label">ComfyUI 在线版本<select className="field-input font-mono text-sm" value={ref} onChange={(event) => setRef(event.target.value)}>{refs.map((item) => <option key={`${item.type}-${item.name}`} value={item.name}>{item.name} · {item.type === 'branch' ? '分支' : '标签'}</option>)}</select></label></div>
          <div className="grid gap-4 sm:grid-cols-2"><label className="field-label">ComfyUI 版本手动修正<input className="field-input font-mono text-sm" value={ref} onChange={(event) => setRef(event.target.value)} placeholder="master 或标签" /></label><label className="field-label">pip 源<select className="field-input" value={pip} onChange={(event) => setPip(event.target.value)}><option value="tsinghua">清华镜像</option><option value="aliyun">阿里云镜像</option><option value="pypi">PyPI 官方</option></select></label></div>
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

    <Dialog open={Boolean(logsTask)} onOpenChange={(open) => !open && setLogsTaskId(null)}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader><DialogTitle className="flex items-center gap-2"><ScrollText className="size-5 text-teal-700" />{logsTask?.kind === 'pull' ? '拉取日志' : '构建日志'}</DialogTitle><DialogDescription>{logsTask ? `${logsTask.image} · ${taskStateText[logsTask.state] ?? logsTask.state}${logsTask.kind === 'build' ? ` · ${logsTask.progress || 0}%` : ''}` : '暂无任务'}</DialogDescription></DialogHeader>
        <pre className="max-h-[65dvh] overflow-auto rounded-lg bg-slate-950 p-4 font-mono text-xs leading-5 whitespace-pre-wrap text-slate-100">{logsTask?.logs?.length ? logsTask.logs.join('\n') : '等待任务开始。'}</pre>
      </DialogContent>
    </Dialog>
  </>

  if (mode === 'manage') return <div className="grid gap-5">
    <Panel>
      <PanelHeader title="容器镜像管理" description="镜像分为 ComfyUI 镜像与机器其他镜像；新建实例只会从 ComfyUI 镜像中选择。" action={<div className="flex flex-wrap gap-2"><label className={`btn-secondary ${busy ? 'pointer-events-none opacity-60' : ''}`}><Upload className="size-4" />导入 tar<input type="file" accept=".tar,application/x-tar" className="sr-only" disabled={busy} onChange={(event) => void importImage(event.target.files?.[0])} /></label><button type="button" className="btn-secondary" onClick={refresh} disabled={loading}><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />刷新</button></div>} />
      {diskUsage && <div className="flex flex-col gap-2 border-t border-slate-200 px-5 py-3 text-xs text-slate-600 sm:flex-row sm:items-center sm:justify-between">
        <span>磁盘占用：镜像 {formatBytes(diskUsage.imagesBytes)}（{diskUsage.imagesCount} 个） · 构建缓存 {formatBytes(diskUsage.buildCacheBytes)}</span>
        <button type="button" className="btn-quiet px-2 text-xs" disabled={pruning} onClick={() => void pruneCache()}>{pruning ? <LoaderCircle className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}清理构建缓存</button>
      </div>}
    </Panel>
    {renderImageTable('ComfyUI 镜像', '仅此分类会出现在新建实例的镜像选择框中。', comfyRowsWithVirtual, '暂无 ComfyUI 镜像')}
    {renderImageTable('机器其他镜像', '用于环境排查和维护，不会作为 ComfyUI 实例候选。', otherRows, '暂无其他镜像')}
    {taskPanel}
    {dialogs}
  </div>

  return <div className="grid gap-5">
    <Panel>
      <PanelHeader title="手动构建 / 在线拉取" description="构建和拉取已拆为两个弹窗；任务支持并行与排队，可在下方任务列表中查看进度、停止或删除记录。" />
      <div className="grid gap-4 p-5 md:grid-cols-2">
        <button type="button" className="rounded-xl border border-slate-200 p-5 text-left transition-colors hover:border-teal-300 hover:bg-teal-50" onClick={() => setBuildOpen(true)}><Hammer className="mb-3 size-6 text-teal-700" /><span className="block font-semibold text-slate-900">一键构建 ComfyUI 镜像</span><span className="mt-2 block text-sm leading-6 text-slate-500">选择 CPU 架构、GPU 配方、ComfyUI 版本、pip 源和 Manager 开关，并可下载同参数构建脚本。</span></button>
        <button type="button" className="rounded-xl border border-slate-200 p-5 text-left transition-colors hover:border-teal-300 hover:bg-teal-50" onClick={() => setPullOpen(true)}><CloudDownload className="mb-3 size-6 text-teal-700" /><span className="block font-semibold text-slate-900">拉取在线容器镜像</span><span className="mt-2 block text-sm leading-6 text-slate-500">输入远程镜像名后台拉取；拉取完成后按 ComfyUI/其他镜像自动归类。</span></button>
      </div>
    </Panel>
    {renderImageTable('ComfyUI 镜像清单', '构建任务会以虚拟行进入清单，展示百分比、参数简述、日志与停止入口。', comfyRowsWithVirtual, '暂无 ComfyUI 镜像')}
    {taskPanel}
    {dialogs}
  </div>
}
