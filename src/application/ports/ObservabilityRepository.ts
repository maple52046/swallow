import type { GPUDevice, GPUMetrics, GPUProfile, GPUVendor, GPUStatus } from '@/domain/gpu/types'
import type { Alert, AlertSeverity, AlertStatus } from '@/domain/alert/types'

export interface ListGPUDevicesFilters {
  vendor?: GPUVendor
  status?: GPUStatus
  datacenter?: string
  serverId?: string
  search?: string
}

export interface ListAlertsFilters {
  severity?: AlertSeverity
  status?: AlertStatus
  category?: string
  search?: string
  limit?: number
}

export interface ObservabilityRepository {
  listGPUDevices(filters?: ListGPUDevicesFilters): Promise<GPUDevice[]>
  getGPUMetrics(gpuIds?: string[]): Promise<GPUMetrics[]>
  getTopCriticalGPUs(limit: number): Promise<Array<GPUDevice & { metrics: GPUMetrics }>>
  listAlerts(filters?: ListAlertsFilters): Promise<Alert[]>
  getAlert(id: string): Promise<Alert | null>
  acknowledgeAlert(id: string, actor: string): Promise<Alert>
  resolveAlert(id: string, actor: string): Promise<Alert>
  listGPUProfiles(limit?: number): Promise<GPUProfile[]>
  getGPUProfile(id: string): Promise<GPUProfile | null>
}
