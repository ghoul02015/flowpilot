import { useEffect, useState } from 'react'
import { CheckCircle2, CircleAlert, Download, FileJson, RefreshCw, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'
import { formatBytes, getHardware, getRuntimeSnapshot, type HardwareReport, type RuntimeSnapshot } from '@/api'
import { PageHeader, Panel, PanelHeader } from '@/components/common'
import type { Instance, ModelItem, TaskItem } from '@/types'

type SystemTab = 'environment' | 'about'
type RuntimeView = { status: 'loading' | 'connected' | 'fallback'; snapshot?: RuntimeSnapshot; error?: string }

interface Props {
  instances: Instance[]
  models: ModelItem[]
  tasks: TaskItem[]
}

export function System({ instances, models, tasks }: Props) {
  const [tab, setTab] = useState<SystemTab>('environment')
  const [checking, setChecking] = useState(false)
  const [lastCheck, setLastCheck] = useState('尚未检测')
  const [runtime, setRuntime] = useState<RuntimeView>({ status: 'loading' })
  const [hardware, setHardware] = useState<HardwareReport | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    getRuntimeSnapshot(controller.signal)
      .then((snapshot) => {
        setRuntime({ status: 'connected', snapshot })
        setLastCheck(new Date().toLocaleString('zh-CN', { hour12: false }))
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return
        setRuntime({ status: 'fallback', error: error instanceof Error ? error.message : '后端连接失败' })
      })
    getHardware(controller.signal)
      .then((report) => setHardware(report))
      .catch(() => {})
    return () => controller.abort()
  }, [])

  function checkEnvironment() {
    setChecking(true)
    getRuntimeSnapshot()
      .then((snapshot) => {
        setRuntime({ status: 'connected', snapshot })
        setLastCheck(new Date().toLocaleString('zh-CN', { hour12: false }))
        toast.success('环境检测完成', { description: '后端、Docker、硬件采集与容器列表均已连接。' })
      })
      .catch((error: unknown) => {
        setRuntime({ status: 'fallback', error: error instanceof Error ? error.message : '后端连接失败' })
        toast.error('后端连接失败', { description: '请确认 FlowPilot 服务已启动后重新检测。' })
      })
      .finally(() => setChecking(false))
    getHardware()
      .then((report) => setHardware(report))
      .catch(() => setHardware(null))
  }

  function exportDiagnostics() {
    const content = JSON.stringify({ product: 'FlowPilot', version: '0.0.6.3', mode: 'backend', exportedAt: new Date().toISOString(), host: runtime.snapshot?.engine.info.Name ?? hardware?.hostName ?? '未知主机', runtime, hardware, instances, models, tasks }, null, 2)
    const url = URL.createObjectURL(new Blob([content], { type: 'application/json' }))
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = 'flowpilot-diagnostics-0.0.6.3.json'
    anchor.click()
    URL.revokeObjectURL(url)
    toast.success('诊断包已导出', { description: '包含后端、Docker、硬件采集与业务数据摘要，不含密码或令牌。' })
  }

  const tabs: { key: SystemTab; label: string }[] = [{ key: 'environment', label: '环境检测' }, { key: 'about', label: '诊断与关于' }]
  const hostName = runtime.snapshot?.engine.info.Name ?? hardware?.hostName ?? '未知主机'
  return <>
    <PageHeader eyebrow="SYSTEM" title="系统管理" description="查看主机环境、Docker 与硬件采集状态，导出用于售后排障的脱敏诊断信息。" />
    <div className="mb-5 flex gap-1 overflow-x-auto rounded-lg border border-slate-200 bg-white p-1" role="tablist" aria-label="系统管理页面">{tabs.map((item) => <button type="button" role="tab" aria-selected={tab === item.key} key={item.key} className={`min-h-11 flex-1 cursor-pointer rounded-md px-4 text-sm font-medium whitespace-nowrap transition-colors ${tab === item.key ? 'bg-slate-900 text-white' : 'text-slate-600 hover:bg-slate-100'}`} onClick={() => setTab(item.key)}>{item.label}</button>)}</div>

    {tab === 'environment' && <Panel>
      <PanelHeader title="主机环境报告" description={`${hostName} · 最近检测 ${lastCheck}`} action={<button type="button" className="btn-secondary" disabled={checking} onClick={checkEnvironment}><RefreshCw className={`size-4 ${checking ? 'animate-spin' : ''}`} />{checking ? '检测中' : '重新检测'}</button>} />
      <div className={`flex items-start gap-3 border-b px-5 py-4 text-sm ${runtime.status === 'connected' ? 'border-emerald-200 bg-emerald-50 text-emerald-900' : runtime.status === 'fallback' ? 'border-amber-200 bg-amber-50 text-amber-900' : 'border-slate-200 bg-slate-50 text-slate-600'}`}>
        {runtime.status === 'connected' ? <CheckCircle2 className="mt-0.5 size-5 shrink-0" /> : <CircleAlert className="mt-0.5 size-5 shrink-0" />}
        <div><strong>{runtime.status === 'connected' ? '后端已连接' : runtime.status === 'fallback' ? '后端连接失败' : '正在连接后端'}</strong><div className="mt-0.5 leading-6">{runtime.status === 'connected' ? `Go 后端 ${runtime.snapshot?.health.version} · Docker、硬件采集与业务接口均为真实数据。` : runtime.error ?? '正在读取健康状态、Docker Engine 和容器列表。'}</div></div>
      </div>
      <div className="grid gap-px overflow-hidden bg-slate-200 sm:grid-cols-2">{[
        ['操作系统', hardware ? `${hardware.os} · ${hardware.architecture}` : '等待硬件采集接口', Boolean(hardware)],
        ['处理器与内存', hardware ? `${hardware.cpuModel} · ${hardware.cpuCores} 核 · ${Math.round(hardware.memoryTotalMB / 1024)} GB` : '等待硬件采集接口', Boolean(hardware)],
        ['GPU', hardware ? (hardware.gpus.length ? `${hardware.gpus.length} × ${hardware.gpus[0].name}${hardware.gpuMemoryMode === 'unified' ? ' · 统一内存' : ''}` : `未检测到 GPU${hardware.gpuError ? `：${hardware.gpuError}` : ''}`) : '等待硬件采集接口', Boolean(hardware?.gpus.length)],
        ['数据盘', hardware ? `${formatBytes(hardware.storageUsedBytes)}／${formatBytes(hardware.storageTotalBytes)} 已用 · ${hardware.storagePath}` : '等待硬件采集接口', Boolean(hardware)],
        ['Docker Engine', runtime.status === 'connected' ? `${runtime.snapshot?.engine.version.Version} · API 正常` : runtime.status === 'fallback' ? '连接失败' : '正在读取', runtime.status === 'connected'],
        ['Docker 容器', runtime.status === 'connected' ? `共 ${runtime.snapshot?.engine.info.Containers} 个，运行 ${runtime.snapshot?.engine.info.ContainersRunning} 个` : '等待后端连接', runtime.status === 'connected'],
        ['管理端口', runtime.status === 'connected' ? '18080 · 后端健康检查通过' : '18080 · 等待健康检查', runtime.status === 'connected'],
      ].map(([label, value, ok]) => <div key={String(label)} className="flex items-start gap-3 bg-white p-5">{ok ? <CheckCircle2 className="mt-0.5 size-5 shrink-0 text-emerald-600" /> : <CircleAlert className="mt-0.5 size-5 text-amber-600" />}<div><div className="text-sm font-medium text-slate-900">{label}</div><div className="mt-1 text-sm leading-6 text-slate-600">{value}</div></div></div>)}
      </div>
      <div className="border-t border-slate-200 px-5 py-4 text-xs leading-5 text-slate-500">管理端口建议仅在管理网开放，不要将 FlowPilot 管理台直接暴露到公网；当前版本无认证机制。</div>
    </Panel>}

    {tab === 'about' && <div className="grid gap-5 lg:grid-cols-2"><Panel><PanelHeader title="诊断信息" description="导出环境、实例、模型和任务摘要，不包含密码或令牌。" /><div className="p-5"><FileJson className="size-8 text-teal-700" /><h3 className="mt-4 font-semibold text-slate-900">FlowPilot 诊断包</h3><p className="mt-2 text-sm leading-6 text-slate-600">用于售后定位安装、Docker、GPU 和任务问题。诊断包全部来自真实后端与硬件采集数据。</p><button type="button" className="btn-primary mt-5" onClick={exportDiagnostics}><Download className="size-4" />导出诊断包</button></div></Panel><Panel><PanelHeader title="关于 FlowPilot" description="慧浪智算 GPU 服务器配套软件" /><div className="p-5"><div className="flex items-center gap-3"><ShieldCheck className="size-8 text-teal-700" /><div><div className="font-semibold text-slate-900">慧浪智算·FlowPilot</div><div className="mt-1 text-sm text-slate-500">ComfyUI 一体化部署与管理平台</div></div></div><dl className="mt-5 space-y-3 text-sm"><div className="flex justify-between"><dt className="text-slate-500">项目版本</dt><dd className="font-mono font-medium">0.0.6.3</dd></div><div className="flex justify-between"><dt className="text-slate-500">运行模式</dt><dd>Go 后端＋Web 管理台</dd></div><div className="flex justify-between"><dt className="text-slate-500">界面语言</dt><dd>简体中文</dd></div><div className="flex justify-between"><dt className="text-slate-500">数据方式</dt><dd>真实 Docker API＋硬件采集＋共享仓库</dd></div></dl></div></Panel></div>}
  </>
}
