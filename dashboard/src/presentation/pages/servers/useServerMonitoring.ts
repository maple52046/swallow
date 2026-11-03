import { useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import {
  MetricsBackendUnavailableError,
  type MetricName,
  type ServerMetrics,
} from '@/domain/monitoring/types'
import type { Server } from '@/domain/server/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'

/** The monitoring view for one server: its current metrics and links. */
export interface ServerMonitoring {
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

const LOADING: ServerMonitoring = {
  metrics: {},
  grafana: null,
  metricsError: null,
  metricsUnavailable: false,
  loading: true,
}

/**
 * Loads a server's current metric values from the Site metrics backend: every named metric, since
 * each is basic monitoring and applies to every Server.
 *
 * Metrics failure degrades to `metricsUnavailable` (no backend registered) or `metricsError`
 * with empty values rather than failing, so the UI can show "no data" instead of an error. The
 * request is re-issued when the Server changes; a superseded answer is ignored.
 */
export function useServerMonitoring(server: Server): ServerMonitoring {
  const { monitoring } = useApp()
  const [state, setState] = useState<ServerMonitoring>(LOADING)

  useEffect(() => {
    let cancelled = false

    // Both outcomes settle into state, so the chain never rejects and needs no await.
    void monitoring.getServerMetrics([server.id]).then(
      (result) => {
        if (cancelled) return
        const item = result.items.find((entry: ServerMetrics) => entry.serverId === server.id)
        setState({
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
  }, [server, monitoring])

  return state
}
