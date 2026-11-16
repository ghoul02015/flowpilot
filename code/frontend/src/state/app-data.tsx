// 应用数据层：认证状态、按页面启用的轮询、业务写操作与全局弹窗。
// 路由壳只负责导航与布局，数据生命周期集中在这里，避免页面各自全量轮询。
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useLocation } from 'react-router'
import { toast } from 'sonner'
import { emitRefresh, subscribeRefresh } from '@/lib/refresh-events'
import { pageFromPath } from '@/lib/routes'
import { usePolling } from '@/hooks/use-polling'
import {
  createDownloadApi, createImportApi, createInstanceApi, deleteDownloadApi, deleteInstanceApi, deleteModelApi,
  downloadActionApi, formatBytes, getComfyStatus, getDownloads, getHardware, getHealth, getInstances, getModels,
  getSetupStatus, getSession, getTaskSummary, logout as logoutApi, modelTypeToCategory, postInstanceAction, setCSRFToken,
  setUnauthorizedHandler, type CatalogEntry, type ComfyStatus, type DownloadTask, type InstanceDefaults,
  type ModelReport, type OnlineFile, type RealInstance,
} from '@/api'
import type { AddModelInput, CreateInstanceInput, Instance, ModelItem, PageKey, TaskItem } from '@/types'

export type AuthState = 'checking' | 'setup' | 'login' | 'ready'

interface AppDataValue {
  authState: AuthState
  adminUser: string
  backendOnline: boolean
  backendVersion: string
  hostInfo: { name: string; gpuSummary: string }
  gpuNames: string[]
  page: PageKey
  realInstances: RealInstance[] | null
  realModels: ModelReport | null
  realDownloads: DownloadTask[] | null
  instanceDefaults: InstanceDefaults | null
  displayInstances: Instance[]
  displayModels: ModelItem[]
  displayTasks: TaskItem[]
  busyIds: string[]
  taskSummary: { active: number; total: number }
  activeTasks: number
  existingModelPaths: string[]
  createOpen: boolean
  addModelOpen: boolean
  setCreateOpen: (open: boolean) => void
  setAddModelOpen: (open: boolean) => void
  logout: () => Promise<void>
  markLoggedIn: (username?: string) => void
  openCreateDialog: () => void
  openAddModelDialog: () => void
  requireBackend: (action: string) => boolean
  instanceAction: (id: string, action: 'start' | 'stop' | 'restart') => void
  createInstance: (input: CreateInstanceInput) => Promise<boolean>
  deleteInstance: (id: string, purge?: boolean) => void
  addModel: (input: AddModelInput) => Promise<boolean>
  pauseModel: (id: string) => void
  downloadActionModel: (id: string, action: 'pause' | 'resume' | 'cancel' | 'retry') => void
  deleteModel: (id: string) => void
  localizeModel: (id: string) => void
  batchDeleteModels: (ids: string[]) => Promise<void>
  batchLocalizeModels: (ids: string[]) => Promise<void>
  marketDownload: (entry: CatalogEntry, useFallback: boolean) => void
  onlineDownload: (file: OnlineFile) => void
  refreshInstances: () => Promise<void>
  refreshModels: () => Promise<void>
  refreshDownloads: () => Promise<void>
}

const AppDataContext = createContext<AppDataValue | null>(null)

// 主动取消与超时不应被当作离线或错误提示。
function isAbortError(error: unknown): boolean {
  return error instanceof Error && (error.name === 'AbortError' || error.name === 'TimeoutError')
}

export function AppDataProvider({ children }: { children: ReactNode }) {
  const location = useLocation()
  const page = pageFromPath(location.pathname)

  const [authState, setAuthState] = useState<AuthState>('checking')
  const [adminUser, setAdminUser] = useState('admin')
  const [createOpen, setCreateOpen] = useState(false)
  const [addModelOpen, setAddModelOpen] = useState(false)
  const [hostInfo, setHostInfo] = useState({ name: '正在连接…', gpuSummary: '—' })
  const [gpuNames, setGpuNames] = useState<string[]>([])
  const [realInstances, setRealInstances] = useState<RealInstance[] | null>(null)
  const [instanceDefaults, setInstanceDefaults] = useState<InstanceDefaults | null>(null)
  const [realModels, setRealModels] = useState<ModelReport | null>(null)
  const [realDownloads, setRealDownloads] = useState<DownloadTask[] | null>(null)
  const [backendOnline, setBackendOnline] = useState(true)
  const [comfyStatuses, setComfyStatuses] = useState<Record<string, ComfyStatus>>({})
  const [busyIds, setBusyIds] = useState<string[]>([])
  const busyRef = useRef<Set<string>>(new Set())
  const [backendVersion, setBackendVersion] = useState('0.1.0.15')
  const [taskSummary, setTaskSummary] = useState({ active: 0, total: 0 })

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

  const markLoggedIn = useCallback((username?: string) => {
    if (username) setAdminUser(username)
    setAuthState('ready')
  }, [])

  const logout = useCallback(async () => {
    try {
      await logoutApi()
    } finally {
      setCSRFToken('')
      setAuthState('login')
    }
  }, [])

  const pageRef = useRef(page)
  pageRef.current = page
  const busyCountRef = useRef(busyIds.length)
  busyCountRef.current = busyIds.length
  const summaryActiveRef = useRef(0)
  const downloadsActiveRef = useRef(false)
  const completedTasksRef = useRef<Set<string>>(new Set())
  const downloadsInitializedRef = useRef(false)

  // 导航摘要：3 秒活跃 / 30 秒空闲，不再拉全量任务列表。
  const summaryPoll = usePolling(async (signal) => {
    const summary = await getTaskSummary(signal)
    summaryActiveRef.current = summary.active
    setTaskSummary({ active: summary.active, total: summary.total })
  }, { enabled: authState === 'ready', interval: 3000, idleInterval: 30000, isActive: () => summaryActiveRef.current > 0 })

  // 实例列表：首页／实例页／模型页／系统页需要；状态迁移期 3 秒，稳定后 5 秒。
  const instancesPoll = usePolling(async (signal) => {
    try {
      const next = await getInstances(signal)
      setRealInstances(next.instances)
      setInstanceDefaults(next.defaults)
      setBackendOnline(true)
      const current = pageRef.current
      if (current === 'instances' || current === 'dashboard') {
        const running = next.instances.filter((item) => item.status === 'running')
        const statuses = await Promise.allSettled(running.map((item) => getComfyStatus(item.name, signal)))
        setComfyStatuses((previous) => {
          const merged = { ...previous }
          running.forEach((item, index) => {
            const result = statuses[index]
            if (result.status === 'fulfilled') merged[item.name] = result.value
          })
          return merged
        })
      }
    } catch (error) {
      if (isAbortError(error)) return
      setRealInstances(null)
      setBackendOnline(false)
    }
  }, {
    enabled: authState === 'ready' && ['dashboard', 'instances', 'models', 'system'].includes(page),
    interval: 3000, idleInterval: 5000, isActive: () => busyCountRef.current > 0,
  })

  // 模型列表：仅首页／模型页／系统页，空闲 60 秒；写操作通过刷新事件立即失效。
  const modelsPoll = usePolling(async (signal) => {
    try {
      setRealModels(await getModels(signal))
      setBackendOnline(true)
    } catch (error) {
      if (!isAbortError(error)) setRealModels(null)
    }
  }, { enabled: authState === 'ready' && ['dashboard', 'models', 'system'].includes(page), interval: 60000 })

  // 下载任务：仅需要任务投影的页面；活跃 3 秒、空闲 15 秒。
  const downloadsPoll = usePolling(async (signal) => {
    try {
      const tasks = await getDownloads(signal)
      const nextActive = tasks.some((task) => ['running', 'paused'].includes(task.status))
      const completedIds = tasks.filter((task) => task.status === 'completed').map((task) => task.id)
      // 首次轮询只建立基线：历史完成任务不代表本次会话有新文件，避免多打一次模型扫描。
      if (downloadsInitializedRef.current) {
        const hasNewCompleted = completedIds.some((id) => !completedTasksRef.current.has(id))
        if (hasNewCompleted) emitRefresh('models', 'task-summary')
      } else {
        downloadsInitializedRef.current = true
      }
      completedTasksRef.current = new Set(completedIds)
      downloadsActiveRef.current = nextActive
      setRealDownloads(tasks)
      setBackendOnline(true)
    } catch (error) {
      if (!isAbortError(error)) setRealDownloads(null)
    }
  }, {
    enabled: authState === 'ready' && ['dashboard', 'models', 'system'].includes(page),
    interval: 3000, idleInterval: 15000, isActive: () => downloadsActiveRef.current,
  })

  // 主机与 GPU 摘要：首页 5 秒，其他页面 60 秒；失败按退避重试，不形成紧循环。
  const hardwarePoll = usePolling(async (signal) => {
    const report = await getHardware(signal)
    setHostInfo({
      name: report.hostName,
      gpuSummary: report.gpus.length ? `${report.gpus.length} × ${report.gpus[0].name}` : '未检测到 GPU',
    })
    setGpuNames(report.gpus.map((gpu) => gpu.name))
  }, {
    enabled: authState === 'ready',
    interval: page === 'dashboard' ? 5000 : 60000,
    onError: () => setHostInfo({ name: '硬件接口不可用', gpuSummary: '—' }),
  })

  // 版本信息：登录后 60 秒，写操作不触发。
  usePolling(async (signal) => {
    const health = await getHealth(signal)
    setBackendVersion(health.version)
  }, { enabled: authState === 'ready', interval: 60000 })

  // 刷新事件：业务写操作后只通知相关数据。
  const pollRef = useRef({ summaryPoll, instancesPoll, modelsPoll, downloadsPoll, hardwarePoll })
  pollRef.current = { summaryPoll, instancesPoll, modelsPoll, downloadsPoll, hardwarePoll }
  useEffect(() => {
    const unsubscribe = [
      subscribeRefresh('task-summary', () => void pollRef.current.summaryPoll.refresh()),
      subscribeRefresh('tasks', () => void pollRef.current.summaryPoll.refresh()),
      subscribeRefresh('instances', () => void pollRef.current.instancesPoll.refresh()),
      subscribeRefresh('models', () => void pollRef.current.modelsPoll.refresh()),
      subscribeRefresh('downloads', () => void pollRef.current.downloadsPoll.refresh()),
      subscribeRefresh('hardware', () => void pollRef.current.hardwarePoll.refresh()),
    ]
    return () => unsubscribe.forEach((off) => off())
  }, [])

  const refreshInstances = useCallback(() => instancesPoll.refresh(), [instancesPoll])
  const refreshModels = useCallback(() => modelsPoll.refresh(), [modelsPoll])
  const refreshDownloads = useCallback(() => downloadsPoll.refresh(), [downloadsPoll])

  const requireBackend = useCallback((action: string): boolean => {
    if (backendOnline) return true
    toast.error(`${action}失败`, { description: 'FlowPilot 后端未连接，请确认服务已启动。' })
    return false
  }, [backendOnline])

  // 真实实例映射为界面模型；id 使用实例名称（全局唯一）。
  const displayInstances: Instance[] = useMemo(() => (realInstances ?? []).map((item) => ({
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
  })), [realInstances, comfyStatuses])

  // 真实仓库文件映射为界面模型；进行中的下载任务以独立 id 合入列表。
  const displayModels: ModelItem[] = useMemo(() => realModels !== null
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
    : [], [realModels, realDownloads])

  // 真实下载／导入任务映射为任务中心条目。
  const displayTasks: TaskItem[] = useMemo(() => (realDownloads ?? []).map((task) => ({
    id: task.id,
    name: `${task.kind === 'import' ? '导入' : '下载'} ${task.fileName}`,
    type: task.kind === 'import' ? '模型导入' : '模型下载',
    target: task.path,
    status: task.status,
    progress: task.totalBytes > 0 ? Math.round((task.downloadedBytes / task.totalBytes) * 100) : 0,
    createdAt: new Date(task.createdAt).toLocaleString('zh-CN', { hour12: false }),
    message: task.totalBytes > 0 ? `${task.message} · ${formatBytes(task.downloadedBytes)}／${formatBytes(task.totalBytes)}` : task.message,
  })), [realDownloads])

  const openCreateDialog = useCallback(() => {
    if (!requireBackend('打开新建实例向导')) return
    setCreateOpen(true)
  }, [requireBackend])

  const openAddModelDialog = useCallback(() => {
    if (!requireBackend('打开添加模型')) return
    setAddModelOpen(true)
  }, [requireBackend])

  const instanceAction = useCallback((id: string, action: 'start' | 'stop' | 'restart') => {
    const target = displayInstances.find((item) => item.id === id)
    if (!target || !requireBackend('实例操作')) return
    if (busyRef.current.has(id)) return
    busyRef.current.add(id)
    const actionName = action === 'stop' ? '停止' : action === 'start' ? '启动' : '重启'
    setBusyIds((previous) => [...previous, id])
    postInstanceAction(target.name, action)
      .then((result) => {
        if (result.queueWarning) toast.warning(`${actionName}提示`, { description: result.queueWarning })
        toast.success(`实例已${actionName}`, { description: target.name })
        emitRefresh('instances', 'task-summary')
      })
      .catch((error: unknown) => {
        toast.error(`实例${actionName}失败`, { description: error instanceof Error ? error.message : '请求失败' })
      })
      .finally(() => {
        void refreshInstances()
        busyRef.current.delete(id)
        setBusyIds((previous) => previous.filter((item) => item !== id))
      })
  }, [displayInstances, refreshInstances, requireBackend])

  const createInstance = useCallback(async (input: CreateInstanceInput): Promise<boolean> => {
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
  }, [instanceDefaults, refreshInstances])

  const deleteInstance = useCallback((id: string, purge = false) => {
    const target = displayInstances.find((item) => item.id === id)
    if (!target || !requireBackend(purge ? '彻底删除实例' : '删除实例')) return
    deleteInstanceApi(target.name, target.status === 'running', purge === true)
      .then(() => {
        toast.success(purge ? '实例及数据已彻底删除' : '实例容器已删除', { description: purge ? `${target.name} · 容器与全部实例数据已清理` : `${target.name} · 实例数据已保留，重建同名实例自动沿用` })
        emitRefresh('instances', 'task-summary')
        void refreshInstances()
      })
      .catch((error: unknown) => {
        toast.error('删除实例失败', { description: error instanceof Error ? error.message : '请求失败' })
        void refreshInstances()
      })
  }, [displayInstances, refreshInstances, requireBackend])

  // 返回是否创建成功：失败时弹窗保持打开，用户输入不丢失。
  const addModel = useCallback(async (input: AddModelInput): Promise<boolean> => {
    if (!requireBackend('创建模型任务')) return false
    try {
      const task = input.sourceType === '本地导入'
        ? await createImportApi({ path: input.source, category: modelTypeToCategory(input.type), name: input.name || undefined })
        : await createDownloadApi({ url: input.source, category: modelTypeToCategory(input.type), name: input.name || undefined })
      toast.success(input.sourceType === '本地导入' ? '导入任务已创建' : '下载任务已创建', { description: `${task.fileName} → ${task.path}` })
      emitRefresh('downloads', 'tasks', 'task-summary')
      void refreshDownloads()
      return true
    } catch (error) {
      toast.error(input.sourceType === '本地导入' ? '创建导入任务失败' : '创建下载任务失败', { description: error instanceof Error ? error.message : '请求失败' })
      return false
    }
  }, [refreshDownloads, requireBackend])

  const pauseModel = useCallback((id: string) => {
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
  }, [realDownloads, refreshDownloads, requireBackend])

  // 下载任务操作：暂停／继续／取消／重试；成功后同时刷新下载与模型投影。
  const downloadActionModel = useCallback((id: string, action: 'pause' | 'resume' | 'cancel' | 'retry') => {
    if (!requireBackend('下载操作')) return
    downloadActionApi(id, action)
      .then(() => {
        const label = action === 'pause' ? '已暂停' : action === 'resume' ? '已继续' : action === 'cancel' ? '已取消' : '已重新开始'
        toast.success(`下载任务${label}`, { description: id })
        emitRefresh('downloads', 'models', 'task-summary')
        void refreshDownloads()
        void refreshModels()
      })
      .catch((error: unknown) => {
        toast.error('下载操作失败', { description: error instanceof Error ? error.message : '请求失败' })
        void refreshDownloads()
      })
  }, [refreshDownloads, refreshModels, requireBackend])

  const deleteModel = useCallback((id: string) => {
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
        emitRefresh('models')
        void refreshModels()
      })
      .catch((error: unknown) => {
        toast.error('删除模型失败', { description: error instanceof Error ? error.message : '请求失败' })
        void refreshModels()
      })
  }, [realDownloads, refreshDownloads, refreshModels, requireBackend])

  // 本地化：把网络存储（NAS）中的模型复制进本地仓库，实例加载走本机高速盘。
  const localizeModel = useCallback((id: string) => {
    if (!requireBackend('本地化模型')) return
    const file = realModels?.files.find((item) => `network:${item.path}` === id)
    const networkRoot = realModels?.network?.root
    if (!file || !networkRoot) return
    createImportApi({ path: `${networkRoot}/${file.path}`, category: file.category, name: file.name })
      .then((task) => {
        toast.success('本地化任务已创建', { description: `${task.fileName} → ${task.path}（复制到本地仓库）` })
        void refreshDownloads()
      })
      .catch((error: unknown) => {
        toast.error('创建本地化任务失败', { description: error instanceof Error ? error.message : '请求失败' })
      })
  }, [realModels, refreshDownloads, requireBackend])

  // 批量删除（仅本地仓库文件）：逐个真实删除并汇总结果。
  const batchDeleteModels = useCallback(async (ids: string[]) => {
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
  }, [displayInstances, refreshModels, requireBackend])

  // 批量本地化（仅网络存储文件）。
  const batchLocalizeModels = useCallback(async (ids: string[]) => {
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
  }, [realModels, refreshDownloads, requireBackend])

  // 模型市场：选中目录条目直接下载（ModelScope 主源，HF 镜像备选）。
  const marketDownload = useCallback((entry: CatalogEntry, useFallback: boolean) => {
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
  }, [refreshDownloads, requireBackend])

  // 在线源：选中仓库权重文件直接下载（地址与类别由后端按 ComfyUI 约定推断）。
  const onlineDownload = useCallback((file: OnlineFile) => {
    if (!requireBackend('创建下载任务')) return
    createDownloadApi({ url: file.url, category: file.category, name: file.name })
      .then((task) => {
        toast.success(`已开始下载 ${file.name}`, { description: `${task.fileName} → ${task.path}` })
        void refreshDownloads()
      })
      .catch((error: unknown) => {
        toast.error('创建下载任务失败', { description: error instanceof Error ? error.message : '请求失败' })
      })
  }, [refreshDownloads, requireBackend])

  // 已入库或进行中任务占用的仓库路径，用于模型市场标注。
  const existingModelPaths = useMemo(() => [
    ...(realModels?.files.map((file) => file.path) ?? []),
    ...(realDownloads ?? []).filter((task) => ['running', 'paused'].includes(task.status)).map((task) => task.path),
  ], [realModels, realDownloads])

  const value: AppDataValue = {
    authState, adminUser, backendOnline, backendVersion, hostInfo, gpuNames, page,
    realInstances, realModels, realDownloads, instanceDefaults,
    displayInstances, displayModels, displayTasks, busyIds, taskSummary,
    activeTasks: taskSummary.active,
    existingModelPaths, createOpen, addModelOpen, setCreateOpen, setAddModelOpen,
    logout, markLoggedIn, openCreateDialog, openAddModelDialog, requireBackend,
    instanceAction, createInstance, deleteInstance, addModel, pauseModel, downloadActionModel, deleteModel, localizeModel,
    batchDeleteModels, batchLocalizeModels, marketDownload, onlineDownload,
    refreshInstances, refreshModels, refreshDownloads,
  }

  return <AppDataContext.Provider value={value}>{children}</AppDataContext.Provider>
}

// eslint-disable-next-line react-refresh/only-export-components -- 数据层与 Hook 同文件导出，便于页面统一订阅。
export function useAppData(): AppDataValue {
  const value = useContext(AppDataContext)
  if (!value) throw new Error('useAppData 必须在 AppDataProvider 内使用')
  return value
}
