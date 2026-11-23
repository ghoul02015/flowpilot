// 路由表：集中定义页面地址、懒加载与认证后的布局出口。
import { lazy, Suspense, type ReactNode } from 'react'
import { Navigate, Route, Routes, useNavigate, useParams, useSearchParams } from 'react-router'
import { LoaderCircle } from 'lucide-react'
import { ConsoleLayout } from '@/layouts/ConsoleLayout'
import { formatBytes } from '@/api'
import { isModelTab, pagePath, pagePaths } from '@/lib/routes'
import { useAppData } from '@/state/app-data'
import type { PageKey } from '@/types'

const Dashboard = lazy(() => import('@/pages/Dashboard').then((module) => ({ default: module.Dashboard })))
const Instances = lazy(() => import('@/pages/Instances').then((module) => ({ default: module.Instances })))
const InstanceDetail = lazy(() => import('@/pages/InstanceDetail').then((module) => ({ default: module.InstanceDetail })))
const Models = lazy(() => import('@/pages/Models').then((module) => ({ default: module.Models })))
const Tasks = lazy(() => import('@/pages/Tasks').then((module) => ({ default: module.Tasks })))
const TaskDetail = lazy(() => import('@/pages/TaskDetail').then((module) => ({ default: module.TaskDetail })))
const Hardware = lazy(() => import('@/pages/Hardware').then((module) => ({ default: module.Hardware })))
const SystemPage = lazy(() => import('@/pages/System').then((module) => ({ default: module.System })))
const Images = lazy(() => import('@/components/ImageManager').then((module) => ({ default: module.ImageManager })))

// 路由级加载态：稳定高度，避免侧栏与顶栏跳动。
function RouteFallback() {
  return <div className="flex min-h-[320px] items-center justify-center gap-2 text-sm text-slate-500"><LoaderCircle className="size-4 animate-spin" />正在加载页面…</div>
}

function Lazy({ children }: { children: ReactNode }) {
  return <Suspense fallback={<RouteFallback />}>{children}</Suspense>
}

function NotFound() {
  const navigate = useNavigate()
  return <div className="mx-auto max-w-xl py-16 text-center">
    <div className="text-5xl font-bold text-slate-300">404</div>
    <h1 className="mt-4 text-xl font-semibold text-slate-900">页面不存在</h1>
    <p className="mt-2 text-sm leading-6 text-slate-600">地址可能已变更或被手动修改。可返回首页看板继续操作。</p>
    <button type="button" className="btn-primary mt-6" onClick={() => navigate(pagePaths.dashboard)}>返回首页看板</button>
  </div>
}

// 各页面通过路由包装器消费统一数据层，页面组件本身保持只接收所需的展示属性。
function DashboardRoute() {
  const data = useAppData()
  const navigate = useNavigate()
  return <Lazy><Dashboard
    instances={data.displayInstances}
    models={data.displayModels}
    tasks={data.displayTasks}
    gpuCount={data.gpuNames.length}
    gpuSummary={data.hostInfo.gpuSummary}
    modelsTotal={data.realModels ? formatBytes(data.realModels.totalBytes) : undefined}
    onNavigate={(page: PageKey, options) => navigate(pagePath(page, options))}
    onCreate={data.openCreateDialog}
    onAddModel={data.openAddModelDialog}
  /></Lazy>
}

function InstancesRoute() {
  const data = useAppData()
  const navigate = useNavigate()
  return <Lazy><Instances
    instances={data.displayInstances}
    gpuNames={data.gpuNames}
    realMode={data.realInstances !== null}
    busyIds={data.busyIds}
    models={data.realModels}
    onCreate={data.openCreateDialog}
    onAction={data.instanceAction}
    onDelete={data.deleteInstance}
    onOpenDetail={(name) => navigate(`/instances/${encodeURIComponent(name)}`)}
  /></Lazy>
}

function InstanceDetailRoute() {
  const { name } = useParams()
  return <Lazy><InstanceDetail name={name ?? ''} /></Lazy>
}

function ModelsRoute() {
  const data = useAppData()
  const [params, setParams] = useSearchParams()
  const raw = params.get('tab')
  const tab = isModelTab(raw) ? raw : 'local'
  return <Lazy><Models
    tab={tab}
    onTabChange={(next) => {
      const updated = new URLSearchParams(params)
      updated.set('tab', next)
      setParams(updated, { replace: true })
    }}
    models={data.displayModels}
    realMode={data.realModels !== null}
    modelsRoot={data.realModels?.root}
    totalBytes={data.realModels?.totalBytes}
    network={data.realModels?.network}
    runningInstanceNames={data.displayInstances.filter((item) => item.status === 'running').map((item) => item.name)}
    onAdd={data.openAddModelDialog}
    onDownloadAction={data.downloadActionModel}
    onDelete={data.deleteModel}
    onLocalize={data.localizeModel}
    onBatchDelete={data.batchDeleteModels}
    onBatchLocalize={data.batchLocalizeModels}
    onMarketDownload={data.marketDownload}
    onOnlineDownload={data.onlineDownload}
    existingPaths={data.existingModelPaths}
  /></Lazy>
}

function TasksRoute() {
  return <Lazy><Tasks /></Lazy>
}

function TaskDetailRoute() {
  const { id } = useParams()
  return <Lazy><TaskDetail id={id ?? ''} /></Lazy>
}

function SystemRoute() {
  const { section } = useParams()
  const data = useAppData()
  return <Lazy><SystemPage
    section={section}
    instances={data.displayInstances}
    models={data.displayModels}
    tasks={data.displayTasks}
    version={data.backendVersion}
    onLoggedOut={() => void data.logout()}
  /></Lazy>
}

export function AppRoutes() {
  return <Routes>
    <Route path="/" element={<Navigate to={pagePaths.dashboard} replace />} />
    <Route path="/login" element={<Navigate to={pagePaths.dashboard} replace />} />
    <Route path="/setup" element={<Navigate to={pagePaths.dashboard} replace />} />
    <Route element={<ConsoleLayout />}>
      <Route path="/dashboard" element={<DashboardRoute />} />
      <Route path="/instances" element={<InstancesRoute />} />
      <Route path="/instances/:name" element={<InstanceDetailRoute />} />
      <Route path="/images" element={<Lazy><Images /></Lazy>} />
      <Route path="/models" element={<ModelsRoute />} />
      <Route path="/tasks" element={<TasksRoute />} />
      <Route path="/tasks/:id" element={<TaskDetailRoute />} />
      <Route path="/hardware" element={<Lazy><Hardware /></Lazy>} />
      <Route path="/system" element={<SystemRoute />} />
      <Route path="/system/:section" element={<SystemRoute />} />
      <Route path="*" element={<NotFound />} />
    </Route>
  </Routes>
}
