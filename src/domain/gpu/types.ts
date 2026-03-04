export type GPUVendor = 'nvidia' | 'amd'
export type GPUHealth = 'healthy' | 'degraded' | 'critical' | 'offline'

export interface GPUDevice {
  id: string
  hostId: string
  hostName: string
  index: number
  vendor: GPUVendor
  model: string
  serial: string
  uuid: string
  driverVersion: string
  cudaVersion?: string
  rocmVersion?: string
  health: GPUHealth
  site: string
  rack: string
  memoryGB: number
}

export interface GPUMetrics {
  gpuId: string
  utilization: number
  memoryUsedMB: number
  memoryTotalMB: number
  temperatureC: number
  powerDrawW: number
  powerLimitW: number
  fanSpeedPct?: number
  eccErrors: number
  xidErrors: number
  throttling: boolean
  timestamp: string
}

export interface GPUProfile {
  id: string
  gpuId: string
  gpuModel: string
  hostId: string
  hostName: string
  runId?: string
  missionId?: string
  missionName?: string
  status: 'completed' | 'running' | 'failed'
  startedAt: string
  completedAt?: string
  durationMs?: number
  summary: GPUProfileSummary
  reportUrl: string
}

export interface GPUProfileSummary {
  topKernels: Array<{ name: string; durationPct: number; callCount: number }>
  memoryBandwidthGBs: number
  computeUtilizationPct: number
  memoryUtilizationPct: number
  rooflineEfficiency: number
}
