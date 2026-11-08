import { useEffect, useState } from 'react'
import { ArrowLeft, ArrowRight, Check, CloudDownload, LoaderCircle, Server } from 'lucide-react'
import { checkImage, getImagePullStatus, startImagePull, type ImagePullStatus } from '@/api'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { CreateInstanceInput, Instance, ModelItem } from '@/types'

// 真实容器名称需要兼容 Docker 命名规则。
const realNamePattern = /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$/

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (input: CreateInstanceInput) => Promise<boolean>
  models: ModelItem[]
  instances: Instance[]
  gpuNames: string[]
  defaultImage?: string
  defaultContainerPort?: number
}

export function CreateInstanceDialog({ open, onOpenChange, onSubmit, models, instances, gpuNames, defaultImage, defaultContainerPort }: Props) {
  const [step, setStep] = useState(1)
  const [name, setName] = useState(`comfy-${String(instances.length + 1).padStart(2, '0')}`)
  const [image, setImage] = useState(defaultImage ?? 'comfyui:latest')
  const [containerPort, setContainerPort] = useState(defaultContainerPort ?? 8188)
  const [gpu, setGpu] = useState(0)
  const [port, setPort] = useState(8188 + instances.length)
  const [autoStart, setAutoStart] = useState(true)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  // 镜像本地存在性检查与后台拉取状态。
  const [imageState, setImageState] = useState<'unknown' | 'checking' | 'exists' | 'missing'>('unknown')
  const [pull, setPull] = useState<ImagePullStatus | null>(null)

  const readyModels = models.filter((model) => model.status === 'ready')

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

  function next() {
    if (!name.trim()) {
      setError('请输入实例名称。')
      return
    }
    if (!realNamePattern.test(name.trim())) {
      setError('实例名称需以字母或数字开头，仅含字母、数字、下划线和连字符。')
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
    setError('')
    setStep(2)
  }

  async function submit() {
    setSubmitting(true)
    try {
      const ok = await onSubmit({ name: name.trim(), gpu, port, autoStart, image: image.trim(), containerPort })
      if (ok) onOpenChange(false)
    } finally {
      setSubmitting(false)
    }
  }

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
          <label className="field-label">容器镜像
            <input className="field-input font-mono text-sm" value={image} onChange={(event) => setImage(event.target.value)} placeholder="comfyui:latest" autoComplete="off" />
          </label>
          <div className={`rounded-lg border p-3 text-sm leading-6 ${imageState === 'exists' ? 'border-teal-200 bg-teal-50 text-teal-950' : imageState === 'missing' ? 'border-amber-200 bg-amber-50 text-amber-900' : 'border-slate-200 bg-slate-50 text-slate-600'}`} aria-live="polite">
            {imageState === 'checking' && '正在检查本机镜像…'}
            {imageState === 'exists' && <>镜像 <span className="font-mono">{image.trim()}</span> 已在本机，可直接创建。</>}
            {imageState === 'missing' && <>镜像 <span className="font-mono">{image.trim()}</span> 不在本机，可在线拉取或离线导入。</>}
            {imageState === 'unknown' && '将创建真实 Docker 容器；镜像必须已存在于本机，离线环境请先导入。'}
            {imageState === 'missing' && pull?.state !== 'pulling' && <button type="button" className="btn-secondary mt-2 w-full" onClick={pullImage}><CloudDownload className="size-4" />在线拉取镜像</button>}
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
          <label className="field-label">容器端口
            <input className="field-input" type="number" min={1} max={65535} value={containerPort} onChange={(event) => setContainerPort(Number(event.target.value))} />
          </label>
          <label className="flex min-h-11 cursor-pointer items-center gap-3 text-sm text-slate-700"><input type="checkbox" className="size-4 accent-teal-700" checked={autoStart} onChange={(event) => setAutoStart(event.target.checked)} />服务器重启后自动启动</label>
          {error && <p role="alert" className="rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700">{error}</p>}
        </div>}

        {step === 2 && <div className="py-2">
          <p className="mb-3 rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm leading-6 text-slate-600">共享模型仓库将自动挂载进实例容器，以下 {readyModels.length} 个已就绪模型对新实例立即可见，无需复制或选择。</p>
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
            {[['实例名称', name], ['容器镜像', image], ['计算资源', gpu >= 0 ? `GPU ${gpu} · ${gpuNames[gpu] ?? 'GPU'}` : '不分配 GPU（仅 CPU）'], ['访问地址', `http://服务器IP:${port} → 容器 ${containerPort}`], ['共享模型', `${readyModels.length} 个已就绪模型自动可见`], ['开机自启', autoStart ? '启用' : '关闭']].map(([term, value]) => <div key={term} className="grid grid-cols-[100px_1fr] gap-4 py-3 text-sm"><dt className="text-slate-500">{term}</dt><dd className="break-all font-medium text-slate-800">{value}</dd></div>)}
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
