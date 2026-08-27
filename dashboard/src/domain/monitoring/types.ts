/**
 * Per-server monitoring metrics read from the backend's fixed named-query set.
 *
 * The backend evaluates a bounded set of PromQL queries against the metrics store and
 * returns instant values keyed by metric name. swallow stores no metrics; these are
 * point-in-time reads, and historical exploration is Grafana's job (see `grafanaUrl`).
 *
 * See the api-server `server-metrics` contract and
 * docs/decisions/003-metrics-label-contract.md.
 */

/** The metric names the backend can answer, matching `GET /monitoring/metrics/names`. */
export type MetricName =
  | 'cpuUsagePercent'
  | 'memoryUsedPercent'
  | 'gpuUtilizationPercent'
  | 'gpuTemperatureCelsius'
  | 'gpuPowerWatts'
  | 'gpuMemoryUsedPercent'

/**
 * One server's metric values. A metric the store had no answer for is **absent** from the
 * map rather than zero, because "no data" and "zero" are different facts: render a missing
 * value as "no data", never as `0`.
 */
export interface ServerMetrics {
  serverId: string
  metrics: Partial<Record<MetricName, number>>
}

/** The metrics endpoint result: per-server values plus an optional Grafana deep link. */
export interface ServerMetricsResult {
  items: ServerMetrics[]
  /** Grafana base URL to explore beyond the fixed query set, or null when unconfigured. */
  grafana: string | null
}

/** Display metadata for a metric: how to label and format its value. Pure presentation-free. */
export interface MetricDescriptor {
  name: MetricName
  label: string
  /** True for GPU-only metrics, so a CPU server does not show empty GPU cards. */
  gpuOnly: boolean
  /** Formats a raw value into a human string with its unit. */
  format: (value: number) => string
}

const percent = (value: number): string => `${value.toFixed(1)}%`

/**
 * The metrics shown on a server, in display order. Host metrics first, then GPU metrics.
 * A single source so the card set and any future consumer stay consistent.
 */
export const METRIC_DESCRIPTORS: MetricDescriptor[] = [
  { name: 'cpuUsagePercent', label: 'CPU usage', gpuOnly: false, format: percent },
  { name: 'memoryUsedPercent', label: 'Memory used', gpuOnly: false, format: percent },
  { name: 'gpuUtilizationPercent', label: 'GPU utilization', gpuOnly: true, format: percent },
  { name: 'gpuMemoryUsedPercent', label: 'GPU memory used', gpuOnly: true, format: percent },
  { name: 'gpuTemperatureCelsius', label: 'GPU temperature', gpuOnly: true, format: (v) => `${Math.round(v)} °C` },
  { name: 'gpuPowerWatts', label: 'GPU power', gpuOnly: true, format: (v) => `${Math.round(v)} W` },
]
