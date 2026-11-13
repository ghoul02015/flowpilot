import { useCallback, useEffect, useRef, useState } from 'react'

export interface PollingOptions {
  /** 是否启用；false 时清理计时器并停止请求。 */
  enabled?: boolean
  /** 活跃周期（毫秒）。 */
  interval: number
  /** 空闲周期；未设置时沿用 interval。 */
  idleInterval?: number
  /** 返回 true 表示当前处于活跃状态，使用 interval。 */
  isActive?: () => boolean
  /** 挂载后是否立即执行一次，默认 true。 */
  immediate?: boolean
  /** 页面隐藏时暂停，默认 true。 */
  pauseWhenHidden?: boolean
  /** 失败退避档位（毫秒），默认 3/5/10/30/60 秒。 */
  backoff?: number[]
  onError?: (error: unknown) => void
}

export interface PollingState {
  running: boolean
  lastSuccessAt: number | null
  lastErrorAt: number | null
  error: unknown
}

const defaultBackoff = [3000, 5000, 10000, 30000, 60000]

/**
 * 统一轮询器：上一次请求结束后才安排下一次，支持可见性暂停、取消、退避与手动刷新。
 * 同一数据源同一时间最多一个自动请求；手动刷新会取消进行中的请求。
 */
export function usePolling(task: (signal: AbortSignal) => Promise<void>, options: PollingOptions) {
  const taskRef = useRef(task)
  taskRef.current = task
  const optionsRef = useRef(options)
  optionsRef.current = options
  const [state, setState] = useState<PollingState>({ running: false, lastSuccessAt: null, lastErrorAt: null, error: null })
  const timerRef = useRef<number | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const generationRef = useRef(0)
  const failureRef = useRef(0)
  const scheduleRef = useRef<(delay: number, generation: number) => void>(() => {})

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current)
      timerRef.current = null
    }
  }, [])

  const abortCurrent = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
  }, [])

  const run = useCallback(async (generation: number) => {
    if (generation !== generationRef.current) return
    const opts = optionsRef.current
    if (opts.enabled === false) return
    if (opts.pauseWhenHidden !== false && document.visibilityState === 'hidden') {
      scheduleRef.current(1000, generation)
      return
    }
    // 防重叠：上一轮尚未结束时直接跳过。
    if (abortRef.current) return
    const controller = new AbortController()
    abortRef.current = controller
    setState((prev) => ({ ...prev, running: true }))
    try {
      await taskRef.current(controller.signal)
      if (generation !== generationRef.current) return
      failureRef.current = 0
      setState({ running: false, lastSuccessAt: Date.now(), lastErrorAt: null, error: null })
    } catch (error) {
      if (controller.signal.aborted || generation !== generationRef.current) return
      failureRef.current += 1
      setState((prev) => ({ ...prev, running: false, lastErrorAt: Date.now(), error }))
      opts.onError?.(error)
    } finally {
      if (abortRef.current === controller) abortRef.current = null
    }
    if (generation !== generationRef.current) return
    const current = optionsRef.current
    const active = current.isActive ? current.isActive() : false
    const base = active ? current.interval : (current.idleInterval ?? current.interval)
    const backoff = current.backoff ?? defaultBackoff
    const delay = failureRef.current > 0 ? backoff[Math.min(failureRef.current - 1, backoff.length - 1)] : base
    scheduleRef.current(delay, generation)
  }, [])

  const schedule = useCallback((delay: number, generation: number) => {
    clearTimer()
    timerRef.current = window.setTimeout(() => {
      void run(generation)
    }, Math.max(0, delay))
  }, [clearTimer, run])
  scheduleRef.current = schedule

  const refresh = useCallback(async () => {
    clearTimer()
    abortCurrent()
    generationRef.current += 1
    await run(generationRef.current)
  }, [abortCurrent, clearTimer, run])

  const stop = useCallback(() => {
    generationRef.current += 1
    clearTimer()
    abortCurrent()
    setState((prev) => ({ ...prev, running: false }))
  }, [abortCurrent, clearTimer])

  useEffect(() => {
    generationRef.current += 1
    const generation = generationRef.current
    failureRef.current = 0
    clearTimer()
    abortCurrent()
    if (options.enabled === false) {
      setState((prev) => ({ ...prev, running: false }))
      return () => stop()
    }
    if (options.immediate !== false) {
      void run(generation)
    } else {
      schedule(options.interval, generation)
    }
    const onVisibility = () => {
      if (document.visibilityState === 'visible') {
        failureRef.current = 0
        void refresh()
      } else {
        clearTimer()
        abortCurrent()
      }
    }
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      document.removeEventListener('visibilitychange', onVisibility)
      stop()
    }
  }, [abortCurrent, clearTimer, options.enabled, options.immediate, options.interval, options.idleInterval, options.pauseWhenHidden, refresh, run, schedule, stop])

  return { ...state, refresh, stop }
}
