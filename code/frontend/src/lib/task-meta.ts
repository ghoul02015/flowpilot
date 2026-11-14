// 统一任务展示元数据：状态、类型、进度与时间格式在列表页和详情页共用。

export const statusMeta: Record<string, { label: string; className: string }> = {
  queued: { label: '排队中', className: 'border-slate-200 bg-slate-50 text-slate-600' },
  running: { label: '运行中', className: 'border-teal-200 bg-teal-50 text-teal-700' },
  paused: { label: '已暂停', className: 'border-amber-200 bg-amber-50 text-amber-700' },
  succeeded: { label: '已完成', className: 'border-emerald-200 bg-emerald-50 text-emerald-700' },
  failed: { label: '失败', className: 'border-rose-200 bg-rose-50 text-rose-700' },
  cancelled: { label: '已取消', className: 'border-slate-200 bg-slate-50 text-slate-500' },
  partial: { label: '部分完成', className: 'border-orange-200 bg-orange-50 text-orange-700' },
}

export const typeMeta: Record<string, string> = {
  download: '模型下载',
  import: '模型导入',
  'image-pull': '镜像拉取',
  'image-build': '镜像构建',
  migration: '数据迁移',
  backup: '平台备份',
}

export const taskTypes = Object.keys(typeMeta)
export const taskStatuses = Object.keys(statusMeta)

export function isActiveStatus(status: string): boolean {
  return ['queued', 'running', 'paused'].includes(status)
}

export function isDownloadType(type: string): boolean {
  return type === 'download' || type === 'import'
}

export function formatTaskTime(value?: string): string {
  if (!value) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString('zh-CN', { hour12: false })
}
