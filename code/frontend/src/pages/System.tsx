import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router'
import { CheckCircle2, CircleAlert, Download, FileJson, RefreshCw, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'
import { formatBytes, getHardware, getRuntimeSnapshot, type HardwareReport, type RuntimeSnapshot } from '@/api'
import { PageHeader, Panel, PanelHeader } from '@/components/common'
import { NetworkStoragePanel } from '@/components/NetworkStoragePanel'
import { DatabasePanel } from '@/components/DatabasePanel'
import { SecurityPanel } from '@/components/SecurityPanel'
import { AuditPanel } from '@/components/AuditPanel'
import { MigrationPanel } from '@/components/MigrationPanel'
import { BackupPanel } from '@/components/BackupPanel'
import { isSystemSection, type SystemSection } from '@/lib/routes'
import type { Instance, ModelItem, TaskItem } from '@/types'

type RuntimeView = { status: 'loading' | 'connected' | 'fallback'; snapshot?: RuntimeSnapshot; error?: string }

interface Props {
  instances: Instance[]
  models: ModelItem[]
  tasks: TaskItem[]
  version: string
  onLoggedOut: () => void
  /** 当前系统分区来自 /system/:section；非法值回退到环境检测并提示。 */
  section?: string
}

// 四组信息架构：环境与兼容性／存储与数据／安全与审计／维护与支持。
const sectionGroups: { key: string; label: string; sections: { key: SystemSection; label: string }[] }[] = [
  { key: 'environment', label: '环境与兼容性', sections: [{ key: 'environment', label: '环境检测' }] },
  { key: 'storage', label: '存储与数据', sections: [{ key: 'storage', label: '网络存储' }, { key: 'database', label: '数据底座' }, { key: 'migration', label: '数据迁移' }, { key: 'backup', label: '存储与备份' }] },
  { key: 'security', label: '安全与审计', sections: [{ key: 'security', label: '安全与会话' }, { key: 'audit', label: '操作审计' }] },
  { key: 'support', label: '维护与支持', sections: [{ key: 'about', label: '诊断与关于' }] },
]

export function System({ instances, models, tasks, version, onLoggedOut, section }: Props) {
  const navigate = useNavigate()
  const active: SystemSection = isSystemSection(section) ? section : 'environment'
  const group = sectionGroups.find((item) => item.sections.some((entry) => entry.key === active)) ?? sectionGroups[0]
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
    const content = JSON.stringify({ product: 'FlowPilot', version, mode: 'backend', exportedAt: new Date().toISOString(), host: runtime.snapshot?.engine.info.Name ?? hardware?.hostName ?? '未知主机', runtime, hardware, instances, models, tasks }, null, 2)
    const url = URL.createObjectURL(new Blob([content], { type: 'application/json' }))
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `flowpilot-diagnostics-${version}.json`
    anchor.click()
    URL.revokeObjectURL(url)
    toast.success('诊断包已导出', { description: '包含后端、Docker、硬件采集与业务数据摘要，不含密码或令牌。' })
  }

  const tabs = sectionGroups
  const hostName = runtime.snapshot?.engine.info.Name ?? hardware?.hostName ?? '未知主机'
  return <>
    <PageHeader eyebrow="SYSTEM" title="系统管理" description="查看主机环境、Docker 与硬件采集状态，管理存储、数据、安全审计，并导出用于售后排障的脱敏诊断信息。" />
    <div className="mb-3 flex gap-1 overflow-x-auto rounded-lg border border-slate-200 bg-white p-1" role="tablist" aria-label="系统管理分组">
      {tabs.map((item) => {
        const selected = group.key === item.key
        return <button key={item.key} type="button" role="tab" aria-selected={selected} className={`min-h-11 flex-1 cursor-pointer rounded-md px-4 text-sm font-medium whitespace-nowrap transition-colors ${selected ? 'bg-teal-700 text-white' : 'text-slate-600 hover:bg-slate-100'}`} onClick={() => { if (!selected) navigate(`/system/${item.sections[0].key}`) }}>{item.label}</button>
      })}
    </div>
    {group.sections.length > 1 && <div className="mb-5 flex flex-wrap gap-1 rounded-lg bg-slate-100 p-1 text-sm" role="tablist" aria-label={`${group.label}二级导航`}>
      {group.sections.map((item) => <button key={item.key} type="button" role="tab" aria-selected={active === item.key} className={`min-h-9 rounded-md px-3 py-1.5 font-medium transition ${active === item.key ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-600 hover:text-slate-900'}`} onClick={() => navigate(`/system/${item.key}`)}>{item.label}</button>)}
    </div>}
    {section && section !== active && <div className="mb-5 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">未识别的系统分区「{section}」，已回到环境检测。可通过上方分组进入网络存储、安全审计或诊断与关于。</div>}

    {active === 'environment' && <Panel>
      <PanelHeader title="主机环境报告" description={`${hostName} · 最近检测 ${lastCheck}`} action={<button type="button" className="btn-secondary" disabled={checking} onClick={checkEnvironment}><RefreshCw className={`size-4 ${checking ? 'animate-spin' : ''}`} />{checking ? '检测中' : '重新检测'}</button>} />
      <div className={`flex items-start gap-3 border-b px-5 py-4 text-sm ${runtime.status === 'connected' ? 'border-emerald-200 bg-emerald-50 text-emerald-900' : runtime.status === 'fallback' ? 'border-amber-200 bg-amber-50 text-amber-900' : 'border-slate-200 bg-slate-50 text-slate-600'}`}>
        {runtime.status === 'connected' ? <CheckCircle2 className="mt-0.5 size-5 shrink-0" /> : <CircleAlert className="mt-0.5 size-5 shrink-0" />}
        <div><strong>{runtime.status === 'connected' ? '管理服务已连接' : runtime.status === 'fallback' ? '管理服务连接失败' : '正在连接管理服务'}</strong><div className="mt-0.5 leading-6">{runtime.status === 'connected' ? `管理服务 ${runtime.snapshot?.health.version} · Docker、硬件采集与业务接口连接正常。` : runtime.error ?? '正在读取健康状态、Docker Engine 和容器列表。'}</div></div>
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
      <div className="border-t border-slate-200 px-5 py-4 text-xs leading-5 text-slate-500">管理端口建议仅在管理网开放，不要将 FlowPilot 管理台直接暴露到公网；请妥善保管管理员凭据，并通过 HTTPS 反向代理访问。</div>
    </Panel>}

    {active === 'storage' && <NetworkStoragePanel />}
    {active === 'database' && <DatabasePanel />}
    {active === 'security' && <SecurityPanel onLoggedOut={onLoggedOut} />}
    {active === 'audit' && <AuditPanel />}
    {active === 'migration' && <MigrationPanel instances={instances} />}
    {active === 'backup' && <BackupPanel />}
    {active === 'about' && <div className="grid gap-5 lg:grid-cols-2"><Panel><PanelHeader title="诊断信息" description="导出环境、实例、模型和任务摘要，不包含密码或令牌。" /><div className="p-5"><FileJson className="size-8 text-teal-700" /><h3 className="mt-4 font-semibold text-slate-900">FlowPilot 诊断包</h3><p className="mt-2 text-sm leading-6 text-slate-600">用于售后定位安装、Docker、GPU 和任务问题。诊断包包含服务、Docker、硬件与业务摘要，不包含密码或令牌。</p><button type="button" className="btn-primary mt-5" onClick={exportDiagnostics}><Download className="size-4" />导出诊断包</button></div></Panel><Panel><PanelHeader title="关于 FlowPilot" description="慧浪智算 GPU 服务器配套软件" /><div className="p-5"><div className="flex items-center gap-3"><ShieldCheck className="size-8 text-teal-700" /><div><div className="font-semibold text-slate-900">慧浪智算·FlowPilot</div><div className="mt-1 text-sm text-slate-500">ComfyUI 一体化部署与管理平台</div></div></div><dl className="mt-5 space-y-3 text-sm"><div className="flex justify-between"><dt className="text-slate-500">项目版本</dt><dd className="font-mono font-medium">{version}</dd></div><div className="flex justify-between"><dt className="text-slate-500">运行模式</dt><dd>Go 后端＋Web 管理台</dd></div><div className="flex justify-between"><dt className="text-slate-500">界面语言</dt><dd>简体中文</dd></div><div className="flex justify-between"><dt className="text-slate-500">数据来源</dt><dd>Docker Engine、硬件采集与共享仓库</dd></div></dl></div></Panel></div>}
  </>
}
