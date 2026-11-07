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
