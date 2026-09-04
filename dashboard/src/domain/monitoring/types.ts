/** Provider-owned alert and instant-metrics values used by the Monitoring console. */

/** Alertmanager state exposed by the active alerts contract. */
export type MonitoringAlertState = 'firing' | 'suppressed' | 'unknown'

/** One Alertmanager alert with optional Swallow resource correlation. */
export interface MonitoringAlert {
  /** Alertmanager fingerprint. It is opaque and cannot be used as a silence matcher. */
  fingerprint: string
  name: string
  severity: string
  state: MonitoringAlertState
  summary: string
  description: string
  /** Exact labels that can be sent back as Alertmanager silence matchers. */
  labels: Record<string, string>
  startsAt: string | null
  serverId: string | null
  siteId: string | null
  platformId: string | null
}

/** Server-side alert filters supported by the active API contract. */
export interface MonitoringAlertFilters {
  siteId?: string
  serverId?: string
  severity?: string
  state?: MonitoringAlertState
}

/** Input for creating a bounded Alertmanager silence. */
export interface AcknowledgeAlertInput {
  fingerprint: string
  siteId?: string
  matchers: Record<string, string>
  /** Positive Go duration such as `4h`. */
  duration?: string
  comment?: string
}

/** Alertmanager silence identity returned after acknowledgement. */
export interface AcknowledgeAlertResult {
  silenceId: string
}

/** The metric names the backend can answer, matching `GET /monitoring/metrics/names`. */
export type MetricName =
  | 'cpuUsagePercent'
  | 'memoryUsedPercent'
  | 'gpuUtilizationPercent'
  | 'gpuTemperatureCelsius'
  | 'gpuPowerWatts'
  | 'gpuMemoryUsedPercent'

/**
 * One server's metric values. Missing metrics are absent rather than zero because
 * "no data" and "zero" are different operating facts.
 */
export interface ServerMetrics {
  serverId: string
  metrics: Partial<Record<MetricName, number>>
}

/** Per-server current values plus the exact provider-returned Grafana deep link. */
export interface ServerMetricsResult {
  items: ServerMetrics[]
  grafana: string | null
}

/** Presentation metadata shared by single-server and fleet metric tables. */
export interface MetricDescriptor {
  name: MetricName
  label: string
  gpuOnly: boolean
  format: (value: number) => string
}

const percent = (value: number): string => `${value.toFixed(1)}%`

/** Fixed named metrics in operator scan order. */
export const METRIC_DESCRIPTORS: MetricDescriptor[] = [
  { name: 'cpuUsagePercent', label: 'CPU usage', gpuOnly: false, format: percent },
  { name: 'memoryUsedPercent', label: 'Memory used', gpuOnly: false, format: percent },
  { name: 'gpuUtilizationPercent', label: 'GPU utilization', gpuOnly: true, format: percent },
  { name: 'gpuMemoryUsedPercent', label: 'GPU memory used', gpuOnly: true, format: percent },
  { name: 'gpuTemperatureCelsius', label: 'GPU temperature', gpuOnly: true, format: (value) => `${Math.round(value)} °C` },
  { name: 'gpuPowerWatts', label: 'GPU power', gpuOnly: true, format: (value) => `${Math.round(value)} W` },
]
