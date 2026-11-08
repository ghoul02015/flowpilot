export interface HealthResponse {
  status: string
  service: string
  version: string
}

export interface DockerEngine {
  info: {
    Name: string
    ServerVersion: string
    OperatingSystem: string
    OSType: string
    Architecture: string
    NCPU: number
    MemTotal: number
    Containers: number
    ContainersRunning: number
    ContainersPaused: number
    ContainersStopped: number
    DockerRootDir: string
  }
  version: {
    Version: string
    ApiVersion: string
    Os: string
    Arch: string
  }
}

export interface DockerContainer {
  Id: string
  Names: string[]
  Image: string
  State: string
  Status: string
  Created: number
  Labels: Record<string, string>
}

export interface RealGPU {
  index: number
  name: string
  memoryTotalMB: number | null
  memoryUsedMB: number | null
  utilizationPercent: number | null
  temperatureC: number | null
  powerDrawW: number | null
  powerLimitW: number | null
}

export interface HardwareReport {
  collectedAt: string
  hostName: string
  machineModel?: string
  os: string
  architecture: string
  cpuModel: string
  cpuCores: number
  cpuUsagePercent: number | null
  memoryTotalMB: number
  memoryUsedMB: number
  storagePath: string
  storageTotalBytes: number
  storageUsedBytes: number
  gpuAvailable: boolean
  gpuError?: string
  gpuMemoryMode: 'discrete' | 'unified' | 'none'
  gpus: RealGPU[]
}

export interface RuntimeSnapshot {
  health: HealthResponse
  engine: DockerEngine
  containers: DockerContainer[]
}

export interface RealInstance {
  name: string
  containerId: string
  containerName: string
  image: string
  status: 'running' | 'stopped' | 'error'
  gpu: number
  port: number
  autoStart: boolean
  createdAt: string
}

export interface InstanceDefaults {
  image: string
  containerPort: number
  dataDir: string
}

export interface InstanceListResponse {
  instances: RealInstance[]
  defaults: InstanceDefaults
}

export interface CreateInstanceSpec {
  name: string
  image: string
  gpu: number
  port: number
  containerPort: number
  autoStart: boolean
}

async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { headers: { Accept: 'application/json' }, signal })
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { detail?: string; error?: string } | null
    throw new Error(body?.detail ?? body?.error ?? `请求失败：HTTP ${response.status}`)
  }
  return response.json() as Promise<T>
}

export async function getRuntimeSnapshot(signal?: AbortSignal): Promise<RuntimeSnapshot> {
  const [health, engine, containers] = await Promise.all([
    getJSON<HealthResponse>('/api/v1/health', signal),
    getJSON<DockerEngine>('/api/v1/docker/info', signal),
    getJSON<DockerContainer[]>('/api/v1/docker/containers', signal),
  ])
  return { health, engine, containers }
}

export async function getHardware(signal?: AbortSignal): Promise<HardwareReport> {
  // 面板每 2 秒轮询一次，请求必须带超时，避免挂起的请求持续堆积。
  return getJSON<HardwareReport>('/api/v1/hardware', signal ?? AbortSignal.timeout(4000))
}

async function sendJSON<T>(method: string, path: string, payload?: unknown): Promise<T> {
  const response = await fetch(path, {
    method,
    headers: { Accept: 'application/json', ...(payload !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    body: payload !== undefined ? JSON.stringify(payload) : undefined,
  })
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { detail?: string; error?: string } | null
    throw new Error(body?.detail ?? body?.error ?? `请求失败：HTTP ${response.status}`)
  }
  return response.json() as Promise<T>
}

export async function getInstances(signal?: AbortSignal): Promise<InstanceListResponse> {
  return getJSON<InstanceListResponse>('/api/v1/instances', signal ?? AbortSignal.timeout(5000))
}

export async function createInstanceApi(spec: CreateInstanceSpec): Promise<RealInstance> {
  return sendJSON<RealInstance>('POST', '/api/v1/instances', spec)
}

export async function postInstanceAction(name: string, action: 'start' | 'stop' | 'restart'): Promise<RealInstance> {
  return sendJSON<RealInstance>('POST', `/api/v1/instances/${encodeURIComponent(name)}/${action}`)
}

export async function deleteInstanceApi(name: string, force: boolean): Promise<void> {
  await sendJSON<{ status: string }>('DELETE', `/api/v1/instances/${encodeURIComponent(name)}${force ? '?force=1' : ''}`)
}
