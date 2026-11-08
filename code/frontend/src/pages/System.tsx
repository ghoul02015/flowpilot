import { useEffect, useState } from 'react'
import { Check, CheckCircle2, CircleAlert, Download, FileJson, RefreshCw, Save, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'
import { getRuntimeSnapshot, type RuntimeSnapshot } from '@/api'
import { PageHeader, Panel, PanelHeader } from '@/components/common'
import type { Instance, ModelItem, TaskItem } from '@/types'

type SystemTab = 'environment' | 'settings' | 'audit' | 'about'
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
  const [registry, setRegistry] = useState('registry.huilang.local/comfyui')
  const [proxy, setProxy] = useState('http://proxy.example.com:7890')

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
    return () => controller.abort()
  }, [])

  function checkEnvironment() {
    setChecking(true)
    getRuntimeSnapshot()
      .then((snapshot) => {
        setRuntime({ status: 'connected', snapshot })
        setLastCheck(new Date().toLocaleString('zh-CN', { hour12: false }))
        toast.success('环境检测完成', { description: '后端、Docker 与硬件采集接口已连接；实例和模型仍为 Demo 数据。' })
      })
      .catch((error: unknown) => {
        setRuntime({ status: 'fallback', error: error instanceof Error ? error.message : '后端连接失败' })
        toast.error('后端连接失败', { description: '页面继续显示明确标记的 Demo 环境数据。' })
      })
      .finally(() => setChecking(false))
  }

  function exportDiagnostics() {
    const content = JSON.stringify({ product: 'FlowPilot', version: '0.0.5.0', mode: 'backend-and-demo', exportedAt: new Date().toISOString(), host: runtime.snapshot?.engine.info.Name ?? '慧浪 GPU-S01（Demo）', runtime, instances, models, tasks }, null, 2)
    const url = URL.createObjectURL(new Blob([content], { type: 'application/json' }))
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = 'flowpilot-diagnostics-0.0.5.0.json'
    anchor.click()
    URL.revokeObjectURL(url)
    toast.success('诊断包已导出', { description: '已包含后端与真实 Docker 信息，以及标记为 Demo 的业务数据。' })
  }

  const tabs: { key: SystemTab; label: string }[] = [{ key: 'environment', label: '环境检测' }, { key: 'settings', label: '系统设置' }, { key: 'audit', label: '操作日志' }, { key: 'about', label: '诊断与关于' }]
  return <>
    <PageHeader eyebrow="SYSTEM" title="系统管理" description="查看主机兼容性、交付配置和操作记录，导出用于售后排障的脱敏诊断信息。" />
    <div className="mb-5 flex gap-1 overflow-x-auto rounded-lg border border-slate-200 bg-white p-1" role="tablist" aria-label="系统管理页面">{tabs.map((item) => <button type="button" role="tab" aria-selected={tab === item.key} key={item.key} className={`min-h-11 flex-1 cursor-pointer rounded-md px-4 text-sm font-medium whitespace-nowrap transition-colors ${tab === item.key ? 'bg-slate-900 text-white' : 'text-slate-600 hover:bg-slate-100'}`} onClick={() => setTab(item.key)}>{item.label}</button>)}</div>

    {tab === 'environment' && <Panel>
      <PanelHeader title="主机环境报告" description={`${runtime.snapshot?.engine.info.Name ?? '慧浪 GPU-S01（Demo）'} · 最近检测 ${lastCheck}`} action={<button type="button" className="btn-secondary" disabled={checking} onClick={checkEnvironment}><RefreshCw className={`size-4 ${checking ? 'animate-spin' : ''}`} />{checking ? '检测中' : '重新检测'}</button>} />
      <div className={`flex items-start gap-3 border-b px-5 py-4 text-sm ${runtime.status === 'connected' ? 'border-emerald-200 bg-emerald-50 text-emerald-900' : runtime.status === 'fallback' ? 'border-amber-200 bg-amber-50 text-amber-900' : 'border-slate-200 bg-slate-50 text-slate-600'}`}>
        {runtime.status === 'connected' ? <CheckCircle2 className="mt-0.5 size-5 shrink-0" /> : <CircleAlert className="mt-0.5 size-5 shrink-0" />}
        <div><strong>{runtime.status === 'connected' ? '后端已连接' : runtime.status === 'fallback' ? '后端连接失败，已回退 Demo 数据' : '正在连接后端'}</strong><div className="mt-0.5 leading-6">{runtime.status === 'connected' ? `Go 后端 ${runtime.snapshot?.health.version} · Docker 与容器列表来自真实 API；硬件指标由 /api/v1/hardware 实时采集；实例、模型和任务仍为 Demo。` : runtime.error ?? '正在读取健康状态、Docker Engine 和容器列表。'}</div></div>
      </div>
      <div className="grid gap-px overflow-hidden bg-slate-200 sm:grid-cols-2">{[
        ['操作系统', 'Demo · Ubuntu 24.04 LTS · x86_64', true], ['处理器与内存', 'Demo · 64 核 · 512 GB', true], ['GPU 与驱动', 'Demo · 2 × NVIDIA L40S · Driver 580.95', true],
        ['Docker Engine', runtime.status === 'connected' ? `${runtime.snapshot?.engine.version.Version} · 真实 API 正常` : runtime.status === 'fallback' ? '连接失败 · 未使用模拟版本替代' : '正在读取真实 Docker 信息', runtime.status === 'connected'],
        ['Docker 容器', runtime.status === 'connected' ? `真实数据 · 共 ${runtime.snapshot?.engine.info.Containers} 个，运行 ${runtime.snapshot?.engine.info.ContainersRunning} 个` : '等待后端连接', runtime.status === 'connected'],
        ['NVIDIA 容器环境', 'Demo · 尚未执行真实容器内 GPU 验证', false], ['数据盘', 'Demo · 3.42 TB 可用 · 空间充足', true], ['管理端口', runtime.status === 'connected' ? '18080 · 后端健康检查通过' : '18080 · 等待健康检查', runtime.status === 'connected'],
      ].map(([label, value, ok]) => <div key={String(label)} className="flex items-start gap-3 bg-white p-5">{ok ? <CheckCircle2 className="mt-0.5 size-5 shrink-0 text-emerald-600" /> : <CircleAlert className="mt-0.5 size-5 text-amber-600" />}<div><div className="text-sm font-medium text-slate-900">{label}</div><div className="mt-1 text-sm leading-6 text-slate-600">{value}</div></div></div>)}</div>
    </Panel>}

    {tab === 'settings' && <Panel><PanelHeader title="交付与网络设置" description="Demo 设置仅保存在当前页面，刷新后恢复默认值。" /><form className="grid max-w-3xl gap-5 p-5" onSubmit={(event) => { event.preventDefault(); toast.success('系统设置已保存', { description: 'Demo 不会修改真实主机配置。' }) }}>
      <label className="field-label">内部镜像仓库<input className="field-input font-mono text-sm" value={registry} onChange={(event) => setRegistry(event.target.value)} required /></label>
      <label className="field-label">HTTP／HTTPS 代理<input className="field-input font-mono text-sm" value={proxy} onChange={(event) => setProxy(event.target.value)} /></label>
      <label className="field-label">数据根目录<input className="field-input bg-slate-100 font-mono text-sm" value="/data/flowpilot" readOnly /></label>
      <label className="field-label">模型导入允许目录<input className="field-input bg-slate-100 font-mono text-sm" value="/data/import" readOnly /></label>
      <div className="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">管理端口建议仅在管理网开放，不要将 FlowPilot 管理台直接暴露到公网。</div>
      <div><button type="submit" className="btn-primary"><Save className="size-4" />保存设置</button></div>
    </form></Panel>}

    {tab === 'audit' && <Panel><PanelHeader title="操作日志" description="记录管理操作、来源和结果，便于交付审计。" /><div className="overflow-x-auto"><table className="data-table min-w-[760px]"><thead><tr><th>时间</th><th>操作者</th><th>操作</th><th>来源 IP</th><th>结果</th></tr></thead><tbody>{[
      ['2026-11-04 16:21', 'admin', '创建模型下载任务', '10.10.8.26'], ['2026-11-04 15:48', 'admin', '启动 节点测试-03', '10.10.8.26'], ['2026-11-04 14:32', 'admin', '部署 生产绘图-01', '10.10.8.26'], ['2026-11-04 14:08', 'admin', '修改镜像仓库配置', '10.10.8.26'],
    ].map((row) => <tr key={`${row[0]}-${row[2]}`}><td className="font-mono text-xs">{row[0]}</td><td>{row[1]}</td><td>{row[2]}</td><td className="font-mono text-xs">{row[3]}</td><td><span className="inline-flex items-center gap-1 text-emerald-700"><Check className="size-4" />{row[2].includes('节点测试') ? '已记录' : '成功'}</span></td></tr>)}</tbody></table></div></Panel>}

    {tab === 'about' && <div className="grid gap-5 lg:grid-cols-2"><Panel><PanelHeader title="诊断信息" description="导出环境、实例、模型和任务摘要，不包含密码或令牌。" /><div className="p-5"><FileJson className="size-8 text-teal-700" /><h3 className="mt-4 font-semibold text-slate-900">FlowPilot 诊断包</h3><p className="mt-2 text-sm leading-6 text-slate-600">用于售后定位安装、Docker、GPU 和任务问题。诊断包同时包含真实后端信息和明确标记的 Demo 业务数据。</p><button type="button" className="btn-primary mt-5" onClick={exportDiagnostics}><Download className="size-4" />导出诊断包</button></div></Panel><Panel><PanelHeader title="关于 FlowPilot" description="慧浪智算 GPU 服务器配套软件" /><div className="p-5"><div className="flex items-center gap-3"><ShieldCheck className="size-8 text-teal-700" /><div><div className="font-semibold text-slate-900">慧浪智算·FlowPilot</div><div className="mt-1 text-sm text-slate-500">ComfyUI 一体化部署与管理平台</div></div></div><dl className="mt-5 space-y-3 text-sm"><div className="flex justify-between"><dt className="text-slate-500">项目版本</dt><dd className="font-mono font-medium">0.0.5.0</dd></div><div className="flex justify-between"><dt className="text-slate-500">运行模式</dt><dd>Go 后端＋Web Demo</dd></div><div className="flex justify-between"><dt className="text-slate-500">界面语言</dt><dd>简体中文</dd></div><div className="flex justify-between"><dt className="text-slate-500">数据方式</dt><dd>真实 Docker＋浏览器模拟业务数据</dd></div></dl></div></Panel></div>}
  </>
}
