import { useEffect, useState } from 'react'
import { Database, HardDriveDownload, RefreshCw, ShieldCheck, TriangleAlert } from 'lucide-react'
import { toast } from 'sonner'
import {
  backupDatabase,
  formatBytes,
  getDatabaseStatus,
  runDatabaseCheck,
  type DatabaseInstanceDiff,
  type DatabaseStatus,
  type DatabaseSyncReport,
} from '@/api'
import { Panel, PanelHeader } from '@/components/common'

const kindLabels: Record<DatabaseInstanceDiff['kind'], string> = {
  imported: '新导入',
  updated: '已更新',
  rebuilt: '容器重建',
  unchanged: '无变化',
  orphan: '孤儿记录',
}

const kindBadges: Record<DatabaseInstanceDiff['kind'], string> = {
  imported: 'bg-blue-50 text-blue-700',
  updated: 'bg-amber-50 text-amber-700',
  rebuilt: 'bg-purple-50 text-purple-700',
  unchanged: 'bg-slate-100 text-slate-600',
  orphan: 'bg-rose-50 text-rose-700',
}

function formatTime(value?: string): string {
  if (!value) return '—'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString('zh-CN', { hour12: false })
}

export function DatabasePanel() {
  const [status, setStatus] = useState<DatabaseStatus | null>(null)
  const [report, setReport] = useState<DatabaseSyncReport | null>(null)
  const [checking, setChecking] = useState(false)
  const [backing, setBacking] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    getDatabaseStatus(controller.signal)
      .then(setStatus)
      .catch((reason: unknown) => {
        if (controller.signal.aborted) return
        setError(reason instanceof Error ? reason.message : '数据底座接口不可用')
      })
    return () => controller.abort()
  }, [])

  function check() {
    setChecking(true)
    runDatabaseCheck()
      .then((result) => {
        setReport(result)
        toast.success('一致性检查完成', {
          description: `导入 ${result.imported.length} · 更新 ${result.updated.length} · 重建 ${result.rebuilt.length} · 孤儿 ${result.orphans.length} · 冲突 ${result.conflicts.length}`,
        })
        return getDatabaseStatus().then(setStatus)
      })
      .catch((reason: unknown) => toast.error('一致性检查失败', { description: reason instanceof Error ? reason.message : '未知错误' }))
      .finally(() => setChecking(false))
  }

  function backup() {
    setBacking(true)
    backupDatabase()
      .then((result) => toast.success('数据库备份完成', { description: `${result.name} · ${formatBytes(result.size)}` }))
      .catch((reason: unknown) => toast.error('备份失败', { description: reason instanceof Error ? reason.message : '未知错误' }))
      .finally(() => setBacking(false))
  }

  if (error) {
    return <Panel data-testid="database-panel">
      <PanelHeader title="数据底座" description="SQLite 数据库状态" />
      <div className="flex items-start gap-3 border-b border-amber-200 bg-amber-50 px-5 py-4 text-sm text-amber-900">
        <TriangleAlert className="mt-0.5 size-5 shrink-0" />
        <div><strong>数据底座接口不可用</strong><div className="mt-0.5 leading-6">{error}</div></div>
      </div>
    </Panel>
  }

  const anomalies = (status?.orphanMappings ?? 0) + (report?.conflicts.length ?? 0)
  const changedDiffs = report?.diffs.filter((item) => item.kind !== 'unchanged') ?? []

  return <Panel data-testid="database-panel">
    <PanelHeader
      title="数据底座"
      description="SQLite 数据库 · 迁移与实例映射状态"
      action={<div className="flex gap-2">
        <button type="button" className="btn-secondary" disabled={checking} onClick={check}>
          <RefreshCw className={`size-4 ${checking ? 'animate-spin' : ''}`} />{checking ? '检查中' : '立即检查'}
        </button>
        <button type="button" className="btn-secondary" disabled={backing} onClick={backup}>
          <HardDriveDownload className={`size-4 ${backing ? 'animate-pulse' : ''}`} />{backing ? '备份中' : '备份数据库'}
        </button>
      </div>}
    />
    {status && <div className="grid gap-px overflow-hidden bg-slate-200 sm:grid-cols-2" data-testid="database-status">
      {[
        { label: '数据库引擎', value: `${status.driver} · ${status.journalMode}`, ok: true },
        { label: 'Schema 版本', value: `v${status.schemaVersion}（${status.migrations.length} 个迁移已应用）`, ok: true },
        { label: '实例映射', value: `${status.instanceMappings} 条记录`, ok: true },
        { label: '异常映射', value: anomalies > 0 ? `孤儿 ${status.orphanMappings} · 冲突 ${report?.conflicts.length ?? 0}` : '无', ok: anomalies === 0 },
        { label: '完整性检查', value: status.integrity.ok ? 'integrity_check 通过' : `异常：${status.integrity.message}`, ok: status.integrity.ok },
        { label: '最近同步', value: status.lastSync?.checkedAt ? `${formatTime(status.lastSync.checkedAt)} · 导入 ${status.lastSync.imported}／更新 ${status.lastSync.updated}／重建 ${status.lastSync.rebuilt}` : '尚未同步', ok: true },
      ].map((item) => <div key={item.label} className="flex items-start gap-3 bg-white p-5">
        {item.ok ? <ShieldCheck className="mt-0.5 size-5 shrink-0 text-emerald-600" /> : <TriangleAlert className="mt-0.5 size-5 shrink-0 text-amber-600" />}
        <div>
          <div className="text-sm font-medium text-slate-900">{item.label}</div>
          <div className="mt-1 text-sm leading-6 text-slate-600">{item.value}</div>
        </div>
      </div>)}
    </div>}

    {report && <div className="border-t border-slate-200 px-5 py-4" data-testid="database-report">
      <div className="flex items-center gap-2 text-sm font-medium text-slate-900">
        <Database className="size-4 text-teal-700" />
        检查报告 · {formatTime(report.checkedAt)} · 耗时 {report.duration}
      </div>
      {report.conflicts.length > 0 && <div className="mt-3 space-y-2">
        {report.conflicts.map((conflict) => <div key={`${conflict.name}-${conflict.reason}`} className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900">
          冲突 · {conflict.name}：{conflict.reason}
        </div>)}
      </div>}
      {changedDiffs.length === 0 && report.conflicts.length === 0
        ? <p className="mt-2 text-sm text-slate-500">全部实例映射一致（{report.unchanged} 个无变化），无需处理。</p>
        : <div className="mt-3 overflow-x-auto">
          <table className="data-table">
            <thead>
              <tr>
                <th>实例</th>
                <th>结果</th>
                <th>差异（数据库值 → Docker 值 → 采用值）</th>
              </tr>
            </thead>
            <tbody>
              {changedDiffs.map((item) => <tr key={item.uuid || item.name} data-testid={`database-diff-${item.name}`}>
                <td className="font-medium">{item.name}</td>
                <td><span className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${kindBadges[item.kind]}`}>{kindLabels[item.kind]}</span></td>
                <td className="text-slate-600">
                  {item.orphan
                    ? '容器已消失，记录保留为孤儿（不自动删除）'
                    : (item.diffs?.length ?? 0) === 0
                      ? item.kind === 'imported' ? '新实例首次入库' : '容器身份变化，沿用实例 UUID'
                      : <ul className="space-y-1">{item.diffs?.map((diff) => <li key={diff.field}>
                        <span className="font-mono text-xs">{diff.field}</span>：{diff.database || '（空）'} → {diff.docker || '（空）'} → <strong>{diff.adopted || '（空）'}</strong>
                        <span className="ml-1 text-xs text-slate-400">（{diff.reason}）</span>
                      </li>)}</ul>}
                </td>
              </tr>)}
            </tbody>
          </table>
        </div>}
    </div>}

    <div className="border-t border-slate-200 px-5 py-4 text-xs leading-5 text-slate-500">
      数据底座为平台提供稳定对象 ID 与事务性存储；Docker 容器仍是运行时状态的唯一来源，启动与手动检查时对账。备份写入数据目录 backups/ 子目录。
    </div>
  </Panel>
}
