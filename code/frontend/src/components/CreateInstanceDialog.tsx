import { useEffect, useMemo, useState } from 'react'
import { ArrowLeft, ArrowRight, Check, ChevronDown, CloudDownload, Download, LoaderCircle, Server } from 'lucide-react'
import { checkImage, formatBytes, getImageCatalog, getImagePullStatus, getLocalImages, startImagePull, type ImagePullStatus, type ImageRecipe, type LocalImage } from '@/api'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { CreateInstanceInput, Instance, ModelItem } from '@/types'

// 真实容器名称需要兼容 Docker 命名规则。
const realNamePattern = /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$/
// 容器内挂载目标必须是绝对路径。
const targetPattern = /^\/[a-zA-Z0-9_./-]{0,127}$/

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (input: CreateInstanceInput) => Promise<boolean>
  models: ModelItem[]
  instances: Instance[]
  gpuNames: string[]
  defaultImage?: string
  defaultContainerPort?: number
  dataDir?: string
  defaultModelsTarget?: string
  defaultOutputsTarget?: string
  defaultWorkflowsTarget?: string
}

type ImageMode = 'local' | 'recipe' | 'custom'

export function CreateInstanceDialog({ open, onOpenChange, onSubmit, models, instances, gpuNames, defaultImage, defaultContainerPort, dataDir, defaultModelsTarget, defaultOutputsTarget, defaultWorkflowsTarget }: Props) {
  const [step, setStep] = useState(1)
  const [name, setName] = useState(`comfy-${String(instances.length + 1).padStart(2, '0')}`)
  // 镜像选择：本机已有镜像 / 预置构建配方 / 手动输入。
  const [imageMode, setImageMode] = useState<ImageMode>('local')
  const [localTag, setLocalTag] = useState('')
  const [recipeId, setRecipeId] = useState('')
  const [customImage, setCustomImage] = useState(defaultImage ?? 'comfyui:latest')
  const [localImages, setLocalImages] = useState<LocalImage[] | null>(null)
  const [recipes, setRecipes] = useState<ImageRecipe[]>([])
  const [containerPort, setContainerPort] = useState(defaultContainerPort ?? 8188)
  const [gpu, setGpu] = useState(gpuNames.length > 0 ? 0 : -1)
  const [port, setPort] = useState(8188 + instances.length)
  const [autoStart, setAutoStart] = useState(true)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  // 高级设置：共享仓库与导出资产的容器内挂载目标（主机侧固定为共享数据目录）。
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [modelsTarget, setModelsTarget] = useState(defaultModelsTarget ?? '/data/models')
  const [outputsTarget, setOutputsTarget] = useState(defaultOutputsTarget ?? '/data/outputs')
  const [workflowsTarget, setWorkflowsTarget] = useState(defaultWorkflowsTarget ?? '/data/workflows')
  // 镜像本地存在性检查与后台拉取状态。
  const [imageState, setImageState] = useState<'unknown' | 'checking' | 'exists' | 'missing'>('unknown')
  const [pull, setPull] = useState<ImagePullStatus | null>(null)
  // 一键安装脚本向导参数（镜像缺失时可见）。
  const [installRef, setInstallRef] = useState('master')
  const [installPip, setInstallPip] = useState('tsinghua')
  const [installManager, setInstallManager] = useState(true)

  const readyModels = models.filter((model) => model.status === 'ready')

  // 打开向导时加载本机镜像列表与预置配方目录。
  useEffect(() => {
    if (!open) return
    getLocalImages()
      .then((images) => {
        setLocalImages(images)
        const tags = images.flatMap((item) => item.RepoTags).filter((tag) => tag && tag !== '<none>:<none>')
        if (tags.length > 0) {
          const preferred = defaultImage && tags.includes(defaultImage) ? defaultImage : tags[0]
          setLocalTag(preferred)
        } else {
          setImageMode('recipe')
        }
      })
      .catch(() => setLocalImages([]))
    getImageCatalog()
      .then((list) => {
        setRecipes(list)
        if (list.length > 0) setRecipeId((current) => current || list[0].id)
      })
      .catch(() => {})
  }, [open, defaultImage])

  const localTags = useMemo(() => (localImages ?? []).flatMap((item) => item.RepoTags).filter((tag) => tag && tag !== '<none>:<none>'), [localImages])
  const selectedRecipe = recipes.find((item) => item.id === recipeId)
  const image = imageMode === 'local' ? localTag : imageMode === 'recipe' ? selectedRecipe?.image ?? '' : customImage

  // 镜像名输入后延迟检查本地是否存在。
  useEffect(() => {
    if (!image.trim()) {
      setImageState('unknown')
      return
    }
    setImageState('checking')
    const timer = window.setTimeout(() => {
      checkImage(image.trim())
        .then((result) => setImageState(result.exists ? 'exists' : 'missing'))
        .catch(() => setImageState('unknown'))
    }, 500)
    return () => window.clearTimeout(timer)
  }, [image])

  // 拉取进行中每 2 秒轮询进度。
  useEffect(() => {
    if (pull?.state !== 'pulling') return
    const timer = window.setInterval(() => {
      getImagePullStatus()
        .then((status) => {
          setPull(status)
          if (status.state === 'done') setImageState('exists')
        })
        .catch(() => {})
    }, 2000)
    return () => window.clearInterval(timer)
  }, [pull])

  function pullImage() {
    startImagePull(image.trim())
      .then(setPull)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : '启动拉取失败'))
  }

  function downloadInstallScript() {
    // 配方模式直接固化对应显卡配方；其余由目标机器自动检测显卡。
    const variant = imageMode === 'recipe' && selectedRecipe ? selectedRecipe.id : 'auto'
    const params = new URLSearchParams({ variant, ref: installRef.trim() || 'master', pip: installPip, manager: installManager ? '1' : '0' })
    if (image.trim()) params.set('image', image.trim())
    window.open(`/api/v1/images/install-script?${params.toString()}`, '_blank', 'noopener')
  }

  function next() {
    if (!name.trim()) {
      setError('请输入实例名称。')
      return
    }
    if (!realNamePattern.test(name.trim())) {
      setError('实例名称需以字母或数字开头，仅含字母、数字、下划线和连字符。')
      return
    }
    if (!image.trim()) {
      setError('请选择或输入容器镜像。')
      return
    }
    if (instances.some((item) => item.name === name.trim())) {
      setError('实例名称已存在，请更换名称。')
      return
    }
    if (instances.some((item) => item.port === port)) {
      setError('端口已被其他实例占用，请更换端口。')
      return
    }
    if (gpu >= 0 && instances.some((item) => item.gpu === gpu && ['running', 'starting', 'creating'].includes(item.status))) {
      setError(`GPU ${gpu} 正在被运行实例使用，请选择另一块 GPU。`)
      return
    }
    for (const [label, target] of [['模型挂载目标', modelsTarget], ['输出挂载目标', outputsTarget], ['工作流挂载目标', workflowsTarget]] as const) {
      if (!targetPattern.test(target) || target.includes('..')) {
        setError(`${label}需为合法绝对路径，如 /data/models。`)
        return
      }
    }
    setError('')
    setStep(2)
  }

  async function submit() {
    setSubmitting(true)
    try {
      const ok = await onSubmit({ name: name.trim(), gpu, port, autoStart, image: image.trim(), containerPort, modelsTarget: modelsTarget.trim(), outputsTarget: outputsTarget.trim(), workflowsTarget: workflowsTarget.trim() })
      if (ok) onOpenChange(false)
    } finally {
      setSubmitting(false)
    }
  }

  const hostDataDir = dataDir ?? '/data/flowpilot'

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Server className="size-5 text-teal-700" />新建 ComfyUI 实例</DialogTitle>
          <DialogDescription>创建独立 Docker 容器，共享模型仓库自动挂载，业务数据持久化保存。</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2 py-2" aria-label={`当前第 ${step} 步，共 3 步`}>
          {['镜像与资源', '模型可见性', '确认部署'].map((label, index) => (
            <div key={label} className="flex flex-1 items-center gap-2">
              <span className={`flex size-7 shrink-0 items-center justify-center rounded-full text-xs font-semibold ${step > index + 1 ? 'bg-teal-600 text-white' : step === index + 1 ? 'bg-slate-900 text-white' : 'bg-slate-100 text-slate-500'}`}>{step > index + 1 ? <Check className="size-4" /> : index + 1}</span>
              <span className={`hidden text-xs sm:inline ${step === index + 1 ? 'font-semibold text-slate-900' : 'text-slate-500'}`}>{label}</span>
              {index < 2 && <div className="h-px flex-1 bg-slate-200" />}
            </div>
          ))}
        </div>

        {step === 1 && <div className="grid gap-4 py-2">
          <label className="field-label">镜像来源
            <select className="field-input" value={imageMode} onChange={(event) => setImageMode(event.target.value as ImageMode)}>
              <option value="local" disabled={localTags.length === 0}>本机已有镜像{localTags.length === 0 ? '（暂无）' : `（${localTags.length} 个）`}</option>
              <option value="recipe">预置构建配方（按显卡选择）</option>
              <option value="custom">手动输入镜像名</option>
            </select>
          </label>
          {imageMode === 'local' && <label className="field-label">容器镜像
            <select className="field-input font-mono text-sm" value={localTag} onChange={(event) => setLocalTag(event.target.value)}>
              {localTags.map((tag) => {
                const meta = (localImages ?? []).find((item) => item.RepoTags.includes(tag))
                return <option key={tag} value={tag}>{tag}{meta ? `（${formatBytes(meta.Size)}）` : ''}</option>
              })}
            </select>
          </label>}
          {imageMode === 'recipe' && <div className="grid gap-2">
            <label className="field-label">显卡配方
              <select className="field-input" value={recipeId} onChange={(event) => setRecipeId(event.target.value)}>
                {recipes.map((recipe) => <option key={recipe.id} value={recipe.id}>{recipe.label}</option>)}
              </select>
            </label>
            {selectedRecipe && <p className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm leading-6 text-slate-600">{selectedRecipe.description}；构建后本地镜像名为 <span className="font-mono">{selectedRecipe.image}</span>。</p>}
          </div>}
          {imageMode === 'custom' && <label className="field-label">容器镜像
            <input className="field-input font-mono text-sm" value={customImage} onChange={(event) => setCustomImage(event.target.value)} placeholder="comfyui:latest" autoComplete="off" />
          </label>}
          <div className={`rounded-lg border p-3 text-sm leading-6 ${imageState === 'exists' ? 'border-teal-200 bg-teal-50 text-teal-950' : imageState === 'missing' ? 'border-amber-200 bg-amber-50 text-amber-900' : 'border-slate-200 bg-slate-50 text-slate-600'}`} aria-live="polite">
            {imageState === 'checking' && '正在检查本机镜像…'}
            {imageState === 'exists' && <>镜像 <span className="font-mono">{image.trim()}</span> 已在本机，可直接创建。</>}
            {imageState === 'missing' && <>镜像 <span className="font-mono">{image.trim()}</span> 不在本机，可在线拉取，或用一键安装脚本在本机构建。</>}
            {imageState === 'unknown' && '将创建真实 Docker 容器；镜像必须已存在于本机，离线环境请先导入或用安装脚本构建。'}
            {imageState === 'missing' && pull?.state !== 'pulling' && <div className="mt-2 grid gap-2">
              <button type="button" className="btn-secondary w-full" onClick={pullImage}><CloudDownload className="size-4" />在线拉取镜像</button>
              <details className="rounded-lg border border-amber-200 bg-white/60 p-3">
                <summary className="cursor-pointer text-sm font-medium text-amber-900">一键安装脚本参数（纯净机器适用）</summary>
                <div className="mt-3 grid gap-3 sm:grid-cols-3">
                  <label className="field-label">ComfyUI 版本
                    <input className="field-input font-mono text-sm" value={installRef} onChange={(event) => setInstallRef(event.target.value)} placeholder="master 或 v0.3.x" autoComplete="off" />
                  </label>
                  <label className="field-label">pip 源
                    <select className="field-input" value={installPip} onChange={(event) => setInstallPip(event.target.value)}>
                      <option value="tsinghua">清华镜像</option>
                      <option value="aliyun">阿里云镜像</option>
                      <option value="pypi">PyPI 官方</option>
                    </select>
                  </label>
                  <label className="flex min-h-11 cursor-pointer items-end gap-2 pb-2 text-sm text-slate-700"><input type="checkbox" className="size-4 accent-teal-700" checked={installManager} onChange={(event) => setInstallManager(event.target.checked)} />内置 ComfyUI-Manager</label>
                </div>
                <button type="button" className="btn-secondary mt-3 w-full" onClick={downloadInstallScript}><Download className="size-4" />下载一键安装脚本（自动识别 NVIDIA / AMD / CPU）</button>
                <p className="mt-2 text-xs leading-5 text-slate-500">将脚本拷到目标服务器执行 <span className="font-mono">bash install-comfyui-image.sh</span>：自动安装 Docker、按显卡选择 PyTorch 轮子并构建镜像；模型由模型中心统一分发，脚本不下载模型。</p>
              </details>
            </div>}
            {pull?.state === 'pulling' && <span className="mt-1 flex items-center gap-2 text-xs"><LoaderCircle className="size-3.5 animate-spin" />{pull.message || '正在拉取镜像…'}</span>}
            {pull?.state === 'error' && <span className="mt-1 block text-xs text-rose-700">拉取失败：{pull.message}</span>}
          </div>
          <label className="field-label">实例名称
            <input className="field-input" value={name} onChange={(event) => setName(event.target.value)} maxLength={32} autoComplete="off" />
          </label>
          <div className="grid gap-4 sm:grid-cols-2">
            <label className="field-label">分配 GPU
              <select className="field-input" value={gpu} onChange={(event) => setGpu(Number(event.target.value))}>
                {gpuNames.map((gpuName, index) => <option key={index} value={index}>GPU {index} · {gpuName}</option>)}
                <option value={-1}>不分配 GPU（仅 CPU）</option>
              </select>
            </label>
            <label className="field-label">访问端口
              <input className="field-input" type="number" min={1024} max={65535} value={port} onChange={(event) => setPort(Number(event.target.value))} />
            </label>
          </div>
          <label className="flex min-h-11 cursor-pointer items-center gap-3 text-sm text-slate-700"><input type="checkbox" className="size-4 accent-teal-700" checked={autoStart} onChange={(event) => setAutoStart(event.target.checked)} />服务器重启后自动启动</label>

          <button type="button" className="flex items-center gap-1 text-sm font-medium text-slate-600 hover:text-slate-900" onClick={() => setShowAdvanced(!showAdvanced)} aria-expanded={showAdvanced}>
            <ChevronDown className={`size-4 transition-transform ${showAdvanced ? '' : '-rotate-90'}`} />高级设置（挂载路径与容器端口）
          </button>
          {showAdvanced && <div className="grid gap-4 rounded-lg border border-slate-200 bg-slate-50 p-4">
            <p className="text-xs leading-5 text-slate-500">共享数据目录固定在宿主机 <span className="font-mono">{hostDataDir}</span> 下；这里设置的是各目录在实例容器内的挂载目标，需与镜像内 ComfyUI 布局匹配。导出资产（生成结果）保存到宿主机 <span className="font-mono">{hostDataDir}/outputs</span>。</p>
            <div className="grid gap-4 sm:grid-cols-2">
              <label className="field-label">模型仓库挂载目标（只读）
                <input className="field-input font-mono text-sm" value={modelsTarget} onChange={(event) => setModelsTarget(event.target.value)} placeholder="/data/models" autoComplete="off" />
              </label>
              <label className="field-label">输出目录挂载目标（导出资产）
                <input className="field-input font-mono text-sm" value={outputsTarget} onChange={(event) => setOutputsTarget(event.target.value)} placeholder="/data/outputs" autoComplete="off" />
              </label>
              <label className="field-label">工作流挂载目标
                <input className="field-input font-mono text-sm" value={workflowsTarget} onChange={(event) => setWorkflowsTarget(event.target.value)} placeholder="/data/workflows" autoComplete="off" />
              </label>
              <label className="field-label">容器端口
                <input className="field-input" type="number" min={1} max={65535} value={containerPort} onChange={(event) => setContainerPort(Number(event.target.value))} />
              </label>
            </div>
          </div>}
          {error && <p role="alert" className="rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700">{error}</p>}
        </div>}

        {step === 2 && <div className="py-2">
          <p className="mb-3 rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm leading-6 text-slate-600">共享模型仓库将自动挂载进实例容器（只读），以下 {readyModels.length} 个已就绪模型对新实例立即可见，无需复制或选择。</p>
          <div className="max-h-72 space-y-2 overflow-y-auto pr-1">
            {readyModels.map((model) => <div key={model.id} className="flex min-h-14 items-center gap-3 rounded-lg border border-slate-200 px-3 py-2">
              <Check className="size-4 shrink-0 text-teal-600" />
              <span className="min-w-0 flex-1"><span className="block truncate text-sm font-medium text-slate-800">{model.name}</span><span className="text-xs text-slate-500">{model.type} · {model.size}</span></span>
            </div>)}
            {readyModels.length === 0 && <p className="rounded-lg border border-dashed border-slate-300 p-6 text-center text-sm text-slate-500">共享仓库暂无模型，可创建后在模型中心下载或导入。</p>}
          </div>
        </div>}

        {step === 3 && <div className="py-2">
          <dl className="divide-y divide-slate-200 rounded-lg border border-slate-200 px-4">
            {[
              ['实例名称', name],
              ['容器镜像', image],
              ['计算资源', gpu >= 0 ? `GPU ${gpu} · ${gpuNames[gpu] ?? 'GPU'}` : '不分配 GPU（仅 CPU）'],
              ['访问地址', `http://服务器IP:${port} → 容器 ${containerPort}`],
              ['共享模型', `${readyModels.length} 个已就绪模型自动可见`],
              ['模型仓库挂载', `${hostDataDir}/models → ${modelsTarget}（只读）`],
              ['导出资产路径', `${hostDataDir}/outputs → ${outputsTarget}`],
              ['工作流挂载', `${hostDataDir}/workflows → ${workflowsTarget}`],
              ['开机自启', autoStart ? '启用' : '关闭'],
            ].map(([term, value]) => <div key={term} className="grid grid-cols-[100px_1fr] gap-4 py-3 text-sm"><dt className="text-slate-500">{term}</dt><dd className="break-all font-medium text-slate-800">{value}</dd></div>)}
          </dl>
          <p className="mt-4 text-xs leading-5 text-slate-500">将通过 Docker API 真实创建并启动容器，失败会自动回滚清理。</p>
        </div>}

        <DialogFooter>
          {step > 1 && <button type="button" className="btn-secondary" disabled={submitting} onClick={() => setStep(step - 1)}><ArrowLeft className="size-4" />上一步</button>}
          {step === 1 && <button type="button" className="btn-primary" onClick={next}>下一步<ArrowRight className="size-4" /></button>}
          {step === 2 && <button type="button" className="btn-primary" onClick={() => setStep(3)}>下一步<ArrowRight className="size-4" /></button>}
          {step === 3 && <button type="button" className="btn-primary" disabled={submitting} onClick={() => void submit()}>{submitting ? <LoaderCircle className="size-4 animate-spin" /> : <Check className="size-4" />}{submitting ? '正在创建容器' : '确认部署'}</button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
