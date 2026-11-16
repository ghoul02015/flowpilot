// 控制台布局：侧栏、顶栏、移动端抽屉、全局弹窗与路由出口。
// 布局只承载导航与全局状态，页面数据仍由各页面按需订阅。
import { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router'
import { Activity, Box, ChevronRight, Cpu, Database, Images, LayoutDashboard, LogOut, Menu, Server, Settings, X } from 'lucide-react'
import { Toaster } from 'sonner'
import { AddModelDialog } from '@/components/AddModelDialog'
import { CreateInstanceDialog } from '@/components/CreateInstanceDialog'
import logoFull from '@/assets/logo-hl.png'
import { documentTitle, pageFromPath, pageLabels, pagePaths, pageTitles } from '@/lib/routes'
import { useAppData } from '@/state/app-data'
import type { PageKey } from '@/types'

const navigation: { key: PageKey; label: string; icon: typeof LayoutDashboard }[] = [
  { key: 'dashboard', label: pageLabels.dashboard, icon: LayoutDashboard },
  { key: 'instances', label: pageLabels.instances, icon: Box },
  { key: 'images', label: pageLabels.images, icon: Images },
  { key: 'models', label: pageLabels.models, icon: Database },
  { key: 'tasks', label: pageLabels.tasks, icon: Activity },
  { key: 'hardware', label: pageLabels.hardware, icon: Cpu },
  { key: 'system', label: pageLabels.system, icon: Settings },
]

export function ConsoleLayout() {
  const location = useLocation()
  const page = pageFromPath(location.pathname)
  const {
    adminUser, backendOnline, backendVersion, hostInfo, activeTasks, logout,
    displayInstances, displayModels, instanceDefaults, gpuNames,
    createOpen, setCreateOpen, addModelOpen, setAddModelOpen,
    createInstance, addModel, marketDownload, onlineDownload, existingModelPaths,
  } = useAppData()
  const [mobileOpen, setMobileOpen] = useState(false)

  // 详情页在面包屑追加对象名，避免所有详情都显示同一个一级标题。
  const segments = location.pathname.split('/').filter(Boolean)
  let detailName = ''
  if ((segments[0] === 'instances' || segments[0] === 'tasks') && segments.length > 1) {
    try {
      detailName = decodeURIComponent(segments[1])
    } catch {
      detailName = segments[1]
    }
  }

  // 页面标题跟随后续路由，浏览器标签与历史记录保持一致。
  useEffect(() => {
    document.title = documentTitle(page)
  }, [page])

  return (
    <div className="min-h-dvh bg-slate-50 text-slate-800">
      <Toaster position="top-right" richColors closeButton />
      {mobileOpen && <button type="button" aria-label="关闭导航" className="fixed inset-0 z-30 cursor-pointer bg-slate-950/45 md:hidden" onClick={() => setMobileOpen(false)} />}
      <aside className={`fixed inset-y-0 left-0 z-40 flex w-[248px] flex-col border-r border-slate-200 bg-white text-slate-600 transition-transform duration-200 md:translate-x-0 ${mobileOpen ? 'translate-x-0' : '-translate-x-full'}`}>
        <div className="flex h-20 items-center gap-3 border-b border-slate-100 px-5">
          <NavLink to={pagePaths.dashboard} aria-label="返回主页面" title="返回主页面" className="flex min-w-0 flex-1 flex-col justify-center gap-1 rounded-lg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-600" onClick={() => setMobileOpen(false)}>
            <img src={logoFull} alt="慧浪智算" className="h-11 w-auto max-w-full object-contain object-left" />
            <span className="text-[11px] font-medium tracking-wide text-slate-400">FlowPilot · ComfyUI 一体化管理平台</span>
          </NavLink>
          <button type="button" className="ml-auto cursor-pointer rounded-md p-2 text-slate-400 hover:bg-slate-100 md:hidden" aria-label="关闭菜单" onClick={() => setMobileOpen(false)}><X className="size-5" /></button>
        </div>
        <nav className="flex-1 space-y-1 px-3 py-5" aria-label="主导航">
          {navigation.map(({ key, label, icon: Icon }) => (
            <NavLink
              key={key}
              to={pagePaths[key]}
              onClick={() => setMobileOpen(false)}
              className={({ isActive }) => `flex min-h-12 w-full items-center gap-3 rounded-lg px-3 text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-600 ${isActive ? 'bg-brand-50 font-semibold text-brand-700' : 'text-slate-500 hover:bg-brand-50/60 hover:text-brand-700'}`}
              aria-current={page === key ? 'page' : undefined}
            >
              {({ isActive }) => <>
                <Icon className={`size-5 ${isActive ? 'text-brand-600' : ''}`} />
                <span className="flex-1 text-left">{label}</span>
                {key === 'tasks' && activeTasks > 0 && <span className="rounded-full bg-brand-100 px-2 py-0.5 text-xs text-brand-700">{activeTasks}</span>}
              </>}
            </NavLink>
          ))}
        </nav>
        <div className="mx-3 mb-3 rounded-lg border border-slate-200 bg-slate-50/70 p-4">
          <div className="flex items-center gap-2 text-sm font-medium text-slate-800"><span className={`size-2 rounded-full ${backendOnline ? 'bg-emerald-500' : 'bg-rose-500'}`} />{hostInfo.name}</div>
          <div className="mt-2 text-xs leading-5 text-slate-500">{hostInfo.gpuSummary}<br />{backendOnline ? '管理端运行正常' : '等待后端连接'}</div>
        </div>
        <div className="border-t border-slate-100 px-5 py-4 text-xs text-slate-400">FlowPilot · v{backendVersion}</div>
      </aside>

      <div className="min-h-dvh md:pl-[248px]">
        <header className="sticky top-0 z-20 flex h-16 items-center gap-3 border-b border-slate-200 bg-white/95 px-4 backdrop-blur md:px-8">
          <button type="button" className="icon-button md:hidden" aria-label="打开菜单" onClick={() => setMobileOpen(true)}><Menu className="size-5" /></button>
          <div className="hidden min-w-0 items-center gap-2 text-sm text-slate-500 sm:flex">
            <Server className="size-4 shrink-0" />
            <span className="max-w-40 truncate">{hostInfo.name}</span>
            <ChevronRight className="size-3.5 shrink-0" />
            <span className="max-w-52 truncate text-slate-800" title={detailName || pageLabels[page]}>{detailName ? `${pageLabels[page]} / ${detailName}` : location.pathname === '/dashboard' ? pageTitles[page] : pageLabels[page]}</span>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <span className={`hidden rounded-full border px-3 py-1.5 text-xs font-medium sm:inline ${backendOnline ? 'border-emerald-200 bg-emerald-50 text-emerald-800' : 'border-rose-200 bg-rose-50 text-rose-800'}`}>{backendOnline ? '后端已连接' : '后端连接失败'}</span>
            <NavLink to={pagePaths.tasks} className="btn-quiet" onClick={() => setMobileOpen(false)}>
              <Activity className="size-4" /><span className="hidden sm:inline">任务</span>
              {activeTasks > 0 && <span className="rounded-full bg-teal-100 px-1.5 text-xs text-teal-800">{activeTasks}</span>}
            </NavLink>
            <div className="flex items-center gap-2">
              <div className="flex size-9 items-center justify-center rounded-full bg-slate-900 text-xs font-semibold text-white" title={`管理员 ${adminUser}`}>AD</div>
              <button type="button" className="btn-quiet" title={`退出登录（${adminUser}）`} aria-label="退出登录" onClick={() => void logout()} data-testid="logout-button"><LogOut className="size-4" /></button>
            </div>
          </div>
        </header>
        <main className="mx-auto max-w-[1600px] p-4 md:p-8">
          <Outlet />
        </main>
      </div>

      <CreateInstanceDialog
        key={createOpen ? 'create-open' : 'create-closed'}
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSubmit={createInstance}
        models={displayModels}
        instances={displayInstances}
        gpuNames={gpuNames}
        defaultImage={instanceDefaults?.image}
        defaultContainerPort={instanceDefaults?.containerPort}
        dataDir={instanceDefaults?.dataDir}
        defaultModelsTarget={instanceDefaults?.modelsTarget}
        defaultOutputsTarget={instanceDefaults?.outputsTarget}
        defaultWorkflowsTarget={instanceDefaults?.workflowsTarget}
      />
      <AddModelDialog
        key={addModelOpen ? 'model-open' : 'model-closed'}
        open={addModelOpen}
        onOpenChange={setAddModelOpen}
        onSubmit={addModel}
        onMarketDownload={marketDownload}
        onOnlineDownload={onlineDownload}
        existingPaths={existingModelPaths}
      />
    </div>
  )
}
