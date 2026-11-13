// 轻量刷新事件：只表达“哪些数据过期”，不承担数据存储。
export type RefreshTopic =
  | 'tasks'
  | 'task-summary'
  | 'instances'
  | 'models'
  | 'downloads'
  | 'images'
  | 'bundles'
  | 'hardware'
  | 'system'

type Listener = () => void
const listeners = new Map<RefreshTopic, Set<Listener>>()

export function subscribeRefresh(topic: RefreshTopic, listener: Listener): () => void {
  const set = listeners.get(topic) ?? new Set<Listener>()
  listeners.set(topic, set)
  set.add(listener)
  return () => {
    set.delete(listener)
    if (set.size === 0) listeners.delete(topic)
  }
}

export function emitRefresh(...topics: RefreshTopic[]) {
  for (const topic of topics) {
    const set = listeners.get(topic)
    if (!set) continue
    for (const listener of [...set]) listener()
  }
}
