import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router'
import { usePolling } from '@/hooks/use-polling'
import { AlertTriangle, ArrowRight, CheckCircle2, Cpu, Download, ExternalLink, FileText, Layers, LoaderCircle, Pause, Play, RefreshCw, ShieldAlert, XCircle } from 'lucide-react'
import { toast } from 'sonner'
import { EmptyState, Panel, PanelHeader, ProgressBar } from '@/components/common'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import {
  bundleInstallAction,
  confirmModelBundleLicense,
  createBundleAcceptance,
  createBundleInstall,
  formatBytes,
  getBundleInstall,
  getInstances,
  getModelBundle,
  getModelBundles,
  linkModelBundle,
  modelBundleLicenseFileURL,
  preflightModelBundle,
  type BundleCard,
  type BundleCheck,
  type BundleDetail,
  type BundleInstall,
  type BundlePreflight,
} from '@/api'

type Filter = 'all' | 'image' | 'video' | 'studio' | 'installable' | 'experimental'

const stateText: Record<string, string> = {
  not_installed: '未安装',
  partial: '部分文件已就绪',
  files_ready: '文件齐备',
  queued: '排队中',
  installing: '安装中',
  paused: '已暂停',
  failed: '安装失败',
  cancelled: '已取消',
}

const itemStatusText: Record<string, string> = {
  missing: '未下载',
  downloading: '下载中',
  paused: '已暂停',
  verifying: '校验中',
  present: '已存在',
  hash_mismatch: '大小不符',
  conflict: '内容冲突',
}

const levelText: Record<string, { label: string; tone: string }> = {
  priority: { label: '优先认证候选', tone: 'bg-teal-100 text-teal-800' },
  candidate: { label: '推荐认证候选', tone: 'bg-sky-100 text-sky-800' },
  conditional: { label: '条件认证', tone: 'bg-amber-100 text-amber-800' },
  experimental: { label: '实验／待认证', tone: 'bg-amber-100 text-amber-800' },
  not_recommended: { label: '不建议标准交付', tone: 'bg-rose-100 text-rose-700' },
  not_committed: { label: '不承诺', tone: 'bg-rose-100 text-rose-700' },
  unknown: { label: '无法评估', tone: 'bg-slate-100 text-slate-600' },
}

function stateTone(state: string): string {
  switch (state) {
    case 'files_ready':
      return 'bg-teal-100 text-teal-800'
    case 'installing':
    case 'queued':
      return 'bg-sky-100 text-sky-800'
    case 'paused':
      return 'bg-amber-100 text-amber-800'
    case 'failed':
      return 'bg-rose-100 text-rose-700'
    case 'partial':
      return 'bg-amber-100 text-amber-800'
    default:
      return 'bg-slate-100 text-slate-600'
  }
}

function capabilityText(capability: string[]): string {
  const map: Record<string, string> = { image: '图像', video: '视频', audio: '音频', reference: '全参考' }
  return capability.map((item) => map[item] ?? item).join(' · ')
}

export default function ModelBundles() {
  const [instances, setInstances] = useState<string[]>([])
  const [cards, setCards] = useState<BundleCard[]>([])
  const [checksum, setChecksum] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  // 套装详情支持 ?bundle= 深链接：刷新或分享后仍能打开同一套装。
  const [searchParams, setSearchParams] = useSearchParams()
  const [detailId, setDetailId] = useState<string | null>(() => searchParams.get('bundle'))
  const [detail, setDetail] = useState<BundleDetail | null>(null)
  const [detailError, setDetailError] = useState('')
  const [detailTab, setDetailTab] = useState<'overview' | 'files' | 'hardware' | 'license' | 'acceptance'>('overview')
  const [wizardOpen, setWizardOpen] = useState(false)
  const [acceptanceOpen, setAcceptanceOpen] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const data = await getModelBundles()
      setCards(data.bundles ?? [])
      setChecksum(data.checksum ?? '')
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : '读取套装列表失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
    void getInstances().then((data) => setInstances((data.instances ?? []).map((item) => item.name))).catch(() => {})
  }, [load])

  // 有活动安装时轮询进度；不触发全量列表刷新。
  const cardsRef = useRef(cards)
  cardsRef.current = cards
  const activeInstallRef = useRef(false)
  activeInstallRef.current = cards.some((card) => card.activeInstall && ['queued', 'installing'].includes(card.activeInstall.status))
  usePolling(async (signal) => {
    for (const card of cardsRef.current) {
      if (!card.activeInstall || !['queued', 'installing'].includes(card.activeInstall.status)) continue
      const install = await getBundleInstall(card.id, card.activeInstall.id, signal)
      setCards((prev) => prev.map((item) => (item.id === card.id ? { ...item, activeInstall: install, state: { ...item.state, state: install.status } } : item)))
      if (detailId === card.id && detail) {
        setDetail({ ...detail, install })
      }
    }
  }, { enabled: activeInstallRef.current, interval: 3000 })

  const filtered = useMemo(() => {
    return cards.filter((card) => {
      switch (filter) {
        case 'image':
          return card.capability.includes('image')
        case 'video':
          return card.capability.includes('video')
        case 'studio':
          return card.capability.length >= 3
        case 'installable':
          return card.state.state === 'files_ready' || Boolean(card.activeInstall)
        case 'experimental':
          return card.experimental
        default:
          return true
      }
    })
  }, [cards, filter])

  function syncBundleParam(id: string | null) {
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      if (id) next.set('bundle', id)
      else next.delete('bundle')
      return next
    }, { replace: true })
  }

  // 浏览器前进后退或手动改地址时，详情抽屉跟随 URL。
  useEffect(() => {
    const id = searchParams.get('bundle')
    if (!id) {
      if (detailId) { setDetailId(null); setDetail(null) }
      return
    }
    if (id === detailId && detail) return
    void openDetail(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams])

  async function openDetail(id: string) {
    setDetailId(id)
    syncBundleParam(id)
    setDetail(null)
    setDetailError('')
    setDetailTab('overview')
    try {
      setDetail(await getModelBundle(id))
    } catch (err) {
      setDetailError(err instanceof Error ? err.message : '读取套装详情失败')
    }
  }

  function openLicense(id: string) {
    void openDetail(id)
    // openDetail 先把页签重置为概览，这里在同一轮事件中覆盖为许可页。
    setDetailTab('license')
  }

  async function refreshDetail() {
    if (!detailId) return
    try {
      const next = await getModelBundle(detailId)
      setDetail(next)
      setCards((prev) => prev.map((card) => (card.id === detailId ? { ...card, state: next.card.state, activeInstall: next.install } : card)))
    } catch (err) {
      toast.error('刷新详情失败', { description: err instanceof Error ? err.message : '请稍后重试' })
    }
  }

  return <>
    <Panel>
      <PanelHeader
        title="组合套装"
        description="按场景整组部署与验收；套装只做编排与校验，不随发布包附带权重。"
        action={<button type="button" className="btn-secondary" onClick={() => void load()} disabled={loading}><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />刷新</button>}
      />
      <div className="flex flex-wrap items-center gap-2 border-b border-slate-100 px-4 py-3">
        {([['all', '全部'], ['image', '图像'], ['video', '视频'], ['studio', '图视一体'], ['installable', '可继续'], ['experimental', '实验']] as [Filter, string][]).map(([key, label]) => (
          <button
            key={key}
            type="button"
            onClick={() => setFilter(key)}
            className={`rounded-full px-3 py-1.5 text-xs font-medium transition ${filter === key ? 'bg-teal-700 text-white' : 'bg-slate-100 text-slate-600 hover:bg-slate-200'}`}
          >{label}</button>
        ))}
        <span className="ml-auto text-[11px] text-slate-400">清单 {checksum ? checksum.slice(0, 12) : '—'}</span>
      </div>

      {error && <div className="m-4 rounded-lg border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700">{error}</div>}
      {loading && cards.length === 0 && <div className="flex items-center gap-2 p-6 text-sm text-slate-500"><LoaderCircle className="size-4 animate-spin" />正在读取套装清单…</div>}

      <div className="grid gap-4 p-4 sm:grid-cols-2 xl:grid-cols-3">
        {filtered.map((card) => {
          const active = card.activeInstall
          const level = levelText[card.hardwareLevel] ?? levelText.unknown
          return <article key={card.id} className="flex flex-col rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
            <div className="flex items-start justify-between gap-2">
              <div>
                <h3 className="text-sm font-semibold text-slate-900">{card.name}</h3>
                <p className="mt-1 text-[13px] leading-5 text-slate-700">模型：{card.modelName}</p>
              </div>
              <span className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium ${stateTone(card.state.state)}`}>{stateText[card.state.state] ?? card.state.state}</span>
            </div>

            <p className="mt-2 line-clamp-2 text-xs leading-5 text-slate-600">{card.summary}</p>

            <div className="mt-3 flex flex-wrap items-center gap-1.5 text-[11px]">
              <span className="rounded-full bg-slate-100 px-2 py-0.5 text-slate-600">{capabilityText(card.capability)}</span>
              <span className={`rounded-full px-2 py-0.5 ${level.tone}`}>{level.label}</span>
              {card.experimental && <span className="rounded-full bg-purple-100 px-2 py-0.5 text-purple-700">实验</span>}
            </div>

            <dl className="mt-3 grid grid-cols-2 gap-2 text-[11px] text-slate-500">
              <div><dt className="text-slate-400">必需 / 可选文件</dt><dd className="mt-0.5 font-medium text-slate-700">{card.requiredItems} / {card.optionalItems}</dd></div>
              <div><dt className="text-slate-400">必需字节</dt><dd className="mt-0.5 font-medium text-slate-700">{formatBytes(card.requiredBytes)}</dd></div>
              <div><dt className="text-slate-400">已就绪</dt><dd className="mt-0.5 font-medium text-slate-700">{card.state.requiredPresent} / {card.state.requiredTotal}</dd></div>
              {card.license.name && <div><dt className="text-slate-400">许可</dt><dd className={`mt-0.5 font-medium ${card.licenseConfirmed ? 'text-teal-700' : 'text-amber-700'}`}>{card.licenseConfirmed ? '已确认' : '待确认'}</dd></div>}
            </dl>

            {card.license.name && <p className="mt-2 text-[11px] leading-5 text-slate-500">下载前需确认模型许可 · <button type="button" className="text-teal-700 underline underline-offset-2" onClick={() => openLicense(card.id)}>查看许可</button></p>}

            {active && <div className="mt-3">
              <ProgressBar value={active.totalBytes > 0 ? Math.round((active.completedBytes / active.totalBytes) * 100) : 0} />
              <p className="mt-1 text-[11px] text-slate-500">{stateText[active.status] ?? active.status} · {formatBytes(active.completedBytes)} / {formatBytes(active.totalBytes)}</p>
            </div>}

            <div className="mt-auto flex items-center gap-2 pt-3">
              <button type="button" className="btn-secondary h-9 px-3 text-xs" onClick={() => void openDetail(card.id)}>查看</button>
              <button type="button" className="btn-primary h-9 px-3 text-xs" onClick={() => { void openDetail(card.id); setWizardOpen(true) }}>
                {active && ['queued', 'installing'].includes(active.status) ? '查看进度' : card.state.state === 'partial' ? '继续安装' : '预检安装'}
              </button>
            </div>
          </article>
        })}
      </div>
      {!loading && filtered.length === 0 && <EmptyState title="没有符合条件的套装" description="调整筛选，或刷新以重新读取清单。" />}
    </Panel>

    <Dialog open={Boolean(detailId) && !wizardOpen && !acceptanceOpen} onOpenChange={(open) => { if (!open) { setDetailId(null); setDetail(null); syncBundleParam(null) } }}>
      <DialogContent className="max-h-[92dvh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Layers className="size-5 text-teal-700" />{detail?.card.name ?? '套装详情'}</DialogTitle>
          <DialogDescription>{detail ? `${detail.card.modelName} · ${capabilityText(detail.card.capability)} · 清单 ${detail.manifest.checksum.slice(0, 12)}` : '正在读取…'}</DialogDescription>
        </DialogHeader>

        {detailError && <div className="rounded-lg border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700">{detailError} <button type="button" className="underline" onClick={() => void refreshDetail()}>重试</button></div>}

        {detail && <>
          <div className="flex flex-wrap gap-1 border-b border-slate-100 pb-2">
            {([['overview', '概览'], ['files', '文件与依赖'], ['hardware', '硬件与工作流'], ['license', '许可与声明'], ['acceptance', '验收记录']] as ['overview' | 'files' | 'hardware' | 'license' | 'acceptance', string][]).map(([key, label]) => (
              <button key={key} type="button" onClick={() => setDetailTab(key)} className={`rounded-md px-3 py-1.5 text-xs font-medium ${detailTab === key ? 'bg-teal-50 text-teal-800' : 'text-slate-500 hover:bg-slate-100'}`}>{label}</button>
            ))}
          </div>

          {detailTab === 'overview' && <div className="space-y-3 text-sm">
            <p className="leading-6 text-slate-600">{detail.card.summary}</p>
            <div className="grid gap-2 sm:grid-cols-2">
              <Info label="安装状态" value={stateText[detail.card.state.state] ?? detail.card.state.state} />
              <Info label="必需文件就绪" value={`${detail.card.state.requiredPresent} / ${detail.card.state.requiredTotal}`} />
              <Info label="必需字节" value={formatBytes(detail.card.state.requiredBytes)} />
              <Info label="缺失字节" value={formatBytes(detail.card.state.missingBytes)} />
              <Info label="认证状态" value={detail.card.certification.length > 0 ? '已认证' : '待实机认证'} />
              <Info label="实验标记" value={detail.card.experimental ? '实验，不作销售承诺' : '标准'} />
            </div>
            {detail.install && <InstallProgress install={detail.install} bundleId={detail.card.id} onChanged={() => void refreshDetail()} />}
            <div className="flex flex-wrap gap-2 pt-1">
              <button type="button" className="btn-primary h-9 px-3 text-xs" onClick={() => setWizardOpen(true)}>预检安装</button>
              {detail.install && <button type="button" className="btn-secondary h-9 px-3 text-xs" onClick={() => setAcceptanceOpen(true)}>记录验收</button>}
            </div>
          </div>}

          {detailTab === 'files' && <div className="overflow-x-auto">
            <table className="data-table min-w-[720px]">
              <thead><tr><th>文件</th><th>角色</th><th>必需</th><th>大小</th><th>状态</th></tr></thead>
              <tbody>
                {detail.items.map((item) => <tr key={item.key}>
                  <td className="font-mono text-xs">{item.targetPath}</td>
                  <td className="text-xs">{item.role}</td>
                  <td className="text-xs">{item.required ? '是' : '可选'}</td>
                  <td className="text-xs">{formatBytes(item.expectedBytes)}</td>
                  <td className="text-xs"><span className={item.status === 'present' ? 'text-teal-700' : item.status === 'missing' ? 'text-slate-500' : 'text-rose-600'}>{itemStatusText[item.status] ?? item.status}</span>{item.reason && <span className="ml-1 text-[11px] text-slate-400">{item.reason}</span>}</td>
                </tr>)}
              </tbody>
            </table>
          </div>}

          {detailTab === 'hardware' && <div className="space-y-3 text-sm">
            <div className="rounded-lg border border-slate-200 p-3">
              <p className="flex items-center gap-1.5 text-xs font-semibold text-slate-700"><Cpu className="size-4" />当前主机</p>
              <p className="mt-1 text-xs text-slate-600">{detail.hardware.gpus.length > 0 ? detail.hardware.gpus.map((gpu) => `#${gpu.index} ${gpu.name}`).join('；') : detail.hardware.error ?? '未检测到 GPU'}{detail.hardware.unifiedMemory ? ' · 统一内存' : ''}</p>
            </div>
            <div className="rounded-lg border border-slate-200 p-3 text-xs text-slate-600">
              <p className="font-semibold text-slate-700">镜像与工作流要求</p>
              <p className="mt-1">ComfyUI {detail.card.version ? `≥ ${detail.card.id.includes('image') ? '0.45.0' : '0.45.0'}` : ''}；模板包 0.11.104+</p>
              {detail.workflows.map((workflow) => <p key={workflow.name} className="mt-1 flex items-center gap-1">
                <FileText className="size-3.5" />{workflow.name} · {workflow.templateStatus === 'verified' ? '模板已确认' : '模板待确认'}
                <a className="text-teal-700 underline" href={workflow.officialURL} target="_blank" rel="noreferrer">官方说明<ExternalLink className="ml-0.5 inline size-3" /></a>
              </p>)}
            </div>
          </div>}

          {detailTab === 'license' && <div className="space-y-3 text-sm">
            {!detail.card.license.name && <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs leading-6 text-slate-600">该模型未配置额外许可，下载无需确认。</div>}
            {detail.card.license.name && <div className="overflow-hidden rounded-lg border border-slate-200">
              <dl className="divide-y divide-slate-100 text-xs">
                <LicenseRow label="模型名称" value={detail.card.license.modelName} strong />
                <LicenseRow label="许可名称" value={detail.card.license.name} />
                <LicenseRow label="许可日期" value={detail.card.license.licenseDate} />
                <LicenseRow label="本地快照日期" value={detail.card.license.snapshotDate} />
                <LicenseRow label="许可确认" value={detail.card.licenseConfirmed ? '已确认' : '待确认'} tone={detail.card.licenseConfirmed ? 'text-teal-700' : 'text-amber-700'} />
                {detail.card.licenseConfirmed && <LicenseRow label="确认人" value={detail.card.licenseConfirmedBy ?? '—'} />}
                {detail.card.licenseConfirmed && <LicenseRow label="确认时间" value={detail.card.licenseConfirmedAt ? new Date(detail.card.licenseConfirmedAt).toLocaleString('zh-CN', { hour12: false }) : '—'} />}
                <div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
                  <dt className="text-slate-500">官方许可</dt>
                  <dd><a className="inline-flex items-center gap-1 text-teal-700 underline" href={detail.card.license.officialURL} target="_blank" rel="noreferrer">查看官方许可<ExternalLink className="size-3" /></a></dd>
                </div>
                <div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
                  <dt className="text-slate-500">本地文件</dt>
                  <dd className="flex gap-3">
                    <a className="inline-flex items-center gap-1 text-teal-700 underline" href={modelBundleLicenseFileURL(detail.card.id, 'license')} download>下载 LICENSE<Download className="size-3" /></a>
                    <a className="inline-flex items-center gap-1 text-teal-700 underline" href={modelBundleLicenseFileURL(detail.card.id, 'notice')} download>下载 NOTICE<Download className="size-3" /></a>
                  </dd>
                </div>
              </dl>
            </div>}

            {detail.card.license.name && <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs leading-6 text-slate-600">
              <p className="flex items-center gap-1.5 font-semibold text-slate-700"><ShieldAlert className="size-4" />主要许可限制</p>
              <ul className="mt-1 list-disc space-y-0.5 pl-5">
                {detail.card.license.summary.map((line) => <li key={line}>{line}</li>)}
              </ul>
              <p className="mt-2">NOTICE：{detail.card.license.noticeRequired ? '随交付提供' : '无'} · 商用界面展示模型名：{detail.card.license.commercialUIRequired ? `需要（${detail.card.license.modelName}）` : '无'}</p>
            </div>}

            {detail.card.license.name && detail.licenseConfirmations.length > 0 && <div className="rounded-lg border border-slate-200 p-3">
              <p className="text-xs font-semibold text-slate-700">确认历史</p>
              <ul className="mt-1 space-y-1 text-[11px] text-slate-600">
                {detail.licenseConfirmations.map((item) => <li key={item.id}>
                  {new Date(item.confirmedAt).toLocaleString('zh-CN', { hour12: false })} · {item.operator || '—'} 确认了 {item.modelName} 许可（版本 {item.licenseVersion}）· 套餐 {item.bundleId}
                </li>)}
              </ul>
            </div>}

            {detail.card.license.name && <p className="text-xs leading-5 text-slate-500">确认仅表示你已阅读该许可，不改变模型本身的许可要求。</p>}
          </div>}

          {detailTab === 'acceptance' && <div className="space-y-2 text-sm">
            <div className="flex items-center justify-between">
              <p className="text-xs text-slate-500">人工验收记录与“慧浪认证”是两件事；记录只说明现场执行过。</p>
              <button type="button" className="btn-secondary h-8 px-2 text-xs" onClick={() => setAcceptanceOpen(true)}>新增记录</button>
            </div>
            {detail.acceptances.length === 0 && <p className="rounded-lg bg-slate-50 p-3 text-xs text-slate-500">暂无验收记录。</p>}
            {detail.acceptances.map((item) => <div key={item.id} className="rounded-lg border border-slate-200 p-3 text-xs">
              <p className="font-medium text-slate-700">{item.instanceName} · {item.result === 'pass' ? '通过' : item.result === 'fail' ? '失败' : '部分通过'} · {new Date(item.createdAt).toLocaleString('zh-CN', { hour12: false })}</p>
              {item.notes && <p className="mt-1 text-slate-600">{item.notes}</p>}
              <a className="mt-1 inline-block text-teal-700 underline" href={`/api/v1/model-bundles/${detail.card.id}/acceptances/${item.id}/export`} target="_blank" rel="noreferrer">导出脱敏 JSON</a>
            </div>)}
          </div>}
        </>}
      </DialogContent>
    </Dialog>

    {wizardOpen && detailId && <InstallWizard
      bundleId={detailId}
      instances={instances}
      onClose={() => {
        setWizardOpen(false)
        // 从卡片直接进入向导时没有加载详情，关闭后必须清掉 detailId，
        // 否则详情抽屉会被残留 ID 重新打开成“正在读取…”。
        if (!detail) { setDetailId(null); syncBundleParam(null) }
        void load()
      }}
    />}

    {acceptanceOpen && detail && <AcceptanceDialog
      bundleId={detail.card.id}
      instances={instances}
      installId={detail.install?.id}
      onClose={() => { setAcceptanceOpen(false); void refreshDetail() }}
    />}
  </>
}

function Info({ label, value }: { label: string; value: string }) {
  return <div className="rounded-lg border border-slate-200 p-2.5">
    <p className="text-[11px] text-slate-400">{label}</p>
    <p className="mt-0.5 text-sm font-medium text-slate-700">{value}</p>
  </div>
}

function LicenseRow({ label, value, strong, tone }: { label: string; value: string; strong?: boolean; tone?: string }) {
  return <div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
    <dt className="text-slate-500">{label}</dt>
    <dd className={`text-right ${strong ? 'font-semibold text-slate-900' : 'text-slate-700'} ${tone ?? ''}`}>{value}</dd>
  </div>
}

function checkTone(status: string): string {
  switch (status) {
    case 'pass':
      return 'text-teal-700'
    case 'warn':
      return 'text-amber-700'
    case 'block':
      return 'text-rose-700'
    default:
      return 'text-slate-500'
  }
}

function InstallProgress({ install, bundleId, onChanged }: { install: BundleInstall; bundleId: string; onChanged: () => void }) {
  const [busy, setBusy] = useState('')
  const percent = install.totalBytes > 0 ? Math.round((install.completedBytes / install.totalBytes) * 100) : 0
  const active = ['queued', 'installing'].includes(install.status)
  const finished = ['files_ready', 'failed', 'cancelled'].includes(install.status)

  async function act(action: 'pause' | 'resume' | 'cancel' | 'retry') {
    setBusy(action)
    try {
      await bundleInstallAction(bundleId, install.id, action)
      toast.success(action === 'pause' ? '已请求暂停' : action === 'resume' ? '已继续安装' : action === 'cancel' ? '已取消安装' : '已重试失败项')
      onChanged()
    } catch (err) {
      toast.error('操作失败', { description: err instanceof Error ? err.message : '请稍后重试' })
    } finally {
      setBusy('')
    }
  }

  return <div className="rounded-lg border border-slate-200 p-3">
    <div className="flex items-center justify-between text-xs">
      <span className="font-medium text-slate-700">安装计划 {install.id}</span>
      <span className={install.status === 'failed' ? 'text-rose-700' : 'text-slate-500'}>{stateText[install.status] ?? install.status}</span>
    </div>
    <div className="mt-2"><ProgressBar value={percent} /></div>
    <p className="mt-1 text-[11px] text-slate-500">{formatBytes(install.completedBytes)} / {formatBytes(install.totalBytes)}{install.currentItemKey ? ` · 当前 ${install.currentItemKey}` : ''}</p>
    {install.currentItemDownloaded ? <p className="text-[11px] text-slate-400">当前文件已下载 {formatBytes(install.currentItemDownloaded)}</p> : null}
    {install.errorMessage && <p className="mt-1 text-[11px] text-rose-600">{install.errorMessage}</p>}
    {install.items && install.items.length > 0 && <ul className="mt-2 space-y-1">
      {install.items.map((item) => <li key={item.itemKey} className="flex items-center justify-between gap-2 text-[11px] text-slate-600">
        <span className="truncate font-mono">{item.targetPath}</span>
        <span className={item.status === 'present' ? 'text-teal-700' : item.status === 'missing' ? 'text-slate-400' : 'text-amber-700'}>{itemStatusText[item.status] ?? item.status}</span>
      </li>)}
    </ul>}
    <div className="mt-2 flex flex-wrap gap-2">
      {active && <button type="button" className="btn-secondary h-8 px-2 text-xs" disabled={busy !== ''} onClick={() => void act('pause')}><Pause className="size-3.5" />暂停</button>}
      {install.status === 'paused' && <button type="button" className="btn-primary h-8 px-2 text-xs" disabled={busy !== ''} onClick={() => void act('resume')}><Play className="size-3.5" />继续</button>}
      {!finished && <button type="button" className="btn-secondary h-8 px-2 text-xs text-rose-600" disabled={busy !== ''} onClick={() => void act('cancel')}><XCircle className="size-3.5" />取消</button>}
      {['failed', 'paused'].includes(install.status) && <button type="button" className="btn-secondary h-8 px-2 text-xs" disabled={busy !== ''} onClick={() => void act('retry')}><RefreshCw className="size-3.5" />只重试失败项</button>}
    </div>
    {install.status === 'files_ready' && <p className="mt-2 text-[11px] text-teal-700">文件已齐备；如需在实例中看到模型，请使用“关联”并刷新 ComfyUI 页面。关联后仍需人工运行工作流验证。</p>}
  </div>
}

function LicenseConfirmDialog({ bundleId, detail, onConfirmed, onClose }: { bundleId: string; detail: BundleDetail; onConfirmed: () => void; onClose: () => void }) {
  const license = detail.card.license
  const [busy, setBusy] = useState(false)

  async function confirm() {
    if (busy) return
    setBusy(true)
    try {
      await confirmModelBundleLicense(bundleId)
      toast.success('许可已确认', { description: `${license.modelName} · 版本 ${license.version}` })
      onConfirmed()
    } catch (err) {
      toast.error('许可确认失败', { description: err instanceof Error ? err.message : '请稍后重试' })
    } finally {
      setBusy(false)
    }
  }

  return <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent className="max-h-[92dvh] overflow-y-auto sm:max-w-2xl">
      <DialogHeader>
        <DialogTitle>使用 {license.modelName} 前请阅读许可</DialogTitle>
        <DialogDescription>
          当前安装套餐：{detail.card.name}<br />
          模型：{license.modelName}<br />
          许可版本：{license.version}
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-3 text-sm">
        <p className="text-xs leading-6 text-slate-600">本模型适用《{license.name}》，下载前请阅读以下要点：</p>
        <ul className="list-disc space-y-1 rounded-lg border border-slate-200 bg-slate-50 p-3 pl-7 text-xs leading-5 text-slate-700">
          {license.summary.map((line) => <li key={line}>{line}</li>)}
        </ul>
        <div className="flex flex-wrap gap-3 text-xs">
          <a className="inline-flex items-center gap-1 text-teal-700 underline" href={license.officialURL} target="_blank" rel="noreferrer">查看官方许可<ExternalLink className="size-3" /></a>
          <a className="inline-flex items-center gap-1 text-teal-700 underline" href={modelBundleLicenseFileURL(bundleId, 'license')} download>下载许可快照<Download className="size-3" /></a>
          {license.noticeRequired && <a className="inline-flex items-center gap-1 text-teal-700 underline" href={modelBundleLicenseFileURL(bundleId, 'notice')} download>下载 NOTICE<Download className="size-3" /></a>}
        </div>
        <p className="text-[11px] leading-5 text-slate-500">官方页面暂时无法访问时，可先查看平台保存的许可快照。</p>
        <p className="text-[11px] leading-5 text-slate-500">点击确认即表示你已阅读该许可。</p>
      </div>

      <DialogFooter className="flex-wrap items-center gap-2">
        <a className="btn-secondary mr-auto h-9 px-3 text-xs" href={license.officialURL} target="_blank" rel="noreferrer">查看官方许可<ExternalLink className="size-3.5" /></a>
        <button type="button" className="btn-secondary h-9 px-3 text-xs" onClick={onClose}>取消</button>
        <button type="button" className="btn-primary h-9 px-3 text-xs" disabled={busy} onClick={() => void confirm()}>
          {busy ? <LoaderCircle className="size-4 animate-spin" /> : <CheckCircle2 className="size-4" />}确认并继续下载
        </button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
}

function InstallWizard({ bundleId, instances, onClose }: { bundleId: string; instances: string[]; onClose: () => void }) {
  const [detail, setDetail] = useState<BundleDetail | null>(null)
  const [step, setStep] = useState(1)
  const [variant, setVariant] = useState('default')
  const [selected, setSelected] = useState<string[]>([])
  const [preflight, setPreflight] = useState<BundlePreflight | null>(null)
  const [preflightError, setPreflightError] = useState('')
  const [licenseConfirmed, setLicenseConfirmed] = useState(false)
  const [licenseOpen, setLicenseOpen] = useState(false)
  const [afterLicense, setAfterLicense] = useState<'preflight' | 'install'>('preflight')
  const [install, setInstall] = useState<BundleInstall | null>(null)
  const [linkInstance, setLinkInstance] = useState('')
  const [busy, setBusy] = useState(false)
  const requestKey = useRef(`bundle-${bundleId}-${Date.now()}`)

  useEffect(() => {
    void getModelBundle(bundleId).then((data) => {
      setDetail(data)
      // 未配置许可的模型不需要确认，直接视为已满足。
      setLicenseConfirmed(data.card.licenseConfirmed || !data.card.license.name)
    }).catch((err: unknown) => setPreflightError(err instanceof Error ? err.message : '读取详情失败'))
  }, [bundleId])

  const installRef = useRef(install)
  installRef.current = install
  usePolling(async (signal) => {
    const current = installRef.current
    if (!current) return
    setInstall(await getBundleInstall(bundleId, current.id, signal))
  }, { enabled: Boolean(install && ['queued', 'installing'].includes(install.status)), interval: 2500 })

  const optionalGroups = useMemo(() => {
    const groups = new Map<string, { key: string; label: string }[]>()
    for (const item of detail?.items ?? []) {
      if (item.required) continue
      // 后端返回的 items 无 optionalGroup 字段，这里按 key 前缀归类。
      const group = item.key.includes('turbo') ? 'turbo' : item.key.includes('ref2va') ? 'reference' : 'other'
      const list = groups.get(group) ?? []
      list.push({ key: item.key, label: item.targetPath.split('/').pop() ?? item.key })
      groups.set(group, list)
    }
    return [...groups.entries()]
  }, [detail])

  async function runPreflight() {
    setPreflightError('')
    setBusy(true)
    try {
      const result = await preflightModelBundle(bundleId, variant, selected)
      setPreflight(result)
      setStep(2)
    } catch (err) {
      setPreflightError(err instanceof Error ? err.message : '预检失败')
    } finally {
      setBusy(false)
    }
  }

  function openLicenseDialog(next: 'preflight' | 'install') {
    setAfterLicense(next)
    setLicenseOpen(true)
  }

  async function handleLicenseConfirmed() {
    setLicenseConfirmed(true)
    setLicenseOpen(false)
    if (afterLicense === 'install') {
      await startInstall(true)
    } else {
      await runPreflight()
    }
  }

  async function startInstall(skipLicenseCheck = false) {
    const requiresLicense = Boolean(detail?.card.license.name)
    if (!skipLicenseCheck && requiresLicense && !licenseConfirmed) {
      openLicenseDialog('install')
      return
    }
    setBusy(true)
    try {
      const created = await createBundleInstall(bundleId, { variant, selectedOptions: selected, requestKey: requestKey.current })
      setInstall(created)
      setStep(4)
      toast.success('安装计划已创建', { description: '任务已进入串行队列，可在任务中心查看' })
    } catch (err) {
      toast.error('创建安装失败', { description: err instanceof Error ? err.message : '请检查预检结果' })
    } finally {
      setBusy(false)
    }
  }

  async function link() {
    if (!install || !linkInstance) return
    setBusy(true)
    try {
      const result = await linkModelBundle(bundleId, install.id, linkInstance)
      toast.success('已关联到实例', { description: `新增 ${result.added?.length ?? 0} 项，请刷新 ComfyUI 页面` })
    } catch (err) {
      toast.error('关联失败', { description: err instanceof Error ? err.message : '请稍后重试' })
    } finally {
      setBusy(false)
    }
  }

  const blockingCheck = preflight?.checks.find((check: BundleCheck) => check.status === 'block')
  const licenseBlocked = blockingCheck?.code === 'license'

  if (licenseOpen && detail) {
    return <LicenseConfirmDialog
      bundleId={bundleId}
      detail={detail}
      onConfirmed={() => void handleLicenseConfirmed()}
      onClose={() => { setLicenseOpen(false); setAfterLicense('preflight') }}
    />
  }

  return <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent className="max-h-[92dvh] overflow-y-auto sm:max-w-3xl">
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2"><Download className="size-5 text-teal-700" />安装向导 · {detail?.card.name ?? ''}</DialogTitle>
        <DialogDescription>第 {Math.min(step, 4)} 步 / 共 4 步 · 串行下载，单文件失败不会影响已完成文件</DialogDescription>
      </DialogHeader>

      {step === 1 && detail && <div className="space-y-3">
        <div>
          <p className="text-xs font-medium text-slate-700">选择变体</p>
          <div className="mt-1 space-y-1.5">
            <label className="flex items-center gap-2 text-sm"><input type="radio" name="variant" checked={variant === 'default'} onChange={() => { setVariant('default'); setSelected([]) }} />默认（必需文件）</label>
            {detail.card.capability.includes('video') && <>
              <label className="flex items-center gap-2 text-sm"><input type="radio" name="variant" checked={variant === 'turbo-4step'} onChange={() => { setVariant('turbo-4step'); setSelected(['lora-fl2v-turbo-4step']) }} />Turbo 4-step（可选）</label>
              <label className="flex items-center gap-2 text-sm"><input type="radio" name="variant" checked={variant === 'turbo-8step'} onChange={() => { setVariant('turbo-8step'); setSelected(['lora-fl2v-turbo-8step']) }} />Turbo 8-step（可选）</label>
            </>}
            <label className="flex items-center gap-2 text-sm"><input type="radio" name="variant" checked={variant === 'with-reference'} onChange={() => { setVariant('with-reference'); setSelected(['vid-diffusion-ref2va-int8']) }} />含全参考扩展（实验）</label>
          </div>
        </div>
        {optionalGroups.length > 0 && <div>
          <p className="text-xs font-medium text-slate-700">可选扩展</p>
          <div className="mt-1 space-y-1">
            {optionalGroups.flatMap(([, items]) => items).map((item) => <label key={item.key} className="flex items-center gap-2 text-xs text-slate-600">
              <input type="checkbox" checked={selected.includes(item.key)} onChange={(event) => {
                setSelected((prev) => event.target.checked ? [...prev.filter((key) => key !== item.key), item.key] : prev.filter((key) => key !== item.key))
              }} />{item.label}
            </label>)}
          </div>
          <p className="mt-1 text-[11px] text-slate-400">Turbo 4-step 与 8-step 互斥；全参考独立选项。</p>
        </div>}
        {preflightError && <p className="rounded-md bg-rose-50 p-2 text-xs text-rose-700">{preflightError}</p>}
      </div>}

      {step === 2 && preflight && <div className="space-y-2">
        {preflight.checks.map((check) => <div key={check.code} className="rounded-lg border border-slate-200 p-3">
          <p className={`flex items-center gap-1.5 text-xs font-semibold ${checkTone(check.status)}`}>
            {check.status === 'pass' ? <CheckCircle2 className="size-4" /> : check.status === 'block' ? <XCircle className="size-4" /> : <AlertTriangle className="size-4" />}
            {check.title} · {check.status === 'pass' ? '通过' : check.status === 'block' ? '阻断' : check.status === 'warn' ? '警告' : '待确认'}
          </p>
          <p className="mt-1 text-xs text-slate-600">{check.detail}</p>
          {check.observed && <p className="mt-0.5 text-[11px] text-slate-500">当前：{check.observed}</p>}
          {check.required && <p className="text-[11px] text-slate-500">要求：{check.required}</p>}
          {check.remediation && <p className="text-[11px] text-amber-700">建议：{check.remediation}</p>}
        </div>)}
        <p className="text-[11px] text-slate-500">预检不产生任何写操作；磁盘不足或镜像版本不满足时将阻断安装。许可只在下载前提示一次，同模型的其他套餐无需重复确认。</p>
      </div>}

      {step === 3 && preflight && <div className="space-y-2 text-sm">
        <div className="rounded-lg border border-slate-200 p-3 text-xs text-slate-600">
          <p className="font-semibold text-slate-700">执行摘要</p>
          <p className="mt-1">变体：{variant} · 总下载量约 {formatBytes(preflight.plan?.totalBytes ?? 0)}</p>
          <p>串行执行：同一时刻只下载一个文件，失败项可单独重试。</p>
          <p>创建后可在任务中心查看进度；取消不会删除已完成文件。</p>
          <p className="mt-1">许可：{detail?.card.license.name ? (licenseConfirmed ? `已确认 ${detail.card.license.modelName}（版本 ${detail.card.license.version}）` : `待确认 ${detail.card.license.modelName}`) : '该模型无需额外确认'}</p>
        </div>
        {blockingCheck && <p className="rounded-md bg-rose-50 p-2 text-xs text-rose-700">存在阻断项：{blockingCheck.detail}</p>}
      </div>}

      {step === 4 && install && <div className="space-y-3">
        <InstallProgress install={install} bundleId={bundleId} onChanged={() => {
          void getBundleInstall(bundleId, install.id).then(setInstall).catch(() => {})
        }} />
        {install.status === 'files_ready' && <div className="rounded-lg border border-slate-200 p-3">
          <p className="text-xs font-medium text-slate-700">关联到实例（可选）</p>
          <div className="mt-2 flex gap-2">
            <select className="field-input h-9 flex-1 text-xs" value={linkInstance} onChange={(event) => setLinkInstance(event.target.value)}>
              <option value="">选择实例</option>
              {instances.map((name) => <option key={name} value={name}>{name}</option>)}
            </select>
            <button type="button" className="btn-secondary h-9 px-3 text-xs" disabled={!linkInstance || busy} onClick={() => void link()}>关联</button>
          </div>
          <p className="mt-1 text-[11px] text-slate-500">关联后需在 ComfyUI 中刷新页面，并人工运行工作流验证。</p>
        </div>}
      </div>}

      <DialogFooter>
        {step === 1 && <button type="button" className="btn-primary" disabled={busy} onClick={() => void runPreflight()}>{busy ? <LoaderCircle className="size-4 animate-spin" /> : <ArrowRight className="size-4" />}运行预检</button>}
        {step === 2 && <>
          <button type="button" className="btn-secondary" onClick={() => setStep(1)}>上一步</button>
          {licenseBlocked
            ? <button type="button" className="btn-primary" onClick={() => openLicenseDialog('preflight')}>确认许可并继续</button>
            : <button type="button" className="btn-primary" disabled={Boolean(blockingCheck)} onClick={() => setStep(3)}>下一步</button>}
        </>}
        {step === 3 && <>
          <button type="button" className="btn-secondary" onClick={() => setStep(2)}>上一步</button>
          <button type="button" className="btn-primary" disabled={busy || Boolean(blockingCheck)} onClick={() => void startInstall()}>{busy ? <LoaderCircle className="size-4 animate-spin" /> : <Download className="size-4" />}创建安装任务</button>
        </>}
        {step === 4 && <button type="button" className="btn-secondary" onClick={onClose}>关闭（任务在后台继续）</button>}
      </DialogFooter>
    </DialogContent>
  </Dialog>
}

function AcceptanceDialog({ bundleId, instances, installId, onClose }: { bundleId: string; instances: string[]; installId?: string; onClose: () => void }) {
  const [instance, setInstance] = useState(instances[0] ?? '')
  const [result, setResult] = useState('pass')
  const [notes, setNotes] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    if (!instance) {
      toast.error('请选择验收实例')
      return
    }
    setBusy(true)
    try {
      await createBundleAcceptance(bundleId, { installId, instance, result, notes })
      toast.success('已保存人工验收记录', { description: '记录不代表慧浪认证' })
      onClose()
    } catch (err) {
      toast.error('保存失败', { description: err instanceof Error ? err.message : '请稍后重试' })
    } finally {
      setBusy(false)
    }
  }

  return <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent className="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>新增人工验收记录</DialogTitle>
        <DialogDescription>只记录人工执行结果，不自动生成性能认证。</DialogDescription>
      </DialogHeader>
      <div className="space-y-3">
        <label className="field-label">验收实例
          <select className="field-input" value={instance} onChange={(event) => setInstance(event.target.value)}>
            <option value="">选择实例</option>
            {instances.map((name) => <option key={name} value={name}>{name}</option>)}
          </select>
        </label>
        <label className="field-label">结果
          <select className="field-input" value={result} onChange={(event) => setResult(event.target.value)}>
            <option value="pass">通过</option>
            <option value="partial">部分通过</option>
            <option value="fail">失败</option>
          </select>
        </label>
        <label className="field-label">备注
          <textarea className="field-input min-h-20" value={notes} onChange={(event) => setNotes(event.target.value)} placeholder="记录工作流、参数与人工观察到的结果" />
        </label>
        <p className="text-[11px] text-slate-500">该记录与“慧浪认证”独立；认证需后续审核发布。</p>
      </div>
      <DialogFooter>
        <button type="button" className="btn-secondary" onClick={onClose}>取消</button>
        <button type="button" className="btn-primary" disabled={busy} onClick={() => void submit()}>{busy ? <LoaderCircle className="size-4 animate-spin" /> : <CheckCircle2 className="size-4" />}保存记录</button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
}
