/**
 * Provider-owned alert, current-value, and usage-history values used by the Monitoring console
 * and the Server detail Monitoring tab. swallow stores none of these; they mirror the
 * `server-metrics.md` and `monitoring-alerts.md` contracts.
 */
import { formatBytes } from '@/shared/utils/bytes'

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
  | 'diskUsedPercent'
  | 'networkReceiveBytesPerSecond'
  | 'networkTransmitBytesPerSecond'
  | 'gpuUtilizationPercent'
  | 'gpuTemperatureCelsius'
  | 'gpuPowerWatts'
  | 'gpuMemoryUsedPercent'

/**
 * Unit of a named metric. `percent` values live on a fixed 0–100 scale; the others are open-ended
 * and a chart scales them to the data.
 */
export type MetricUnit = 'percent' | 'celsius' | 'watts' | 'bytesPerSecond'

/**
 * One server's metric values. Missing metrics are absent rather than zero because
 * "no data" and "zero" are different operating facts.
 */
export interface ServerMetrics {
  serverId: string
  metrics: Partial<Record<MetricName, number>>
}

/** Per-server current values plus the optional provider-returned Grafana link. */
export interface ServerMetricsResult {
  items: ServerMetrics[]
  grafana: string | null
}

/**
 * Presentation metadata shared by current-value tiles, fleet tables, and usage charts, so one
 * metric reads the same everywhere it appears.
 */
export interface MetricDescriptor {
  name: MetricName
  label: string
  /** GPU metrics only apply to GPU servers; CPU-only views skip them rather than show "no data". */
  gpuOnly: boolean
  unit: MetricUnit
  format: (value: number) => string
}

const percent = (value: number): string => `${value.toFixed(1)}%`

/** Bytes per second with binary units; zero is valid traffic and must not be treated as unknown. */
const bytesPerSecond = (value: number): string => (value > 0 ? `${formatBytes(value)}/s` : '0 B/s')

/** Fixed named metrics in operator scan order: host resources first, then GPU. */
export const METRIC_DESCRIPTORS: MetricDescriptor[] = [
  { name: 'cpuUsagePercent', label: 'CPU usage', gpuOnly: false, unit: 'percent', format: percent },
  { name: 'memoryUsedPercent', label: 'Memory used', gpuOnly: false, unit: 'percent', format: percent },
  { name: 'diskUsedPercent', label: 'Disk used (/)', gpuOnly: false, unit: 'percent', format: percent },
  { name: 'networkReceiveBytesPerSecond', label: 'Network received', gpuOnly: false, unit: 'bytesPerSecond', format: bytesPerSecond },
  { name: 'networkTransmitBytesPerSecond', label: 'Network sent', gpuOnly: false, unit: 'bytesPerSecond', format: bytesPerSecond },
  { name: 'gpuUtilizationPercent', label: 'GPU utilization', gpuOnly: true, unit: 'percent', format: percent },
  { name: 'gpuMemoryUsedPercent', label: 'GPU memory used', gpuOnly: true, unit: 'percent', format: percent },
  { name: 'gpuTemperatureCelsius', label: 'GPU temperature', gpuOnly: true, unit: 'celsius', format: (value) => `${Math.round(value)} °C` },
  { name: 'gpuPowerWatts', label: 'GPU power', gpuOnly: true, unit: 'watts', format: (value) => `${Math.round(value)} W` },
]

/** Usage-history windows the range contract accepts, each ending at the request time. */
export type MetricRangeWindow = '1h' | '6h' | '24h' | '7d'

/** Window choices in display order; `1h` is the default view. */
export const METRIC_RANGE_WINDOWS: ReadonlyArray<{ value: MetricRangeWindow; label: string }> = [
  { value: '1h', label: '1 hour' },
  { value: '6h', label: '6 hours' },
  { value: '24h', label: '24 hours' },
  { value: '7d', label: '7 days' },
]

/** Narrows an untrusted value (for example a URL parameter) to a supported window. */
export function isMetricRangeWindow(value: string | null | undefined): value is MetricRangeWindow {
  return METRIC_RANGE_WINDOWS.some((window) => window.value === value)
}

/**
 * What a usage chart covers. A grouping is resolved by the backend to its **current** members,
 * so a Pool's chart is the history of the Servers in it now; explicit Servers are capped at 200
 * by the contract.
 */
export type MetricScope =
  | { kind: 'servers'; serverIds: string[] }
  | { kind: 'pool'; id: string }
  | { kind: 'zone'; id: string }
  | { kind: 'platform'; id: string }

/** `aggregate` combines the scope per metric; `perServer` keeps each Server's series (≤ 50). */
export type MetricRangeMode = 'aggregate' | 'perServer'

/** One usage-history request. */
export interface MetricRangeQuery {
  scope: MetricScope
  /** Requested names; omitted means every name. */
  metrics?: MetricName[]
  window: MetricRangeWindow
  mode?: MetricRangeMode
}

/** One point of a series; `time` is epoch milliseconds, mapped from the contract's ISO 8601. */
export interface MetricPoint {
  time: number
  value: number
}

/**
 * One metric's history for the whole scope (`serverId` null) or for one Server. Points are in
 * ascending time order; missing timestamps are gaps (no data), never zero.
 */
export interface MetricSeries {
  metric: MetricName
  serverId: string | null
  points: MetricPoint[]
}

/**
 * A usage-history answer. A metric with no data has no entry in `series`, so a chart renders
 * "No data" instead of a flat line. `start`/`end` are epoch milliseconds of the evaluated range.
 */
export interface MetricRangeResult {
  scope: { kind: MetricScope['kind']; id: string | null; serverCount: number }
  mode: MetricRangeMode
  start: number
  end: number
  stepSeconds: number
  series: MetricSeries[]
  /** Optional advanced-exploration link; charts never depend on it. */
  grafana: string | null
}

/**
 * The Site has no reachable metrics backend (no metrics Integration, or it cannot be reached).
 *
 * The adapter raises it for the contract's `provider_unavailable` so screens can tell the
 * operator to register a metrics Integration instead of implying the Servers are down. It is a
 * domain error so presentation never inspects transport error codes.
 */
export class MetricsBackendUnavailableError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'MetricsBackendUnavailableError'
  }
}
