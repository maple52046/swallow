import { Flex } from '@radix-ui/themes'
import { MonitoringCard } from '@/presentation/components/serverSummary/MonitoringCard'
import { useServerDetailContext } from './useServerDetail'
import { useServerMonitoring } from './useServerMonitoring'

/**
 * The server's Monitoring tab: its effective exporter owner and current metric values.
 *
 * Kept separate from the Summary tab so the projection view stays about identity and
 * hardware, while live metrics — which depend on a metrics backend and can be empty — have
 * their own place. Reads the loaded server from the detail outlet context, so switching to
 * this tab does not refetch the server, and loads only the metrics on mount.
 */
export function ServerMonitoringTab() {
  const { server } = useServerDetailContext()
  const monitoring = useServerMonitoring(server)

  return (
    <Flex direction="column" gap="4">
      <MonitoringCard monitoring={monitoring} />
    </Flex>
  )
}
