import { useEffect, useMemo, useState } from 'react'
import { Activity, Cpu, Database, Gauge, HardDrive, MemoryStick, Server, Thermometer, Zap } from 'lucide-react'
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

function sampleDevices(profile: HardwareProfile, sample: number) {
  return profile.gpus.map((gpu, index) => ({
    ...gpu,
    utilization: Math.round(clamp(gpu.utilization + Math.sin(sample * 0.72 + index) * 7)),
    temperatureC: Math.round(gpu.temperatureC + Math.sin(sample * 0.51 + index * 0.7) * 3),
    powerW: Math.max(0, Math.round(gpu.powerW + Math.sin(sample * 0.66 + index * 0.8) * gpu.powerLimitW * 0.035)),
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
  const memoryPercent = gpu.memoryUsageSupported ? (gpu.memoryUsedGB / gpu.memoryTotalGB) * 100 : (profile.memoryUsedGB / profile.memoryTotalGB) * 100
  return <article className="min-w-0 rounded-lg border border-slate-200 bg-slate-50/70 p-4" data-testid={`gpu-card-${gpu.id}`}>
    <div className="flex items-start justify-between gap-2">
      <div className="min-w-0"><div className="text-xs font-semibold tracking-wide text-teal-700">GPU {gpu.id}</div><h3 className="mt-1 truncate text-sm font-semibold text-slate-900" title={gpu.model}>{gpu.model}</h3></div>
      <span className="inline-flex shrink-0 items-center gap-1 text-xs text-emerald-700"><span className="size-1.5 rounded-full bg-emerald-500" />在线</span>
    </div>
    <div className="mt-4 grid grid-cols-2 gap-3 text-xs">
      <div><div className="flex items-center gap-1 text-slate-500"><Gauge className="size-3.5" />计算利用率</div><div className="mt-1 text-base font-semibold text-slate-800">{gpu.utilization}%</div></div>
      <div><div className="flex items-center gap-1 text-slate-500"><Thermometer className="size-3.5" />核心温度</div><div className={`mt-1 text-base font-semibold ${gpu.temperatureC >= 85 ? 'text-rose-600' : 'text-slate-800'}`}>{gpu.temperatureC}°C</div></div>
      <div><div className="flex items-center gap-1 text-slate-500"><MemoryStick className="size-3.5" />{memoryLabel}</div><div className="mt-1 truncate text-sm font-semibold text-slate-800">{gpu.memoryUsageSupported ? `${gpu.memoryUsedGB}／${gpu.memoryTotalGB} GB` : `${formatCapacity(gpu.memoryTotalGB)} 共享`}</div></div>
      <div><div className="flex items-center gap-1 text-slate-500"><Zap className="size-3.5" />功耗</div><div className="mt-1 text-sm font-semibold text-slate-800">{gpu.powerW} W</div><div className="text-[11px] text-slate-400">上限 {gpu.powerLimitW} W</div></div>
    </div>
    <div className="mt-4 space-y-2">
      <div className="flex items-center gap-2"><span className="w-8 text-[11px] text-slate-500">计算</span><ProgressBar value={gpu.utilization} /><span className="w-8 text-right text-[11px] text-slate-500">{gpu.utilization}%</span></div>
      <div className="flex items-center gap-2"><span className="w-8 text-[11px] text-slate-500">内存</span><ProgressBar value={memoryPercent} tone="slate" /><span className="w-8 text-right text-[11px] text-slate-500">{Math.round(memoryPercent)}%</span></div>
    </div>
    {!gpu.memoryUsageSupported && <p className="mt-3 text-[11px] leading-4 text-slate-500">独立显存占用不适用；此处显示系统统一内存使用率。</p>}
  </article>
}

export function HardwareMonitor() {
  const [profileId, setProfileId] = useState(hardwareProfiles[0].id)
  const [sample, setSample] = useState(0)
  const profile = hardwareProfiles.find((item) => item.id === profileId) ?? hardwareProfiles[0]
  const devices = useMemo(() => sampleDevices(profile, sample), [profile, sample])
  const averageUsage = Math.round(devices.reduce((total, gpu) => total + gpu.utilization, 0) / devices.length)
  const totalGpuMemory = devices.reduce((total, gpu) => total + gpu.memoryTotalGB, 0)
  const memorySummary = profile.gpuMemoryMode === 'unified'
    ? `${formatCapacity(profile.memoryTotalGB)} CPU／GPU 共享，不重复累计`
    : profile.gpuMemoryMode === 'coherent'
      ? `${formatCapacity(profile.memoryTotalGB + totalGpuMemory)} 一致性地址空间`
      : `${devices.length} 卡共 ${formatCapacity(totalGpuMemory)} 显存`

  useEffect(() => {
    const timer = window.setInterval(() => setSample((current) => current + 1), 2000)
    return () => window.clearInterval(timer)
  }, [])

  const cpuUsage = Math.round(clamp(profile.cpuUsage + Math.sin(sample * 0.43) * 5))
  const memoryUsage = Math.round((profile.memoryUsedGB / profile.memoryTotalGB) * 100)
  const storageUsage = Math.round((profile.storageUsedGB / profile.storageTotalGB) * 100)

  return <Panel className="mt-5">
    <PanelHeader title="硬件与实时监控" description={`${profile.hostName} · ${profile.productName}`} action={<label className="flex items-center gap-2 text-xs whitespace-nowrap text-slate-500">演示机型<select aria-label="演示硬件机型" className="field-input min-h-10 max-w-64 bg-white py-1 text-xs" value={profileId} onChange={(event) => setProfileId(event.target.value)}>{hardwareProfiles.map((item) => <option key={item.id} value={item.id}>{item.productName}</option>)}</select></label>} />
    <div className="flex flex-col gap-2 border-b border-slate-200 bg-teal-50/60 px-5 py-3 text-xs text-teal-900 sm:flex-row sm:items-center sm:justify-between">
      <span className="inline-flex items-center gap-2"><span className="relative flex size-2"><span className="absolute inline-flex size-full animate-ping rounded-full bg-emerald-400 opacity-60" /><span className="relative inline-flex size-2 rounded-full bg-emerald-500" /></span>2 秒自动刷新 · Demo 遥测，后续接入宿主机采集</span>
      <span className="inline-flex items-center gap-1.5 font-medium"><Activity className="size-3.5" />{profile.architecture} · {memoryModeNames[profile.gpuMemoryMode]}</span>
    </div>
    <div className="grid gap-px bg-slate-200 sm:grid-cols-2 xl:grid-cols-4" data-telemetry-sample={sample}>
      <SummaryCard icon={Server} label="主机名称" value={profile.hostName} description={`${profile.os} · ${profile.architecture}`} />
      <SummaryCard icon={Cpu} label="CPU" value={`${profile.cpuModel} · ${profile.cpuCores} 核`} description={`实时利用率 ${cpuUsage}%`} usage={cpuUsage} />
      <SummaryCard icon={MemoryStick} label={profile.gpuMemoryMode === 'discrete' ? '内存' : profile.gpuMemoryMode === 'unified' ? '统一内存' : 'CPU 内存'} value={`${profile.memoryUsedGB}／${formatCapacity(profile.memoryTotalGB)} 已用`} description={profile.memoryDescription} usage={memoryUsage} />
      <SummaryCard icon={HardDrive} label="存储" value={`${formatCapacity(profile.storageUsedGB)}／${formatCapacity(profile.storageTotalGB)} 已用`} description={profile.storageDescription} usage={storageUsage} />
    </div>
    <div className="border-t border-slate-200 p-5">
      <div className="mb-4 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between"><div className="flex items-center gap-2"><Database className="size-4 text-teal-700" /><h3 className="text-sm font-semibold text-slate-900">GPU 设备</h3><span className="rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-600">{devices.length} 个</span></div><div className="text-xs text-slate-500">平均利用率 <strong className="font-semibold text-slate-800">{averageUsage}%</strong><span className="mx-2 text-slate-300">|</span>{memorySummary}</div></div>
      <div className={`grid gap-3 ${devices.length >= 4 ? 'sm:grid-cols-2 xl:grid-cols-4' : 'sm:grid-cols-2'}`}>{devices.map((gpu) => <GpuCard key={gpu.id} gpu={gpu} profile={profile} />)}</div>
    </div>
  </Panel>
}
