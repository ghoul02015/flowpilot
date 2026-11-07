import { useEffect, useState } from 'react'
import { Activity, Box, ChevronRight, Database, LayoutDashboard, Menu, RotateCcw, Server, Settings, Waves, X } from 'lucide-react'
import { Toaster, toast } from 'sonner'
import { getHardware } from '@/api'
import { CreateInstanceDialog } from '@/components/CreateInstanceDialog'
import { AddModelDialog } from '@/components/AddModelDialog'
import { Dashboard } from '@/pages/Dashboard'
import { Instances } from '@/pages/Instances'
import { Models } from '@/pages/Models'
import { Tasks } from '@/pages/Tasks'
import { System } from '@/pages/System'
import { initialInstances, initialModels, initialTasks } from '@/data/mock'
import type { AddModelInput, CreateInstanceInput, Instance, ModelItem, PageKey, TaskItem } from '@/types'

function usePersistentState<T>(key: string, initialValue: T) {
  const [value, setValue] = useState<T>(() => {
    try {
      const saved = localStorage.getItem(key)
      return saved ? JSON.parse(saved) as T : initialValue
    } catch {
      return initialValue
    }
  })
  useEffect(() => {
    localStorage.setItem(key, JSON.stringify(value))
  }, [key, value])
  return [value, setValue] as const
}

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
  const [instances, setInstances] = usePersistentState<Instance[]>('flowpilot-demo-instances', initialInstances)
  const [models, setModels] = usePersistentState<ModelItem[]>('flowpilot-demo-models', initialModels)
  const [tasks, setTasks] = usePersistentState<TaskItem[]>('flowpilot-demo-tasks', initialTasks)
  // 真实硬件可用时用真实主机名与 GPU 概要替换默认演示文案。
  const [hostInfo, setHostInfo] = useState({ name: '慧浪 GPU-S01', gpuSummary: '2 × NVIDIA L40S' })

  useEffect(() => {
    getHardware()
      .then((report) => setHostInfo({
        name: report.hostName,
        gpuSummary: report.gpus.length ? `${report.gpus.length} × ${report.gpus[0].name}` : '未检测到 GPU',
      }))
      .catch(() => {})
  }, [])

  useEffect(() => {
    const timer = window.setInterval(() => {
      setModels((current) => current.map((model) => model.status === 'downloading' ? { ...model, progress: Math.min(100, model.progress + 4), status: model.progress + 4 >= 100 ? 'ready' : 'downloading', size: model.progress + 4 >= 100 && model.size === '等待计算' ? '3.24 GB' : model.size } : model))
      setTasks((current) => current.map((task) => task.status === 'running' && task.modelId ? { ...task, progress: Math.min(100, task.progress + 4), status: task.progress + 4 >= 100 ? 'completed' : 'running', message: task.progress + 4 >= 100 ? 'SHA256 校验通过，模型已写入共享仓库' : `下载中 · 当前进度 ${task.progress + 4}%` } : task))
    }, 1600)
    return () => window.clearInterval(timer)
  }, [setModels, setTasks])

  function navigate(target: PageKey) {
    setPage(target)
    setMobileOpen(false)
  }

  function instanceAction(id: string, action: 'start' | 'stop' | 'restart') {
    const target = instances.find((item) => item.id === id)
    if (!target) return
    if (action !== 'stop' && instances.some((item) => item.id !== id && item.gpu === target.gpu && ['running', 'starting', 'creating'].includes(item.status))) {
      toast.error('GPU 资源冲突', { description: `GPU ${target.gpu} 已被其他运行实例占用。` })
      return
    }
    const transitional = action === 'start' ? 'starting' : action === 'stop' ? 'stopping' : 'restarting'
    setInstances((current) => current.map((item) => item.id === id ? { ...item, status: transitional } : item))
    window.setTimeout(() => {
      setInstances((current) => current.map((item) => item.id === id ? { ...item, status: action === 'stop' ? 'stopped' : 'running', uptime: action === 'stop' ? '刚刚停止' : '刚刚启动' } : item))
      toast.success(action === 'stop' ? '实例已停止' : action === 'start' ? '实例已启动' : '实例已重启', { description: target.name })
    }, 900)
  }

  function createInstance(input: CreateInstanceInput) {
    const id = `ins-${Date.now()}`
    const taskId = `task-${Date.now()}`
    const instance: Instance = { ...input, id, status: 'creating', createdAt: new Date().toLocaleString('zh-CN', { hour12: false }), uptime: '正在准备容器' }
    setInstances((current) => [instance, ...current])
    setTasks((current) => [{ id: taskId, name: `部署 ${input.name}`, type: '实例部署', target: input.name, status: 'running', progress: 12, createdAt: instance.createdAt, message: '正在校验镜像并创建持久化目录', instanceId: id }, ...current])
    navigate('instances')
    toast.success('部署任务已创建', { description: `${input.name} · GPU ${input.gpu} · 端口 ${input.port}` })
    window.setTimeout(() => {
      setInstances((current) => current.map((item) => item.id === id ? { ...item, status: 'running', uptime: '刚刚启动' } : item))
      setTasks((current) => current.map((task) => task.id === taskId ? { ...task, status: 'completed', progress: 100, message: '镜像校验、容器创建及健康检查均已通过' } : task))
    }, 1800)
  }

  function deleteInstance(id: string) {
    const target = instances.find((item) => item.id === id)
    setInstances((current) => current.filter((item) => item.id !== id))
    toast.success('实例记录已删除', { description: `${target?.name ?? id} · 共享模型未受影响` })
  }

  function addModel(input: AddModelInput) {
    const id = `mod-${Date.now()}`
    const createdAt = new Date().toLocaleString('zh-CN', { hour12: false })
    setModels((current) => [{ id, name: input.name, type: input.type, size: '等待计算', source: input.sourceType, status: 'downloading', progress: 4, createdAt }, ...current])
    setTasks((current) => [{ id: `task-${Date.now()}`, name: `${input.sourceType === 'URL 下载' ? '下载' : '导入'} ${input.name}`, type: input.sourceType === 'URL 下载' ? '模型下载' : '模型导入', target: input.name, status: 'running', progress: 4, createdAt, message: input.sourceType === 'URL 下载' ? '已建立模拟下载连接' : '正在复制文件并准备校验', modelId: id }, ...current])
    navigate('models')
    toast.success('模型任务已创建', { description: 'Demo 将模拟下载进度和校验过程。' })
  }

  function pauseModel(id: string) {
    const target = models.find((model) => model.id === id)
    if (!target) return
    const nextStatus = target.status === 'downloading' ? 'paused' : 'downloading'
    setModels((current) => current.map((model) => model.id === id ? { ...model, status: nextStatus } : model))
    setTasks((items) => items.map((task) => task.modelId === id && ['running', 'paused'].includes(task.status) ? { ...task, status: nextStatus === 'paused' ? 'paused' : 'running', message: nextStatus === 'paused' ? '用户暂停了下载任务' : '下载任务已继续' } : task))
  }

  function deleteModel(id: string) {
    setModels((current) => current.filter((model) => model.id !== id))
    toast.success('模型记录已删除', { description: 'Demo 不会删除真实文件。' })
  }

  function taskAction(id: string, action: 'cancel' | 'retry') {
    const target = tasks.find((task) => task.id === id)
    if (!target) return
    if (action === 'cancel') {
      setTasks((current) => current.map((task) => task.id === id ? { ...task, status: 'cancelled', message: '用户取消了任务，可稍后重试' } : task))
      if (target.modelId) setModels((current) => current.map((model) => model.id === target.modelId ? { ...model, status: 'cancelled' } : model))
      toast.success('任务已取消')
      return
    }
    setTasks((current) => current.map((task) => task.id === id ? { ...task, status: 'running', progress: Math.max(4, task.progress), message: '任务已重新进入执行队列' } : task))
    if (target.modelId) setModels((current) => current.map((model) => model.id === target.modelId ? { ...model, status: 'downloading' } : model))
    if (target.instanceId) {
      setInstances((current) => current.map((instance) => instance.id === target.instanceId ? { ...instance, status: 'starting' } : instance))
      window.setTimeout(() => {
        setInstances((current) => current.map((instance) => instance.id === target.instanceId ? { ...instance, status: 'running', uptime: '刚刚恢复' } : instance))
        setTasks((current) => current.map((task) => task.id === id ? { ...task, status: 'completed', progress: 100, message: '重试成功，健康检查已通过' } : task))
      }, 1100)
    }
    toast.success('任务已重试')
  }

  function resetDemo() {
    setInstances(initialInstances)
    setModels(initialModels)
    setTasks(initialTasks)
    navigate('dashboard')
    toast.success('Demo 数据已重置')
  }

  const activeTasks = tasks.filter((task) => ['running', 'paused'].includes(task.status)).length

  return <div className="min-h-dvh bg-[oklch(0.975_0.006_220)] text-slate-800">
    <Toaster position="top-right" richColors closeButton />
    {mobileOpen && <button type="button" aria-label="关闭导航" className="fixed inset-0 z-30 cursor-pointer bg-slate-950/45 md:hidden" onClick={() => setMobileOpen(false)} />}
    <aside className={`fixed inset-y-0 left-0 z-40 flex w-[248px] flex-col bg-[oklch(0.22_0.03_240)] text-slate-300 transition-transform duration-200 md:translate-x-0 ${mobileOpen ? 'translate-x-0' : '-translate-x-full'}`}>
      <div className="flex h-20 items-center gap-3 border-b border-white/8 px-5"><div className="flex size-10 items-center justify-center rounded-lg bg-teal-500 text-slate-950"><Waves className="size-6" /></div><div><div className="font-bold tracking-wide text-white">慧浪智算</div><div className="mt-0.5 text-xs font-medium tracking-[0.16em] text-teal-300">FLOWPILOT</div></div><button type="button" className="ml-auto cursor-pointer rounded-md p-2 text-slate-300 hover:bg-white/10 md:hidden" aria-label="关闭菜单" onClick={() => setMobileOpen(false)}><X className="size-5" /></button></div>
      <nav className="flex-1 space-y-1 px-3 py-5" aria-label="主导航">{navigation.map(({ key, label, icon: Icon }) => <button type="button" key={key} onClick={() => navigate(key)} className={`flex min-h-12 w-full cursor-pointer items-center gap-3 rounded-lg px-3 text-sm font-medium transition-colors focus-visible:outline-teal-300 ${page === key ? 'bg-white/10 text-white' : 'text-slate-400 hover:bg-white/5 hover:text-white'}`}><Icon className={`size-5 ${page === key ? 'text-teal-300' : ''}`} /><span className="flex-1 text-left">{label}</span>{key === 'tasks' && activeTasks > 0 && <span className="rounded-full bg-teal-400/15 px-2 py-0.5 text-xs text-teal-300">{activeTasks}</span>}</button>)}</nav>
      <div className="mx-3 mb-3 rounded-lg border border-white/8 p-4"><div className="flex items-center gap-2 text-sm font-medium text-white"><span className="size-2 rounded-full bg-emerald-400" />{hostInfo.name}</div><div className="mt-2 text-xs leading-5 text-slate-400">{hostInfo.gpuSummary}<br />管理端运行正常</div><button type="button" className="mt-3 flex min-h-10 cursor-pointer items-center gap-2 text-xs text-slate-400 transition-colors hover:text-white" onClick={resetDemo}><RotateCcw className="size-3.5" />重置演示数据</button></div>
      <div className="border-t border-white/8 px-5 py-4 text-xs text-slate-500">MVP Demo · v0.0.0.6</div>
    </aside>

    <div className="min-h-dvh md:pl-[248px]">
      <header className="sticky top-0 z-20 flex h-16 items-center gap-3 border-b border-slate-200 bg-[oklch(0.995_0.003_220)]/95 px-4 backdrop-blur md:px-8"><button type="button" className="icon-button md:hidden" aria-label="打开菜单" onClick={() => setMobileOpen(true)}><Menu className="size-5" /></button><div className="hidden items-center gap-2 text-sm text-slate-500 sm:flex"><Server className="size-4" /><span>{hostInfo.name}</span><ChevronRight className="size-3.5" /><span className="text-slate-800">{pageNames[page]}</span></div><div className="ml-auto flex items-center gap-2"><span className="hidden rounded-full border border-amber-200 bg-amber-50 px-3 py-1.5 text-xs font-medium text-amber-800 sm:inline">Demo · 本地模拟数据</span><button type="button" className="btn-quiet" onClick={() => navigate('tasks')}><Activity className="size-4" /><span className="hidden sm:inline">任务</span>{activeTasks > 0 && <span className="rounded-full bg-teal-100 px-1.5 text-xs text-teal-800">{activeTasks}</span>}</button><div className="flex size-9 items-center justify-center rounded-full bg-slate-900 text-xs font-semibold text-white" title="Demo 管理员">AD</div></div></header>
      <main className="mx-auto max-w-[1600px] p-4 md:p-8">
        {page === 'dashboard' && <Dashboard instances={instances} models={models} tasks={tasks} onNavigate={navigate} onCreate={() => setCreateOpen(true)} onAddModel={() => setAddModelOpen(true)} />}
        {page === 'instances' && <Instances instances={instances} onCreate={() => setCreateOpen(true)} onAction={instanceAction} onDelete={deleteInstance} onToggleAutoStart={(id) => setInstances((current) => current.map((item) => item.id === id ? { ...item, autoStart: !item.autoStart } : item))} />}
        {page === 'models' && <Models models={models} hasRunningInstances={instances.some((item) => item.status === 'running')} onAdd={() => setAddModelOpen(true)} onPause={pauseModel} onDelete={deleteModel} />}
        {page === 'tasks' && <Tasks tasks={tasks} onAction={taskAction} />}
        {page === 'system' && <System instances={instances} models={models} tasks={tasks} />}
      </main>
    </div>
    <CreateInstanceDialog key={createOpen ? 'create-open' : 'create-closed'} open={createOpen} onOpenChange={setCreateOpen} onSubmit={createInstance} models={models} instances={instances} />
    <AddModelDialog key={addModelOpen ? 'model-open' : 'model-closed'} open={addModelOpen} onOpenChange={setAddModelOpen} onSubmit={addModel} />
  </div>
}
