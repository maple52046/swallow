import { useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import {
  MetricsBackendUnavailableError,
  METRIC_DESCRIPTORS,
  type MetricName,
  type ServerMetrics,
} from '@/domain/monitoring/types'
import { isGpuServer } from '@/domain/server/gpu'
import type { Server } from '@/domain/server/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'

/** The monitoring view for one server: its current metrics and links. */
export interface ServerMonitoring {
  /** Whether GPU metrics apply to this server. */
  gpu: boolean
  /** Current metric values; a metric with no data is absent from the map. */
  metrics: Partial<Record<MetricName, number>>
  /** Optional Grafana base URL for advanced exploration, or null when unconfigured. */
  grafana: string | null
  /**
   * A metrics-backend problem other than "no backend registered", with its request ID. Surfaced
   * as a notice rather than a page error: the metric cards still render empty so the absence is
   * explicit.
   */
  metricsError: DisplayedFailure | null
  /**
   * The Site has no reachable metrics backend. The usage charts below explain how to register
   * one, so the current-value panel does not repeat the warning.
   */
  metricsUnavailable: boolean
  loading: boolean
}

/**
 * Loads a server's current metric values from the Site metrics backend.
 *
 * Metrics failure degrades to `metricsUnavailable` (no backend registered) or `metricsError`
 * with empty values rather than failing, so the UI can show "no data" instead of an error.
 */
export function useServerMonitoring(server: Server): ServerMonitoring {
  const { monitoring } = useApp()
  const gpu = isGpuServer(server)
  const [state, setState] = useState<ServerMonitoring>({
    gpu,
    metrics: {},
    grafana: null,
    metricsError: null,
    metricsUnavailable: false,
    loading: true,
  })

  useEffect(() => {
    let cancelled = false

    // Only ask for the metrics that apply to this server, so a CPU server does not query
    // GPU series it can never have.
    const wanted = METRIC_DESCRIPTORS.filter((d) => gpu || !d.gpuOnly).map((d) => d.name)

    // Both outcomes settle into state, so the chain never rejects and needs no await.
    void monitoring.getServerMetrics([server.id], wanted).then(
      (result) => {
        if (cancelled) return
        const item = result.items.find((entry: ServerMetrics) => entry.serverId === server.id)
        setState({
          gpu,
          metrics: item?.metrics ?? {},
          grafana: result.grafana,
          metricsError: null,
          metricsUnavailable: false,
          loading: false,
        })
      },
      (err: Error) => {
        if (cancelled) return
        setState({
          gpu,
          metrics: {},
          grafana: null,
          metricsError: err instanceof MetricsBackendUnavailableError ? null : failureOf(err, 'Metrics could not be read.'),
          metricsUnavailable: err instanceof MetricsBackendUnavailableError,
          loading: false,
        })
      },
    )

    return () => {
      cancelled = true
    }
  }, [server, monitoring, gpu])

  return state
}
