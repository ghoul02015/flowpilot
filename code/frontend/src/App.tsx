import { useEffect, useRef, useState } from 'react'
import { Activity, Box, ChevronRight, Database, LayoutDashboard, Menu, Server, Settings, Waves, X } from 'lucide-react'
import { Toaster, toast } from 'sonner'
import { createDownloadApi, createImportApi, createInstanceApi, deleteDownloadApi, deleteInstanceApi, deleteModelApi, downloadActionApi, formatBytes, getComfyStatus, getDownloads, getHardware, getHealth, getInstances, getModels, modelTypeToCategory, postInstanceAction, type CatalogEntry, type ComfyStatus, type DownloadTask, type InstanceDefaults, type ModelReport, type OnlineFile, type RealInstance } from '@/api'
import { CreateInstanceDialog } from '@/components/CreateInstanceDialog'
import { AddModelDialog } from '@/components/AddModelDialog'
import { Dashboard } from '@/pages/Dashboard'
import { Instances } from '@/pages/Instances'
import { Models } from '@/pages/Models'
import { Tasks } from '@/pages/Tasks'
import { System } from '@/pages/System'
import type { AddModelInput, CreateInstanceInput, Instance, ModelItem, PageKey, TaskItem } from '@/types'

const navigation: { key: PageKey; label: string; icon: typeof LayoutDashboard }[] = [
  { key: 'dashboard', label: '首页看板', icon: LayoutDashboard },
  { key: 'instances', label: 'ComfyUI 实例', icon: Box },
  { key: 'models', label: '模型中心', icon: Database },
  { key: 'tasks', label: '任务中心', icon: Activity },
  { key: 'system', label: '系统管理', icon: Settings },
]

const pageNames: Record<PageKey, string> = { dashboard: '首页看板', instances: 'ComfyUI 实例', models: '模型中心', tasks: '任务中心', system: '系统管理' }

export default function App() {
  const [page, setPage] = useState<PageKey>('dashboard')
  const [mobileOpen, setMobileOpen] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [addModelOpen, setAddModelOpen] = useState(false)
  const [hostInfo, setHostInfo] = useState({ name: '正在连接…', gpuSummary: '—' })
  const [gpuNames, setGpuNames] = useState<string[]>([])
  // 全部业务数据来自真实后端；接口不可用时为 null，页面显示空态与离线提示。
  const [realInstances, setRealInstances] = useState<RealInstance[] | null>(null)
  const [instanceDefaults, setInstanceDefaults] = useState<InstanceDefaults | null>(null)
  const [realModels, setRealModels] = useState<ModelReport | null>(null)
  const [realDownloads, setRealDownloads] = useState<DownloadTask[] | null>(null)
  const [backendOnline, setBackendOnline] = useState(true)
  // 运行中实例的 ComfyUI 制图队列摘要（后端代理探测）。
  const [comfyStatuses, setComfyStatuses] = useState<Record<string, ComfyStatus>>({})
  // 实例操作进行中集合：请求发出到下一次列表刷新期间禁用该实例的操作按钮。
  const [busyIds, setBusyIds] = useState<string[]>([])
  // 同步镜像 busyIds，避免同一帧内双击穿透渲染快照的 includes 检查。
  const busyRef = useRef<Set<string>>(new Set())
  // 界面版本号跟随后端 /health，避免硬编码漂移；离线时回退常量。
  const [backendVersion, setBackendVersion] = useState('0.0.9.1')

  const realMode = realInstances !== null
  const realModelMode = realModels !== null

  // 主机名与 GPU 列表：硬件接口冷启动（如 nvidia-smi 首次调用）可能超时，
  // 失败后每 3 秒重试，避免单次失败导致整个会话无法创建 GPU 实例。
  useEffect(() => {
    let cancelled = false
    let retryTimer = 0
    async function load() {
      try {
        const report = await getHardware()
        if (cancelled) return
        setHostInfo({
          name: report.hostName,
          gpuSummary: report.gpus.length ? `${report.gpus.length} × ${report.gpus[0].name}` : '未检测到 GPU',
        })
        setGpuNames(report.gpus.map((gpu) => gpu.name))
      } catch {
        if (cancelled) return
        setHostInfo({ name: '硬件接口不可用', gpuSummary: '—' })
        retryTimer = window.setTimeout(() => void load(), 3000)
      }
    }
    void load()
    return () => {
      cancelled = true
      window.clearTimeout(retryTimer)
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    async function poll() {
      let online = false
      try {
        const next = await getInstances()
        if (!cancelled) {
          setRealInstances(next.instances)
          setInstanceDefaults(next.defaults)
          online = true
          // 并行探测运行中实例的 ComfyUI 制图队列；失败的实例本轮缺席。
          const running = next.instances.filter((item) => item.status === 'running')
          const statuses = await Promise.allSettled(running.map((item) => getComfyStatus(item.name)))
          if (!cancelled) {
            setComfyStatuses((previous) => {
              const merged = { ...previous }
              running.forEach((item, index) => {
                const result = statuses[index]
                if (result.status === 'fulfilled') merged[item.name] = result.value
              })
              return merged
            })
          }
        }
      } catch {
        if (!cancelled) setRealInstances(null)
      }
      try {
        const report = await getModels()
        if (!cancelled) {
          setRealModels(report)
          online = true
        }
      } catch {
        if (!cancelled) setRealModels(null)
      }
      try {
        const downloadTasks = await getDownloads()
        if (!cancelled) {
          setRealDownloads(downloadTasks)
          online = true
        }
      } catch {
        if (!cancelled) setRealDownloads(null)
      }
      if (!cancelled) setBackendOnline(online)
      if (online) {
        getHealth().then((health) => { if (!cancelled) setBackendVersion(health.version) }).catch(() => {})
      }
    }
    void poll()
    const timer = window.setInterval(() => void poll(), 3000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [])

  async function refreshInstances() {
    try {
      const next = await getInstances()
      setRealInstances(next.instances)
      setInstanceDefaults(next.defaults)
    } catch {
      setRealInstances(null)
    }
  }

  async function refreshModels() {
    try {
      setRealModels(await getModels())
    } catch {
      setRealModels(null)
    }
  }

  async function refreshDownloads() {
    try {
      setRealDownloads(await getDownloads())
    } catch {
      setRealDownloads(null)
    }
  }

  function requireBackend(action: string): boolean {
    if (backendOnline) return true
    toast.error(`${action}失败`, { description: 'FlowPilot 后端未连接，请确认服务已启动。' })
    return false
  }

  // 真实实例映射为界面模型；id 使用实例名称（全局唯一）。
  const displayInstances: Instance[] = (realInstances ?? []).map((item) => ({
    id: item.name,
    name: item.name,
    template: item.image,
    version: `容器 ${item.containerId}`,
    status: item.status,
    gpu: item.gpu,
    port: item.port,
    autoStart: item.autoStart,
    createdAt: new Date(item.createdAt).toLocaleString('zh-CN', { hour12: false }),
    uptime: item.status === 'running' ? '容器运行中' : item.status === 'stopped' ? '容器已停止' : '容器状态异常',
    modelsTarget: item.modelsTarget,
    outputsTarget: item.outputsTarget,
    workflowsTarget: item.workflowsTarget,
    comfy: comfyStatuses[item.name]
      ? {
        reachable: comfyStatuses[item.name].reachable,
        queueRunning: comfyStatuses[item.name].queueRunning,
        queuePending: comfyStatuses[item.name].queuePending,
        comfyuiVersion: comfyStatuses[item.name].comfyuiVersion,
      }
      : undefined,
  }))

  // 真实仓库文件映射为界面模型；id 使用相对路径（全局唯一）。
  // 进行中的真实下载任务以 dl- 前缀合入列表（完成任务由仓库文件体现）。
  const displayModels: ModelItem[] = realModels !== null
    ? [
      ...(realDownloads ?? [])
        .filter((task) => task.status !== 'completed')
        .map((task) => ({
          id: task.id,
          name: task.fileName,
          type: task.category === 'other' ? '其他' : task.category,
          size: task.totalBytes > 0 ? formatBytes(task.totalBytes) : '等待计算',
          source: task.kind === 'import' ? '本地导入' : 'URL 下载',
          status: task.status === 'running' ? 'downloading' : task.status === 'paused' ? 'paused' : task.status === 'failed' ? 'failed' : 'cancelled',
          progress: task.totalBytes > 0 ? Math.round((task.downloadedBytes / task.totalBytes) * 100) : 0,
          createdAt: new Date(task.createdAt).toLocaleString('zh-CN', { hour12: false }),
        } satisfies ModelItem)),
      ...realModels.files.map((file) => ({
        id: file.path,
        name: file.name,
        type: file.type,
        size: formatBytes(file.sizeBytes),
        source: '共享仓库',
        status: 'ready' as const,
        progress: 100,
        createdAt: new Date(file.modifiedAt).toLocaleString('zh-CN', { hour12: false }),
      })),
    ]
    : []

  // 真实下载／导入任务映射为任务中心条目。
  const displayTasks: TaskItem[] = (realDownloads ?? []).map((task) => ({
    id: task.id,
    name: `${task.kind === 'import' ? '导入' : '下载'} ${task.fileName}`,
    type: task.kind === 'import' ? '模型导入' : '模型下载',
    target: task.path,
    status: task.status,
    progress: task.totalBytes > 0 ? Math.round((task.downloadedBytes / task.totalBytes) * 100) : 0,
    createdAt: new Date(task.createdAt).toLocaleString('zh-CN', { hour12: false }),
    message: task.totalBytes > 0 ? `${task.message} · ${formatBytes(task.downloadedBytes)}／${formatBytes(task.totalBytes)}` : task.message,
  }))

  function navigate(target: PageKey) {
    setPage(target)
    setMobileOpen(false)
  }

  function openCreateDialog() {
    if (!requireBackend('打开新建实例向导')) return
    setCreateOpen(true)
  }

  function openAddModelDialog() {
    if (!requireBackend('打开添加模型')) return
    setAddModelOpen(true)
  }

  function instanceAction(id: string, action: 'start' | 'stop' | 'restart') {
    const target = displayInstances.find((item) => item.id === id)
    if (!target || !requireBackend('实例操作')) return
    if (busyRef.current.has(id)) return
    busyRef.current.add(id)
    const actionName = action === 'stop' ? '停止' : action === 'start' ? '启动' : '重启'
    setBusyIds((previous) => [...previous, id])
    postInstanceAction(target.name, action)
      .then(() => {
        toast.success(`实例已${actionName}`, { description: target.name })
      })
      .catch((error: unknown) => {
        toast.error(`实例${actionName}失败`, { description: error instanceof Error ? error.message : '请求失败' })
      })
      .finally(() => {
        void refreshInstances()
        busyRef.current.delete(id)
        setBusyIds((previous) => previous.filter((item) => item !== id))
      })
  }

  async function createInstance(input: CreateInstanceInput): Promise<boolean> {
    try {
      await createInstanceApi({
        name: input.name,
        image: input.image ?? instanceDefaults?.image ?? 'comfyui:latest',
        gpu: input.gpu,
        port: input.port,
        containerPort: input.containerPort ?? instanceDefaults?.containerPort ?? 8188,
        autoStart: input.autoStart,
        modelsTarget: input.modelsTarget,
        outputsTarget: input.outputsTarget,
        workflowsTarget: input.workflowsTarget,
      })
      toast.success('实例已创建并启动', { description: `${input.name} · GPU ${input.gpu} · 端口 ${input.port}` })
      await refreshInstances()
      return true
    } catch (error) {
      toast.error('创建实例失败', { description: error instanceof Error ? error.message : '请求失败' })
      return false
    }
  }

  function deleteInstance(id: string) {
    const target = displayInstances.find((item) => item.id === id)
    if (!target || !requireBackend('删除实例')) return
    deleteInstanceApi(target.name, target.status === 'running')
      .then(() => {
        toast.success('实例容器已删除', { description: `${target.name} · 共享模型目录保留` })
        void refreshInstances()
      })
      .catch((error: unknown) => {
        toast.error('删除实例失败', { description: error instanceof Error ? error.message : '请求失败' })
        void refreshInstances()
      })
  }

  // 返回是否创建成功：失败时弹窗保持打开，用户输入不丢失。
  async function addModel(input: AddModelInput): Promise<boolean> {
    if (!requireBackend('创建模型任务')) return false
    try {
      const task = input.sourceType === '本地导入'
        ? await createImportApi({ path: input.source, category: modelTypeToCategory(input.type), name: input.name || undefined })
        : await createDownloadApi({ url: input.source, category: modelTypeToCategory(input.type), name: input.name || undefined })
      toast.success(input.sourceType === '本地导入' ? '导入任务已创建' : '下载任务已创建', { description: `${task.fileName} → ${task.path}` })
      void refreshDownloads()
      return true
    } catch (error) {
      toast.error(input.sourceType === '本地导入' ? '创建导入任务失败' : '创建下载任务失败', { description: error instanceof Error ? error.message : '请求失败' })
      return false
    }
  }

  function pauseModel(id: string) {
    if (!requireBackend('下载操作')) return
    const task = (realDownloads ?? []).find((item) => item.id === id)
    if (!task) return
    const action = task.status === 'running' ? 'pause' : 'resume'
    downloadActionApi(id, action)
      .then(() => void refreshDownloads())
      .catch((error: unknown) => {
        toast.error('下载操作失败', { description: error instanceof Error ? error.message : '请求失败' })
        void refreshDownloads()
      })
  }

  function deleteModel(id: string) {
    // 下载任务条目（dl- 前缀）：取消并移除任务记录；仓库文件条目：物理删除文件。
    if (id.startsWith('dl-')) {
      if (!requireBackend('移除下载任务')) return
      const task = (realDownloads ?? []).find((item) => item.id === id)
      const remove = () => deleteDownloadApi(id).then(() => {
        toast.success('下载任务已移除', { description: id })
        void refreshDownloads()
      })
      const active = task && ['running', 'paused'].includes(task.status)
      ;(active ? downloadActionApi(id, 'cancel').then(() => undefined) : Promise.resolve())
        .then(remove)
        .catch((error: unknown) => {
          toast.error('移除下载任务失败', { description: error instanceof Error ? error.message : '请求失败' })
          void refreshDownloads()
        })
      return
    }
    if (!requireBackend('删除模型')) return
    deleteModelApi(id)
      .then(() => {
        toast.success('模型文件已删除', { description: id })
        void refreshModels()
      })
      .catch((error: unknown) => {
        toast.error('删除模型失败', { description: error instanceof Error ? error.message : '请求失败' })
        void refreshModels()
      })
  }

  // 模型市场：选中目录条目直接下载（ModelScope 主源，HF 镜像备选）。
  function marketDownload(entry: CatalogEntry, useFallback: boolean) {
    if (!requireBackend('创建下载任务')) return
    const url = useFallback ? entry.fallbackUrl : entry.url
    createDownloadApi({ url, category: entry.category, name: entry.fileName })
      .then((task) => {
        toast.success(`已开始下载 ${entry.name}`, { description: `${task.fileName} → ${task.path} · 来源 ${useFallback ? 'HF 镜像' : 'ModelScope'}` })
        void refreshDownloads()
      })
      .catch((error: unknown) => {
        toast.error('创建下载任务失败', { description: error instanceof Error ? error.message : '请求失败' })
      })
  }

  // 在线源：选中仓库权重文件直接下载（地址与类别由后端按 ComfyUI 约定推断）。
  function onlineDownload(file: OnlineFile) {
    if (!requireBackend('创建下载任务')) return
    createDownloadApi({ url: file.url, category: file.category, name: file.name })
      .then((task) => {
        toast.success(`已开始下载 ${file.name}`, { description: `${task.fileName} → ${task.path}` })
        void refreshDownloads()
      })
      .catch((error: unknown) => {
        toast.error('创建下载任务失败', { description: error instanceof Error ? error.message : '请求失败' })
      })
  }

  // 已入库或进行中任务占用的仓库路径，用于模型市场标注。
  const existingModelPaths = [
    ...(realModels?.files.map((file) => file.path) ?? []),
    ...(realDownloads ?? []).filter((task) => ['running', 'paused'].includes(task.status)).map((task) => task.path),
  ]

  function taskAction(id: string, action: 'cancel' | 'retry') {
    if (!requireBackend('任务操作')) return
    downloadActionApi(id, action)
      .then(() => {
        toast.success(action === 'cancel' ? '任务已取消' : '任务已重试')
        void refreshDownloads()
      })
      .catch((error: unknown) => {
        toast.error('任务操作失败', { description: error instanceof Error ? error.message : '请求失败' })
        void refreshDownloads()
      })
  }

  const activeTasks = displayTasks.filter((task) => ['running', 'paused'].includes(task.status)).length

  return <div className="min-h-dvh bg-[oklch(0.975_0.006_220)] text-slate-800">
    <Toaster position="top-right" richColors closeButton />
    {mobileOpen && <button type="button" aria-label="关闭导航" className="fixed inset-0 z-30 cursor-pointer bg-slate-950/45 md:hidden" onClick={() => setMobileOpen(false)} />}
    <aside className={`fixed inset-y-0 left-0 z-40 flex w-[248px] flex-col bg-[oklch(0.22_0.03_240)] text-slate-300 transition-transform duration-200 md:translate-x-0 ${mobileOpen ? 'translate-x-0' : '-translate-x-full'}`}>
      <div className="flex h-20 items-center gap-3 border-b border-white/8 px-5"><div className="flex size-10 items-center justify-center rounded-lg bg-teal-500 text-slate-950"><Waves className="size-6" /></div><div><div className="font-bold tracking-wide text-white">慧浪智算</div><div className="mt-0.5 text-xs font-medium tracking-[0.16em] text-teal-300">FLOWPILOT</div></div><button type="button" className="ml-auto cursor-pointer rounded-md p-2 text-slate-300 hover:bg-white/10 md:hidden" aria-label="关闭菜单" onClick={() => setMobileOpen(false)}><X className="size-5" /></button></div>
      <nav className="flex-1 space-y-1 px-3 py-5" aria-label="主导航">{navigation.map(({ key, label, icon: Icon }) => <button type="button" key={key} onClick={() => navigate(key)} className={`flex min-h-12 w-full cursor-pointer items-center gap-3 rounded-lg px-3 text-sm font-medium transition-colors focus-visible:outline-teal-300 ${page === key ? 'bg-white/10 text-white' : 'text-slate-400 hover:bg-white/5 hover:text-white'}`}><Icon className={`size-5 ${page === key ? 'text-teal-300' : ''}`} /><span className="flex-1 text-left">{label}</span>{key === 'tasks' && activeTasks > 0 && <span className="rounded-full bg-teal-400/15 px-2 py-0.5 text-xs text-teal-300">{activeTasks}</span>}</button>)}</nav>
      <div className="mx-3 mb-3 rounded-lg border border-white/8 p-4"><div className="flex items-center gap-2 text-sm font-medium text-white"><span className={`size-2 rounded-full ${backendOnline ? 'bg-emerald-400' : 'bg-rose-400'}`} />{hostInfo.name}</div><div className="mt-2 text-xs leading-5 text-slate-400">{hostInfo.gpuSummary}<br />{backendOnline ? '管理端运行正常' : '等待后端连接'}</div></div>
      <div className="border-t border-white/8 px-5 py-4 text-xs text-slate-500">FlowPilot · v{backendVersion}</div>
    </aside>

    <div className="min-h-dvh md:pl-[248px]">
      <header className="sticky top-0 z-20 flex h-16 items-center gap-3 border-b border-slate-200 bg-[oklch(0.995_0.003_220)]/95 px-4 backdrop-blur md:px-8"><button type="button" className="icon-button md:hidden" aria-label="打开菜单" onClick={() => setMobileOpen(true)}><Menu className="size-5" /></button><div className="hidden items-center gap-2 text-sm text-slate-500 sm:flex"><Server className="size-4" /><span>{hostInfo.name}</span><ChevronRight className="size-3.5" /><span className="text-slate-800">{pageNames[page]}</span></div><div className="ml-auto flex items-center gap-2"><span className={`hidden rounded-full border px-3 py-1.5 text-xs font-medium sm:inline ${backendOnline ? 'border-emerald-200 bg-emerald-50 text-emerald-800' : 'border-rose-200 bg-rose-50 text-rose-800'}`}>{backendOnline ? '后端已连接' : '后端连接失败'}</span><button type="button" className="btn-quiet" onClick={() => navigate('tasks')}><Activity className="size-4" /><span className="hidden sm:inline">任务</span>{activeTasks > 0 && <span className="rounded-full bg-teal-100 px-1.5 text-xs text-teal-800">{activeTasks}</span>}</button><div className="flex size-9 items-center justify-center rounded-full bg-slate-900 text-xs font-semibold text-white" title="管理员">AD</div></div></header>
      <main className="mx-auto max-w-[1600px] p-4 md:p-8">
        {page === 'dashboard' && <Dashboard instances={displayInstances} models={displayModels} tasks={displayTasks} gpuCount={gpuNames.length} gpuSummary={hostInfo.gpuSummary} modelsTotal={realModels ? formatBytes(realModels.totalBytes) : undefined} onNavigate={navigate} onCreate={openCreateDialog} onAddModel={openAddModelDialog} />}
        {page === 'instances' && <Instances instances={displayInstances} gpuNames={gpuNames} realMode={realMode} busyIds={busyIds} onCreate={openCreateDialog} onAction={instanceAction} onDelete={deleteInstance} />}
        {page === 'models' && <Models models={displayModels} realMode={realModelMode} modelsRoot={realModels?.root} totalBytes={realModels?.totalBytes} hasRunningInstances={displayInstances.some((item) => item.status === 'running')} onAdd={openAddModelDialog} onPause={pauseModel} onDelete={deleteModel} />}
        {page === 'tasks' && <Tasks tasks={displayTasks} onAction={taskAction} />}
        {page === 'system' && <System instances={displayInstances} models={displayModels} tasks={displayTasks} version={backendVersion} />}
      </main>
    </div>
    <CreateInstanceDialog key={createOpen ? 'create-open' : 'create-closed'} open={createOpen} onOpenChange={setCreateOpen} onSubmit={createInstance} models={displayModels} instances={displayInstances} gpuNames={gpuNames} defaultImage={instanceDefaults?.image} defaultContainerPort={instanceDefaults?.containerPort} dataDir={instanceDefaults?.dataDir} defaultModelsTarget={instanceDefaults?.modelsTarget} defaultOutputsTarget={instanceDefaults?.outputsTarget} defaultWorkflowsTarget={instanceDefaults?.workflowsTarget} />
    <AddModelDialog key={addModelOpen ? 'model-open' : 'model-closed'} open={addModelOpen} onOpenChange={setAddModelOpen} onSubmit={addModel} onMarketDownload={marketDownload} onOnlineDownload={onlineDownload} existingPaths={existingModelPaths} />
  </div>
}
