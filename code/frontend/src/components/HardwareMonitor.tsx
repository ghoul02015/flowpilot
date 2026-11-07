import { useEffect, useMemo, useState } from 'react'
import { Activity, Cpu, Database, Gauge, HardDrive, MemoryStick, Server, Thermometer, Zap } from 'lucide-react'
import { getHardware, type HardwareReport } from '@/api'
import { Panel, PanelHeader, ProgressBar } from '@/components/common'
import { hardwareProfiles } from '@/data/hardware'
import type { GpuDevice, HardwareProfile } from '@/types'

const memoryModeNames = { discrete: '独立显存', unified: '统一内存', coherent: '一致性内存' }

function clamp(value: number) {
  return Math.max(0, Math.min(100, value))
}

function formatCapacity(gb: number) {
  return gb >= 1024 ? `${Number((gb / 1024).toFixed(2))} TB` : `${gb} GB`
}

// 采集失败时总量可能为 0，统一防除零产生 NaN。
function safePercent(used: number, total: number) {
  return total > 0 ? Math.round((used / total) * 100) : 0
}

function mbToGB(mb: number) {
  return Number((mb / 1024).toFixed(1))
}

function bytesToGB(bytes: number) {
  return Number((bytes / 1024 ** 3).toFixed(1))
}

function profileFromReport(report: HardwareReport): HardwareProfile {
  const unified = report.gpuMemoryMode === 'unified'
  return {
    id: 'real-host',
    hostName: report.hostName,
    productName: report.machineModel ? `${report.machineModel} · 实时采集` : '本机硬件 · 实时采集',
    os: report.os,
    architecture: report.architecture === 'arm64' ? 'arm64' : 'x86_64',
    cpuModel: report.cpuModel,
    cpuCores: report.cpuCores,
    cpuUsage: report.cpuUsagePercent ?? 0,
    memoryTotalGB: mbToGB(report.memoryTotalMB),
    memoryUsedGB: mbToGB(report.memoryUsedMB),
    memoryDescription: unified ? '统一内存 · CPU／GPU 动态共享（实测）' : '系统内存（/proc/meminfo 实测）',
    storageTotalGB: bytesToGB(report.storageTotalBytes),
    storageUsedGB: bytesToGB(report.storageUsedBytes),
    storageDescription: `数据盘实测 · ${report.storagePath}`,
    gpuMemoryMode: unified ? 'unified' : 'discrete',
    gpus: report.gpus.map((gpu) => ({
      id: gpu.index,
      model: gpu.name,
      memoryTotalGB: gpu.memoryTotalMB != null ? mbToGB(gpu.memoryTotalMB) : mbToGB(report.memoryTotalMB),
      memoryUsedGB: gpu.memoryUsedMB != null ? mbToGB(gpu.memoryUsedMB) : mbToGB(report.memoryUsedMB),
      memoryUsageSupported: gpu.memoryTotalMB != null,
      utilization: gpu.utilizationPercent != null ? Math.round(gpu.utilizationPercent) : null,
      temperatureC: gpu.temperatureC != null ? Math.round(gpu.temperatureC) : null,
      powerW: gpu.powerDrawW != null ? Math.round(gpu.powerDrawW) : null,
      powerLimitW: gpu.powerLimitW != null ? Math.round(gpu.powerLimitW) : null,
    })),
  }
}

function sampleDevices(profile: HardwareProfile, sample: number) {
  return profile.gpus.map((gpu, index) => ({
    ...gpu,
    utilization: gpu.utilization != null ? Math.round(clamp(gpu.utilization + Math.sin(sample * 0.72 + index) * 7)) : null,
    temperatureC: gpu.temperatureC != null ? Math.round(gpu.temperatureC + Math.sin(sample * 0.51 + index * 0.7) * 3) : null,
    powerW: gpu.powerW != null && gpu.powerLimitW != null ? Math.max(0, Math.round(gpu.powerW + Math.sin(sample * 0.66 + index * 0.8) * gpu.powerLimitW * 0.035)) : gpu.powerW,
    memoryUsedGB: Math.min(gpu.memoryTotalGB, Math.max(0, Number((gpu.memoryUsedGB + Math.sin(sample * 0.29 + index) * 0.4).toFixed(1)))),
  }))
}

function SummaryCard({ icon: Icon, label, value, description, usage }: { icon: typeof Server; label: string; value: string; description: string; usage?: number }) {
  return <div className="min-w-0 bg-white p-5">
    <div className="flex items-center gap-2 text-xs font-medium text-slate-500"><Icon className="size-4 text-teal-700" />{label}</div>
    <div className="mt-2 truncate text-sm font-semibold text-slate-900" title={value}>{value}</div>
    <div className="mt-1 min-h-8 text-xs leading-4 text-slate-500">{description}</div>
    {usage !== undefined && <div className="mt-3 flex items-center gap-2"><ProgressBar value={usage} tone="slate" /><span className="w-9 text-right text-xs font-medium text-slate-600">{usage}%</span></div>}
  </div>
}

function GpuCard({ gpu, profile }: { gpu: GpuDevice; profile: HardwareProfile }) {
  const memoryLabel = profile.gpuMemoryMode === 'unified' ? '统一内存' : profile.gpuMemoryMode === 'coherent' ? 'HBM 显存' : '显存'
  const memoryPercent = gpu.memoryUsageSupported ? safePercent(gpu.memoryUsedGB, gpu.memoryTotalGB) : safePercent(profile.memoryUsedGB, profile.memoryTotalGB)
  const utilization = gpu.utilization ?? 0
  return <article className="min-w-0 rounded-lg border border-slate-200 bg-slate-50/70 p-4" data-testid={`gpu-card-${gpu.id}`}>
    <div className="flex items-start justify-between gap-2">
      <div className="min-w-0"><div className="text-xs font-semibold tracking-wide text-teal-700">GPU {gpu.id}</div><h3 className="mt-1 truncate text-sm font-semibold text-slate-900" title={gpu.model}>{gpu.model}</h3></div>
      <span className="inline-flex shrink-0 items-center gap-1 text-xs text-emerald-700"><span className="size-1.5 rounded-full bg-emerald-500" />在线</span>
    </div>
    <div className="mt-4 grid grid-cols-2 gap-3 text-xs">
      <div><div className="flex items-center gap-1 text-slate-500"><Gauge className="size-3.5" />计算利用率</div><div className="mt-1 text-base font-semibold text-slate-800">{gpu.utilization != null ? `${gpu.utilization}%` : '—'}</div></div>
      <div><div className="flex items-center gap-1 text-slate-500"><Thermometer className="size-3.5" />核心温度</div><div className={`mt-1 text-base font-semibold ${(gpu.temperatureC ?? 0) >= 85 ? 'text-rose-600' : 'text-slate-800'}`}>{gpu.temperatureC != null ? `${gpu.temperatureC}°C` : '—'}</div></div>
      <div><div className="flex items-center gap-1 text-slate-500"><MemoryStick className="size-3.5" />{memoryLabel}</div><div className="mt-1 truncate text-sm font-semibold text-slate-800">{gpu.memoryUsageSupported ? `${gpu.memoryUsedGB}／${gpu.memoryTotalGB} GB` : `${formatCapacity(gpu.memoryTotalGB)} 共享`}</div></div>
      <div><div className="flex items-center gap-1 text-slate-500"><Zap className="size-3.5" />功耗</div><div className="mt-1 text-sm font-semibold text-slate-800">{gpu.powerW != null ? `${gpu.powerW} W` : '—'}</div>{gpu.powerLimitW != null && <div className="text-[11px] text-slate-400">上限 {gpu.powerLimitW} W</div>}</div>
    </div>
    <div className="mt-4 space-y-2">
      <div className="flex items-center gap-2"><span className="w-8 text-[11px] text-slate-500">计算</span><ProgressBar value={utilization} /><span className="w-8 text-right text-[11px] text-slate-500">{gpu.utilization != null ? `${gpu.utilization}%` : '—'}</span></div>
      <div className="flex items-center gap-2"><span className="w-8 text-[11px] text-slate-500">内存</span><ProgressBar value={memoryPercent} tone="slate" /><span className="w-8 text-right text-[11px] text-slate-500">{Math.round(memoryPercent)}%</span></div>
    </div>
    {!gpu.memoryUsageSupported && <p className="mt-3 text-[11px] leading-4 text-slate-500">独立显存占用不适用；此处显示系统统一内存使用率。</p>}
  </article>
}

export function HardwareMonitor() {
  const [profileId, setProfileId] = useState(hardwareProfiles[0].id)
  const [sample, setSample] = useState(0)
  const [report, setReport] = useState<HardwareReport | null>(null)
  const [reportFailed, setReportFailed] = useState(false)

  useEffect(() => {
    let cancelled = false
    async function poll() {
      try {
        const next = await getHardware()
        if (!cancelled) {
          setReport(next)
          setReportFailed(false)
        }
      } catch {
        if (!cancelled) setReportFailed(true)
      }
    }
    void poll()
    const timer = window.setInterval(() => {
      setSample((current) => current + 1)
      void poll()
    }, 2000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [])

  const realProfile = useMemo(() => (report ? profileFromReport(report) : null), [report])
  const isReal = realProfile !== null
  const profile = realProfile ?? (hardwareProfiles.find((item) => item.id === profileId) ?? hardwareProfiles[0])
  const devices = useMemo(() => (isReal ? profile.gpus : sampleDevices(profile, sample)), [isReal, profile, sample])
  const knownUtilizations = devices.map((gpu) => gpu.utilization).filter((value): value is number => value != null)
  const averageUsage = knownUtilizations.length ? Math.round(knownUtilizations.reduce((total, value) => total + value, 0) / knownUtilizations.length) : 0
  const totalGpuMemory = devices.reduce((total, gpu) => total + gpu.memoryTotalGB, 0)
  const memorySummary = profile.gpuMemoryMode === 'unified'
    ? `${formatCapacity(profile.memoryTotalGB)} CPU／GPU 共享，不重复累计`
    : profile.gpuMemoryMode === 'coherent'
      ? `${formatCapacity(profile.memoryTotalGB + totalGpuMemory)} 一致性地址空间`
      : `${devices.length} 卡共 ${formatCapacity(totalGpuMemory)} 显存`

  const cpuUsage = isReal ? Math.round(profile.cpuUsage) : Math.round(clamp(profile.cpuUsage + Math.sin(sample * 0.43) * 5))
  const memoryUsage = safePercent(profile.memoryUsedGB, profile.memoryTotalGB)
  const storageUsage = safePercent(profile.storageUsedGB, profile.storageTotalGB)

  return <Panel className="mt-5" data-hardware-source={isReal ? 'real' : 'demo'}>
    <PanelHeader title="硬件与实时监控" description={`${profile.hostName} · ${profile.productName}`} action={isReal
      ? <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 px-2.5 py-1 text-xs font-medium text-emerald-700"><Activity className="size-3.5" />本机实测</span>
      : <label className="flex items-center gap-2 text-xs whitespace-nowrap text-slate-500">演示机型<select aria-label="演示硬件机型" className="field-input min-h-10 max-w-64 bg-white py-1 text-xs" value={profileId} onChange={(event) => setProfileId(event.target.value)}>{hardwareProfiles.map((item) => <option key={item.id} value={item.id}>{item.productName}</option>)}</select></label>} />
    <div className="flex flex-col gap-2 border-b border-slate-200 bg-teal-50/60 px-5 py-3 text-xs text-teal-900 sm:flex-row sm:items-center sm:justify-between">
      <span className="inline-flex items-center gap-2"><span className="relative flex size-2"><span className="absolute inline-flex size-full animate-ping rounded-full bg-emerald-400 opacity-60" /><span className="relative inline-flex size-2 rounded-full bg-emerald-500" /></span>{isReal ? '2 秒自动刷新 · 真实采集自宿主机 nvidia-smi 与 /proc' : `2 秒自动刷新 · Demo 遥测${reportFailed ? '（硬件接口不可用，显示演示数据）' : '，后续接入宿主机采集'}`}</span>
      <span className="inline-flex items-center gap-1.5 font-medium"><Activity className="size-3.5" />{profile.architecture} · {memoryModeNames[profile.gpuMemoryMode]}</span>
    </div>
    <div className="grid gap-px bg-slate-200 sm:grid-cols-2 xl:grid-cols-4" data-telemetry-sample={sample}>
      <SummaryCard icon={Server} label="主机名称" value={profile.hostName} description={`${profile.os} · ${profile.architecture}`} />
      <SummaryCard icon={Cpu} label="CPU" value={`${profile.cpuModel} · ${profile.cpuCores} 核`} description={isReal && report?.cpuUsagePercent == null ? '利用率采样中…' : `实时利用率 ${cpuUsage}%`} usage={isReal && report?.cpuUsagePercent == null ? undefined : cpuUsage} />
      <SummaryCard icon={MemoryStick} label={profile.gpuMemoryMode === 'discrete' ? '内存' : profile.gpuMemoryMode === 'unified' ? '统一内存' : 'CPU 内存'} value={`${profile.memoryUsedGB}／${formatCapacity(profile.memoryTotalGB)} 已用`} description={profile.memoryDescription} usage={memoryUsage} />
      <SummaryCard icon={HardDrive} label="存储" value={`${formatCapacity(profile.storageUsedGB)}／${formatCapacity(profile.storageTotalGB)} 已用`} description={profile.storageDescription} usage={storageUsage} />
    </div>
    <div className="border-t border-slate-200 p-5">
      <div className="mb-4 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between"><div className="flex items-center gap-2"><Database className="size-4 text-teal-700" /><h3 className="text-sm font-semibold text-slate-900">GPU 设备</h3><span className="rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-600">{devices.length} 个</span></div><div className="text-xs text-slate-500">平均利用率 <strong className="font-semibold text-slate-800">{knownUtilizations.length ? `${averageUsage}%` : '—'}</strong><span className="mx-2 text-slate-300">|</span>{memorySummary}</div></div>
      {devices.length > 0
        ? <div className={`grid gap-3 ${devices.length >= 4 ? 'sm:grid-cols-2 xl:grid-cols-4' : 'sm:grid-cols-2'}`}>{devices.map((gpu) => <GpuCard key={gpu.id} gpu={gpu} profile={profile} />)}</div>
        : <p className="rounded-lg border border-dashed border-slate-300 bg-slate-50 p-6 text-center text-sm text-slate-500" data-testid="gpu-empty">未检测到 GPU 设备{report?.gpuError ? `：${report.gpuError}` : '。'}GPU 实例创建将被阻塞。</p>}
    </div>
  </Panel>
}
