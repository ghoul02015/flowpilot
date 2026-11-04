import { useState } from 'react'
import { ArrowLeft, ArrowRight, Check, Server } from 'lucide-react'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { CreateInstanceInput, Instance, ModelItem } from '@/types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (input: CreateInstanceInput) => void
  models: ModelItem[]
  instances: Instance[]
}

export function CreateInstanceDialog({ open, onOpenChange, onSubmit, models, instances }: Props) {
  const [step, setStep] = useState(1)
  const [name, setName] = useState(`生产绘图-${String(instances.length + 1).padStart(2, '0')}`)
  const [template, setTemplate] = useState('ComfyUI 标准版')
  const [gpu, setGpu] = useState(0)
  const [port, setPort] = useState(8188 + instances.length)
  const [autoStart, setAutoStart] = useState(true)
  const [selectedModels, setSelectedModels] = useState<string[]>([])
  const [error, setError] = useState('')

  const readyModels = models.filter((model) => model.status === 'ready')
  const version = template === 'ComfyUI 视频版' ? 'v0.3.76-video' : 'v0.3.76'

  function next() {
    if (!name.trim()) {
      setError('请输入实例名称。')
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
    if (instances.some((item) => item.gpu === gpu && ['running', 'starting', 'creating'].includes(item.status))) {
      setError(`GPU ${gpu} 正在被运行实例使用，请选择另一块 GPU。`)
      return
    }
    setError('')
    setStep(2)
  }

  function submit() {
    onSubmit({ name: name.trim(), template, version, gpu, port, autoStart, models: selectedModels.length })
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Server className="size-5 text-teal-700" />新建 ComfyUI 实例</DialogTitle>
          <DialogDescription>通过已验证模板创建独立容器，业务数据将持久化保存。</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2 py-2" aria-label={`当前第 ${step} 步，共 3 步`}>
          {['模板与资源', '模型准备', '确认部署'].map((label, index) => (
            <div key={label} className="flex flex-1 items-center gap-2">
              <span className={`flex size-7 shrink-0 items-center justify-center rounded-full text-xs font-semibold ${step > index + 1 ? 'bg-teal-600 text-white' : step === index + 1 ? 'bg-slate-900 text-white' : 'bg-slate-100 text-slate-500'}`}>{step > index + 1 ? <Check className="size-4" /> : index + 1}</span>
              <span className={`hidden text-xs sm:inline ${step === index + 1 ? 'font-semibold text-slate-900' : 'text-slate-500'}`}>{label}</span>
              {index < 2 && <div className="h-px flex-1 bg-slate-200" />}
            </div>
          ))}
        </div>

        {step === 1 && <div className="grid gap-4 py-2">
          <label className="field-label">镜像模板
            <select className="field-input" value={template} onChange={(event) => setTemplate(event.target.value)}>
              <option>ComfyUI 标准版</option><option>ComfyUI 视频版</option>
            </select>
          </label>
          <div className="rounded-lg border border-teal-200 bg-teal-50 p-3 text-sm leading-6 text-teal-950">镜像版本 {version} · 固定摘要 · 健康检查已配置</div>
          <label className="field-label">实例名称
            <input className="field-input" value={name} onChange={(event) => setName(event.target.value)} maxLength={32} autoComplete="off" />
          </label>
          <div className="grid gap-4 sm:grid-cols-2">
            <label className="field-label">分配 GPU
              <select className="field-input" value={gpu} onChange={(event) => setGpu(Number(event.target.value))}><option value={0}>GPU 0 · NVIDIA L40S</option><option value={1}>GPU 1 · NVIDIA L40S</option></select>
            </label>
            <label className="field-label">访问端口
              <input className="field-input" type="number" min={1024} max={65535} value={port} onChange={(event) => setPort(Number(event.target.value))} />
            </label>
          </div>
          <label className="flex min-h-11 cursor-pointer items-center gap-3 text-sm text-slate-700"><input type="checkbox" className="size-4 accent-teal-700" checked={autoStart} onChange={(event) => setAutoStart(event.target.checked)} />服务器重启后自动启动</label>
          {error && <p role="alert" className="rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700">{error}</p>}
        </div>}

        {step === 2 && <div className="py-2">
          <p className="mb-3 text-sm leading-6 text-slate-600">选择需要预置的全局模型，也可以跳过，先创建空实例。</p>
          <div className="max-h-72 space-y-2 overflow-y-auto pr-1">
            {readyModels.map((model) => <label key={model.id} className="flex min-h-14 cursor-pointer items-center gap-3 rounded-lg border border-slate-200 px-3 py-2 transition-colors hover:border-teal-400">
              <input type="checkbox" className="size-4 accent-teal-700" checked={selectedModels.includes(model.id)} onChange={() => setSelectedModels((current) => current.includes(model.id) ? current.filter((id) => id !== model.id) : [...current, model.id])} />
              <span className="min-w-0 flex-1"><span className="block truncate text-sm font-medium text-slate-800">{model.name}</span><span className="text-xs text-slate-500">{model.type} · {model.size}</span></span>
            </label>)}
          </div>
          <p className="mt-3 text-xs text-slate-500">已选择 {selectedModels.length} 个模型 · 数据盘剩余 3.42 TB</p>
        </div>}

        {step === 3 && <div className="py-2">
          <dl className="divide-y divide-slate-200 rounded-lg border border-slate-200 px-4">
            {[['实例名称', name], ['镜像模板', `${template} · ${version}`], ['计算资源', `GPU ${gpu} · NVIDIA L40S`], ['访问地址', `http://服务器IP:${port}`], ['预置模型', `${selectedModels.length} 个`], ['开机自启', autoStart ? '启用' : '关闭']].map(([term, value]) => <div key={term} className="grid grid-cols-[100px_1fr] gap-4 py-3 text-sm"><dt className="text-slate-500">{term}</dt><dd className="break-all font-medium text-slate-800">{value}</dd></div>)}
          </dl>
          <p className="mt-4 text-xs leading-5 text-slate-500">Demo 将模拟镜像拉取、容器创建和健康检查，不会操作真实 Docker 容器。</p>
        </div>}

        <DialogFooter>
          {step > 1 && <button type="button" className="btn-secondary" onClick={() => setStep(step - 1)}><ArrowLeft className="size-4" />上一步</button>}
          {step === 1 && <button type="button" className="btn-primary" onClick={next}>下一步<ArrowRight className="size-4" /></button>}
          {step === 2 && <button type="button" className="btn-primary" onClick={() => setStep(3)}>下一步<ArrowRight className="size-4" /></button>}
          {step === 3 && <button type="button" className="btn-primary" onClick={submit}><Check className="size-4" />确认部署</button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
