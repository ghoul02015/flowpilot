import { useEffect, useRef, useState } from 'react'
import { ArrowDownToLine, CircleAlert, Download, ScrollText } from 'lucide-react'
import { openInstanceLogs, type InstanceLogEvent, type InstanceLogMeta } from '@/api'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'

// 前端日志缓冲上限：超出后丢弃最早的行，防止长时间跟随导致内存膨胀。
const MAX_LINES = 2000

interface Props {
  instance: { name: string; status: string } | null
  onClose: () => void
}

const endReasonText: Record<string, string> = {
  eof: '日志流已结束（实例未运行或容器已停止，以上为历史日志）',
  container_gone: '实例容器已删除，日志流结束',
  error: '日志流异常中断',
  closed: '日志流已断开',
}

export function InstanceLogsDialog({ instance, onClose }: Props) {
  // 父级按实例名做 key 重挂载，因此本地缓冲始终随新实例归零，无需在 effect 内重置状态。
  const instanceName = instance?.name
  const [lines, setLines] = useState<InstanceLogEvent[]>([])
  const [meta, setMeta] = useState<InstanceLogMeta | null>(null)
  const [ended, setEnded] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [follow, setFollow] = useState(true)
  const scrollRef = useRef<HTMLPreElement>(null)

  useEffect(() => {
    if (!instanceName) return
    const source = openInstanceLogs(instanceName)
    source.addEventListener('meta', (event) => {
      try {
        setMeta(JSON.parse((event as MessageEvent).data) as InstanceLogMeta)
      } catch {
        // 元信息解析失败不影响日志主体
      }
    })
    source.addEventListener('end', (event) => {
      try {
        setEnded((JSON.parse((event as MessageEvent).data) as { reason?: string }).reason ?? 'eof')
      } catch {
        setEnded('eof')
      }
      source.close()
    })
    source.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data) as InstanceLogEvent
        setLines((previous) => {
          const next = [...previous, payload]
          return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next
        })
      } catch {
        // 忽略非 JSON 数据帧
      }
    }
    source.onerror = () => {
      if (source.readyState === EventSource.CLOSED) {
        setEnded((previous) => previous ?? 'closed')
        return
      }
      setError('日志连接中断，正在自动重试…')
    }
    return () => source.close()
  }, [instanceName])

  // 跟随滚动：有新行且处于跟随状态时贴底；用户上滚超过 40px 自动暂停。
  useEffect(() => {
    const element = scrollRef.current
    if (!element || !follow) return
    element.scrollTop = element.scrollHeight
  }, [lines, follow])

  function handleScroll() {
    const element = scrollRef.current
    if (!element) return
    const atBottom = element.scrollHeight - element.scrollTop - element.clientHeight < 40
    setFollow(atBottom)
  }

  function jumpToBottom() {
    const element = scrollRef.current
    if (!element) return
    element.scrollTop = element.scrollHeight
    setFollow(true)
  }

  function downloadLogs() {
    const text = lines.map((line) => `${line.time ? `[${line.time}] ` : ''}${line.stream === 'stderr' ? '[stderr] ' : ''}${line.line}`).join('\n')
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `${instance?.name ?? 'instance'}-logs.log`
    anchor.click()
    URL.revokeObjectURL(url)
  }

  const stopped = instance != null && instance.status !== 'running'

  return <Dialog open={Boolean(instance)} onOpenChange={(open) => !open && onClose()}>
    <DialogContent className="sm:max-w-4xl">
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2"><ScrollText className="size-5 text-teal-700" />日志 - {instance?.name}</DialogTitle>
        <DialogDescription>
          {meta ? `实时流式日志 · 回放最近 ${meta.tail} 行${meta.tty ? ' · TTY' : ''}` : '正在建立日志流…'}
        </DialogDescription>
      </DialogHeader>

      {stopped && <div className="flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900">
        <CircleAlert className="mt-0.5 size-3.5 shrink-0" />实例当前未运行，仅显示历史日志；启动实例后重新打开可继续跟随。
      </div>}
      {error && <div className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-800">{error}</div>}

      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-slate-500">
        <span data-testid="instance-log-count">{lines.length} 行{ended ? ' · 已结束' : ' · 实时跟随中'}</span>
        <div className="flex items-center gap-1">
          {!follow && !ended && <button type="button" className="btn-quiet h-8 px-2 text-xs" onClick={jumpToBottom}><ArrowDownToLine className="size-3.5" />回到底部</button>}
          <button type="button" className="btn-quiet h-8 px-2 text-xs" onClick={downloadLogs} disabled={lines.length === 0}><Download className="size-3.5" />下载日志</button>
        </div>
      </div>

      <pre ref={scrollRef} onScroll={handleScroll} role="log" aria-live="polite" aria-label={`${instance?.name} 实例日志`} className="max-h-[60dvh] min-h-48 overflow-auto rounded-lg bg-slate-950 p-4 font-mono text-xs leading-5 whitespace-pre-wrap text-slate-100">
        {lines.length === 0
          ? <span className="text-slate-400">{ended ? '没有可显示的日志行。' : '等待日志输出…'}</span>
          : lines.map((line, index) => <span key={index} className={`block ${line.stream === 'stderr' ? 'text-rose-300' : ''}`}>{line.line}</span>)}
      </pre>

      {ended && <p className="text-xs text-slate-500">{endReasonText[ended] ?? '日志流已结束'}</p>}
    </DialogContent>
  </Dialog>
}
