import { useMemo, useState } from 'react'
import { Ban, FileText, RotateCcw } from 'lucide-react'
import { EmptyState, PageHeader, Panel, ProgressBar, StatusBadge } from '@/components/common'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { TaskItem } from '@/types'

interface Props {
  tasks: TaskItem[]
  onAction: (id: string, action: 'cancel' | 'retry') => void
}

export function Tasks({ tasks, onAction }: Props) {
  const [status, setStatus] = useState('all')
  const [logId, setLogId] = useState<string | null>(null)
  const filtered = useMemo(() => tasks.filter((task) => status === 'all' || task.status === status), [tasks, status])
  const logTask = tasks.find((task) => task.id === logId)
  return <>
    <PageHeader eyebrow="TASK CENTER" title="任务中心" description="集中跟踪镜像拉取、实例部署、模型下载和诊断导出，失败原因清晰可查。" />
    <Panel>
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 p-4"><div className="text-sm text-slate-600">共 {filtered.length} 个任务</div><select className="field-input sm:w-44" aria-label="按任务状态筛选" value={status} onChange={(event) => setStatus(event.target.value)}><option value="all">全部状态</option><option value="running">进行中</option><option value="paused">已暂停</option><option value="completed">已完成</option><option value="failed">失败</option><option value="cancelled">已取消</option></select></div>
      <div className="divide-y divide-slate-100">{filtered.map((task) => <div key={task.id} className="grid gap-4 px-5 py-5 lg:grid-cols-[1.3fr_1fr_auto] lg:items-center">
        <div><div className="flex flex-wrap items-center gap-2"><span className="font-medium text-slate-900">{task.name}</span><StatusBadge status={task.status} /></div><div className="mt-2 text-xs text-slate-500">{task.type} · {task.createdAt}</div><p className="mt-2 text-sm leading-6 text-slate-600">{task.message}</p></div>
        <div className="flex items-center gap-3"><ProgressBar value={task.progress} tone={task.status === 'failed' ? 'slate' : 'teal'} /><span className="w-10 text-right text-xs font-medium text-slate-600">{task.progress}%</span></div>
        <div className="flex items-center gap-1"><button type="button" className="btn-quiet" onClick={() => setLogId(task.id)}><FileText className="size-4" />日志</button>{['running', 'paused'].includes(task.status) && <button type="button" className="icon-button" title="取消任务" aria-label={`取消 ${task.name}`} onClick={() => onAction(task.id, 'cancel')}><Ban className="size-4" /></button>}{['failed', 'cancelled'].includes(task.status) && <button type="button" className="btn-quiet" onClick={() => onAction(task.id, 'retry')}><RotateCcw className="size-4" />重试</button>}</div>
      </div>)}{filtered.length === 0 && <EmptyState title="当前筛选下没有任务" description="切换状态筛选查看其他任务。" />}</div>
    </Panel>
    <Dialog open={Boolean(logTask)} onOpenChange={(open) => !open && setLogId(null)}><DialogContent className="sm:max-w-2xl">{logTask && <><DialogHeader><DialogTitle>任务日志</DialogTitle><DialogDescription>{logTask.name} · {logTask.id}</DialogDescription></DialogHeader><pre className="max-h-80 overflow-auto rounded-lg bg-slate-950 p-4 text-xs leading-6 whitespace-pre-wrap text-slate-200">{`[${logTask.createdAt}] 创建任务 ${logTask.id}\n[INFO] 任务类型：${logTask.type}\n[INFO] 目标对象：${logTask.target}\n[INFO] 当前进度：${logTask.progress}%\n[${logTask.status === 'failed' ? 'ERROR' : 'INFO'}] ${logTask.message}`}</pre><DialogFooter><button type="button" className="btn-primary" onClick={() => setLogId(null)}>关闭</button></DialogFooter></>}</DialogContent></Dialog>
  </>
}
