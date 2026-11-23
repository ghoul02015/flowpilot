// 集中路由契约：页面路径、标题与跨页跳转映射。
// 所有页面地址只在这里定义，避免各处硬编码字符串漂移。
import type { NavigateOptions, PageKey } from '@/types'

export const pagePaths: Record<PageKey, string> = {
  dashboard: '/dashboard',
  instances: '/instances',
  images: '/images',
  models: '/models',
  tasks: '/tasks',
  hardware: '/hardware',
  system: '/system',
}

export const pageLabels: Record<PageKey, string> = {
  dashboard: '首页看板',
  instances: 'ComfyUI 实例',
  images: '镜像中心',
  models: '模型中心',
  tasks: '任务中心',
  hardware: '硬件管理',
  system: '系统管理',
}

export const pageTitles: Record<PageKey, string> = {
  dashboard: '首页看板',
  instances: '实例管理',
  images: '镜像中心',
  models: '模型中心',
  tasks: '任务中心',
  hardware: '硬件管理',
  system: '系统管理',
}

export const systemSections = ['environment', 'storage', 'database', 'migration', 'backup', 'security', 'audit', 'about'] as const
export type SystemSection = (typeof systemSections)[number]

export const modelTabs = ['local', 'online', 'bundles'] as const
export type ModelTab = (typeof modelTabs)[number]

export function isSystemSection(value: string | undefined): value is SystemSection {
  return Boolean(value) && (systemSections as readonly string[]).includes(value as string)
}

export function isModelTab(value: string | null | undefined): value is ModelTab {
  return Boolean(value) && (modelTabs as readonly string[]).includes(value as string)
}

// navigate(page, { focus })：把旧的页面状态跳转映射为真实路径。
// 实例与任务的定位信息进入路径参数，其余页面忽略 focus。
export function pagePath(page: PageKey, options?: NavigateOptions): string {
  if (options?.focus) {
    if (page === 'instances') return `/instances/${encodeURIComponent(options.focus)}`
    if (page === 'tasks') return `/tasks/${encodeURIComponent(options.focus)}`
    if (page === 'models') return `/models?tab=${isModelTab(options.focus) ? options.focus : 'local'}`
  }
  return pagePaths[page]
}

// 从路径反推当前一级页面，用于轮询启用判断与导航高亮。
export function pageFromPath(pathname: string): PageKey {
  if (pathname === '/' || pathname === '') return 'dashboard'
  const segment = pathname.split('/').filter(Boolean)[0]
  const match = (Object.keys(pagePaths) as PageKey[]).find((key) => key === segment)
  return match ?? 'dashboard'
}

// 只接受站内绝对路径，拒绝协议、协议相对地址与反斜杠，避免开放重定向。
export function safeNextPath(raw: string | null): string | null {
  if (!raw) return null
  let value = raw
  try {
    value = decodeURIComponent(raw)
  } catch {
    return null
  }
  if (!value.startsWith('/') || value.startsWith('//') || value.startsWith('/\\')) return null
  if (value.includes('\\') || /^[a-z]+:/i.test(value)) return null
  if (value.startsWith('/login') || value.startsWith('/setup')) return null
  return value
}

export function documentTitle(page?: PageKey, extra?: string): string {
  const base = '慧浪智算 · FlowPilot'
  if (extra) return `${extra} · ${base}`
  if (page) return `${pageTitles[page]} · ${base}`
  return base
}
