import { useEffect, useState } from 'react'
import { Activity, Box, ChevronRight, Cpu, Database, Download, Gauge, HardDrive, MemoryStick, Plus, Server, Workflow } from 'lucide-react'
import { formatBytes, getHardware, type HardwareReport } from '@/api'
import { EmptyState, PageHeader, Panel, PanelHeader, ProgressBar, StatusBadge, TextButton } from '@/components/common'
import type { Instance, ModelItem, NavigateOptions, PageKey, TaskItem } from '@/types'

interface Props {
  instances: Instance[]
  models: ModelItem[]
  tasks: TaskItem[]
  gpuCount: number
  gpuSummary: string
  modelsTotal?: string
  onNavigate: (page: PageKey, options?: NavigateOptions) => void
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

function usagePercent(used: number, total: number) {
  return total > 0 ? Math.round((used / total) * 100) : 0
}

// 首页硬件简述条：一行概要 + 三条细进度条；机器名可点击进入硬件管理页。
function HardwareBrief({ gpuCount, gpuSummary, onNavigate }: { gpuCount: number; gpuSummary: string; onNavigate: Props['onNavigate'] }) {
  const [report, setReport] = useState<HardwareReport | null>(null)

  useEffect(() => {
    let cancelled = false
    async function poll() {
      try {
        const next = await getHardware()
        if (!cancelled) setReport(next)
      } catch {
        // 首页简述容忍采集失败，保留上一次数据
      }
    }
    void poll()
    const timer = window.setInterval(() => void poll(), 2000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [])

  const bars = report
    ? [
      { key: 'cpu', label: 'CPU', icon: Cpu, percent: report.cpuUsagePercent != null ? Math.round(report.cpuUsagePercent) : 0 },
      { key: 'memory', label: report.gpuMemoryMode === 'unified' ? '统一内存' : '内存', icon: MemoryStick, percent: usagePercent(report.memoryUsedMB, report.memoryTotalMB) },
      { key: 'storage', label: '存储', icon: HardDrive, percent: usagePercent(report.storageUsedBytes, report.storageTotalBytes) },
    ]
    : []

  return <Panel className="mt-5" data-testid="hardware-brief">
    <div className="flex flex-col gap-4 px-5 py-4 xl:flex-row xl:items-center xl:justify-between">
      <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center sm:gap-4">
        <button type="button" className="group flex min-w-0 cursor-pointer items-center gap-2 text-left" aria-label="打开硬件管理" onClick={() => onNavigate('hardware')}>
          <Server className="size-4 shrink-0 text-teal-700" />
          <span className="truncate text-sm font-semibold text-slate-900 group-hover:text-teal-700" title={report?.hostName}>{report?.hostName ?? '正在读取硬件…'}</span>
          <ChevronRight className="size-3.5 shrink-0 text-slate-400 transition-transform group-hover:translate-x-0.5 group-hover:text-teal-700" />
        </button>
        <div className="flex flex-wrap items-center gap-1.5 text-xs">
          {report?.machineModel && <span className="rounded bg-slate-100 px-1.5 py-0.5 text-slate-600">{report.machineModel}</span>}
          <span className="inline-flex items-center gap-1 rounded bg-teal-50 px-1.5 py-0.5 font-medium text-teal-700"><Gauge className="size-3" />{gpuCount > 0 ? `${gpuCount} 卡 · ${gpuSummary}` : '未检测到 GPU'}</span>
          {report?.driverVersion && <span className="rounded bg-slate-100 px-1.5 py-0.5 text-slate-500">驱动 {report.driverVersion}</span>}
          {report?.cudaVersion && <span className="rounded bg-slate-100 px-1.5 py-0.5 text-slate-500">CUDA {report.cudaVersion}</span>}
        </div>
      </div>
      {report
        ? <div className="grid shrink-0 gap-3 sm:grid-cols-3 xl:w-[460px]">
          {bars.map(({ key, label, icon: Icon, percent }) => <div key={key} className="min-w-0">
            <div className="flex items-center justify-between gap-2 text-[11px] text-slate-500"><span className="inline-flex items-center gap-1"><Icon className="size-3" />{label}</span><strong className="font-semibold text-slate-700">{percent}%</strong></div>
            <div className="mt-1"><ProgressBar value={percent} tone="slate" /></div>
          </div>)}
        </div>
        : <span className="text-xs text-slate-400">硬件采集接口连接中…</span>}
    </div>
    <div className="flex flex-col gap-1 border-t border-slate-200 px-5 py-2.5 text-xs text-slate-500 sm:flex-row sm:items-center sm:justify-between">
      <span>硬件状态每 2 秒自动刷新 · 点击机器名进入硬件管理</span>
      {report && <span>数据盘 {formatBytes(report.storageUsedBytes)}／{formatBytes(report.storageTotalBytes)}</span>}
    </div>
  </Panel>
}

export function Dashboard({ instances, models, tasks, gpuCount, gpuSummary, modelsTotal, onNavigate, onCreate, onAddModel }: Props) {
  const running = instances.filter((item) => item.status === 'running').length
  const activeTasks = tasks.filter((item) => ['running', 'paused'].includes(item.status)).length
  const metrics = [
    { label: '运行实例', value: `${running}／${instances.length}`, note: '单主机 Docker 实例', icon: Box, page: 'instances' as PageKey },
    { label: 'GPU 设备', value: gpuCount > 0 ? `${gpuCount} 卡` : '未检测到', note: gpuSummary, icon: Gauge, page: 'hardware' as PageKey },
    { label: '就绪模型', value: String(models.filter((item) => item.status === 'ready').length), note: modelsTotal ? `共享仓库 ${modelsTotal}` : '共享仓库', icon: Database, page: 'models' as PageKey },
    { label: '进行中任务', value: String(activeTasks), note: '下载与导入任务', icon: Activity, page: 'tasks' as PageKey },
  ]
  return <>
    <PageHeader eyebrow="OVERVIEW" title={`${greeting()}，管理员`} description="集中查看单机硬件、GPU 遥测、ComfyUI 实例和模型任务状态。" action={<button type="button" className="btn-primary" onClick={onCreate}><Plus className="size-4" />新建实例</button>} />
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      {metrics.map(({ label, value, note, icon: Icon, page }) => <Panel key={label} className="p-5">
        <button type="button" className="w-full cursor-pointer text-left" onClick={() => onNavigate(page)} title={`进入${label}`}>
          <div className="flex items-start justify-between"><div><div className="text-sm text-slate-500">{label}</div><div className="mt-3 text-3xl font-bold tracking-tight text-slate-900">{value}</div><div className="mt-2 max-w-52 truncate text-xs text-slate-500" title={note}>{note}</div></div><Icon className="size-5 text-teal-700" /></div>
        </button>
      </Panel>)}
    </div>
    <HardwareBrief gpuCount={gpuCount} gpuSummary={gpuSummary} onNavigate={onNavigate} />

    <Panel className="mt-5">
      <PanelHeader title="快捷操作" description="常用交付与运维入口" />
      <div className="grid gap-2 p-3 md:grid-cols-3">
        {[{ label: '部署 ComfyUI', note: '选择镜像、GPU 和端口', icon: Server, action: onCreate }, { label: '添加模型', note: 'URL 下载或本地导入', icon: Download, action: onAddModel }, { label: '查看任务', note: `${activeTasks} 个任务等待处理`, icon: Workflow, action: () => onNavigate('tasks') }].map(({ label, note, icon: Icon, action }) => <button type="button" key={label} onClick={action} className="flex min-h-16 cursor-pointer items-center gap-3 rounded-lg px-3 text-left transition-colors hover:bg-slate-100 focus-visible:outline-teal-600"><Icon className="size-5 text-teal-700" /><span className="flex-1"><span className="block text-sm font-medium text-slate-800">{label}</span><span className="mt-0.5 block text-xs text-slate-500">{note}</span></span></button>)}
      </div>
    </Panel>
    <div className="mt-5 grid gap-5 xl:grid-cols-[1.45fr_1fr]">
      <Panel>
        <PanelHeader title="实例概览" description="最近创建的 ComfyUI 实例 · 点击实例名进入管理，图标直达 ComfyUI" action={<TextButton onClick={() => onNavigate('instances')}>全部实例</TextButton>} />
        <div className="overflow-x-auto"><table className="data-table min-w-[760px]"><thead><tr><th>实例</th><th>状态</th><th>GPU</th><th>端口</th><th>制图任务</th><th>应用</th></tr></thead><tbody>{instances.slice(0, 3).map((item) => <tr key={item.id}>
          <td><button type="button" className="cursor-pointer text-left font-medium text-slate-800 hover:text-teal-700" onClick={() => onNavigate('instances', { focus: item.name })}>{item.name}</button><div className="mt-1 text-xs text-slate-500">{item.version}</div></td>
          <td><StatusBadge status={item.status} /></td><td>{item.gpu >= 0 ? `GPU ${item.gpu}` : '仅 CPU'}</td><td className="font-mono text-xs">{item.port}</td>
          <td>{item.status === 'running'
            ? item.comfy
              ? item.comfy.reachable
                ? <span className="text-sm text-slate-800">{item.comfy.queueRunning > 0 || item.comfy.queuePending > 0 ? <>运行 <strong className="text-teal-700">{item.comfy.queueRunning}</strong> · 排队 <strong>{item.comfy.queuePending}</strong></> : <span className="text-slate-500">空闲</span>}</span>
                : <span className="text-xs text-slate-400">服务启动中…</span>
              : <span className="text-xs text-slate-400">探测中…</span>
            : <span className="text-xs text-slate-400">—</span>}</td>
          <td>{item.status === 'running'
            ? <a className="inline-flex items-center gap-1 text-xs font-medium text-teal-700 hover:text-teal-900" href={`http://${window.location.hostname}:${item.port}`} target="_blank" rel="noopener noreferrer">打开<ChevronRight className="size-3.5" /></a>
            : <span className="text-xs text-slate-400">—</span>}</td>
        </tr>)}</tbody></table></div>
        {instances.length === 0 && <EmptyState title="暂无 ComfyUI 实例" description="点击右上角「新建实例」开始部署。" />}
      </Panel>
      <Panel>
        <PanelHeader title="最近任务" description="模型下载与导入任务动态" action={<TextButton onClick={() => onNavigate('tasks')}>任务中心</TextButton>} />
        <div className="divide-y divide-slate-100 px-5">{tasks.slice(0, 3).map((task) => <div key={task.id} className="py-4"><div className="flex items-center justify-between gap-3"><span className="truncate text-sm font-medium text-slate-800">{task.name}</span><StatusBadge status={task.status} /></div><div className="mt-3 flex items-center gap-3"><ProgressBar value={task.progress} /><span className="w-9 text-right text-xs text-slate-500">{task.progress}%</span></div></div>)}{tasks.length === 0 && <EmptyState title="暂无任务" description="添加模型后，下载与导入任务会显示在这里。" />}</div>
      </Panel>
    </div>
  </>
}
