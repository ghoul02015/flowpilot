import { useEffect, useMemo, useState } from 'react'
import { CircleAlert, Link2, LoaderCircle, Search } from 'lucide-react'
import { toast } from 'sonner'
import { getModelLinks, putModelLinks, type ModelLink, type ModelReport } from '@/api'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

interface Props {
  instance: { name: string; status: string } | null
  models: ModelReport | null
  onClose: () => void
}

// 关联键形如「类别/相对路径」，与后端 ModelLink 的 category+name 对应。
function linkKey(category: string, name: string) {
  return `${category}/${name}`
}

function relativeToCategory(filePath: string): { category: string; name: string } {
  const parts = filePath.split('/')
  return { category: parts[0], name: parts.slice(1).join('/') }
}

export function ModelLinksDialog({ instance, models, onClose }: Props) {
  // 父级按实例名做 key 重挂载：打开即处于 loading 态，关闭后状态自然丢弃。
  const instanceName = instance?.name
  const [loading, setLoading] = useState(Boolean(instanceName))
  const [saving, setSaving] = useState(false)
  const [supported, setSupported] = useState(true)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [query, setQuery] = useState('')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!instanceName) return
    let cancelled = false
    getModelLinks(instanceName)
      .then((view) => {
        if (cancelled) return
        setSupported(view.supported)
        setSelected(new Set(view.links.map((link) => linkKey(link.category, link.name))))
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : '读取模型关联失败')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [instanceName])

  // 只允许关联本地仓库模型：网络存储为只读来源且不在关联存在性校验范围内。
  const groups = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    const localFiles = (models?.files ?? []).filter((file) => (file.origin ?? 'local') === 'local')
    const buckets = new Map<string, { name: string; key: string; sizeBytes: number }[]>()
    for (const file of localFiles) {
      const { category, name } = relativeToCategory(file.path)
      if (!name) continue
      if (keyword && !name.toLowerCase().includes(keyword)) continue
      const bucket = buckets.get(category) ?? []
      bucket.push({ name, key: linkKey(category, name), sizeBytes: file.sizeBytes })
      buckets.set(category, bucket)
    }
    return [...buckets.entries()]
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([category, items]) => ({ category, items: items.sort((left, right) => left.name.localeCompare(right.name)) }))
  }, [models, query])

  function toggle(key: string) {
    setSelected((previous) => {
      const next = new Set(previous)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  async function save() {
    if (!instance) return
    setSaving(true)
    try {
      const links: ModelLink[] = [...selected].map((key) => {
        const index = key.indexOf('/')
        return { category: key.slice(0, index), name: key.slice(index + 1) }
      })
      const result = await putModelLinks(instance.name, links)
      toast.success('模型关联已更新', {
        description: `${result.hint ?? 'ComfyUI 页面刷新后即可见'}（新增 ${result.added} · 移除 ${result.removed}）`,
      })
      onClose()
    } catch (reason) {
      toast.error('更新模型关联失败', { description: reason instanceof Error ? reason.message : undefined })
    } finally {
      setSaving(false)
    }
  }

  const totalModels = groups.reduce((total, group) => total + group.items.length, 0)

  return <Dialog open={Boolean(instance)} onOpenChange={(open) => !open && onClose()}>
    <DialogContent className="sm:max-w-3xl">
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2"><Link2 className="size-5 text-teal-700" />模型关联 - {instance?.name}</DialogTitle>
        <DialogDescription>
          勾选共享仓库中的模型，实例内将以 flowpilot_linked 来源出现；增删即时生效，ComfyUI 页面刷新即可看到，无需重启实例。
        </DialogDescription>
      </DialogHeader>

      {!supported && <div className="flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-3 text-xs leading-5 text-amber-900">
        <CircleAlert className="mt-0.5 size-4 shrink-0" />
        <span>该实例由旧版本创建，缺少模型关联挂载（Docker 挂载在容器创建时固定，重启不会补加）。请删除并以相同参数重建实例后使用——模型、输出与工作流均保存在共享目录，重建不会丢失数据。</span>
      </div>}

      {error && <div className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-800">{error}</div>}

      {supported && <>
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <label className="relative flex-1"><span className="sr-only">搜索模型</span><Search className="absolute top-3 left-3 size-4 text-slate-400" /><input className="field-input pl-9" placeholder="搜索已下载模型" value={query} onChange={(event) => setQuery(event.target.value)} /></label>
          <span className="text-xs text-slate-500">已选 <strong className="font-semibold text-teal-700">{selected.size}</strong> 个</span>
        </div>

        <div className="max-h-[48dvh] space-y-2 overflow-y-auto rounded-lg border border-slate-200 p-2">
          {loading && <div className="flex items-center justify-center gap-2 py-8 text-sm text-slate-500"><LoaderCircle className="size-4 animate-spin" />正在读取关联…</div>}
          {!loading && totalModels === 0 && <p className="py-8 text-center text-sm text-slate-500">共享仓库暂无已下载的本地模型，请先在模型中心下载或导入。</p>}
          {!loading && groups.map((group) => <details key={group.category} open className="rounded-md">
            <summary className="cursor-pointer rounded-md bg-slate-50 px-3 py-2 text-xs font-semibold text-slate-700 select-none">{group.category} <span className="ml-1 font-normal text-slate-500">({group.items.length})</span></summary>
            <div className="mt-1 space-y-0.5 px-2">
              {group.items.map((item) => <label key={item.key} className="flex min-h-9 cursor-pointer items-center gap-2 rounded px-2 text-sm text-slate-700 hover:bg-slate-50">
                <input type="checkbox" className="size-4 accent-teal-700" checked={selected.has(item.key)} onChange={() => toggle(item.key)} />
                <span className="min-w-0 flex-1 truncate font-mono text-xs" title={item.name}>{item.name}</span>
              </label>)}
            </div>
          </details>)}
        </div>
      </>}

      <DialogFooter>
        <button type="button" className="btn-secondary" onClick={onClose}>取消</button>
        <button type="button" className="btn-primary" disabled={!supported || saving || loading} onClick={() => void save()}>
          {saving ? <LoaderCircle className="size-4 animate-spin" /> : <Link2 className="size-4" />}
          保存关联
        </button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
}
