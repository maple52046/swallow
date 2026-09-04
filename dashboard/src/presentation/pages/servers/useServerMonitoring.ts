import { useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Platform } from '@/domain/platform/types'
import {
  isGpuServer,
  resolveExporterOwner,
  type EffectiveExporterOwner,
} from '@/domain/monitoring/exporterOwner'
import { METRIC_DESCRIPTORS, type MetricName, type ServerMetrics } from '@/domain/monitoring/types'
import type { Server } from '@/domain/server/types'

/** The monitoring view for one server: its effective owner, current metrics, and links. */
export interface ServerMonitoring {
  /** Which subsystem owns this host's exporters, or `unmanaged` (locked / operator-run). */
  owner: EffectiveExporterOwner
  /** Whether GPU metrics apply to this server. */
  gpu: boolean
  /** Current metric values; a metric with no data is absent from the map. */
  metrics: Partial<Record<MetricName, number>>
  /** Grafana base URL to deep-link into, or null when unconfigured. */
  grafana: string | null
  /**
   * A metrics-backend problem, e.g. none registered. Surfaced as a notice rather than a
   * page error: the metric cards still render empty so the absence is explicit.
   */
  metricsError: string | null
  loading: boolean
}

/**
 * Loads a server's monitoring view: its effective exporter owner and its current metric
 * values.
 *
 * The owner is resolved the same way the backend does — locked hosts are `unmanaged`, a
 * platform member follows its platform policy, everything else is `ansible` — fetching the
 * one platform only when the server is a member and unlocked. Metrics failure (for example
 * no metrics backend registered) degrades to `metricsError` with empty values rather than
 * failing, so the UI can show "no data" instead of an error.
 */
export function useServerMonitoring(server: Server): ServerMonitoring {
  const { monitoring, platforms } = useApp()
  const gpu = isGpuServer(server)
  const [state, setState] = useState<ServerMonitoring>({
    owner: 'ansible',
    gpu,
    metrics: {},
    grafana: null,
    metricsError: null,
    loading: true,
  })

  useEffect(() => {
    let cancelled = false

    const resolveOwner = async (): Promise<EffectiveExporterOwner> => {
      if (server.provisioning?.locked) return 'unmanaged'
      const platformId = server.membership?.platformId
      if (!platformId) return 'ansible'
      const platform = await platforms.getPlatform(platformId).catch(() => null)
      const byId = new Map<string, Platform>()
      if (platform) byId.set(platform.id, platform)
      return resolveExporterOwner(server, byId)
    }

    // Only ask for the metrics that apply to this server, so a CPU server does not query
    // GPU series it can never have.
    const wanted = METRIC_DESCRIPTORS.filter((d) => gpu || !d.gpuOnly).map((d) => d.name)

    const loadMetrics = monitoring.getServerMetrics([server.id], wanted).then(
      (result) => {
        const item = result.items.find((entry: ServerMetrics) => entry.serverId === server.id)
        return { metrics: item?.metrics ?? {}, grafana: result.grafana, metricsError: null as string | null }
      },
      (err: Error) => ({ metrics: {}, grafana: null as string | null, metricsError: err.message }),
    )

    Promise.all([resolveOwner(), loadMetrics])
      .then(([owner, m]) => {
        if (cancelled) return
        setState({ owner, gpu, metrics: m.metrics, grafana: m.grafana, metricsError: m.metricsError, loading: false })
      })
      .catch(() => {
        if (!cancelled) setState((prev) => ({ ...prev, loading: false }))
      })

    return () => {
      cancelled = true
    }
  }, [server, monitoring, platforms, gpu])

  return state
}
