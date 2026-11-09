import { useCallback, useEffect, useMemo, useState } from 'react'
import { CloudDownload, Download, Hammer, LoaderCircle, RefreshCw, Tag, Trash2, Upload } from 'lucide-react'
import { toast } from 'sonner'
import { deleteLocalImageApi, exportLocalImageURL, formatBytes, getImageBuildStatus, getImageCatalog, getImagePullStatus, getLocalImages, importLocalImageApi, startImageBuild, startImagePull, tagLocalImageApi, type ImageBuildStatus, type ImagePullStatus, type ImageRecipe, type LocalImage } from '@/api'
import { EmptyState, Panel, PanelHeader } from '@/components/common'

interface Props {
  mode: 'manage' | 'build'
}

export function ImageManager({ mode }: Props) {
  const [images, setImages] = useState<LocalImage[]>([])
  const [recipes, setRecipes] = useState<ImageRecipe[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [pullImage, setPullImage] = useState('')
  const [pull, setPull] = useState<ImagePullStatus | null>(null)
  const [recipeId, setRecipeId] = useState('nvidia-cu130')
  const [imageName, setImageName] = useState('flowpilot/comfyui:cu130')
  const [ref, setRef] = useState('master')
  const [pip, setPip] = useState('tsinghua')
  const [manager, setManager] = useState(true)
  const [build, setBuild] = useState<ImageBuildStatus | null>(null)

  const tags = useMemo(() => images.flatMap((item) => item.RepoTags.map((tag) => ({ tag, image: item }))).filter((item) => item.tag && item.tag !== '<none>:<none>'), [images])
  const selectedRecipe = recipes.find((item) => item.id === recipeId)

  const refresh = useCallback(() => {
    setLoading(true)
    Promise.all([
      getLocalImages().then(setImages),
      getImageCatalog().then((list) => {
        setRecipes(list)
        const selected = list.find((item) => item.id === recipeId) ?? list[0]
        if (selected) {
          setRecipeId(selected.id)
          setImageName((current) => current || selected.image)
        }
      }),
      getImagePullStatus().then(setPull).catch(() => {}),
      getImageBuildStatus().then(setBuild).catch(() => {}),
    ]).catch((error: unknown) => toast.error('镜像信息读取失败', { description: error instanceof Error ? error.message : '请确认后端已连接 Docker。' })).finally(() => setLoading(false))
  }, [recipeId])

  useEffect(() => { refresh() }, [refresh])

  useEffect(() => {
    if (pull?.state !== 'pulling') return
    const timer = window.setInterval(() => getImagePullStatus().then((status) => {
      setPull(status)
      if (status.state === 'done') refresh()
    }).catch(() => {}), 2000)
    return () => window.clearInterval(timer)
  }, [pull?.state, refresh])

  useEffect(() => {
    if (build?.state !== 'building') return
    const timer = window.setInterval(() => getImageBuildStatus().then((status) => {
      setBuild(status)
      if (status.state === 'done') refresh()
    }).catch(() => {}), 2000)
    return () => window.clearInterval(timer)
  }, [build?.state, refresh])

  function onRecipeChange(next: string) {
    setRecipeId(next)
    const recipe = recipes.find((item) => item.id === next)
    if (recipe) setImageName(recipe.image)
  }

  async function retag(source: string) {
    const target = window.prompt('请输入新的镜像标签，例如 flowpilot/comfyui:cu130-test', source)
    if (!target || target === source) return
    setBusy(true)
    try {
      await tagLocalImageApi(source, target)
      toast.success('镜像标签已更新', { description: `${source} → ${target}` })
      refresh()
    } catch (error) {
      toast.error('修改镜像标签失败', { description: error instanceof Error ? error.message : '请稍后重试。' })
    } finally {
      setBusy(false)
    }
  }

  async function remove(tag: string) {
    if (!window.confirm(`确认删除本机镜像标签 ${tag}？如果镜像被容器使用，Docker 会拒绝删除。`)) return
    setBusy(true)
    try {
      await deleteLocalImageApi(tag)
      toast.success('镜像已删除', { description: tag })
      refresh()
    } catch (error) {
      toast.error('删除镜像失败', { description: error instanceof Error ? error.message : '请确认没有实例正在使用该镜像。' })
    } finally {
      setBusy(false)
    }
  }

  async function importImage(file?: File) {
    if (!file) return
    setBusy(true)
    try {
      await importLocalImageApi(file)
      toast.success('镜像导入完成', { description: file.name })
      refresh()
    } catch (error) {
      toast.error('镜像导入失败', { description: error instanceof Error ? error.message : '请确认文件为 docker save 导出的 tar。' })
    } finally {
      setBusy(false)
    }
  }

  async function startPull() {
    if (!pullImage.trim()) {
      toast.error('请输入镜像名')
      return
    }
    try {
      const status = await startImagePull(pullImage.trim())
      setPull(status)
      toast.success('已开始拉取镜像', { description: pullImage.trim() })
    } catch (error) {
      toast.error('启动拉取失败', { description: error instanceof Error ? error.message : '请稍后重试。' })
    }
  }

  async function startBuild() {
    if (!imageName.trim()) {
      toast.error('请输入构建后的镜像名')
      return
    }
    try {
      const status = await startImageBuild({ variant: recipeId, image: imageName.trim(), ref: ref.trim() || 'master', pip, manager: manager ? '1' : '0' })
      setBuild(status)
      toast.success('已开始构建镜像', { description: imageName.trim() })
    } catch (error) {
      toast.error('启动构建失败', { description: error instanceof Error ? error.message : '请稍后重试。' })
    }
  }

  if (mode === 'manage') return <div className="grid gap-5">
    <Panel>
      <PanelHeader title="本机容器镜像" description="管理 Docker 本地已有镜像；新建实例只能从这里选择已存在的镜像。" action={<div className="flex flex-wrap gap-2"><label className={`btn-secondary ${busy ? 'pointer-events-none opacity-60' : ''}`}><Upload className="size-4" />导入 tar<input type="file" accept=".tar,application/x-tar" className="sr-only" disabled={busy} onChange={(event) => void importImage(event.target.files?.[0])} /></label><button type="button" className="btn-secondary" onClick={refresh} disabled={loading}><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />刷新</button></div>} />
      <div className="overflow-x-auto"><table className="data-table min-w-[920px]"><thead><tr><th>镜像标签</th><th>镜像 ID</th><th>大小</th><th>创建时间</th><th>操作</th></tr></thead><tbody>
        {tags.map(({ tag, image }) => <tr key={`${image.Id}-${tag}`}><td className="font-mono text-xs">{tag}</td><td className="font-mono text-xs text-slate-500">{image.Id.slice(0, 20)}</td><td>{formatBytes(image.Size)}</td><td>{new Date(image.Created * 1000).toLocaleString('zh-CN', { hour12: false })}</td><td><div className="flex items-center gap-1"><button type="button" className="icon-button" title="改标签" disabled={busy} onClick={() => void retag(tag)}><Tag className="size-4" /></button><button type="button" className="icon-button" title="导出 tar" onClick={() => window.open(exportLocalImageURL(tag), '_blank', 'noopener')}><Download className="size-4" /></button><button type="button" className="icon-button text-rose-600 hover:bg-rose-50" title="删除镜像" disabled={busy} onClick={() => void remove(tag)}><Trash2 className="size-4" /></button></div></td></tr>)}
      </tbody></table></div>
      {tags.length === 0 && <EmptyState title="暂无本机镜像" description="可先导入 docker save 的 tar 包，或到「手动构建/拉取」子菜单构建、拉取镜像。" />}
    </Panel>
  </div>

  return <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(360px,0.9fr)]">
    <Panel>
      <PanelHeader title="一键手动构建 ComfyUI 镜像" description="按显卡配方在本机 Docker 中构建不同版本 ComfyUI 镜像，构建日志实时回显。" />
      <div className="grid gap-4 p-5">
        <label className="field-label">显卡构建配方<select className="field-input" value={recipeId} onChange={(event) => onRecipeChange(event.target.value)}>{recipes.map((recipe) => <option key={recipe.id} value={recipe.id}>{recipe.label}</option>)}</select></label>
        {selectedRecipe && <p className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm leading-6 text-slate-600">{selectedRecipe.description}；建议镜像名 <span className="font-mono">{selectedRecipe.image}</span>。</p>}
        <div className="grid gap-4 sm:grid-cols-2"><label className="field-label">镜像名<input className="field-input font-mono text-sm" value={imageName} onChange={(event) => setImageName(event.target.value)} placeholder="flowpilot/comfyui:cu130" /></label><label className="field-label">ComfyUI 版本<input className="field-input font-mono text-sm" value={ref} onChange={(event) => setRef(event.target.value)} placeholder="master 或标签" /></label></div>
        <div className="grid gap-4 sm:grid-cols-2"><label className="field-label">pip 源<select className="field-input" value={pip} onChange={(event) => setPip(event.target.value)}><option value="tsinghua">清华镜像</option><option value="aliyun">阿里云镜像</option><option value="pypi">PyPI 官方</option></select></label><label className="flex min-h-11 cursor-pointer items-end gap-2 pb-2 text-sm text-slate-700"><input type="checkbox" className="size-4 accent-teal-700" checked={manager} onChange={(event) => setManager(event.target.checked)} />内置 ComfyUI-Manager</label></div>
        <button type="button" className="btn-primary w-full" disabled={build?.state === 'building'} onClick={() => void startBuild()}>{build?.state === 'building' ? <LoaderCircle className="size-4 animate-spin" /> : <Hammer className="size-4" />}{build?.state === 'building' ? '正在构建' : '开始构建镜像'}</button>
        <div className={`rounded-lg border p-3 text-sm ${build?.state === 'error' ? 'border-rose-200 bg-rose-50 text-rose-800' : build?.state === 'done' ? 'border-emerald-200 bg-emerald-50 text-emerald-800' : 'border-slate-200 bg-slate-50 text-slate-600'}`}>{build?.message || '尚未开始构建。'}</div>
      </div>
    </Panel>

    <div className="grid gap-5">
      <Panel><PanelHeader title="在线拉取镜像" description="手动构建和拉取已从新建实例向导中移出，统一在这里维护。" /><div className="grid gap-3 p-5"><label className="field-label">镜像名<input className="field-input font-mono text-sm" value={pullImage} onChange={(event) => setPullImage(event.target.value)} placeholder="comfyui:latest" /></label><button type="button" className="btn-secondary w-full" disabled={pull?.state === 'pulling'} onClick={() => void startPull()}>{pull?.state === 'pulling' ? <LoaderCircle className="size-4 animate-spin" /> : <CloudDownload className="size-4" />}{pull?.state === 'pulling' ? '正在拉取' : '开始拉取'}</button><p className="text-xs leading-5 text-slate-500">{pull?.message || '拉取完成后会出现在「容器镜像管理」列表中。'}</p></div></Panel>
      <Panel><PanelHeader title="实时构建日志" description={build?.image ? `${build.image} · ${build.state}` : '暂无构建任务'} /><pre className="max-h-[520px] overflow-auto whitespace-pre-wrap p-4 font-mono text-xs leading-5 text-slate-700">{build?.logs?.length ? build.logs.join('\n') : '等待构建任务开始。'}</pre></Panel>
    </div>
  </div>
}
