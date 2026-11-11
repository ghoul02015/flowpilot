export type PageKey = 'dashboard' | 'instances' | 'models' | 'images' | 'tasks' | 'hardware' | 'system'

// NavigateOptions 承载跨页跳转的定位参数（如跳转到实例页并聚焦某实例）。
export interface NavigateOptions {
  focus?: string
}

export type InstanceStatus = 'creating' | 'starting' | 'running' | 'stopping' | 'stopped' | 'restarting' | 'error'
export type ModelStatus = 'waiting' | 'downloading' | 'paused' | 'ready' | 'failed' | 'cancelled'
export type TaskStatus = 'running' | 'paused' | 'completed' | 'failed' | 'cancelled'
export type TaskType = '模型下载' | '模型导入'

export interface Instance {
  id: string
  name: string
  template: string
  version: string
  status: InstanceStatus
  gpu: number
  port: number
  autoStart: boolean
  createdAt: string
  uptime: string
  modelsTarget?: string
  outputsTarget?: string
  workflowsTarget?: string
  // 由后端代理探测的 ComfyUI 制图队列摘要。
  comfy?: {
    reachable: boolean
    queueRunning: number
    queuePending: number
    comfyuiVersion?: string
  }
}

export interface ModelItem {
  id: string
  name: string
  type: string
  size: string
  source: string
  status: ModelStatus
  progress: number
  createdAt: string
  /** 来源仓库：local 本地（可删除）｜ network 网络存储（只读，可本地化）。下载任务条目无此字段。 */
  origin?: 'local' | 'network'
  /** 仓库相对路径（删除/本地化 API 用）。 */
  rawPath?: string
  /** 原始修改时间（ISO），僵尸检测用；createdAt 为展示用本地化字符串。 */
  modifiedRaw?: string
}

export interface TaskItem {
  id: string
  name: string
  type: TaskType
  target: string
  status: TaskStatus
  progress: number
  createdAt: string
  message: string
}

export interface CreateInstanceInput {
  name: string
  gpu: number
  port: number
  autoStart: boolean
  image?: string
  containerPort?: number
  modelsTarget?: string
  outputsTarget?: string
  workflowsTarget?: string
}

export interface AddModelInput {
  name: string
  type: string
  source: string
  sourceType: 'URL 下载' | '本地导入'
}

export type GpuMemoryMode = 'discrete' | 'unified' | 'coherent'

export interface GpuDevice {
  id: number
  model: string
  memoryTotalGB: number
  memoryUsedGB: number
  memoryUsageSupported: boolean
  utilization: number | null
  temperatureC: number | null
  powerW: number | null
  powerLimitW: number | null
}

export interface HardwareProfile {
  id: string
  hostName: string
  productName: string
  os: string
  architecture: 'x86_64' | 'arm64'
  cpuModel: string
  cpuCores: number
  cpuUsage: number
  memoryTotalGB: number
  memoryUsedGB: number
  memoryDescription: string
  storageTotalGB: number
  storageUsedGB: number
  storageDescription: string
  gpuMemoryMode: GpuMemoryMode
  gpus: GpuDevice[]
}
