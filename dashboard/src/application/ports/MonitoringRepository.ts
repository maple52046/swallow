import type { MetricName, ServerMetricsResult } from '@/domain/monitoring/types'

/**
 * Reads per-server metrics from the backend's fixed named-query set. There is no write
 * side: swallow stores no metrics, and alert handling is a separate concern. A missing
 * metrics backend surfaces as an error the caller renders as "no data" rather than a
 * page failure.
 */
export interface MonitoringRepository {
  /**
   * Current metric values for the given servers. `metrics` selects which named queries to
   * evaluate; omit it for the full set. At most 200 servers per call, matching the backend
   * bound.
   */
  getServerMetrics(serverIds: string[], metrics?: MetricName[]): Promise<ServerMetricsResult>
}
