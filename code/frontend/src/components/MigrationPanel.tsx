import { useState } from 'react'
import { ArrowRightLeft, FolderInput, Loader2, RefreshCw, ShieldAlert } from 'lucide-react'
import { toast } from 'sonner'
import {
  formatBytes,
  migrationPreview,
  migrationRun,
  migrationScan,
  type MigrationPreviewPlan,
  type MigrationRunResult,
  type MigrationScanReport,
} from '@/api'
import { Panel, PanelHeader } from '@/components/common'

interface Props {
  instances: { id: string; name: string; status: string }[]
}

// MigrationPanel 历史数据迁移：扫描旧共享目录 → 选实例 → 预览 → 执行（自动备份＋校验）。
export function MigrationPanel({ instances }: Props) {
  const [scan, setScan] = useState<MigrationScanReport | null>(null)
  const [instance, setInstance] = useState('')
  const [plan, setPlan] = useState<MigrationPreviewPlan | null>(null)
  const [mode, setMode] = useState<'copy' | 'move'>('copy')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<MigrationRunResult | null>(null)

  function refreshScan() {
    setBusy(true)
    migrationScan()
      .then((report) => setScan(report))
      .catch((reason: unknown) => toast.error('扫描失败', { description: reason instanceof Error ? reason.message : '未知错误' }))
      .finally(() => setBusy(false))
  }

  function loadPreview(name: string) {
    setInstance(name)
    setPlan(null)
    setResult(null)
    if (!name) return
    setBusy(true)
    migrationPreview(name)
      .then(setPlan)
      .catch((reason: unknown) => toast.error('预览失败', { description: reason instanceof Error ? reason.message : '未知错误' }))
      .finally(() => setBusy(false))
  }

  function run() {
    if (!instance) return
    setBusy(true)
    migrationRun(instance, ['outputs', 'workflows'], mode)
      .then((runResult) => {
        setResult(runResult)
        toast.success('迁移完成', { description: `${runResult.copiedFiles.outputs ?? 0}＋${runResult.copiedFiles.workflows ?? 0} 个文件 · 校验${runResult.verified ? '通过' : '有告警'} · 耗时 ${runResult.duration}` })
        return migrationPreview(instance).then(setPlan)
      })
      .catch((reason: unknown) => toast.error('迁移失败', { description: reason instanceof Error ? reason.message : '源数据未受影响，可重试' }))
      .finally(() => setBusy(false))
  }

  return (
    <div className="space-y-5" data-testid="migration-panel">
      <Panel>
        <PanelHeader
          title="历史数据迁移"
          description="旧共享 outputs／workflows → 实例独立数据目录（执行前自动备份）"
          action={<button type="button" className="btn-secondary" disabled={busy} onClick={refreshScan}><RefreshCw className={`size-4 ${busy ? 'animate-spin' : ''}`} />扫描共享目录</button>}
        />
        {scan && <div className="grid gap-px overflow-hidden bg-slate-200 sm:grid-cols-2" data-testid="migration-scan">
          {[
            { label: '共享输出（outputs）', stats: scan.outputs },
            { label: '共享工作流（workflows）', stats: scan.workflows },
          ].map((item) => <div key={item.label} className="bg-white p-5">
            <div className="text-sm font-medium text-slate-900">{item.label}</div>
            <div className="mt-1 text-sm leading-6 text-slate-600">
              {item.stats.exists ? `${item.stats.files} 个文件 · ${formatBytes(item.stats.bytes)}` : '目录不存在或为空'}
            </div>
          </div>)}
        </div>}
        <div className="border-t border-slate-200 px-5 py-4 text-xs leading-5 text-slate-500">
          <ShieldAlert className="mr-1 inline size-3.5 text-amber-600" />旧实例（0.0.9.6 及之前创建）挂载仍指向共享目录，不受迁移影响；迁移目标需为 0.0.9.7 起新建或重建的实例。
        </div>
      </Panel>

      <Panel>
        <PanelHeader title="迁移执行" description="选择实例 → 预览 → 执行" />
        <div className="grid gap-4 p-5 sm:max-w-xl">
          <div>
            <label htmlFor="migration-instance" className="text-sm font-medium text-slate-700">目标实例（需具备独立数据目录）</label>
            <select id="migration-instance" className="field-input mt-1.5" value={instance} onChange={(event) => loadPreview(event.target.value)} data-testid="migration-instance-select">
              <option value="">选择实例…</option>
              {instances.map((item) => <option key={item.id} value={item.name}>{item.name}（{item.status === 'running' ? '运行中' : '已停止'}）</option>)}
            </select>
          </div>
          {busy && <div className="flex items-center gap-2 text-sm text-slate-500"><Loader2 className="size-4 animate-spin" />处理中…</div>}
          {plan && !plan.available && <div className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900">{plan.reason ?? '该实例不可迁移'}</div>}
          {plan?.available && <>
            <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm leading-6" data-testid="migration-plan">
              <div>迁移范围：outputs（{plan.scope.outputs?.files ?? 0} 文件 · {formatBytes(plan.scope.outputs?.bytes ?? 0)}）＋ workflows（{plan.scope.workflows?.files ?? 0} 文件 · {formatBytes(plan.scope.workflows?.bytes ?? 0)}）</div>
              <div className="text-slate-500">备份空间需求：{formatBytes(plan.backupBytes)}（写入 backups/migration-*）</div>
            </div>
            <div className="flex gap-2">
              <button type="button" onClick={() => setMode('copy')} className={`flex-1 rounded-lg border p-2 text-sm ${mode === 'copy' ? 'border-teal-600 bg-teal-50 font-medium' : 'border-slate-200'}`}>
                <FolderInput className="mr-1 inline size-4" />复制（共享目录保留）
              </button>
              <button type="button" onClick={() => setMode('move')} className={`flex-1 rounded-lg border p-2 text-sm ${mode === 'move' ? 'border-teal-600 bg-teal-50 font-medium' : 'border-slate-200'}`}>
                <ArrowRightLeft className="mr-1 inline size-4" />移动（共享目录清空）
              </button>
            </div>
            <button type="button" className="btn-primary w-fit" disabled={busy} onClick={run} data-testid="migration-run">
              {busy ? <Loader2 className="size-4 animate-spin" /> : <ArrowRightLeft className="size-4" />}开始迁移
            </button>
          </>}
          {result && <div className={`rounded-lg border p-3 text-sm leading-6 ${result.verified ? 'border-emerald-200 bg-emerald-50 text-emerald-900' : 'border-amber-200 bg-amber-50 text-amber-900'}`} data-testid="migration-result">
            <div className="font-medium">{result.verified ? '迁移完成并校验通过' : '迁移完成（有告警）'}</div>
            <div>已迁移：outputs {result.copiedFiles.outputs ?? 0} 个 · workflows {result.copiedFiles.workflows ?? 0} 个 · 耗时 {result.duration}</div>
            <div className="text-xs opacity-80">备份：{result.backupDir}</div>
            {result.warnings.map((warning) => <div key={warning} className="text-xs">· {warning}</div>)}
          </div>}
        </div>
      </Panel>
    </div>
  )
}
