import { MonitoringCard } from '@/presentation/components/serverSummary/MonitoringCard'
import { useServerDetailContext } from './useServerDetail'
import { useServerMonitoring } from './useServerMonitoring'

/** Instant metrics for the loaded Server; historical exploration remains in Grafana. */
export function ServerMonitoringTab() {
  const { server } = useServerDetailContext()
  return <MonitoringCard monitoring={useServerMonitoring(server)} />
}
