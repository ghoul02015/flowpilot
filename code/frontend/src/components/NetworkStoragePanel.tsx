import { useCallback, useEffect, useState } from 'react'
import { FolderDown, LoaderCircle, Network, RefreshCw, Save } from 'lucide-react'
import { toast } from 'sonner'
import { getNetworkStorage, networkMountScriptURL, saveNetworkStorage, type NetworkStorageView } from '@/api'
import { Panel, PanelHeader } from '@/components/common'

// 网络存储（NAS）配置面板：保存配置 → 下载挂载脚本到 GPU 宿主机执行 → 探测生效。
export function NetworkStoragePanel() {
  const [view, setView] = useState<NetworkStorageView | null>(null)
  const [loadError, setLoadError] = useState('')
  const [type, setType] = useState<'nfs' | 'smb'>('nfs')
  const [address, setAddress] = useState('')
  const [remotePath, setRemotePath] = useState('')
  const [enabled, setEnabled] = useState(false)
  const [saving, setSaving] = useState(false)

  const load = useCallback(() => {
    getNetworkStorage()
      .then((next) => {
        setView(next)
        setType(next.type || 'nfs')
        setAddress(next.address)
        setRemotePath(next.remotePath)
        setEnabled(next.enabled)
        setLoadError('')
      })
      .catch((error: unknown) => setLoadError(error instanceof Error ? error.message : '网络存储配置读取失败'))
  }, [])

  useEffect(() => { load() }, [load])

  async function save() {
    setSaving(true)
    try {
      const next = await saveNetworkStorage({ enabled, type, address: address.trim(), remotePath: remotePath.trim() })
      setView(next)
      toast.success('网络存储配置已保存', { description: enabled ? '请下载挂载脚本到 GPU 宿主机执行一次。' : '已停用网络存储。' })
    } catch (error) {
      toast.error('保存失败', { description: error instanceof Error ? error.message : '请检查配置项。' })
    } finally {
      setSaving(false)
    }
  }

  return <Panel>
    <PanelHeader title="网络存储（NAS 模型仓库）" description="一份模型全集群可用：NAS 只读直挂进实例（无需拷贝），热点模型可在模型中心一键「本地化」到本机高速盘。" action={<button type="button" className="btn-secondary" onClick={load}><RefreshCw className="size-4" />刷新状态</button>} />
    <div className="grid gap-4 p-5">
      {loadError && <p className="rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700">{loadError}</p>}
      {view && <div className={`flex items-start gap-2 rounded-lg border px-3 py-2 text-sm leading-6 ${view.detected ? 'border-emerald-200 bg-emerald-50 text-emerald-900' : view.enabled ? 'border-amber-200 bg-amber-50 text-amber-900' : 'border-slate-200 bg-slate-50 text-slate-600'}`}>
        <Network className="mt-1 size-4 shrink-0" />
        {view.detected
          ? <>网络仓库已生效：宿主机 <span className="font-mono">{view.mountPoint}</span> 已挂载，新创建的实例将自动挂载该仓库（只读）。</>
          : view.enabled
            ? <>已配置但未检测到挂载：请在宿主机执行挂载脚本（会自动写入标记并重启平台容器），完成后点「刷新状态」。</>
            : '未启用。保存配置并下载挂载脚本，在 GPU 宿主机执行一次即可启用。'}
      </div>}
      <div className="grid gap-4 sm:grid-cols-2">
        <label className="field-label">存储类型
          <select className="field-input" value={type} onChange={(event) => setType(event.target.value as 'nfs' | 'smb')}>
            <option value="nfs">NFS（推荐，Linux NAS）</option>
            <option value="smb">SMB / CIFS（Windows 共享）</option>
          </select>
        </label>
        <label className="field-label">服务器地址
          <input className="field-input font-mono text-sm" value={address} onChange={(event) => setAddress(event.target.value)} placeholder="192.168.0.10 或 nas.local" autoComplete="off" />
        </label>
        <label className="field-label">远端共享路径
          <input className="field-input font-mono text-sm" value={remotePath} onChange={(event) => setRemotePath(event.target.value)} placeholder={type === 'nfs' ? '/volume1/models' : 'models'} autoComplete="off" />
        </label>
        <label className="flex min-h-11 cursor-pointer items-center gap-2 text-sm text-slate-700">
          <input type="checkbox" className="size-4 accent-teal-700" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />启用网络存储
        </label>
      </div>
      <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs leading-6 text-slate-500">
        <p className="font-semibold text-slate-700">工作原理</p>
        <p>1. 挂载脚本把 NAS 只读挂到宿主机 <span className="font-mono">{view?.mountPoint ?? '共享目录/network'}</span>（含 /etc/fstab 开机自动挂载）。</p>
        <p>2. 新创建的实例自动获得网络仓库挂载（ComfyUI 原生 extra_model_paths 机制），模型直接可用、不占本机磁盘。</p>
        <p>3. 高频使用的模型在模型中心点「本地化」复制到本地仓库；网络仓库在平台内始终只读，删除需在 NAS 侧操作。</p>
      </div>
      <div className="flex flex-wrap gap-2">
        <button type="button" className="btn-primary" disabled={saving} onClick={() => void save()}>{saving ? <LoaderCircle className="size-4 animate-spin" /> : <Save className="size-4" />}保存配置</button>
        {enabled && <a className="btn-secondary" href={networkMountScriptURL()} download="mount-network-models.sh"><FolderDown className="size-4" />下载挂载脚本</a>}
      </div>
    </div>
  </Panel>
}
