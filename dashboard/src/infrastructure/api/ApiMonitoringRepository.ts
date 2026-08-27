import type { MonitoringRepository } from '@/application/ports/MonitoringRepository'
import type { MetricName, ServerMetricsResult } from '@/domain/monitoring/types'
import { apiRequest } from './client'

/**
 * The metrics contract maps directly onto the domain type — `{ items, grafana }` with a
 * per-server `metrics` map — so there is no reshaping here. Absent metric values stay
 * absent from the map, which the presentation layer renders as "no data".
 */
export class ApiMonitoringRepository implements MonitoringRepository {
  async getServerMetrics(serverIds: string[], metrics?: MetricName[]): Promise<ServerMetricsResult> {
    const query = new URLSearchParams()
    query.set('serverIds', serverIds.join(','))
    if (metrics && metrics.length > 0) {
      query.set('metrics', metrics.join(','))
    }
    return apiRequest<ServerMetricsResult>(`/api/v1/monitoring/metrics?${query.toString()}`)
  }
}
