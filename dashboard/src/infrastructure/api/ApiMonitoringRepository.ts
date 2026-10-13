import type { MonitoringRepository } from '@/application/ports/MonitoringRepository'
import {
  MetricsBackendUnavailableError,
  METRIC_DESCRIPTORS,
  type AcknowledgeAlertInput,
  type AcknowledgeAlertResult,
  type MetricName,
  type MetricRangeMode,
  type MetricRangeQuery,
  type MetricRangeResult,
  type MetricScope,
  type MetricSeries,
  type MonitoringAlert,
  type MonitoringAlertFilters,
  type ServerMetricsResult,
} from '@/domain/monitoring/types'
import { ApiRequestError, apiRequest } from './client'

/** Wire shape of `GET /monitoring/metrics/range` (server-metrics.md, "Usage History"). */
interface MetricRangeDto {
  scope: { kind: MetricScope['kind']; id: string | null; serverCount: number }
  mode: MetricRangeMode
  start: string
  end: string
  stepSeconds: number
  series: Array<{ metric: string; serverId: string | null; points: Array<{ timestamp: string; value: number }> }>
  grafana: string | null
}

const KNOWN_METRICS = new Set<string>(METRIC_DESCRIPTORS.map((descriptor) => descriptor.name))

/** Narrows a contract metric name; a name added by a newer backend is dropped rather than mislabelled. */
function isKnownMetric(name: string): name is MetricName {
  return KNOWN_METRICS.has(name)
}

/**
 * Re-raises `provider_unavailable` from a metric read as the domain's
 * `MetricsBackendUnavailableError`, keeping the backend's human message. Every other failure is
 * passed through unchanged.
 */
async function translateMetricsError<T>(request: Promise<T>): Promise<T> {
  try {
    return await request
  } catch (error) {
    if (error instanceof ApiRequestError && error.code === 'provider_unavailable') {
      throw new MetricsBackendUnavailableError(error.message)
    }
    throw error
  }
}

/** Builds the contract's single scope parameter for a usage-history request. */
function scopeParam(scope: MetricScope): [string, string] {
  switch (scope.kind) {
    case 'servers':
      return ['serverIds', scope.serverIds.join(',')]
    case 'pool':
      return ['poolId', scope.id]
    case 'zone':
      return ['zoneId', scope.id]
    case 'platform':
      return ['platformId', scope.id]
  }
}

/**
 * Maps the range DTO onto domain types: ISO 8601 timestamps become epoch milliseconds for the
 * charts, and series of metric names this build does not know are dropped.
 */
function toMetricRangeResult(dto: MetricRangeDto): MetricRangeResult {
  const series: MetricSeries[] = []
  for (const item of dto.series) {
    if (!isKnownMetric(item.metric)) continue
    series.push({
      metric: item.metric,
      serverId: item.serverId,
      points: item.points.map((point) => ({ time: Date.parse(point.timestamp), value: point.value })),
    })
  }
  return {
    scope: dto.scope,
    mode: dto.mode,
    start: Date.parse(dto.start),
    end: Date.parse(dto.end),
    stepSeconds: dto.stepSeconds,
    series,
    grafana: dto.grafana,
  }
}

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
    return translateMetricsError(apiRequest<ServerMetricsResult>(`/api/v1/monitoring/metrics?${query.toString()}`))
  }

  async getMetricRange(rangeQuery: MetricRangeQuery): Promise<MetricRangeResult> {
    const query = new URLSearchParams()
    const [scopeKey, scopeValue] = scopeParam(rangeQuery.scope)
    query.set(scopeKey, scopeValue)
    if (rangeQuery.metrics && rangeQuery.metrics.length > 0) query.set('metrics', rangeQuery.metrics.join(','))
    query.set('window', rangeQuery.window)
    if (rangeQuery.mode) query.set('mode', rangeQuery.mode)
    const dto = await translateMetricsError(apiRequest<MetricRangeDto>(`/api/v1/monitoring/metrics/range?${query.toString()}`))
    return toMetricRangeResult(dto)
  }
}
