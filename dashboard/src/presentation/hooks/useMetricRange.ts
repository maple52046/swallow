import { useEffect, useMemo, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import {
  MetricsBackendUnavailableError,
  type MetricName,
  type MetricRangeResult,
  type MetricRangeWindow,
  type MetricScope,
} from '@/domain/monitoring/types'

/**
 * Usage-history state for one chart group.
 *
 * - `idle`: no scope chosen yet (for example no Servers selected); nothing was requested.
 * - `loading`: a request for the current scope and window is in flight.
 * - `unavailable`: the Site has no reachable metrics backend; the view asks the operator to
 *   register a metrics Integration rather than implying the Servers are down.
 * - `error`: any other failure (unknown grouping, rejected request), with the backend message.
 * - `ready`: the answer for the current scope and window.
 */
export type MetricRangeState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'unavailable'; message: string }
  | { status: 'error'; message: string }
  | { status: 'ready'; result: MetricRangeResult }

/** Explicit Servers beyond the contract bound are refused locally instead of sending a doomed request. */
const MAX_EXPLICIT_SERVERS = 200

/** The settled outcome of one request, remembered with the key of the request it answers. */
interface SettledRange {
  key: string
  state: Extract<MetricRangeState, { status: 'ready' | 'unavailable' | 'error' }>
}

/**
 * Loads aggregate usage history for a scope through the MonitoringRepository port.
 *
 * The request is keyed by the scope's value (not its object identity), the metric list, the
 * window, and `refreshKey`, so callers may rebuild those values every render without refetching;
 * bumping `refreshKey` forces a reload. A `null` scope, or an explicit Server scope with no ids,
 * is `idle`, and more than 200 explicit Servers is refused locally as `error`.
 *
 * `loading` is derived rather than stored: the hook remembers the last settled answer together
 * with its request key, and any newer key reads as loading until its own answer arrives. So a
 * slow answer for a previous scope or window can never be shown for the current one.
 */
export function useMetricRange(
  scope: MetricScope | null,
  metrics: readonly MetricName[],
  window: MetricRangeWindow,
  refreshKey = 0,
): MetricRangeState {
  const { monitoring } = useApp()
  const scopeKey = scope ? JSON.stringify(scope) : ''
  const metricsKey = metrics.join(',')
  const request = useMemo(() => {
    if (!scopeKey) return null
    const parsedScope = JSON.parse(scopeKey) as MetricScope
    if (parsedScope.kind === 'servers' && parsedScope.serverIds.length === 0) return null
    return { scope: parsedScope, metrics: metricsKey ? (metricsKey.split(',') as MetricName[]) : undefined }
  }, [scopeKey, metricsKey])
  const tooMany = request?.scope.kind === 'servers' && request.scope.serverIds.length > MAX_EXPLICIT_SERVERS
  const requestKey = request && !tooMany ? `${scopeKey}|${metricsKey}|${window}|${refreshKey}` : ''
  const [settled, setSettled] = useState<SettledRange | null>(null)

  // Fetches whenever the request key changes. The cleanup marks the in-flight request stale, and
  // every answer is stored with its own key, so only the current request's answer is shown.
  useEffect(() => {
    if (!request || !requestKey) return
    let cancelled = false
    monitoring
      .getMetricRange({ scope: request.scope, metrics: request.metrics, window })
      .then((result) => {
        if (!cancelled) setSettled({ key: requestKey, state: { status: 'ready', result } })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message = error instanceof Error ? error.message : 'Usage history could not be loaded.'
        setSettled({
          key: requestKey,
          state: error instanceof MetricsBackendUnavailableError ? { status: 'unavailable', message } : { status: 'error', message },
        })
      })
    return () => {
      cancelled = true
    }
  }, [monitoring, request, requestKey, window])

  if (!request) return { status: 'idle' }
  if (tooMany) {
    return { status: 'error', message: `Chart at most ${MAX_EXPLICIT_SERVERS} selected Servers at once, or choose a Pool, Zone, or Platform.` }
  }
  return settled?.key === requestKey ? settled.state : { status: 'loading' }
}
