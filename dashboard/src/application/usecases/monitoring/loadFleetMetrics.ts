import type { MonitoringRepository } from '@/application/ports/MonitoringRepository'
import type { MetricName, ServerMetrics, ServerMetricsResult } from '@/domain/monitoring/types'

const MAX_SERVER_IDS = 200
const MAX_CONCURRENCY = 2

/** One failed metrics batch; other batches may still have returned useful values. */
export interface FleetMetricsError {
  serverIds: string[]
  message: string
}

/** Aggregated metrics preserve input order and describe any partial provider failure. */
export interface FleetMetricsResult extends ServerMetricsResult {
  errors: FleetMetricsError[]
}

function chunks(values: string[], size: number): string[][] {
  const result: string[][] = []
  for (let index = 0; index < values.length; index += size) result.push(values.slice(index, index + size))
  return result
}

/**
 * Queries a complete fleet using the API's 200-ID bound and a two-worker pool.
 * Successful batches remain available when another batch fails; absent values stay absent.
 */
export async function loadFleetMetrics(
  repository: MonitoringRepository,
  serverIds: string[],
  metrics?: MetricName[],
): Promise<FleetMetricsResult> {
  if (serverIds.length === 0) return { items: [], grafana: null, errors: [] }
  const batches = chunks(serverIds, MAX_SERVER_IDS)
  const successes: ServerMetricsResult[] = []
  const errors: FleetMetricsError[] = []
  let cursor = 0

  const worker = async () => {
    while (cursor < batches.length) {
      const index = cursor++
      const batch = batches[index]
      try {
        successes.push(await repository.getServerMetrics(batch, metrics))
      } catch (error) {
        errors.push({ serverIds: batch, message: error instanceof Error ? error.message : 'Metrics provider unavailable' })
      }
    }
  }

  await Promise.all(Array.from({ length: Math.min(MAX_CONCURRENCY, batches.length) }, () => worker()))
  const byServer = new Map<string, ServerMetrics>()
  successes.flatMap((result) => result.items).forEach((item) => byServer.set(item.serverId, item))
  return {
    items: serverIds.flatMap((serverId) => { const item = byServer.get(serverId); return item ? [item] : [] }),
    grafana: successes.find((result) => result.grafana)?.grafana ?? null,
    errors,
  }
}
