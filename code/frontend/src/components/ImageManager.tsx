import { useCallback, useEffect, useMemo, useState } from 'react'
import { CloudDownload, Cpu, Download, FileText, Hammer, Layers, LoaderCircle, RefreshCw, ScrollText, Tag, Trash2, Upload } from 'lucide-react'
import { toast } from 'sonner'
import { buildScriptURL, deleteLocalImageApi, exportLocalImageURL, formatBytes, getComfyUIRefs, getImageBuildStatus, getImageCatalog, getImagePullStatus, getLocalImages, imageBuildSummary, importLocalImageApi, isComfyImage, startImageBuild, startImagePull, tagLocalImageApi, type ComfyRef, type ImageBuildSpec, type ImageBuildStatus, type ImagePullStatus, type ImageRecipe, type LocalImage } from '@/api'
import { EmptyState, Panel, PanelHeader } from '@/components/common'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

interface Props {
  mode: 'manage' | 'build'
}

interface ImageTagRow {
  tag: string
  image?: LocalImage
  virtual?: boolean
}

const platformOptions = [
  { value: 'host', label: '宿主机架构', description: '在当前机器本机架构构建，适合快速验证。' },
  { value: 'linux/arm64', label: 'linux/arm64（aarch64）', description: '适合 DGX Spark / ARM 集群节点分发。' },
  { value: 'linux/amd64', label: 'linux/amd64（x86_64）', description: '适合通用 x86_64 GPU 服务器。' },
  { value: 'linux/arm/v7', label: 'linux/arm/v7', description: '预留给边缘 ARMv7 节点，需宿主机支持跨架构构建。' },
]

export function ImageManager({ mode }: Props) {
  const [images, setImages] = useState<LocalImage[]>([])
  const [recipes, setRecipes] = useState<ImageRecipe[]>([])
  const [refs, setRefs] = useState<ComfyRef[]>([{ name: 'master', type: 'branch' }])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [pullImage, setPullImage] = useState('')
  const [pull, setPull] = useState<ImagePullStatus | null>(null)
  const [recipeId, setRecipeId] = useState('nvidia-cu130')
  const [imageName, setImageName] = useState('flowpilot/comfyui:cu130')
  const [ref, setRef] = useState('master')
  const [pip, setPip] = useState('tsinghua')
  const [manager, setManager] = useState(true)
  const [platform, setPlatform] = useState('host')
  const [build, setBuild] = useState<ImageBuildStatus | null>(null)
  const [buildOpen, setBuildOpen] = useState(false)
  const [pullOpen, setPullOpen] = useState(false)
  const [logsOpen, setLogsOpen] = useState(false)

  const tagRows = useMemo<ImageTagRow[]>(() => images.flatMap((image) => image.RepoTags.map((tag) => ({ tag, image }))).filter((item) => item.tag && item.tag !== '<none>:<none>'), [images])
  const comfyRowsBase = tagRows.filter((item) => isComfyImage(item.tag, item.image))
  const otherRows = tagRows.filter((item) => !isComfyImage(item.tag, item.image))
  const buildRowVisible = build?.image && build.state === 'building' && !comfyRowsBase.some((item) => item.tag === build.image)
  const comfyRows = buildRowVisible ? [{ tag: build.image, virtual: true }, ...comfyRowsBase] : comfyRowsBase
  const selectedRecipe = recipes.find((item) => item.id === recipeId)
  const selectedPlatform = platformOptions.find((item) => item.value === platform) ?? platformOptions[0]

  const currentBuildSpec = useMemo<ImageBuildSpec>(() => ({ variant: recipeId, image: imageName.trim() || selectedRecipe?.image || 'flowpilot/comfyui:local', ref: ref.trim() || 'master', pip, manager: manager ? '1' : '0', platform }), [imageName, manager, pip, platform, recipeId, ref, selectedRecipe?.image])

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
      getComfyUIRefs().then(setRefs).catch(() => setRefs([{ name: 'master', type: 'branch' }])),
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
      setPullOpen(false)
      toast.success('已开始拉取镜像', { description: pullImage.trim() })
    } catch (error) {
      toast.error('启动拉取失败', { description: error instanceof Error ? error.message : '请稍后重试。' })
    }
  }

  async function startBuild() {
    if (!currentBuildSpec.image.trim()) {
      toast.error('请输入构建后的镜像名')
      return
    }
    try {
      const status = await startImageBuild(currentBuildSpec)
      setBuild(status)
      setBuildOpen(false)
      setLogsOpen(true)
      toast.success('已开始构建镜像', { description: currentBuildSpec.image })
    } catch (error) {
      toast.error('启动构建失败', { description: error instanceof Error ? error.message : '请稍后重试。' })
    }
  }

  function rowSummary(row: ImageTagRow) {
    if (build?.image === row.tag && build.summary?.length) return build.summary
    return imageBuildSummary(row.image)
  }

  function renderImageTable(title: string, description: string, rows: ImageTagRow[], emptyTitle: string) {
    return <Panel>
      <PanelHeader title={title} description={description} action={<button type="button" className="btn-secondary" onClick={refresh} disabled={loading}><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />刷新</button>} />
      <div className="overflow-x-auto"><table className="data-table min-w-[1080px]"><thead><tr><th>镜像标签</th><th>镜像简述参数</th><th>构建进度</th><th>镜像 ID</th><th>大小</th><th>创建时间</th><th>操作</th></tr></thead><tbody>
        {rows.map((row) => {
          const active = build?.image === row.tag && build.state !== 'idle'
          const summary = rowSummary(row)
          return <tr key={`${row.image?.Id ?? 'building'}-${row.tag}`}>
            <td className="font-mono text-xs">{row.tag}{row.virtual && <span className="ml-2 rounded-full bg-amber-100 px-2 py-0.5 text-[11px] font-medium text-amber-800">构建中</span>}</td>
            <td>{summary.length > 0 ? <div className="flex max-w-[320px] flex-wrap gap-1">{summary.slice(0, 4).map((item) => <span key={`${row.tag}-${item.label}`} className="rounded-full bg-slate-100 px-2 py-1 text-[11px] text-slate-600">{item.label}：{item.value}</span>)}</div> : <span className="text-xs text-slate-400">未记录</span>}</td>
            <td>{active ? <div className="min-w-28"><div className="h-2 overflow-hidden rounded-full bg-slate-100"><div className={`h-full ${build.state === 'error' ? 'bg-rose-500' : 'bg-teal-600'}`} style={{ width: `${Math.max(1, Math.min(100, build.progress || 1))}%` }} /></div><span className="mt-1 block text-xs text-slate-500">{build.progress || 1}% · {build.state}</span></div> : <span className="text-xs text-slate-400">—</span>}</td>
            <td className="font-mono text-xs text-slate-500">{row.image?.Id.slice(0, 20) ?? '等待 Docker 生成'}</td>
            <td>{row.image ? formatBytes(row.image.Size) : '—'}</td>
            <td>{row.image ? new Date(row.image.Created * 1000).toLocaleString('zh-CN', { hour12: false }) : '—'}</td>
            <td><div className="flex items-center gap-1"><button type="button" className="icon-button" title="构建日志" disabled={!active && !(build?.logs?.length && build.image === row.tag)} onClick={() => setLogsOpen(true)}><ScrollText className="size-4" /></button>{row.image && <button type="button" className="icon-button" title="改标签" disabled={busy} onClick={() => void retag(row.tag)}><Tag className="size-4" /></button>}{row.image && <button type="button" className="icon-button" title="导出 tar" onClick={() => window.open(exportLocalImageURL(row.tag), '_blank', 'noopener')}><Download className="size-4" /></button>}{row.image && <button type="button" className="icon-button text-rose-600 hover:bg-rose-50" title="删除镜像" disabled={busy} onClick={() => void remove(row.tag)}><Trash2 className="size-4" /></button>}</div></td>
          </tr>
        })}
      </tbody></table></div>
      {rows.length === 0 && <EmptyState title={emptyTitle} description="可通过导入 tar、在线拉取或手动构建来准备镜像。" />}
    </Panel>
  }

  const dialogs = <>
    <Dialog open={buildOpen} onOpenChange={setBuildOpen}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Hammer className="size-5 text-teal-700" />一键构建 ComfyUI 镜像</DialogTitle>
          <DialogDescription>选择 CPU 架构、GPU/PyTorch 配方、ComfyUI 版本和依赖源；平台会生成同参数构建脚本并记录到镜像简述参数。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid gap-4 sm:grid-cols-2"><label className="field-label">CPU 芯片架构<select className="field-input" value={platform} onChange={(event) => setPlatform(event.target.value)}>{platformOptions.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select></label><label className="field-label">GPU / PyTorch 配方<select className="field-input" value={recipeId} onChange={(event) => onRecipeChange(event.target.value)}>{recipes.map((recipe) => <option key={recipe.id} value={recipe.id}>{recipe.label}</option>)}</select></label></div>
          <div className="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm leading-6 text-slate-600"><p><Cpu className="mr-1 inline size-4" />{selectedPlatform.description}</p>{selectedRecipe && <p><Layers className="mr-1 inline size-4" />{selectedRecipe.description}；建议镜像名 <span className="font-mono">{selectedRecipe.image}</span>。</p>}</div>
          <div className="grid gap-4 sm:grid-cols-2"><label className="field-label">镜像名<input className="field-input font-mono text-sm" value={imageName} onChange={(event) => setImageName(event.target.value)} placeholder="flowpilot/comfyui:cu130" /></label><label className="field-label">ComfyUI 在线版本<select className="field-input font-mono text-sm" value={ref} onChange={(event) => setRef(event.target.value)}>{refs.map((item) => <option key={`${item.type}-${item.name}`} value={item.name}>{item.name} · {item.type === 'branch' ? '分支' : '标签'}</option>)}</select></label></div>
          <div className="grid gap-4 sm:grid-cols-2"><label className="field-label">ComfyUI 版本手动修正<input className="field-input font-mono text-sm" value={ref} onChange={(event) => setRef(event.target.value)} placeholder="master 或标签" /></label><label className="field-label">pip 源<select className="field-input" value={pip} onChange={(event) => setPip(event.target.value)}><option value="tsinghua">清华镜像</option><option value="aliyun">阿里云镜像</option><option value="pypi">PyPI 官方</option></select></label></div>
          <label className="flex min-h-11 cursor-pointer items-center gap-2 text-sm text-slate-700"><input type="checkbox" className="size-4 accent-teal-700" checked={manager} onChange={(event) => setManager(event.target.checked)} />内置 ComfyUI-Manager</label>
          <div className="rounded-lg border border-slate-200 p-3 text-xs leading-6 text-slate-500"><p className="font-semibold text-slate-700">参数简述</p><p>CPU 架构：{selectedPlatform.label}</p><p>GPU 配方：{selectedRecipe?.label ?? recipeId}</p><p>ComfyUI 版本：{currentBuildSpec.ref}</p><p>pip 源：{pip}；Manager：{manager ? '内置' : '不内置'}</p></div>
        </div>
        <DialogFooter>
          <a className="btn-secondary" href={buildScriptURL(currentBuildSpec)} download="build-comfyui-image.sh"><FileText className="size-4" />下载同参数构建脚本</a>
          <button type="button" className="btn-primary" disabled={build?.state === 'building'} onClick={() => void startBuild()}>{build?.state === 'building' ? <LoaderCircle className="size-4 animate-spin" /> : <Hammer className="size-4" />}{build?.state === 'building' ? '正在构建' : '开始构建'}</button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog open={pullOpen} onOpenChange={setPullOpen}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><CloudDownload className="size-5 text-teal-700" />拉取在线容器镜像</DialogTitle>
          <DialogDescription>输入 Docker Hub、私有仓库或镜像代理中的完整镜像名；拉取完成后会自动进入本机镜像清单。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4"><label className="field-label">在线镜像名<input className="field-input font-mono text-sm" value={pullImage} onChange={(event) => setPullImage(event.target.value)} placeholder="ghcr.io/xxx/comfyui:latest" /></label><p className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs leading-5 text-slate-500">建议拉取标签中包含 comfyui 的镜像；非 ComfyUI 镜像会进入「其他机器镜像」，不会出现在新建实例候选中。</p></div>
        <DialogFooter><button type="button" className="btn-primary" disabled={pull?.state === 'pulling'} onClick={() => void startPull()}>{pull?.state === 'pulling' ? <LoaderCircle className="size-4 animate-spin" /> : <CloudDownload className="size-4" />}{pull?.state === 'pulling' ? '正在拉取' : '开始拉取'}</button></DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog open={logsOpen} onOpenChange={setLogsOpen}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader><DialogTitle className="flex items-center gap-2"><ScrollText className="size-5 text-teal-700" />实时构建日志</DialogTitle><DialogDescription>{build?.image ? `${build.image} · ${build.state} · ${build.progress || 0}%` : '暂无构建任务'}</DialogDescription></DialogHeader>
        <pre className="max-h-[65dvh] overflow-auto rounded-lg bg-slate-950 p-4 font-mono text-xs leading-5 whitespace-pre-wrap text-slate-100">{build?.logs?.length ? build.logs.join('\n') : '等待构建任务开始。'}</pre>
      </DialogContent>
    </Dialog>
  </>

  if (mode === 'manage') return <div className="grid gap-5">
    <Panel>
      <PanelHeader title="容器镜像管理" description="镜像分为 ComfyUI 镜像与机器其他镜像；新建实例只会从 ComfyUI 镜像中选择。" action={<div className="flex flex-wrap gap-2"><label className={`btn-secondary ${busy ? 'pointer-events-none opacity-60' : ''}`}><Upload className="size-4" />导入 tar<input type="file" accept=".tar,application/x-tar" className="sr-only" disabled={busy} onChange={(event) => void importImage(event.target.files?.[0])} /></label><button type="button" className="btn-secondary" onClick={refresh} disabled={loading}><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />刷新</button></div>} />
    </Panel>
    {renderImageTable('ComfyUI 镜像', '仅此分类会出现在新建实例的镜像选择框中。', comfyRows, '暂无 ComfyUI 镜像')}
    {renderImageTable('机器其他镜像', '用于环境排查和维护，不会作为 ComfyUI 实例候选。', otherRows, '暂无其他镜像')}
    {dialogs}
  </div>

  return <div className="grid gap-5">
    <Panel>
      <PanelHeader title="手动构建 / 在线拉取" description="构建和拉取已拆为两个弹窗，提交后在 ComfyUI 镜像清单中查看进度、参数简述和实时日志。" />
      <div className="grid gap-4 p-5 md:grid-cols-2">
        <button type="button" className="rounded-xl border border-slate-200 p-5 text-left transition-colors hover:border-teal-300 hover:bg-teal-50" onClick={() => setBuildOpen(true)}><Hammer className="mb-3 size-6 text-teal-700" /><span className="block font-semibold text-slate-900">一键构建 ComfyUI 镜像</span><span className="mt-2 block text-sm leading-6 text-slate-500">选择 CPU 架构、GPU 配方、ComfyUI 版本、pip 源和 Manager 开关，并可下载同参数构建脚本。</span></button>
        <button type="button" className="rounded-xl border border-slate-200 p-5 text-left transition-colors hover:border-teal-300 hover:bg-teal-50" onClick={() => setPullOpen(true)}><CloudDownload className="mb-3 size-6 text-teal-700" /><span className="block font-semibold text-slate-900">拉取在线容器镜像</span><span className="mt-2 block text-sm leading-6 text-slate-500">输入远程镜像名后台拉取；拉取完成后按 ComfyUI/其他镜像自动归类。</span></button>
      </div>
      {(build?.message || pull?.message) && <div className="border-t border-slate-200 p-5 text-sm leading-6 text-slate-600">{build?.message && <p>构建：{build.message}</p>}{pull?.message && <p>拉取：{pull.message}</p>}</div>}
    </Panel>
    {renderImageTable('ComfyUI 镜像清单', '构建任务会以虚拟行进入清单，展示百分比、参数简述和日志入口。', comfyRows, '暂无 ComfyUI 镜像')}
    {dialogs}
  </div>
}
