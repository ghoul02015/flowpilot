import { useEffect, useState } from 'react'
import { Archive, HardDriveDownload, Loader2, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import { createBackup, formatBytes, listBackups, type BackupItem } from '@/api'
import { Panel, PanelHeader } from '@/components/common'

// BackupPanel 存储备份（0.0.9.12）：创建备份任务（统一任务协议）与备份列表。
export function BackupPanel() {
  const [backups, setBackups] = useState<BackupItem[]>([])
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)

  function reload() {
    setLoading(true)
    listBackups()
      .then((result: { backups: BackupItem[] }) => setBackups(result.backups))
      .catch((reason: unknown) => toast.error('备份列表读取失败', { description: reason instanceof Error ? reason.message : '未知错误' }))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    const controller = new AbortController()
    listBackups()
      .then((result: { backups: BackupItem[] }) => setBackups(result.backups))
      .catch(() => {})
      .finally(() => setLoading(false))
    return () => controller.abort()
  }, [])

  function create() {
    setCreating(true)
    createBackup()
      .then((result: { taskId: string }) => {
        toast.success('备份任务已创建', { description: `任务 ${result.taskId} 执行中，进度见任务中心。` })
        setTimeout(reload, 4000)
      })
      .catch((reason: unknown) => toast.error('创建备份失败', { description: reason instanceof Error ? reason.message : '未知错误' }))
      .finally(() => setCreating(false))
  }

  return (
    <Panel data-testid="backup-panel">
      <PanelHeader
        title="存储与备份"
        description="平台备份：SQLite 一致性快照＋实例数据（配置/工作流/输出）＋网络存储配置；模型与镜像不重复打包"
        action={<div className="flex gap-2">
          <button type="button" className="btn-secondary" disabled={loading} onClick={reload}><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />刷新</button>
          <button type="button" className="btn-primary" disabled={creating} onClick={create} data-testid="create-backup">
            {creating ? <Loader2 className="size-4 animate-spin" /> : <HardDriveDownload className="size-4" />}{creating ? '创建中' : '创建备份'}
          </button>
        </div>}
      />
      <div className="overflow-x-auto" data-testid="backup-list">
        <table className="data-table">
          <thead><tr><th>备份</th><th>创建时间</th><th>大小</th><th>实例数</th><th>组件</th><th>Checksum</th></tr></thead>
          <tbody>
            {backups.map((item) => (
              <tr key={item.name}>
                <td className="font-mono text-xs">{item.name}</td>
                <td className="text-slate-600">{item.manifest?.createdAt ? new Date(item.manifest.createdAt).toLocaleString('zh-CN', { hour12: false }) : '—'}</td>
                <td>{formatBytes(item.sizeBytes)}</td>
                <td>{item.manifest?.instanceCount ?? 0}</td>
                <td>{Object.entries(item.manifest?.components ?? {}).filter(([, on]) => on).map(([key]) => key).join('＋') || '—'}</td>
                <td className="font-mono text-xs text-slate-400" title={item.checksum}>{item.checksum ? item.checksum.slice(0, 16) + '…' : '—'}</td>
              </tr>
            ))}
            {backups.length === 0 && !loading && <tr><td colSpan={6} className="text-center text-slate-400">暂无备份</td></tr>}
          </tbody>
        </table>
      </div>
      <div className="border-t border-slate-200 px-5 py-4 text-xs leading-5 text-slate-500">
        <Archive className="mr-1 inline size-3.5" />备份写入数据目录 backups/，含 manifest 与 sha256 校验；恢复操作随 0.0.9.14 升级回滚框架提供。
      </div>
    </Panel>
  )
}
