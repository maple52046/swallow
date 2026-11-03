import { Stack } from '@chakra-ui/react'
import { useSearchParams } from 'react-router-dom'
import { isMetricRangeWindow, METRIC_DESCRIPTORS, METRIC_NAMES, type MetricRangeWindow, type MetricScope } from '@/domain/monitoring/types'
import { MonitoringCard } from '@/presentation/components/serverSummary/MonitoringCard'
import { UsageCharts } from '@/presentation/components/monitoring/UsageCharts'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useMetricRange } from '@/presentation/hooks/useMetricRange'
import { useServerDetailContext } from './useServerDetail'
import { useServerMonitoring } from './useServerMonitoring'

/**
 * Server detail Monitoring tab: current values as a summary, then swallow's built-in usage
 * charts for this Server (CPU, memory, disk, and network: basic monitoring, the same for every
 * Server).
 *
 * The chart window is URL-owned (`?window=6h`) so a link reproduces the view; an unknown value
 * falls back to the 1-hour default. History comes from the range API in aggregate mode, which
 * for a one-Server scope is that Server's own series.
 */
export function ServerMonitoringTab() {
  const { server } = useServerDetailContext()
  const monitoring = useServerMonitoring(server)
  const { scopedHref } = useSiteScope()
  const [searchParams, setSearchParams] = useSearchParams()
  const requestedWindow = searchParams.get('window')
  const window: MetricRangeWindow = isMetricRangeWindow(requestedWindow) ? requestedWindow : '1h'
  const scope: MetricScope = { kind: 'servers', serverIds: [server.id] }
  const usage = useMetricRange(scope, METRIC_NAMES, window)

  const changeWindow = (next: MetricRangeWindow) => {
    const params = new URLSearchParams(searchParams)
    if (next === '1h') params.delete('window')
    else params.set('window', next)
    setSearchParams(params, { replace: true })
  }

  return (
    <Stack gap="4">
      <MonitoringCard monitoring={monitoring} />
      <UsageCharts
        description="This Server's usage over time, read from the Site's metrics backend."
        state={usage}
        descriptors={METRIC_DESCRIPTORS}
        window={window}
        onWindowChange={changeWindow}
        integrationsHref={scopedHref('/infrastructure/integrations')}
      />
    </Stack>
  )
}
