import type {
  AcknowledgeAlertInput,
  AcknowledgeAlertResult,
  MetricName,
  MonitoringAlert,
  MonitoringAlertFilters,
  ServerMetricsResult,
} from '@/domain/monitoring/types'

/** Provider boundary for current metrics and Alertmanager alert operations. */
export interface MonitoringRepository {
  /** Lists current firing and suppressed alerts using supported provider filters. */
  listAlerts(filters?: MonitoringAlertFilters): Promise<MonitoringAlert[]>
  /** Creates an Alertmanager silence using exact label matchers. */
  acknowledgeAlert(input: AcknowledgeAlertInput): Promise<AcknowledgeAlertResult>
  /** Queries at most 200 servers for the backend's fixed named instant metrics. */
  getServerMetrics(serverIds: string[], metrics?: MetricName[]): Promise<ServerMetricsResult>
}
