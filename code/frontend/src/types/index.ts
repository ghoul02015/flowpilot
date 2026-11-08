export type PageKey = 'dashboard' | 'instances' | 'models' | 'tasks' | 'system'

export type InstanceStatus = 'creating' | 'starting' | 'running' | 'stopping' | 'stopped' | 'restarting' | 'error'
export type ModelStatus = 'waiting' | 'downloading' | 'paused' | 'ready' | 'failed' | 'cancelled'
export type TaskStatus = 'running' | 'paused' | 'completed' | 'failed' | 'cancelled'
export type TaskType = '实例部署' | '模型下载' | '模型导入' | '环境检测' | '诊断导出'

export interface Instance {
  id: string
  name: string
  template: string
  version: string
  status: InstanceStatus
  gpu: number
  port: number
  autoStart: boolean
  models: number
  createdAt: string
  uptime: string
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
  modelId?: string
  instanceId?: string
}

export interface CreateInstanceInput {
  name: string
  template: string
  version: string
  gpu: number
  port: number
  autoStart: boolean
  models: number
  image?: string
  containerPort?: number
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
