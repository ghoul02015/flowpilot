import { Activity, Box, Cpu, Database, Download, Gauge, Plus, RefreshCw, Server, Workflow } from 'lucide-react'
import { EmptyState, PageHeader, Panel, PanelHeader, ProgressBar, StatusBadge, TextButton } from '@/components/common'
import type { Instance, ModelItem, PageKey, TaskItem } from '@/types'

interface Props {
  instances: Instance[]
  models: ModelItem[]
  tasks: TaskItem[]
  onNavigate: (page: PageKey) => void
  onCreate: () => void
  onAddModel: () => void
}

export function Dashboard({ instances, models, tasks, onNavigate, onCreate, onAddModel }: Props) {
  const running = instances.filter((item) => item.status === 'running').length
  const activeTasks = tasks.filter((item) => ['running', 'paused'].includes(item.status)).length
  const metrics = [
    { label: '运行实例', value: `${running}／${instances.length}`, note: '单主机 Docker 实例', icon: Box },
    { label: 'GPU 平均利用率', value: '62%', note: '2 × NVIDIA L40S', icon: Gauge },
    { label: '就绪模型', value: String(models.filter((item) => item.status === 'ready').length), note: '共享仓库 86.4 GB', icon: Database },
    { label: '进行中任务', value: String(activeTasks), note: '下载与实例部署', icon: Activity },
  ]
  return <>
    <PageHeader eyebrow="OVERVIEW" title="下午好，管理员" description="慧浪 GPU-S01 运行状态正常，所有关键服务已完成最近一次检测。" action={<button type="button" className="btn-primary" onClick={onCreate}><Plus className="size-4" />新建实例</button>} />
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      {metrics.map(({ label, value, note, icon: Icon }) => <Panel key={label} className="p-5">
        <div className="flex items-start justify-between"><div><div className="text-sm text-slate-500">{label}</div><div className="mt-3 text-3xl font-bold tracking-tight text-slate-900">{value}</div><div className="mt-2 text-xs text-slate-500">{note}</div></div><Icon className="size-5 text-teal-700" /></div>
      </Panel>)}
    </div>
    <div className="mt-5 grid gap-5 xl:grid-cols-[1.45fr_1fr]">
      <Panel>
        <PanelHeader title="GPU 资源" description="实时数据为 Demo 模拟值" action={<button type="button" className="btn-quiet" onClick={() => onNavigate('system')}><RefreshCw className="size-4" />查看环境</button>} />
        <div className="space-y-6 p-5">
          {[{ gpu: 0, usage: 78, memory: 34.2, temp: 67 }, { gpu: 1, usage: 46, memory: 21.8, temp: 58 }].map((item) => <div key={item.gpu}>
            <div className="mb-3 flex flex-wrap items-center justify-between gap-2"><div className="flex items-center gap-2 font-medium text-slate-800"><Cpu className="size-4 text-slate-500" />GPU {item.gpu} · NVIDIA L40S</div><span className="text-xs text-slate-500">{item.temp}°C · 48 GB</span></div>
            <div className="grid grid-cols-[64px_1fr_48px] items-center gap-3 text-xs text-slate-500"><span>计算</span><ProgressBar value={item.usage} /><span className="text-right font-semibold text-slate-700">{item.usage}%</span></div>
            <div className="mt-2 grid grid-cols-[64px_1fr_64px] items-center gap-3 text-xs text-slate-500"><span>显存</span><ProgressBar value={(item.memory / 48) * 100} tone="slate" /><span className="text-right text-slate-600">{item.memory} GB</span></div>
          </div>)}
        </div>
      </Panel>
      <Panel>
        <PanelHeader title="快捷操作" description="常用交付与运维入口" />
        <div className="grid gap-2 p-3">
          {[{ label: '部署 ComfyUI', note: '选择模板、GPU 和模型', icon: Server, action: onCreate }, { label: '添加模型', note: 'URL 下载或本地导入', icon: Download, action: onAddModel }, { label: '查看任务', note: `${activeTasks} 个任务等待处理`, icon: Workflow, action: () => onNavigate('tasks') }].map(({ label, note, icon: Icon, action }) => <button type="button" key={label} onClick={action} className="flex min-h-16 cursor-pointer items-center gap-3 rounded-lg px-3 text-left transition-colors hover:bg-slate-100 focus-visible:outline-teal-600"><Icon className="size-5 text-teal-700" /><span className="flex-1"><span className="block text-sm font-medium text-slate-800">{label}</span><span className="mt-0.5 block text-xs text-slate-500">{note}</span></span></button>)}
        </div>
      </Panel>
    </div>
    <div className="mt-5 grid gap-5 xl:grid-cols-[1.45fr_1fr]">
      <Panel>
        <PanelHeader title="实例概览" description="最近创建的 ComfyUI 实例" action={<TextButton onClick={() => onNavigate('instances')}>全部实例</TextButton>} />
        <div className="overflow-x-auto"><table className="data-table min-w-[620px]"><thead><tr><th>实例</th><th>状态</th><th>GPU</th><th>端口</th></tr></thead><tbody>{instances.slice(0, 3).map((item) => <tr key={item.id}><td><div className="font-medium text-slate-800">{item.name}</div><div className="mt-1 text-xs text-slate-500">{item.version}</div></td><td><StatusBadge status={item.status} /></td><td>GPU {item.gpu}</td><td className="font-mono text-xs">{item.port}</td></tr>)}</tbody></table></div>
      </Panel>
      <Panel>
        <PanelHeader title="最近任务" description="部署和模型任务动态" action={<TextButton onClick={() => onNavigate('tasks')}>任务中心</TextButton>} />
        <div className="divide-y divide-slate-100 px-5">{tasks.slice(0, 3).map((task) => <div key={task.id} className="py-4"><div className="flex items-center justify-between gap-3"><span className="truncate text-sm font-medium text-slate-800">{task.name}</span><StatusBadge status={task.status} /></div><div className="mt-3 flex items-center gap-3"><ProgressBar value={task.progress} /><span className="w-9 text-right text-xs text-slate-500">{task.progress}%</span></div></div>)}{tasks.length === 0 && <EmptyState title="暂无任务" description="新建实例或添加模型后，任务会显示在这里。" />}</div>
      </Panel>
    </div>
  </>
}
