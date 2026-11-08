import { Activity, Box, Database, Download, Gauge, Plus, Server, Workflow } from 'lucide-react'
import { EmptyState, PageHeader, Panel, PanelHeader, ProgressBar, StatusBadge, TextButton } from '@/components/common'
import { HardwareMonitor } from '@/components/HardwareMonitor'
import type { Instance, ModelItem, PageKey, TaskItem } from '@/types'

interface Props {
  instances: Instance[]
  models: ModelItem[]
  tasks: TaskItem[]
  gpuCount: number
  gpuSummary: string
  modelsTotal?: string
  onNavigate: (page: PageKey) => void
  onCreate: () => void
  onAddModel: () => void
}

// 按当前时段生成问候语，避免静态文案与实际时间不符。
function greeting(): string {
  const hour = new Date().getHours()
  if (hour < 6) return '凌晨好'
  if (hour < 12) return '上午好'
  if (hour < 18) return '下午好'
  return '晚上好'
}

export function Dashboard({ instances, models, tasks, gpuCount, gpuSummary, modelsTotal, onNavigate, onCreate, onAddModel }: Props) {
  const running = instances.filter((item) => item.status === 'running').length
  const activeTasks = tasks.filter((item) => ['running', 'paused'].includes(item.status)).length
  const metrics = [
    { label: '运行实例', value: `${running}／${instances.length}`, note: '单主机 Docker 实例', icon: Box },
    { label: 'GPU 设备', value: gpuCount > 0 ? `${gpuCount} 卡` : '未检测到', note: gpuSummary, icon: Gauge },
    { label: '就绪模型', value: String(models.filter((item) => item.status === 'ready').length), note: modelsTotal ? `共享仓库 ${modelsTotal}` : '共享仓库', icon: Database },
    { label: '进行中任务', value: String(activeTasks), note: '下载与导入任务', icon: Activity },
  ]
  return <>
    <PageHeader eyebrow="OVERVIEW" title={`${greeting()}，管理员`} description="集中查看单机硬件、GPU 遥测、ComfyUI 实例和模型任务状态。" action={<button type="button" className="btn-primary" onClick={onCreate}><Plus className="size-4" />新建实例</button>} />
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      {metrics.map(({ label, value, note, icon: Icon }) => <Panel key={label} className="p-5">
        <div className="flex items-start justify-between"><div><div className="text-sm text-slate-500">{label}</div><div className="mt-3 text-3xl font-bold tracking-tight text-slate-900">{value}</div><div className="mt-2 max-w-52 truncate text-xs text-slate-500" title={note}>{note}</div></div><Icon className="size-5 text-teal-700" /></div>
      </Panel>)}
    </div>
    <HardwareMonitor />

    <Panel className="mt-5">
      <PanelHeader title="快捷操作" description="常用交付与运维入口" />
      <div className="grid gap-2 p-3 md:grid-cols-3">
        {[{ label: '部署 ComfyUI', note: '选择镜像、GPU 和端口', icon: Server, action: onCreate }, { label: '添加模型', note: 'URL 下载或本地导入', icon: Download, action: onAddModel }, { label: '查看任务', note: `${activeTasks} 个任务等待处理`, icon: Workflow, action: () => onNavigate('tasks') }].map(({ label, note, icon: Icon, action }) => <button type="button" key={label} onClick={action} className="flex min-h-16 cursor-pointer items-center gap-3 rounded-lg px-3 text-left transition-colors hover:bg-slate-100 focus-visible:outline-teal-600"><Icon className="size-5 text-teal-700" /><span className="flex-1"><span className="block text-sm font-medium text-slate-800">{label}</span><span className="mt-0.5 block text-xs text-slate-500">{note}</span></span></button>)}
      </div>
    </Panel>
    <div className="mt-5 grid gap-5 xl:grid-cols-[1.45fr_1fr]">
      <Panel>
        <PanelHeader title="实例概览" description="最近创建的 ComfyUI 实例" action={<TextButton onClick={() => onNavigate('instances')}>全部实例</TextButton>} />
        <div className="overflow-x-auto"><table className="data-table min-w-[720px]"><thead><tr><th>实例</th><th>状态</th><th>GPU</th><th>端口</th><th>制图任务</th></tr></thead><tbody>{instances.slice(0, 3).map((item) => <tr key={item.id}><td><div className="font-medium text-slate-800">{item.name}</div><div className="mt-1 text-xs text-slate-500">{item.version}</div></td><td><StatusBadge status={item.status} /></td><td>{item.gpu >= 0 ? `GPU ${item.gpu}` : '仅 CPU'}</td><td className="font-mono text-xs">{item.port}</td><td>{item.status === 'running'
          ? item.comfy
            ? item.comfy.reachable
              ? <span className="text-sm text-slate-800">{item.comfy.queueRunning > 0 || item.comfy.queuePending > 0 ? <>运行 <strong className="text-teal-700">{item.comfy.queueRunning}</strong> · 排队 <strong>{item.comfy.queuePending}</strong></> : <span className="text-slate-500">空闲</span>}</span>
              : <span className="text-xs text-slate-400">服务启动中…</span>
            : <span className="text-xs text-slate-400">探测中…</span>
          : <span className="text-xs text-slate-400">—</span>}</td></tr>)}</tbody></table></div>
        {instances.length === 0 && <EmptyState title="暂无 ComfyUI 实例" description="点击右上角「新建实例」开始部署。" />}
      </Panel>
      <Panel>
        <PanelHeader title="最近任务" description="模型下载与导入任务动态" action={<TextButton onClick={() => onNavigate('tasks')}>任务中心</TextButton>} />
        <div className="divide-y divide-slate-100 px-5">{tasks.slice(0, 3).map((task) => <div key={task.id} className="py-4"><div className="flex items-center justify-between gap-3"><span className="truncate text-sm font-medium text-slate-800">{task.name}</span><StatusBadge status={task.status} /></div><div className="mt-3 flex items-center gap-3"><ProgressBar value={task.progress} /><span className="w-9 text-right text-xs text-slate-500">{task.progress}%</span></div></div>)}{tasks.length === 0 && <EmptyState title="暂无任务" description="添加模型后，下载与导入任务会显示在这里。" />}</div>
      </Panel>
    </div>
  </>
}
