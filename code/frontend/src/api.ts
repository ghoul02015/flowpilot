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
