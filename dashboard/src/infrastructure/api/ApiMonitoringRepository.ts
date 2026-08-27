import type { MonitoringRepository } from '@/application/ports/MonitoringRepository'
import type {
  AcknowledgeAlertInput,
  AcknowledgeAlertResult,
  MetricName,
  MonitoringAlert,
  MonitoringAlertFilters,
  ServerMetricsResult,
} from '@/domain/monitoring/types'
import { apiRequest } from './client'

/** HTTP adapter for the active monitoring alerts and server-metrics contracts. */
export class ApiMonitoringRepository implements MonitoringRepository {
  async listAlerts(filters: MonitoringAlertFilters = {}): Promise<MonitoringAlert[]> {
    const query = new URLSearchParams()
    if (filters.siteId) query.set('siteId', filters.siteId)
    if (filters.serverId) query.set('serverId', filters.serverId)
    if (filters.severity) query.set('severity', filters.severity)
    if (filters.state) query.set('state', filters.state)
    const suffix = query.size ? `?${query.toString()}` : ''
    return apiRequest<MonitoringAlert[]>(`/api/v1/monitoring/alerts${suffix}`)
  }

  async acknowledgeAlert(input: AcknowledgeAlertInput): Promise<AcknowledgeAlertResult> {
    const query = new URLSearchParams()
    if (input.siteId) query.set('siteId', input.siteId)
    const suffix = query.size ? `?${query.toString()}` : ''
    return apiRequest<AcknowledgeAlertResult>(`/api/v1/monitoring/alerts/${encodeURIComponent(input.fingerprint)}/acknowledge${suffix}`, {
      method: 'POST',
      body: JSON.stringify({ matchers: input.matchers, duration: input.duration ?? '', comment: input.comment ?? '' }),
    })
  }

  async getServerMetrics(serverIds: string[], metrics?: MetricName[]): Promise<ServerMetricsResult> {
    const query = new URLSearchParams()
    query.set('serverIds', serverIds.join(','))
    if (metrics && metrics.length > 0) query.set('metrics', metrics.join(','))
    return apiRequest<ServerMetricsResult>(`/api/v1/monitoring/metrics?${query.toString()}`)
  }
}
