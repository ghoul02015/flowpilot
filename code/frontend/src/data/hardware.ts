import type { GpuDevice, HardwareProfile } from '@/types'

function discreteGPUs(count: number, model: string, memoryGB: number, powerLimitW: number): GpuDevice[] {
  return Array.from({ length: count }, (_, index) => ({
    id: index,
    model,
    memoryTotalGB: memoryGB,
    memoryUsedGB: Number((memoryGB * (0.34 + (index % 5) * 0.08)).toFixed(1)),
    memoryUsageSupported: true,
    utilization: 38 + (index * 11) % 49,
    temperatureC: 53 + (index * 3) % 17,
    powerW: Math.round(powerLimitW * (0.31 + (index % 4) * 0.09)),
    powerLimitW,
  }))
}

export const hardwareProfiles: HardwareProfile[] = [
  {
    id: 'huilang-l40s', hostName: '慧浪 GPU-S01', productName: '慧浪双卡 GPU 服务器', os: 'Ubuntu 24.04 LTS', architecture: 'x86_64',
    cpuModel: 'Intel Xeon Gold 6530', cpuCores: 64, cpuUsage: 38,
    memoryTotalGB: 512, memoryUsedGB: 186, memoryDescription: 'DDR5 ECC · 系统内存',
    storageTotalGB: 7680, storageUsedGB: 4260, storageDescription: 'NVMe SSD · /data/flowpilot',
    gpuMemoryMode: 'discrete', gpus: discreteGPUs(2, 'NVIDIA L40S', 48, 300),
  },
  {
    id: 'dgx-spark', hostName: 'DGX-SPARK-01', productName: 'NVIDIA DGX Spark', os: 'DGX OS · Ubuntu 24.04', architecture: 'arm64',
    cpuModel: 'NVIDIA GB10 Grace Blackwell', cpuCores: 20, cpuUsage: 31,
    memoryTotalGB: 128, memoryUsedGB: 72, memoryDescription: 'LPDDR5x · CPU／GPU 动态共享',
    storageTotalGB: 4096, storageUsedGB: 1260, storageDescription: 'NVMe M.2 · 系统与数据盘',
    gpuMemoryMode: 'unified',
    gpus: [{ id: 0, model: 'NVIDIA Blackwell 集成 GPU', memoryTotalGB: 128, memoryUsedGB: 72, memoryUsageSupported: false, utilization: 64, temperatureC: 56, powerW: 72, powerLimitW: 140 }],
  },
  {
    id: 'dgx-station-gb300', hostName: 'DGX-STATION-01', productName: 'NVIDIA DGX Station GB300', os: 'DGX OS · Ubuntu 24.04', architecture: 'arm64',
    cpuModel: 'NVIDIA Grace · Neoverse V2', cpuCores: 72, cpuUsage: 43,
    memoryTotalGB: 496, memoryUsedGB: 212, memoryDescription: 'LPDDR5X · 与 252 GB HBM3e 构成最高 748 GB 一致性地址空间',
    storageTotalGB: 8192, storageUsedGB: 2380, storageDescription: 'NVMe SSD · 系统与数据阵列',
    gpuMemoryMode: 'coherent',
    gpus: [{ id: 0, model: 'NVIDIA GB300 Blackwell Ultra', memoryTotalGB: 252, memoryUsedGB: 118, memoryUsageSupported: true, utilization: 71, temperatureC: 62, powerW: 684, powerLimitW: 1200 }],
  },
  {
    id: 'rtx5090-single', hostName: 'HL-RTX5090-01', productName: '慧浪单卡 RTX 5090 工作站', os: 'Ubuntu 24.04 LTS', architecture: 'x86_64',
    cpuModel: 'AMD Ryzen 9 9950X', cpuCores: 16, cpuUsage: 27,
    memoryTotalGB: 96, memoryUsedGB: 38, memoryDescription: 'DDR5 · 系统内存',
    storageTotalGB: 4096, storageUsedGB: 1520, storageDescription: 'NVMe SSD · /data/flowpilot',
    gpuMemoryMode: 'discrete', gpus: discreteGPUs(1, 'NVIDIA GeForce RTX 5090', 32, 575),
  },
  {
    id: 'rtx5090-16', hostName: 'HL-RTX5090-16C-01', productName: '慧浪十六卡 RTX 5090 GPU 服务器', os: 'Ubuntu 24.04 LTS', architecture: 'x86_64',
    cpuModel: '2 × AMD EPYC 9655', cpuCores: 192, cpuUsage: 56,
    memoryTotalGB: 1536, memoryUsedGB: 728, memoryDescription: 'DDR5 ECC · 系统内存',
    storageTotalGB: 30720, storageUsedGB: 12840, storageDescription: 'NVMe SSD 阵列 · /data/flowpilot',
    gpuMemoryMode: 'discrete', gpus: discreteGPUs(16, 'NVIDIA GeForce RTX 5090', 32, 575),
  },
]
