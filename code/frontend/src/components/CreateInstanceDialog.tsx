import { useCallback, useEffect, useMemo, useState } from 'react'
import { ArrowLeft, ArrowRight, Check, ChevronDown, CircleAlert, LoaderCircle, Server } from 'lucide-react'
import { formatBytes, getLocalImages, isComfyImage, type LocalImage } from '@/api'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { CreateInstanceInput, Instance, ModelItem } from '@/types'

// 真实容器名称需要兼容 Docker 命名规则。
const realNamePattern = /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$/
// 容器内挂载目标必须是绝对路径。
const targetPattern = /^\/[a-zA-Z0-9_./-]{0,127}$/

// 扫描第一个空闲的默认实例名（comfy-01、comfy-02…）。
function suggestName(instances: Instance[]): string {
  const taken = new Set(instances.map((item) => item.name))
  for (let index = 1; index < 100; index++) {
    const candidate = `comfy-${String(index).padStart(2, '0')}`
    if (!taken.has(candidate)) return candidate
  }
  return `comfy-${Date.now() % 1000}`
}

// 扫描第一个空闲的访问端口（8188 起，避开现有实例）。
function suggestPort(instances: Instance[]): number {
  const taken = new Set(instances.map((item) => item.port))
  for (let port = 8188; port <= 65535; port++) {
    if (!taken.has(port)) return port
  }
  return 8188
}

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

export function CreateInstanceDialog({ open, onOpenChange, onSubmit, models, instances, gpuNames, defaultImage, defaultContainerPort, dataDir, defaultModelsTarget, defaultOutputsTarget, defaultWorkflowsTarget }: Props) {
  const [step, setStep] = useState(1)
  const [name, setName] = useState(() => suggestName(instances))
  // 新建实例只允许选择本机 ComfyUI 镜像；构建、拉取和导入在实例页「镜像中心」完成。
  const [localTag, setLocalTag] = useState('')
  const [localImages, setLocalImages] = useState<LocalImage[] | null>(null)
  const [loadingImages, setLoadingImages] = useState(false)
  const [containerPort, setContainerPort] = useState(defaultContainerPort ?? 8188)
  const [gpu, setGpu] = useState(gpuNames.length > 0 ? 0 : -1)
  const [port, setPort] = useState(() => suggestPort(instances))
  const [autoStart, setAutoStart] = useState(true)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  // 高级设置：共享仓库与导出资产的容器内挂载目标（主机侧固定为共享数据目录）。
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [modelsTarget, setModelsTarget] = useState(defaultModelsTarget ?? '/data/models')
  const [outputsTarget, setOutputsTarget] = useState(defaultOutputsTarget ?? '/data/outputs')
  const [workflowsTarget, setWorkflowsTarget] = useState(defaultWorkflowsTarget ?? '/data/workflows')

  const readyModels = models.filter((model) => model.status === 'ready')

  const loadImages = useCallback(() => {
    setLoadingImages(true)
    getLocalImages()
      .then((images) => {
        setLocalImages(images)
        const tags = images.flatMap((item) => item.RepoTags.map((tag) => ({ tag, image: item }))).filter((item) => item.tag && item.tag !== '<none>:<none>' && isComfyImage(item.tag, item.image)).map((item) => item.tag)
        if (tags.length > 0) {
          const preferred = defaultImage && tags.includes(defaultImage) ? defaultImage : tags[0]
          setLocalTag((current) => current && tags.includes(current) ? current : preferred)
        } else {
          setLocalTag('')
        }
      })
      .catch(() => {
        setLocalImages([])
        setLocalTag('')
      })
      .finally(() => setLoadingImages(false))
  }, [defaultImage])

  // 打开向导时加载本机镜像列表。
  useEffect(() => {
    if (!open) return
    loadImages()
  }, [open, loadImages])

  const localTags = useMemo(() => (localImages ?? []).flatMap((item) => item.RepoTags.map((tag) => ({ tag, image: item }))).filter((item) => item.tag && item.tag !== '<none>:<none>' && isComfyImage(item.tag, item.image)).map((item) => item.tag), [localImages])
  const image = localTag

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
      setError('请选择一个本机已有镜像；如暂无镜像，请先到「镜像中心」构建、拉取或导入镜像。')
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
  const stepNames = ['镜像与资源', '确认部署']

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Server className="size-5 text-teal-700" />新建 ComfyUI 实例</DialogTitle>
          <DialogDescription>创建独立 Docker 容器；仅可选择本机已准备好的镜像，构建、拉取和导入请先在实例页「镜像中心」完成。</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2 py-2" aria-label={`当前第 ${step} 步，共 2 步`}>
          {stepNames.map((label, index) => (
            <div key={label} className="flex flex-1 items-center gap-2">
              <span className={`flex size-7 shrink-0 items-center justify-center rounded-full text-xs font-semibold ${step > index + 1 ? 'bg-teal-600 text-white' : step === index + 1 ? 'bg-slate-900 text-white' : 'bg-slate-100 text-slate-500'}`}>{step > index + 1 ? <Check className="size-4" /> : index + 1}</span>
              <span className={`hidden text-xs sm:inline ${step === index + 1 ? 'font-semibold text-slate-900' : 'text-slate-500'}`}>{label}</span>
              {index < stepNames.length - 1 && <div className="h-px flex-1 bg-slate-200" />}
            </div>
          ))}
        </div>

        {step === 1 && <div className="grid gap-4 py-2">
          <label className="field-label">容器镜像（本机已有）
            <select className="field-input font-mono text-sm" value={localTag} disabled={loadingImages || localTags.length === 0} onChange={(event) => setLocalTag(event.target.value)}>
              {localTags.map((tag) => {
                const meta = (localImages ?? []).find((item) => item.RepoTags.includes(tag))
                return <option key={tag} value={tag}>{tag}{meta ? `（${formatBytes(meta.Size)}）` : ''}</option>
              })}
            </select>
          </label>
          <div className={`rounded-lg border p-3 text-sm leading-6 ${localTags.length > 0 ? 'border-teal-200 bg-teal-50 text-teal-950' : 'border-amber-200 bg-amber-50 text-amber-900'}`}>
            {loadingImages ? '正在读取本机镜像列表…' : localTags.length > 0 ? <>将使用本机镜像 <span className="font-mono">{image}</span> 创建实例。</> : '当前本机暂无可选镜像；请先在「镜像中心」构建、拉取或导入镜像。'}
            <button type="button" className="btn-secondary mt-2 w-full" onClick={loadImages} disabled={loadingImages}>刷新本机镜像列表</button>
          </div>
          {gpuNames.length === 0 && <p className="flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm leading-6 text-amber-900"><CircleAlert className="mt-0.5 size-4 shrink-0" />未检测到 GPU（硬件采集接口可能尚未就绪），当前将以「仅 CPU」创建；如需 GPU 实例请先返回首页确认硬件监控正常。</p>}
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

        {step === 2 && <div className="grid gap-4 py-2">
          <p className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm leading-6 text-slate-600">共享模型仓库将自动挂载进实例容器（只读），以下 {readyModels.length} 个已就绪模型对新实例立即可见，无需复制或选择。</p>
          <div className="max-h-40 space-y-2 overflow-y-auto pr-1">
            {readyModels.map((model) => <div key={model.id} className="flex min-h-12 items-center gap-3 rounded-lg border border-slate-200 px-3 py-2">
              <Check className="size-4 shrink-0 text-teal-600" />
              <span className="min-w-0 flex-1"><span className="block truncate text-sm font-medium text-slate-800">{model.name}</span><span className="text-xs text-slate-500">{model.type} · {model.size}</span></span>
            </div>)}
            {readyModels.length === 0 && <p className="rounded-lg border border-dashed border-slate-300 p-4 text-center text-sm text-slate-500">共享仓库暂无模型，可创建后在模型中心下载或导入。</p>}
          </div>
          <dl className="divide-y divide-slate-200 rounded-lg border border-slate-200 px-4">
            {[
              ['实例名称', name],
              ['容器镜像', image],
              ['计算资源', gpu >= 0 ? `GPU ${gpu} · ${gpuNames[gpu] ?? 'GPU'}` : '不分配 GPU（仅 CPU）'],
              ['访问地址', `http://服务器IP:${port} → 容器 ${containerPort}`],
              ['模型仓库挂载', `${hostDataDir}/models → ${modelsTarget}（只读）`],
              ['导出资产路径', `${hostDataDir}/outputs → ${outputsTarget}`],
              ['工作流挂载', `${hostDataDir}/workflows → ${workflowsTarget}`],
              ['开机自启', autoStart ? '启用' : '关闭'],
            ].map(([term, value]) => <div key={term} className="grid grid-cols-[100px_1fr] gap-4 py-3 text-sm"><dt className="text-slate-500">{term}</dt><dd className="break-all font-medium text-slate-800">{value}</dd></div>)}
          </dl>
          <p className="text-xs leading-5 text-slate-500">将通过 Docker 创建并启动容器；失败时自动清理本次创建的资源。</p>
        </div>}

        <DialogFooter>
          {step > 1 && <button type="button" className="btn-secondary" disabled={submitting} onClick={() => setStep(step - 1)}><ArrowLeft className="size-4" />上一步</button>}
          {step === 1 && <button type="button" className="btn-primary" onClick={next}>下一步<ArrowRight className="size-4" /></button>}
          {step === 2 && <button type="button" className="btn-primary" disabled={submitting} onClick={() => void submit()}>{submitting ? <LoaderCircle className="size-4 animate-spin" /> : <Check className="size-4" />}{submitting ? '正在创建容器' : '确认部署'}</button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
