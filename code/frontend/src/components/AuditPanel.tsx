import { useEffect, useState } from 'react'
import { ChevronLeft, ChevronRight, FileClock, Search } from 'lucide-react'
import { getAuditLogs, type AuditLogEntry } from '@/api'
import { Panel, PanelHeader } from '@/components/common'

const PAGE_SIZE = 50

function formatTime(value: string): string {
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString('zh-CN', { hour12: false })
}

function resultBadge(result: string): string {
  if (result.includes('success') || result.startsWith('HTTP 2')) return 'bg-emerald-50 text-emerald-700'
  if (result.includes('failure') || result.startsWith('HTTP 4') || result.startsWith('HTTP 5')) return 'bg-rose-50 text-rose-700'
  return 'bg-slate-100 text-slate-600'
}

// AuditPanel 操作审计：登录、写操作与关键事件的只读查询。
export function AuditPanel() {
  const [logs, setLogs] = useState<AuditLogEntry[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [q, setQ] = useState('')
  const [loading, setLoading] = useState(false)

  function load(targetOffset: number, query: string) {
    setLoading(true)
    getAuditLogs({ limit: PAGE_SIZE, offset: targetOffset, q: query || undefined })
      .then((page) => {
        setLogs(page.logs)
        setTotal(page.total)
        setOffset(targetOffset)
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    const controller = new AbortController()
    getAuditLogs({ limit: PAGE_SIZE, offset: 0 })
      .then((page) => {
        setLogs(page.logs)
        setTotal(page.total)
      })
      .catch(() => {})
    return () => controller.abort()
  }, [])

  const page = Math.floor(offset / PAGE_SIZE) + 1
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <Panel data-testid="audit-panel">
      <PanelHeader title="操作审计" description={`共 ${total} 条记录 · 时间倒序 · IP 已脱敏`} />
      <div className="flex items-center gap-2 border-b border-slate-200 px-5 py-3">
        <Search className="size-4 text-slate-400" />
        <input
          type="search"
          className="w-full max-w-xs rounded-lg border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-teal-600"
          placeholder="搜索动作 / 对象 / 详情…"
          value={q}
          onChange={(event) => setQ(event.target.value)}
          onKeyDown={(event) => { if (event.key === 'Enter') load(0, q) }}
          data-testid="audit-search"
        />
        <button type="button" className="btn-secondary" onClick={() => load(0, q)}>搜索</button>
      </div>
      <div className="overflow-x-auto" data-testid="audit-table">
        <table className="data-table">
          <thead>
            <tr><th>时间</th><th>操作者</th><th>动作</th><th>对象</th><th>结果</th><th>IP 摘要</th><th>请求</th></tr>
          </thead>
          <tbody>
            {logs.map((item) => (
              <tr key={item.id}>
                <td className="whitespace-nowrap text-slate-600">{formatTime(item.ts)}</td>
                <td>{item.actor}</td>
                <td className="font-mono text-xs text-slate-700">{item.action}</td>
                <td className="max-w-48 truncate text-slate-600" title={item.object}>{item.object || '—'}</td>
                <td><span className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${resultBadge(item.result)}`}>{item.result}</span></td>
                <td className="font-mono text-xs text-slate-400">{item.ipHash || '—'}</td>
                <td className="font-mono text-xs text-slate-400">{item.requestId || '—'}</td>
              </tr>
            ))}
            {logs.length === 0 && !loading && <tr><td colSpan={7} className="text-center text-slate-400">暂无审计记录</td></tr>}
          </tbody>
        </table>
      </div>
      <div className="flex items-center justify-between border-t border-slate-200 px-5 py-3 text-sm text-slate-500">
        <span className="flex items-center gap-1.5"><FileClock className="size-4" />第 {page}／{pages} 页</span>
        <div className="flex gap-2">
          <button type="button" className="btn-secondary" disabled={offset === 0 || loading} onClick={() => load(Math.max(0, offset - PAGE_SIZE), q)}><ChevronLeft className="size-4" />上一页</button>
          <button type="button" className="btn-secondary" disabled={offset + PAGE_SIZE >= total || loading} onClick={() => load(offset + PAGE_SIZE, q)}>下一页<ChevronRight className="size-4" /></button>
        </div>
      </div>
    </Panel>
  )
}
