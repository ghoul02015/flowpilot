import { useState } from 'react'
import { Download, HardDrive, Link2 } from 'lucide-react'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { AddModelInput } from '@/types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (input: AddModelInput) => void
  realMode?: boolean
}

export function AddModelDialog({ open, onOpenChange, onSubmit, realMode = false }: Props) {
  const [sourceType, setSourceType] = useState<'URL 下载' | '本地导入'>('URL 下载')
  const [name, setName] = useState('dreamshaper-xl.safetensors')
  const [type, setType] = useState('Checkpoints')
  const [source, setSource] = useState('https://models.example.com/dreamshaper-xl.safetensors')
  const [error, setError] = useState('')

  function changeSourceType(value: 'URL 下载' | '本地导入') {
    setSourceType(value)
    setSource(value === 'URL 下载' ? 'https://models.example.com/dreamshaper-xl.safetensors' : '/data/import/dreamshaper-xl.safetensors')
    setError('')
  }

  function submit() {
    // 真实模式文件名可留空，由下载地址自动推导。
    if (!realMode && !name.trim()) {
      setError('请输入模型名称。')
      return
    }
    if (sourceType === 'URL 下载' && !/^https?:\/\//.test(source)) {
      setError('请输入以 http:// 或 https:// 开头的下载地址。')
      return
    }
    if (!realMode && sourceType === '本地导入' && !source.startsWith('/data/import/')) {
      setError('Demo 仅允许从 /data/import/ 目录导入。')
      return
    }
    onSubmit({ name: name.trim(), type, source, sourceType })
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader><DialogTitle>添加模型</DialogTitle><DialogDescription>模型先进入临时任务目录，校验完成后再写入全局共享仓库。</DialogDescription></DialogHeader>
        <div className="grid grid-cols-2 gap-2 rounded-lg bg-slate-100 p-1">
          <button type="button" className={`min-h-11 cursor-pointer rounded-md text-sm font-medium transition-colors ${sourceType === 'URL 下载' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-600 hover:text-slate-900'}`} onClick={() => changeSourceType('URL 下载')}><Link2 className="mr-2 inline size-4" />URL 下载</button>
          <button type="button" className={`min-h-11 cursor-pointer rounded-md text-sm font-medium transition-colors ${sourceType === '本地导入' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-600 hover:text-slate-900'}`} onClick={() => changeSourceType('本地导入')}><HardDrive className="mr-2 inline size-4" />本地导入</button>
        </div>
        <div className="grid gap-4 py-2">
          <label className="field-label">模型名称{realMode && <span className="ml-1 font-normal text-slate-400">（留空则从地址推导）</span>}<input className="field-input" value={name} onChange={(event) => setName(event.target.value)} autoComplete="off" /></label>
          <label className="field-label">模型类型<select className="field-input" value={type} onChange={(event) => setType(event.target.value)}>{['Checkpoints', 'LoRA', 'VAE', 'ControlNet', 'Embeddings', 'Upscale Models', '其他'].map((item) => <option key={item}>{item}</option>)}</select></label>
          <label className="field-label">{sourceType === 'URL 下载' ? '下载地址' : '服务器文件路径'}<input className="field-input font-mono text-xs" value={source} onChange={(event) => setSource(event.target.value)} autoComplete="off" /></label>
          <div className="rounded-lg bg-slate-100 p-3 text-xs leading-5 text-slate-600">{realMode
            ? sourceType === 'URL 下载'
              ? '将从该地址真实下载并写入共享仓库，支持暂停、断点续传与失败重试；任务进度在模型中心与任务中心实时可见。'
              : '本地导入将在后续版本接入；请先将模型文件放入共享仓库目录，模型中心会自动识别。'
            : <>磁盘预检通过 · 可用空间 3.42 TB<br />Demo 使用模拟进度，不会访问该地址或路径。</>}</div>
          {error && <p role="alert" className="rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700">{error}</p>}
        </div>
        <DialogFooter><button type="button" className="btn-secondary" onClick={() => onOpenChange(false)}>取消</button><button type="button" className="btn-primary" onClick={submit}><Download className="size-4" />{sourceType === 'URL 下载' ? '开始下载' : '开始导入'}</button></DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
