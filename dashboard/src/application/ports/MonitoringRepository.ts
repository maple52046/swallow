import type {
  AcknowledgeAlertInput,
  AcknowledgeAlertResult,
  MetricName,
  MetricRangeQuery,
  MetricRangeResult,
  MonitoringAlert,
  MonitoringAlertFilters,
  ServerMetricsResult,
} from '@/domain/monitoring/types'

/**
 * Provider boundary for current metrics, usage history, and Alertmanager alert operations.
 *
 * Implementations return dashboard domain types only. A missing or unreachable metrics backend
 * must be raised as `MetricsBackendUnavailableError` from the metric reads, so screens can ask
 * the operator to register a metrics Integration instead of showing a generic failure.
 */
export interface MonitoringRepository {
  /** Lists current firing and suppressed alerts using supported provider filters. */
  listAlerts(filters?: MonitoringAlertFilters): Promise<MonitoringAlert[]>
  /** Creates an Alertmanager silence using exact label matchers. */
  acknowledgeAlert(input: AcknowledgeAlertInput): Promise<AcknowledgeAlertResult>
  /** Queries at most 200 servers for the backend's fixed named instant metrics. */
  getServerMetrics(serverIds: string[], metrics?: MetricName[]): Promise<ServerMetricsResult>
  /**
   * Reads bounded usage history for a scope. An explicit Server scope must hold at most 200 ids
   * and `perServer` mode at most 50 resolved Servers; the backend refuses larger requests. An
   * unknown Pool, Zone, or Platform id rejects with the backend's not-found error.
   */
  getMetricRange(query: MetricRangeQuery): Promise<MetricRangeResult>
}
