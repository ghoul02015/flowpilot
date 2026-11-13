import { useEffect, useState } from 'react'
import { Activity, ChevronDown, CircleAlert, Cpu, Gauge, HardDrive, MemoryStick, Server, Thermometer, Zap } from 'lucide-react'
import { formatBytes, getHardware, type HardwareReport, type RealGPU } from '@/api'
import { EmptyState, PageHeader, Panel, PanelHeader, ProgressBar } from '@/components/common'
import { usePolling } from '@/hooks/use-polling'
import { getHardwareCompat, type CompatItem } from '@/api'

function clamp(value: number) {
  return Math.max(0, Math.min(100, value))
}

function percent(used: number, total: number) {
  return total > 0 ? Math.round((used / total) * 100) : 0
}

function mbToGB(mb: number) {
  return Number((mb / 1024).toFixed(1))
}

function formatGB(gb: number) {
  return gb >= 1024 ? `${Number((gb / 1024).toFixed(2))} TB` : `${gb} GB`
}

// GPU 状态徽章：高温或高负载给出明确告警色，其余显示在线。
function GpuStateBadge({ gpu }: { gpu: RealGPU }) {
  const hot = (gpu.temperatureC ?? 0) >= 83
  const busy = (gpu.utilizationPercent ?? 0) >= 85
  if (hot) return <span className="inline-flex items-center gap-1.5 rounded-full bg-rose-50 px-2.5 py-1 text-xs font-medium text-rose-700"><CircleAlert className="size-3.5" />高温</span>
  if (busy) return <span className="inline-flex items-center gap-1.5 rounded-full bg-amber-50 px-2.5 py-1 text-xs font-medium text-amber-800"><Gauge className="size-3.5" />高负载</span>
  return <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 px-2.5 py-1 text-xs font-medium text-emerald-700"><span className="size-1.5 rounded-full bg-emerald-500" />在线</span>
}

function Chip({ label, value }: { label: string; value: string }) {
  return <span className="inline-flex items-center gap-1 rounded-md bg-slate-100 px-2 py-1 text-xs text-slate-600">{label}<strong className="font-semibold text-slate-800">{value}</strong></span>
}

// CompatSection 兼容性检查区块（0.0.9.13）。
function CompatSection() {
  const [items, setItems] = useState<CompatItem[]>([])
  const [summary, setSummary] = useState<Record<string, number>>({})
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    let cancelled = false
    getHardwareCompat()
      .then((report: { items: CompatItem[]; summary: Record<string, number> }) => {
        if (!cancelled) {
          setItems(report.items)
          setSummary(report.summary)
        }
      })
      .catch(() => {})
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [])
  const meta: Record<string, { label: string; className: string }> = {
    pass: { label: '通过', className: 'border-emerald-200 bg-emerald-50 text-emerald-700' },
    warn: { label: '警告', className: 'border-amber-200 bg-amber-50 text-amber-700' },
    block: { label: '阻断', className: 'border-rose-200 bg-rose-50 text-rose-700' },
  }
  return <Panel data-testid="compat-section">
    <PanelHeader title="兼容性检查" description={`通过 ${summary.pass ?? 0} · 警告 ${summary.warn ?? 0} · 阻断 ${summary.block ?? 0}（只读判定，附修复建议）`} />
    <div className="divide-y divide-slate-100">
      {loading && <div className="p-6 text-center text-sm text-slate-500">检查中…</div>}
      {items.map((item) => { const badge = meta[item.result] ?? { label: item.result, className: 'border-slate-200 bg-slate-50 text-slate-500' }; return (
        <div key={item.id} className="flex items-start gap-3 px-5 py-4">
          <span className={`mt-0.5 inline-flex shrink-0 items-center rounded-full border px-2 py-0.5 text-xs font-medium ${badge.className}`}>{badge.label}</span>
          <div className="min-w-0">
            <div className="text-sm font-medium text-slate-900">{item.name}</div>
            <div className="mt-0.5 text-sm leading-6 break-all text-slate-600">{item.detail}</div>
            {item.advice && <div className="mt-1 text-xs text-amber-700">建议：{item.advice}</div>}
          </div>
        </div>
      ) })}
    </div>
  </Panel>
}

export function Hardware() {
  const [report, setReport] = useState<HardwareReport | null>(null)
  const [error, setError] = useState<string | null>(null)

  usePolling(async (signal) => {
    try {
      setReport(await getHardware(signal))
      setError(null)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '硬件采集接口不可用')
    }
  }, { interval: 2000 })

  const unified = report?.gpuMemoryMode === 'unified'
  const gpus = report?.gpus ?? []
  const cpuUsage = report?.cpuUsagePercent != null ? Math.round(clamp(report.cpuUsagePercent)) : 0
  const memoryUsage = report ? percent(report.memoryUsedMB, report.memoryTotalMB) : 0
  const storageUsage = report ? percent(report.storageUsedBytes, report.storageTotalBytes) : 0
  const knownUtilizations = gpus.map((gpu) => gpu.utilizationPercent).filter((value): value is number => value != null)
  const averageUtilization = knownUtilizations.length ? Math.round(knownUtilizations.reduce((total, value) => total + value, 0) / knownUtilizations.length) : 0

  return <>
    <PageHeader eyebrow="HARDWARE" title="硬件管理" description="按物理机器分组展示 CPU、内存、存储与 GPU 逐卡状态；数据每 2 秒从宿主机 nvidia-smi 与 /proc 实时采集，只读展示。" />

    {error && <div className="mb-4 flex items-center gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900"><CircleAlert className="size-3.5 shrink-0" />硬件采集接口暂不可用（{error}），每 2 秒自动重试。</div>}

    {!report && !error && <Panel><div className="p-10 text-center text-sm text-slate-500">正在读取宿主机硬件信息…</div></Panel>}

    {report && <Panel data-testid="hardware-machine" data-hardware-collected={report.collectedAt}>
      <PanelHeader
        title={`计算机器 · ${report.hostName}`}
        description={report.machineModel ? `${report.machineModel} · ${report.os} · ${report.architecture}` : `${report.os} · ${report.architecture}`}
        action={<span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 px-2.5 py-1 text-xs font-medium text-emerald-700"><Activity className="size-3.5" />每 2 秒采集</span>}
      />
      <details open className="group">
        <summary className="flex cursor-pointer items-center gap-2 border-b border-slate-200 bg-slate-50/60 px-5 py-3 text-sm font-medium text-slate-700 select-none">
          <ChevronDown className="size-4 transition-transform group-open:rotate-180" aria-hidden="true" />
          机器概要与 GPU 明细（{gpus.length} 卡）
          <span className="ml-auto flex flex-wrap items-center gap-1.5 font-normal">
            {report.driverVersion && <Chip label="驱动" value={report.driverVersion} />}
            {report.cudaVersion && <Chip label="CUDA" value={report.cudaVersion} />}
          </span>
        </summary>

        <div className="grid gap-px bg-slate-200 sm:grid-cols-2 xl:grid-cols-4">
          <div className="min-w-0 bg-white p-5">
            <div className="flex items-center gap-2 text-xs font-medium text-slate-500"><Server className="size-4 text-teal-700" />主机</div>
            <div className="mt-2 truncate text-sm font-semibold text-slate-900" title={report.hostName}>{report.hostName}</div>
            <div className="mt-1 text-xs leading-5 text-slate-500">{report.os} · {report.architecture}</div>
          </div>
          <div className="min-w-0 bg-white p-5">
            <div className="flex items-center gap-2 text-xs font-medium text-slate-500"><Cpu className="size-4 text-teal-700" />CPU</div>
            <div className="mt-2 truncate text-sm font-semibold text-slate-900" title={report.cpuModel}>{report.cpuModel} · {report.cpuCores} 核</div>
            <div className="mt-3 flex items-center gap-2"><ProgressBar value={cpuUsage} tone="slate" /><span className="w-10 text-right text-xs font-medium text-slate-600">{report.cpuUsagePercent == null ? '—' : `${cpuUsage}%`}</span></div>
          </div>
          <div className="min-w-0 bg-white p-5">
            <div className="flex items-center gap-2 text-xs font-medium text-slate-500"><MemoryStick className="size-4 text-teal-700" />{unified ? '统一内存' : '内存'}</div>
            <div className="mt-2 text-sm font-semibold text-slate-900">{formatGB(mbToGB(report.memoryUsedMB))}／{formatGB(mbToGB(report.memoryTotalMB))}</div>
            <div className="mt-3 flex items-center gap-2"><ProgressBar value={memoryUsage} tone="slate" /><span className="w-10 text-right text-xs font-medium text-slate-600">{memoryUsage}%</span></div>
          </div>
          <div className="min-w-0 bg-white p-5">
            <div className="flex items-center gap-2 text-xs font-medium text-slate-500"><HardDrive className="size-4 text-teal-700" />存储</div>
            <div className="mt-2 text-sm font-semibold text-slate-900">{formatBytes(report.storageUsedBytes)}／{formatBytes(report.storageTotalBytes)}</div>
            <div className="mt-3 flex items-center gap-2"><ProgressBar value={storageUsage} tone="slate" /><span className="w-10 text-right text-xs font-medium text-slate-600">{storageUsage}%</span></div>
          </div>
        </div>

        <div className="border-t border-slate-200 p-5">
          <div className="mb-3 flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
            <h3 className="text-sm font-semibold text-slate-900">GPU 明细</h3>
            <span className="text-xs text-slate-500">平均利用率 <strong className="font-semibold text-slate-800">{knownUtilizations.length ? `${averageUtilization}%` : '—'}</strong>{unified ? ' · 统一内存架构，显存为系统共享' : ''}</span>
          </div>
          {gpus.length === 0
            ? <EmptyState title="未检测到 GPU 设备" description={report.gpuError ? `采集返回：${report.gpuError}。GPU 实例创建将被阻塞。` : '当前宿主机没有可用的 GPU。'} />
            : <div className="overflow-x-auto"><table className="data-table min-w-[860px]" data-testid="hardware-gpu-table">
              <thead><tr><th>卡号</th><th>型号</th><th>{unified ? '统一内存' : '显存'}</th><th>利用率</th><th>温度</th><th>功耗</th><th>状态</th></tr></thead>
              <tbody>{gpus.map((gpu) => {
                const memorySupported = gpu.memoryTotalMB != null && gpu.memoryUsedMB != null
                const memoryPercent = memorySupported ? percent(gpu.memoryUsedMB ?? 0, gpu.memoryTotalMB ?? 0) : memoryUsage
                return <tr key={gpu.index} data-testid={`hardware-gpu-${gpu.index}`}>
                  <td className="font-mono text-xs font-semibold text-teal-700">GPU {gpu.index}</td>
                  <td className="max-w-56 truncate font-medium text-slate-800" title={gpu.name}>{gpu.name}</td>
                  <td>{memorySupported
                    ? <div className="min-w-32"><div className="text-xs text-slate-700">{formatGB(mbToGB(gpu.memoryUsedMB ?? 0))}／{formatGB(mbToGB(gpu.memoryTotalMB ?? 0))}</div><div className="mt-1.5 flex items-center gap-2"><ProgressBar value={memoryPercent} tone="slate" /><span className="w-9 text-right text-xs text-slate-500">{memoryPercent}%</span></div></div>
                    : <span className="text-xs text-slate-500">{unified ? `共享 ${formatGB(mbToGB(report.memoryTotalMB))} · ${memoryUsage}%` : '—'}</span>}</td>
                  <td><div className="min-w-28"><div className={gpu.utilizationPercent != null && gpu.utilizationPercent >= 85 ? 'text-sm font-semibold text-amber-700' : 'text-sm text-slate-800'}>{gpu.utilizationPercent != null ? `${Math.round(gpu.utilizationPercent)}%` : '—'}</div><div className="mt-1.5"><ProgressBar value={clamp(gpu.utilizationPercent ?? 0)} /></div></div></td>
                  <td><span className={`inline-flex items-center gap-1 text-sm ${(gpu.temperatureC ?? 0) >= 83 ? 'font-semibold text-rose-600' : 'text-slate-800'}`}><Thermometer className="size-3.5 text-slate-400" />{gpu.temperatureC != null ? `${Math.round(gpu.temperatureC)}°C` : '—'}</span></td>
                  <td><span className="inline-flex items-center gap-1 text-sm text-slate-800"><Zap className="size-3.5 text-slate-400" />{gpu.powerDrawW != null ? `${Math.round(gpu.powerDrawW)} W` : '—'}{gpu.powerLimitW != null && <span className="text-xs text-slate-400">/ {Math.round(gpu.powerLimitW)}</span>}</span></td>
                  <td><GpuStateBadge gpu={gpu} /></td>
                </tr>
              })}</tbody>
            </table></div>}
        </div>
      </details>
    </Panel>}

    <CompatSection />  </>
}
