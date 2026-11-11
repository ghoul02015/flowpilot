import { useEffect, useRef, useState } from 'react'
import { Activity, Box, ChevronRight, Cpu, Database, Images, LayoutDashboard, LogOut, Menu, Server, Settings, Waves, X } from 'lucide-react'
import { Toaster, toast } from 'sonner'
import { createDownloadApi, createImportApi, createInstanceApi, deleteDownloadApi, deleteInstanceApi, deleteModelApi, downloadActionApi, formatBytes, getComfyStatus, getDownloads, getHardware, getHealth, getInstances, getModels, getSetupStatus, getSession, logout, modelTypeToCategory, postInstanceAction, setCSRFToken, setUnauthorizedHandler, type CatalogEntry, type ComfyStatus, type DownloadTask, type InstanceDefaults, type ModelReport, type OnlineFile, type RealInstance } from '@/api'
import { CreateInstanceDialog } from '@/components/CreateInstanceDialog'
import { AddModelDialog } from '@/components/AddModelDialog'
import { LoginPage } from '@/components/LoginPage'
import { SetupPage } from '@/components/SetupPage'
import { Dashboard } from '@/pages/Dashboard'
import { Hardware } from '@/pages/Hardware'
import { Instances } from '@/pages/Instances'
import { Models } from '@/pages/Models'
import { Tasks } from '@/pages/Tasks'
import { ImageManager } from '@/components/ImageManager'
import { System } from '@/pages/System'
import type { AddModelInput, CreateInstanceInput, Instance, ModelItem, NavigateOptions, PageKey, TaskItem } from '@/types'

const navigation: { key: PageKey; label: string; icon: typeof LayoutDashboard }[] = [
  { key: 'dashboard', label: '首页看板', icon: LayoutDashboard },
  { key: 'instances', label: 'ComfyUI 实例', icon: Box },
  { key: 'models', label: '模型中心', icon: Database },
  { key: 'images', label: '镜像中心', icon: Images },
  { key: 'tasks', label: '任务中心', icon: Activity },
  { key: 'hardware', label: '硬件管理', icon: Cpu },
  { key: 'system', label: '系统管理', icon: Settings },
]

const pageNames: Record<PageKey, string> = { dashboard: '首页看板', instances: 'ComfyUI 实例', models: '模型中心', images: '镜像中心', tasks: '任务中心', hardware: '硬件管理', system: '系统管理' }

export default function App() {
  // 认证门卫（0.0.9.6）：checking → setup（未初始化）→ login → ready。
  const [authState, setAuthState] = useState<'checking' | 'setup' | 'login' | 'ready'>('checking')
  const [adminUser, setAdminUser] = useState('admin')
  // 页面状态机：page 决定当前视图，focus 承载跨页定位（如跳转实例页并打开某实例详情）。
  // token 用于区分「连续的同类定位跳转」，页面把它拼进 key 触发重挂载，从而重新应用初始定位。
  const [route, setRoute] = useState<{ page: PageKey; focus?: string; token: number }>({ page: 'dashboard', token: 0 })
  const page = route.page
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
  const [backendVersion, setBackendVersion] = useState('0.0.9.12')

  const realMode = realInstances !== null
  const realModelMode = realModels !== null

  // 认证引导：先查初始化状态，再尝试恢复会话；401 全局跳登录。
  useEffect(() => {
    let cancelled = false
    setUnauthorizedHandler(() => { setCSRFToken(''); setAuthState((state) => (state === 'ready' ? 'login' : state)) })
    async function bootstrap() {
      try {
        const status = await getSetupStatus()
        if (cancelled) return
        if (!status.initialized) {
          setAuthState('setup')
          return
        }
        const session = await getSession()
        if (cancelled) return
        setAdminUser(session.username)
        setAuthState('ready')
      } catch {
        if (!cancelled) setAuthState('login')
      }
    }
    void bootstrap()
    return () => {
      cancelled = true
      setUnauthorizedHandler(() => {})
    }
  }, [])

  async function handleLogout() {
    try {
      await logout()
    } finally {
      setCSRFToken('')
      setAuthState('login')
    }
  }

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

  // 业务轮询仅在已登录（ready）状态运行；登录/初始化页不产生无效 401 请求。
  useEffect(() => {
    if (authState !== 'ready') return
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
  }, [authState])

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
        id: `${file.origin}:${file.path}`,
        name: file.name,
        type: file.type,
        size: formatBytes(file.sizeBytes),
        source: file.origin === 'network' ? '网络存储' : '本地仓库',
        status: 'ready' as const,
        progress: 100,
        createdAt: new Date(file.modifiedAt).toLocaleString('zh-CN', { hour12: false }),
        origin: file.origin,
        rawPath: file.path,
        modifiedRaw: file.modifiedAt,
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

  function navigate(target: PageKey, options?: NavigateOptions) {
    const focus = options?.focus
    // 带定位的跳转递增 token：即使连续点击同一个实例名，目标页也会重新挂载并命中初始定位。
    setRoute((previous) => ({ page: target, focus, token: focus ? previous.token + 1 : previous.token }))
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
      .then((result) => {
        if (result.queueWarning) {
          toast.warning(`${actionName}提示`, { description: result.queueWarning })
        }
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

  function deleteInstance(id: string, purge = false) {
    const target = displayInstances.find((item) => item.id === id)
    if (!target || !requireBackend(purge ? '彻底删除实例' : '删除实例')) return
    deleteInstanceApi(target.name, target.status === 'running', purge === true)
      .then(() => {
        toast.success(purge ? '实例及数据已彻底删除' : '实例容器已删除', { description: purge ? `${target.name} · 容器与全部实例数据已清理` : `${target.name} · 实例数据已保留，重建同名实例自动沿用` })
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
    if (id.startsWith('network:')) {
      toast.error('网络存储为只读来源', { description: '如需下线该模型，请在 NAS 侧操作；平台内可用「本地化」复制到本地仓库后再管理。' })
      return
    }
    if (!requireBackend('删除模型')) return
    const rawPath = id.startsWith('local:') ? id.slice('local:'.length) : id
    deleteModelApi(rawPath)
      .then(() => {
        toast.success('模型文件已删除', { description: rawPath })
        void refreshModels()
      })
      .catch((error: unknown) => {
        toast.error('删除模型失败', { description: error instanceof Error ? error.message : '请求失败' })
        void refreshModels()
      })
  }

  // 本地化：把网络存储（NAS）中的模型复制进本地仓库，实例加载走本机高速盘。
  function localizeModel(id: string) {
    if (!requireBackend('本地化模型')) return
    const file = realModels?.files.find((item) => `network:${item.path}` === id)
    const networkRoot = realModels?.network?.root
    if (!file || !networkRoot) return
    const sourcePath = `${networkRoot}/${file.path}`
    createImportApi({ path: sourcePath, category: file.category, name: file.name })
      .then((task) => {
        toast.success('本地化任务已创建', { description: `${task.fileName} → ${task.path}（复制到本地仓库）` })
        void refreshDownloads()
      })
      .catch((error: unknown) => {
        toast.error('创建本地化任务失败', { description: error instanceof Error ? error.message : '请求失败' })
      })
  }

  // 批量删除（仅本地仓库文件）：逐个真实删除并汇总结果。
  async function batchDeleteModels(ids: string[]) {
    if (!requireBackend('批量删除模型')) return
    const running = displayInstances.filter((item) => item.status === 'running').map((item) => item.name)
    if (running.length > 0) {
      toast.error('当前无法删除模型', { description: `保护策略：请先停止运行中的实例（${running.join('、')}）。` })
      return
    }
    const localIds = ids.filter((id) => id.startsWith('local:'))
    const results = await Promise.allSettled(localIds.map((id) => deleteModelApi(id.slice('local:'.length))))
    const failed = results.filter((item) => item.status === 'rejected').length
    if (failed === 0) {
      toast.success(`已删除 ${localIds.length} 个模型文件`)
    } else {
      toast.error('批量删除部分失败', { description: `成功 ${localIds.length - failed} 个，失败 ${failed} 个。` })
    }
    void refreshModels()
  }

  // 批量本地化（仅网络存储文件）。
  async function batchLocalizeModels(ids: string[]) {
    if (!requireBackend('批量本地化')) return
    const networkRoot = realModels?.network?.root
    if (!networkRoot) return
    const files = ids.map((id) => realModels?.files.find((item) => `network:${item.path}` === id)).filter((item): item is NonNullable<typeof item> => Boolean(item))
    const results = await Promise.allSettled(files.map((file) => createImportApi({ path: `${networkRoot}/${file.path}`, category: file.category, name: file.name })))
    const failed = results.filter((item) => item.status === 'rejected').length
    if (failed === 0) {
      toast.success(`已创建 ${files.length} 个本地化任务`, { description: '进度见任务中心。' })
    } else {
      toast.error('批量本地化部分失败', { description: `成功 ${files.length - failed} 个，失败 ${failed} 个。` })
    }
    void refreshDownloads()
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

  function taskAction(unifiedId: string, action: 'cancel' | 'retry') {
    if (!requireBackend('任务操作')) return
    // 统一任务 ID 带 dl- 前缀（下载/导入任务）；操作路由到下载子系统。
    const id = unifiedId.replace(/^dl-/, '')
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

  // 认证门卫渲染：初始化页／登录页优先于业务界面。
  if (authState === 'setup') {
    return <div className="min-h-dvh"><Toaster position="top-right" richColors closeButton /><SetupPage onReady={() => { setAdminUser('admin'); setAuthState('ready') }} /></div>
  }
  if (authState === 'login') {
    return <div className="min-h-dvh"><Toaster position="top-right" richColors closeButton /><LoginPage version={backendVersion} onLoggedIn={() => setAuthState('ready')} /></div>
  }
  if (authState === 'checking') {
    return <div className="flex min-h-dvh items-center justify-center bg-slate-100 text-sm text-slate-500">正在检查登录状态…</div>
  }

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
      <header className="sticky top-0 z-20 flex h-16 items-center gap-3 border-b border-slate-200 bg-[oklch(0.995_0.003_220)]/95 px-4 backdrop-blur md:px-8"><button type="button" className="icon-button md:hidden" aria-label="打开菜单" onClick={() => setMobileOpen(true)}><Menu className="size-5" /></button><div className="hidden items-center gap-2 text-sm text-slate-500 sm:flex"><Server className="size-4" /><span>{hostInfo.name}</span><ChevronRight className="size-3.5" /><span className="text-slate-800">{pageNames[page]}</span></div><div className="ml-auto flex items-center gap-2"><span className={`hidden rounded-full border px-3 py-1.5 text-xs font-medium sm:inline ${backendOnline ? 'border-emerald-200 bg-emerald-50 text-emerald-800' : 'border-rose-200 bg-rose-50 text-rose-800'}`}>{backendOnline ? '后端已连接' : '后端连接失败'}</span><button type="button" className="btn-quiet" onClick={() => navigate('tasks')}><Activity className="size-4" /><span className="hidden sm:inline">任务</span>{activeTasks > 0 && <span className="rounded-full bg-teal-100 px-1.5 text-xs text-teal-800">{activeTasks}</span>}</button><div className="flex items-center gap-2"><div className="flex size-9 items-center justify-center rounded-full bg-slate-900 text-xs font-semibold text-white" title={`管理员 ${adminUser}`}>AD</div><button type="button" className="btn-quiet" title={`退出登录（${adminUser}）`} aria-label="退出登录" onClick={handleLogout} data-testid="logout-button"><LogOut className="size-4" /></button></div></div></header>
      <main className="mx-auto max-w-[1600px] p-4 md:p-8">
        {page === 'dashboard' && <Dashboard instances={displayInstances} models={displayModels} tasks={displayTasks} gpuCount={gpuNames.length} gpuSummary={hostInfo.gpuSummary} modelsTotal={realModels ? formatBytes(realModels.totalBytes) : undefined} onNavigate={navigate} onCreate={openCreateDialog} onAddModel={openAddModelDialog} />}
        {page === 'instances' && <Instances key={route.focus ? `instance-focus-${route.token}` : 'instances'} instances={displayInstances} gpuNames={gpuNames} realMode={realMode} busyIds={busyIds} initialFocus={route.focus} models={realModels} onCreate={openCreateDialog} onAction={instanceAction} onDelete={deleteInstance} />}
        {page === 'models' && <Models models={displayModels} realMode={realModelMode} modelsRoot={realModels?.root} totalBytes={realModels?.totalBytes} network={realModels?.network} runningInstanceNames={displayInstances.filter((item) => item.status === 'running').map((item) => item.name)} onAdd={openAddModelDialog} onPause={pauseModel} onDelete={deleteModel} onLocalize={localizeModel} onBatchDelete={batchDeleteModels} onBatchLocalize={batchLocalizeModels} />}
        {page === 'tasks' && <Tasks onAction={taskAction} />}
        {page === 'images' && <ImageManager />}
        {page === 'hardware' && <Hardware />}
        {page === 'system' && <System instances={displayInstances} models={displayModels} tasks={displayTasks} version={backendVersion} onLoggedOut={handleLogout} />}
      </main>
    </div>
    <CreateInstanceDialog key={createOpen ? 'create-open' : 'create-closed'} open={createOpen} onOpenChange={setCreateOpen} onSubmit={createInstance} models={displayModels} instances={displayInstances} gpuNames={gpuNames} defaultImage={instanceDefaults?.image} defaultContainerPort={instanceDefaults?.containerPort} dataDir={instanceDefaults?.dataDir} defaultModelsTarget={instanceDefaults?.modelsTarget} defaultOutputsTarget={instanceDefaults?.outputsTarget} defaultWorkflowsTarget={instanceDefaults?.workflowsTarget} />
    <AddModelDialog key={addModelOpen ? 'model-open' : 'model-closed'} open={addModelOpen} onOpenChange={setAddModelOpen} onSubmit={addModel} onMarketDownload={marketDownload} onOnlineDownload={onlineDownload} existingPaths={existingModelPaths} />
  </div>
}
