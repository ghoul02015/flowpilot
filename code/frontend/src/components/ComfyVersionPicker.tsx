import { useEffect, useMemo, useRef, useState } from 'react'
import { CheckCircle2, GitCommitHorizontal, LoaderCircle, RefreshCw, Search } from 'lucide-react'
import { getComfyVersions, resolveComfyVersion, type ComfyVersion, type ComfyVersionCatalog } from '@/api'

interface Props {
  value: ComfyVersion | null
  onChange: (version: ComfyVersion | null) => void
}

function formatDate(value?: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value.slice(0, 10)
  return date.toLocaleDateString('zh-CN')
}

function kindLabel(kind: string): string {
  return { release: '正式 Release', tag: '版本标签', branch: '开发分支', commit: '固定提交' }[kind] ?? kind
}

function optionLabel(version: ComfyVersion): string {
  if (version.kind === 'branch') return `${version.displayLabel} · 开发分支`
  const prefix = version.kind === 'release' ? (version.prerelease ? '[预发布] ' : '[Release] ') : '[仅标签] '
  const date = formatDate(version.publishedAt)
  return `${prefix}${version.version || version.ref} · ${version.commitShort}${date ? ` · ${date}` : ''}`
}

// ComfyVersionPicker 在线选择 ComfyUI 版本，并把选择结果固定到具体 commit。
export function ComfyVersionPicker({ value, onChange }: Props) {
  const [catalog, setCatalog] = useState<ComfyVersionCatalog | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [includePrerelease, setIncludePrerelease] = useState(false)
  const [customMode, setCustomMode] = useState(false)
  const [customRef, setCustomRef] = useState('')
  const [customError, setCustomError] = useState('')
  const [resolving, setResolving] = useState(false)
  const defaultedRef = useRef(false)

  async function load(refresh: boolean) {
    setLoading(true)
    setError('')
    try {
      const next = await getComfyVersions(refresh)
      setCatalog(next)
      if (value?.kind === 'branch' && next.branch) {
        onChange(next.branch)
      } else if (value && (value.kind === 'release' || value.kind === 'tag')) {
        const matched = [...next.releases, ...next.tags].find((item) => item.ref === value.ref)
        if (matched) onChange(matched)
      }
      return next
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '无法读取 ComfyUI 官方版本')
      return null
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    const controller = new AbortController()
    void load(false)
    return () => controller.abort()
    // 仅在弹窗挂载时读取；后续刷新由按钮触发。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (!catalog || defaultedRef.current || value) return
    if (catalog.default) {
      defaultedRef.current = true
      setCustomMode(false)
      onChange(catalog.default)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [catalog, value])

  const releases = useMemo(() => (catalog?.releases ?? []).filter((item) => includePrerelease || !item.prerelease), [catalog, includePrerelease])
  const selectedKey = customMode ? 'custom' : value ? `${value.kind}:${value.ref}` : ''

  function selectVersion(key: string) {
    setCustomError('')
    if (key === 'custom') {
      setCustomMode(true)
      setCustomRef(value?.ref ?? '')
      onChange(null)
      return
    }
    setCustomMode(false)
    const match = [...(catalog?.releases ?? []), ...(catalog?.tags ?? [])].find((item) => `${item.kind}:${item.ref}` === key)
      ?? (catalog?.branch && `branch:${catalog.branch.ref}` === key ? catalog.branch : undefined)
    onChange(match ?? null)
  }

  async function resolveCustom() {
    const ref = customRef.trim()
    if (!ref) {
      setCustomError('请输入版本标签、分支或完整 commit')
      return
    }
    setResolving(true)
    setCustomError('')
    try {
      const resolved = await resolveComfyVersion(ref)
      onChange(resolved)
    } catch (reason) {
      setCustomError(reason instanceof Error ? reason.message : '版本解析失败')
    } finally {
      setResolving(false)
    }
  }

  async function refresh() {
    const next = await load(true)
    if (!next || customMode) return
    if (value?.kind === 'branch' && next.branch) {
      onChange(next.branch)
      return
    }
    if (value) {
      const matched = [...next.releases, ...next.tags].find((item) => item.ref === value.ref)
      if (matched) onChange(matched)
    }
  }

  function togglePrerelease(checked: boolean) {
    setIncludePrerelease(checked)
    if (!checked && value?.prerelease) onChange(catalog?.default ?? null)
  }

  return <div className="grid gap-3 rounded-xl border border-slate-200 bg-slate-50/60 p-4" data-testid="comfy-version-picker">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <label htmlFor="comfyui-version-select" className="text-sm font-medium text-slate-800">ComfyUI 版本（固定到具体提交）</label>
      <div className="flex items-center gap-2 text-xs text-slate-500">
        {catalog && <span className={catalog.stale ? 'text-amber-700' : ''}>{catalog.source === 'online' ? '在线数据' : '缓存数据'} · {formatDate(catalog.fetchedAt)}</span>}
        <label className="flex cursor-pointer items-center gap-1.5"><input type="checkbox" className="size-3.5 accent-teal-700" checked={includePrerelease} onChange={(event) => togglePrerelease(event.target.checked)} />预发布</label>
        <button type="button" className="btn-quiet px-1.5 text-xs" disabled={loading} onClick={() => void refresh()}><RefreshCw className={`size-3.5 ${loading ? 'animate-spin' : ''}`} />刷新</button>
      </div>
    </div>
    <select id="comfyui-version-select" aria-label="ComfyUI 版本" className="field-input font-mono text-sm" value={selectedKey} onChange={(event) => selectVersion(event.target.value)} disabled={loading}>
      <option value="">{loading ? '正在读取官方版本…' : '请选择版本'}</option>
      {releases.length > 0 && <optgroup label="正式 Release（推荐）">{releases.map((item) => <option key={`release:${item.ref}`} value={`release:${item.ref}`}>{optionLabel(item)}</option>)}</optgroup>}
      {(catalog?.tags.length ?? 0) > 0 && <optgroup label="版本标签（未发布 Release）">{catalog?.tags.map((item) => <option key={`tag:${item.ref}`} value={`tag:${item.ref}`}>{optionLabel(item)}</option>)}</optgroup>}
      {catalog?.branch && <optgroup label="开发分支（不建议生产）"><option value={`branch:${catalog.branch.ref}`}>{optionLabel(catalog.branch)}</option></optgroup>}
      <option value="custom">自定义标签 / 分支 / Commit…</option>
    </select>
    {customMode && <div className="grid gap-2 sm:grid-cols-[1fr_auto]">
      <input className="field-input font-mono text-sm" aria-label="自定义 ComfyUI 版本" value={customRef} onChange={(event) => setCustomRef(event.target.value)} placeholder="v0.45.0 或完整 40 位 commit" autoComplete="off" />
      <button type="button" className="btn-secondary" disabled={resolving} onClick={() => void resolveCustom()}>{resolving ? <LoaderCircle className="size-4 animate-spin" /> : <Search className="size-4" />}解析版本</button>
    </div>}
    {error && <p role="alert" className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800">版本接口异常：{error}。为安全起见不会回退到 master，请恢复网络或输入明确 commit。</p>}
    {customError && <p role="alert" className="rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700">{customError}</p>}
    {catalog?.warnings?.length ? <p className="text-xs leading-5 text-amber-700">{catalog.warnings[catalog.warnings.length - 1]}</p> : null}
    {value ? <div className="rounded-lg border border-emerald-200 bg-emerald-50/70 p-3 text-xs leading-5 text-emerald-900">
      <div className="flex flex-wrap items-center justify-between gap-2"><span className="inline-flex items-center gap-1.5 font-semibold"><CheckCircle2 className="size-4" />{value.displayLabel}</span><span className="rounded-full border border-emerald-200 bg-white px-2 py-0.5 text-[11px]">{kindLabel(value.kind)}</span></div>
      <div className="mt-1.5 grid gap-1 sm:grid-cols-2">
        <span className="inline-flex items-center gap-1 break-all"><GitCommitHorizontal className="size-3.5 shrink-0" />{value.commit}</span>
        <span>{value.commitAt ? `提交于 ${formatDate(value.commitAt)}` : value.publishedAt ? `发布于 ${formatDate(value.publishedAt)}` : '提交时间未知'}</span>
      </div>
      <p className="mt-1.5">本次构建固定到该 commit，不会跟随 master 或分支更新；重试沿用同一提交。</p>
    </div> : <p className="text-xs leading-5 text-slate-500">请选择正式 Release，或手动解析明确版本。构建必须固定 commit 后才能开始。</p>}
  </div>
}
