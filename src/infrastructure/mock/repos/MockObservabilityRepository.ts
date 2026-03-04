import type { GPUDevice, GPUMetrics, GPUProfile } from '@/domain/gpu/types'
import type { Alert } from '@/domain/alert/types'
import type { ObservabilityRepository, ListGPUDevicesFilters, ListAlertsFilters } from '@/application/ports/ObservabilityRepository'
import { seedGPUDevices, seedBaseGPUMetrics, seedGPUProfiles } from '@/infrastructure/mock/data/seedGPUs'
import { seedAlerts } from '@/infrastructure/mock/data/seedPlatform'

function clamp(v: number, min: number, max: number) {
  return Math.max(min, Math.min(max, v))
}

function noise(v: number, range: number): number {
  return clamp(v + (Math.random() - 0.5) * range * 2, 0, 9999)
}

export class MockObservabilityRepository implements ObservabilityRepository {
  private alerts: Map<string, Alert>
  private gpuProfiles: GPUProfile[]

  constructor() {
    this.alerts = new Map(seedAlerts.map((a) => [a.id, a]))
    this.gpuProfiles = [...seedGPUProfiles]
  }

  async listGPUDevices(filters?: ListGPUDevicesFilters): Promise<GPUDevice[]> {
    let items = [...seedGPUDevices]
    if (filters?.vendor) items = items.filter((g) => g.vendor === filters.vendor)
    if (filters?.health) items = items.filter((g) => g.health === filters.health)
    if (filters?.site) items = items.filter((g) => g.site === filters.site)
    if (filters?.hostId) items = items.filter((g) => g.hostId === filters.hostId)
    if (filters?.search) {
      const q = filters.search.toLowerCase()
      items = items.filter((g) => g.model.toLowerCase().includes(q) || g.hostName.toLowerCase().includes(q) || g.id.includes(q))
    }
    return items
  }

  async getGPUMetrics(gpuIds?: string[]): Promise<GPUMetrics[]> {
    const devices = gpuIds ? seedGPUDevices.filter((g) => gpuIds.includes(g.id)) : seedGPUDevices
    return devices.map((gpu) => {
      const base = seedBaseGPUMetrics.get(gpu.id)!
      return {
        ...base,
        utilization: clamp(noise(base.utilization, 8), 0, 100),
        memoryUsedMB: clamp(noise(base.memoryUsedMB, base.memoryTotalMB * 0.05), 0, base.memoryTotalMB),
        temperatureC: clamp(noise(base.temperatureC, 3), 20, 105),
        powerDrawW: clamp(noise(base.powerDrawW, 15), 0, base.powerLimitW),
        fanSpeedPct: base.fanSpeedPct != null ? clamp(noise(base.fanSpeedPct, 5), 0, 100) : undefined,
        timestamp: new Date().toISOString(),
      }
    })
  }

  async getTopCriticalGPUs(limit: number): Promise<Array<GPUDevice & { metrics: GPUMetrics }>> {
    const priority: Record<string, number> = { critical: 3, degraded: 2, offline: 1, healthy: 0 }
    const sorted = [...seedGPUDevices].sort((a, b) => (priority[b.health] ?? 0) - (priority[a.health] ?? 0))
    const top = sorted.slice(0, limit)
    const metrics = await this.getGPUMetrics(top.map((g) => g.id))
    const metricsById = new Map(metrics.map((m) => [m.gpuId, m]))
    return top.map((gpu) => ({ ...gpu, metrics: metricsById.get(gpu.id)! }))
  }

  async listAlerts(filters?: ListAlertsFilters): Promise<Alert[]> {
    let items = Array.from(this.alerts.values())
    if (filters?.status) items = items.filter((a) => a.status === filters.status)
    if (filters?.severity) items = items.filter((a) => a.severity === filters.severity)
    if (filters?.category) items = items.filter((a) => a.category === filters.category)
    if (filters?.search) {
      const q = filters.search.toLowerCase()
      items = items.filter((a) => a.title.toLowerCase().includes(q) || a.message.toLowerCase().includes(q))
    }
    items = items.sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime())
    if (filters?.limit) items = items.slice(0, filters.limit)
    return items
  }

  async getAlert(id: string): Promise<Alert | null> {
    return this.alerts.get(id) ?? null
  }

  async acknowledgeAlert(id: string, actor: string): Promise<Alert> {
    const alert = this.alerts.get(id)
    if (!alert) throw new Error('Alert not found: ' + id)
    const updated = { ...alert, status: 'acknowledged' as const, acknowledgedAt: new Date().toISOString(), acknowledgedBy: actor }
    this.alerts.set(id, updated)
    return updated
  }

  async resolveAlert(id: string, _actor: string): Promise<Alert> {
    const alert = this.alerts.get(id)
    if (!alert) throw new Error('Alert not found: ' + id)
    const updated = { ...alert, status: 'resolved' as const, resolvedAt: new Date().toISOString() }
    this.alerts.set(id, updated)
    return updated
  }

  async listGPUProfiles(limit?: number): Promise<GPUProfile[]> {
    const items = [...this.gpuProfiles].sort((a, b) => new Date(b.startedAt).getTime() - new Date(a.startedAt).getTime())
    return limit ? items.slice(0, limit) : items
  }

  async getGPUProfile(id: string): Promise<GPUProfile | null> {
    return this.gpuProfiles.find((p) => p.id === id) ?? null
  }
}
