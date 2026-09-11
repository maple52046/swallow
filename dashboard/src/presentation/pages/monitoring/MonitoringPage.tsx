import { useEffect, useMemo, useState } from 'react'
import { Badge, Button, Field, Stack, Table, Text, Textarea } from '@chakra-ui/react'
import { ExternalLink, RefreshCw } from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'
import { loadFleetMetrics, type FleetMetricsResult } from '@/application/usecases/monitoring/loadFleetMetrics'
import { loadServerWorkingSet } from '@/application/usecases/servers/loadServerWorkingSet'
import { useApp } from '@/di/AppProvider'
import { METRIC_DESCRIPTORS, type MonitoringAlert, type MonitoringAlertState, type ServerMetrics } from '@/domain/monitoring/types'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import { DataToolbar, SectionHeader, StatStrip, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatRelative } from '@/shared/utils/time'

type AlertsState = { status: 'loading' } | { status: 'ready'; alerts: MonitoringAlert[] } | { status: 'error'; message: string }
type FleetState = { status: 'loading' } | { status: 'ready'; servers: Server[]; metrics: FleetMetricsResult } | { status: 'error'; message: string }

const metricByName = new Map(METRIC_DESCRIPTORS.map((descriptor) => [descriptor.name, descriptor]))

function metricValue(item: ServerMetrics | undefined, name: 'cpuUsagePercent' | 'memoryUsedPercent' | 'gpuUtilizationPercent'): string {
  const value = item?.metrics[name]
  return value === undefined ? 'No data' : metricByName.get(name)?.format(value) ?? String(value)
}

/** Alertmanager severity to a semantic colour family; text always carries the severity too. */
function severityColor(severity: string): 'red' | 'orange' | 'blue' | 'gray' {
  if (severity.toLowerCase() === 'critical') return 'red'
  if (severity.toLowerCase() === 'warning') return 'orange'
  if (severity.toLowerCase() === 'info') return 'blue'
  return 'gray'
}

const currentIsoTime = () => new Date(Date.now()).toISOString()

/** Fleet monitoring surface combining Alertmanager actions with bounded instant metrics. */
export function MonitoringPage() {
  const { monitoring, servers: serverRepository } = useApp()
  const { siteId, loading: siteScopeLoading, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [searchParams, setSearchParams] = useSearchParams()
  const [alertsState, setAlertsState] = useState<AlertsState>({ status: 'loading' })
  const [fleetState, setFleetState] = useState<FleetState>({ status: 'loading' })
  const [alertQuery, setAlertQuery] = useState('')
  const [serverQuery, setServerQuery] = useState('')
  const [selectedAlert, setSelectedAlert] = useState<MonitoringAlert | null>(null)
  const [duration, setDuration] = useState('4h')
  const [comment, setComment] = useState('')
  const [acknowledging, setAcknowledging] = useState(false)
  const [ackError, setAckError] = useState<string | null>(null)
  const [nonce, setNonce] = useState(0)
  const [refreshedAt, setRefreshedAt] = useState(currentIsoTime)
  const severity = searchParams.get('severity') ?? ''
  const stateValue = searchParams.get('state')
  const alertState: MonitoringAlertState | undefined = stateValue === 'firing' || stateValue === 'suppressed' ? stateValue : undefined

  useEffect(() => {
    if (siteScopeLoading) return
    let cancelled = false
    setAlertsState({ status: 'loading' })
    monitoring
      .listAlerts({ siteId, severity: severity || undefined, state: alertState })
      .then((alerts) => {
        if (!cancelled) {
          setAlertsState({ status: 'ready', alerts })
          setRefreshedAt(currentIsoTime())
        }
      })
      .catch((error: Error) => {
        if (!cancelled) setAlertsState({ status: 'error', message: error.message })
      })
    return () => {
      cancelled = true
    }
  }, [alertState, monitoring, nonce, severity, siteId, siteScopeLoading])

  useEffect(() => {
    if (siteScopeLoading) return
    let cancelled = false
    setFleetState({ status: 'loading' })
    loadServerWorkingSet(serverRepository, { siteId, includeAbsent: false })
      .then(async (workingSet) => ({ servers: workingSet.servers, metrics: await loadFleetMetrics(monitoring, workingSet.servers.map((server) => server.id)) }))
      .then((data) => {
        if (!cancelled) {
          setFleetState({ status: 'ready', ...data })
          setRefreshedAt(currentIsoTime())
        }
      })
      .catch((error: Error) => {
        if (!cancelled) setFleetState({ status: 'error', message: error.message })
      })
    return () => {
      cancelled = true
    }
  }, [monitoring, nonce, serverRepository, siteId, siteScopeLoading])

  const visibleAlerts = useMemo(() => {
    if (alertsState.status !== 'ready') return []
    const query = alertQuery.trim().toLowerCase()
    if (!query) return alertsState.alerts
    return alertsState.alerts.filter((alert) => [alert.name, alert.summary, alert.description, alert.serverId, ...Object.entries(alert.labels).flat()].some((value) => value?.toLowerCase().includes(query)))
  }, [alertQuery, alertsState])
  const visibleServers = useMemo(() => {
    if (fleetState.status !== 'ready') return []
    const query = serverQuery.trim().toLowerCase()
    if (!query) return fleetState.servers
    return fleetState.servers.filter((server) => [serverDisplayName(server), server.id, serverPrimaryAddress(server)].some((value) => value?.toLowerCase().includes(query)))
  }, [fleetState, serverQuery])
  const alerts = alertsState.status === 'ready' ? alertsState.alerts : []
  const fleet = fleetState.status === 'ready' ? fleetState.servers : []
  const healthUp = fleet.filter((server) => server.health?.state === 'up').length
  const healthDown = fleet.filter((server) => server.health?.state === 'down').length
  const metricsByServer = fleetState.status === 'ready' ? new Map(fleetState.metrics.items.map((item) => [item.serverId, item])) : new Map<string, ServerMetrics>()
  const grafana = fleetState.status === 'ready' ? fleetState.metrics.grafana : null

  const setFilter = (key: 'severity' | 'state', value: string) => {
    const next = new URLSearchParams(searchParams)
    if (value) next.set(key, value)
    else next.delete(key)
    setSearchParams(next, { replace: true })
  }
  const refresh = () => setNonce((value) => value + 1)
  const openAcknowledge = (alert: MonitoringAlert) => {
    setSelectedAlert(alert)
    setDuration('4h')
    setComment('')
    setAckError(null)
  }
  const acknowledge = async () => {
    if (!selectedAlert || Object.keys(selectedAlert.labels).length === 0) return
    setAcknowledging(true)
    setAckError(null)
    try {
      const result = await monitoring.acknowledgeAlert({ fingerprint: selectedAlert.fingerprint, siteId: selectedAlert.siteId ?? siteId, matchers: selectedAlert.labels, duration, comment: comment.trim() || undefined })
      showToast({ title: 'Alert acknowledged', description: `Alertmanager silence ${result.silenceId} was created.`, tone: 'success' })
      setSelectedAlert(null)
      refresh()
    } catch (error) {
      setAckError(error instanceof Error ? error.message : 'Could not acknowledge alert')
    } finally {
      setAcknowledging(false)
    }
  }

  const acknowledgeDisabled = acknowledging || !selectedAlert || Object.keys(selectedAlert.labels).length === 0

  return (
    <div className="operator-page">
      <PageHeader
        title="Monitoring"
        subtitle="Current fleet health, Alertmanager alerts, and instant server metrics."
        metadata={<Text as="span" fontSize="sm" color="fg.muted">Refreshed {formatRelative(refreshedAt)}</Text>}
        actions={
          <>
            <Button variant="outline" onClick={refresh}>
              <RefreshCw size={16} />
              Refresh
            </Button>
            {grafana && (
              <Button asChild colorPalette="brand">
                <a href={grafana} target="_blank" rel="noreferrer">
                  Open Grafana
                  <ExternalLink size={16} />
                </a>
              </Button>
            )}
          </>
        }
      />
      <StatStrip items={[
        { label: 'Fleet servers', value: fleetState.status === 'ready' ? fleet.length : 'Unavailable', detail: `${healthUp} reporting up` },
        { label: 'Health down', value: fleetState.status === 'ready' ? healthDown : 'Unavailable', detail: `${Math.max(0, fleet.length - healthUp - healthDown)} unknown`, tone: healthDown ? 'critical' : 'neutral' },
        { label: 'Critical alerts', value: alertsState.status === 'ready' ? alerts.filter((alert) => alert.severity.toLowerCase() === 'critical' && alert.state === 'firing').length : 'Unavailable', tone: alerts.some((alert) => alert.severity.toLowerCase() === 'critical' && alert.state === 'firing') ? 'critical' : 'neutral' },
        { label: 'Warning alerts', value: alertsState.status === 'ready' ? alerts.filter((alert) => alert.severity.toLowerCase() === 'warning' && alert.state === 'firing').length : 'Unavailable', tone: alerts.some((alert) => alert.severity.toLowerCase() === 'warning' && alert.state === 'firing') ? 'warning' : 'neutral' },
      ]} />

      <section className="sw-section-group">
        <SectionHeader variant="plain" title="Alerts" description="Firing and suppressed alerts from Alertmanager, ordered by provider severity." />
        <div className="sw-section">
          <DataToolbar>
            <SearchInput value={alertQuery} onChange={setAlertQuery} placeholder="Search alerts or labels" aria-label="Search alerts" />
            <Select
              value={severity}
              onChange={(value) => setFilter('severity', value)}
              aria-label="Filter alert severity"
              size="sm"
              width="auto"
              options={[
                { value: '', label: 'All severities' },
                { value: 'critical', label: 'Critical' },
                { value: 'warning', label: 'Warning' },
                { value: 'info', label: 'Info' },
              ]}
            />
            <Select
              value={alertState ?? ''}
              onChange={(value) => setFilter('state', value)}
              aria-label="Filter alert state"
              size="sm"
              width="auto"
              options={[
                { value: '', label: 'All states' },
                { value: 'firing', label: 'Firing' },
                { value: 'suppressed', label: 'Suppressed' },
              ]}
            />
          </DataToolbar>
          {alertsState.status === 'error' && <Alert status="warning" title="Alerts are unavailable">{alertsState.message}</Alert>}
          {alertsState.status === 'loading' && <div className="sw-section-empty">Loading alerts...</div>}
          {alertsState.status === 'ready' && visibleAlerts.length === 0 && <div className="sw-section-empty">No alerts match the current filters.</div>}
          {visibleAlerts.length > 0 && (
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Monitoring alerts">
                <Table.Header>
                  <Table.Row><Table.ColumnHeader>Alert</Table.ColumnHeader><Table.ColumnHeader>Severity</Table.ColumnHeader><Table.ColumnHeader>State</Table.ColumnHeader><Table.ColumnHeader>Resource</Table.ColumnHeader><Table.ColumnHeader>Started</Table.ColumnHeader><Table.ColumnHeader /></Table.Row>
                </Table.Header>
                <Table.Body>
                  {visibleAlerts.map((alert) => (
                    <Table.Row key={alert.fingerprint}>
                      <Table.Cell>
                        <strong>{alert.name}</strong>
                        <Text as="small" display="block" color="fg.muted">{alert.summary || alert.description || 'No description'}</Text>
                      </Table.Cell>
                      <Table.Cell><Badge colorPalette={severityColor(alert.severity)} variant="subtle">{alert.severity || 'unknown'}</Badge></Table.Cell>
                      <Table.Cell><StatusBadge status={alert.state} label={alert.state === 'suppressed' ? 'Acknowledged' : alert.state} /></Table.Cell>
                      <Table.Cell>{alert.serverId ? <Link to={scopedHref(`/servers/${alert.serverId}/monitoring`)}>{alert.serverId}</Link> : alert.platformId ? <Link to={scopedHref(`/platforms/${alert.platformId}`)}>{alert.platformId}</Link> : 'Fleet'}</Table.Cell>
                      <Table.Cell>{formatRelative(alert.startsAt ?? undefined)}</Table.Cell>
                      <Table.Cell textAlign="end">{alert.state === 'firing' ? <Button variant="outline" size="sm" onClick={() => openAcknowledge(alert)}>Acknowledge</Button> : <span className="sw-muted">Silenced</span>}</Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          )}
        </div>
      </section>

      <section className="sw-section-group">
        <SectionHeader variant="plain" title="Server metrics" description="Current named metrics only. Missing samples remain No data; history belongs in Grafana." />
        <div className="sw-section">
          <DataToolbar>
            <SearchInput value={serverQuery} onChange={setServerQuery} placeholder="Search server or address" aria-label="Search server metrics" />
          </DataToolbar>
          {fleetState.status === 'error' && <Alert status="error" title="Inventory is unavailable">{fleetState.message}</Alert>}
          {fleetState.status === 'loading' && <div className="sw-section-empty">Loading fleet metrics...</div>}
          {fleetState.status === 'ready' && fleetState.metrics.errors.length > 0 && <Alert status="warning" title="Some metric batches are unavailable">{`${fleetState.metrics.errors.length} batch request${fleetState.metrics.errors.length === 1 ? '' : 's'} failed. Other values remain current.`}</Alert>}
          {fleetState.status === 'ready' && visibleServers.length === 0 && <div className="sw-section-empty">No Servers match this scope or search.</div>}
          {visibleServers.length > 0 && (
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Server metrics">
                <Table.Header>
                  <Table.Row><Table.ColumnHeader>Server</Table.ColumnHeader><Table.ColumnHeader>Health</Table.ColumnHeader><Table.ColumnHeader>CPU usage</Table.ColumnHeader><Table.ColumnHeader>Memory used</Table.ColumnHeader><Table.ColumnHeader>GPU utilization</Table.ColumnHeader></Table.Row>
                </Table.Header>
                <Table.Body>
                  {visibleServers.map((server) => {
                    const item = metricsByServer.get(server.id)
                    return (
                      <Table.Row key={server.id}>
                        <Table.Cell>
                          <Link to={scopedHref(`/servers/${server.id}/monitoring`)}><strong>{serverDisplayName(server)}</strong></Link>
                          <Text as="small" display="block" color="fg.muted">{serverPrimaryAddress(server) ?? server.id}</Text>
                        </Table.Cell>
                        <Table.Cell><StatusBadge status={server.health?.state ?? 'unknown'} /></Table.Cell>
                        <Table.Cell>{metricValue(item, 'cpuUsagePercent')}</Table.Cell>
                        <Table.Cell>{metricValue(item, 'memoryUsedPercent')}</Table.Cell>
                        <Table.Cell>{server.gpus.length ? metricValue(item, 'gpuUtilizationPercent') : 'Not applicable'}</Table.Cell>
                      </Table.Row>
                    )
                  })}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          )}
        </div>
      </section>

      <Modal
        open={selectedAlert !== null}
        onClose={() => setSelectedAlert(null)}
        title="Acknowledge alert"
        description={selectedAlert ? `${selectedAlert.name}: ${selectedAlert.summary || selectedAlert.description}` : undefined}
        onSubmit={(event) => {
          event.preventDefault()
          void acknowledge()
        }}
        footer={
          <>
            <Button variant="ghost" onClick={() => setSelectedAlert(null)}>
              Cancel
            </Button>
            <Button type="submit" colorPalette="brand" loading={acknowledging} disabled={acknowledgeDisabled}>
              Acknowledge
            </Button>
          </>
        }
      >
        <Stack gap="4">
          {ackError && <Alert status="error" title="Acknowledgement failed">{ackError}</Alert>}
          <Field.Root required>
            <Field.Label>
              Silence duration <Field.RequiredIndicator />
            </Field.Label>
            <Select
              value={duration}
              onChange={setDuration}
              aria-label="Silence duration"
              options={[
                { value: '1h', label: '1 hour' },
                { value: '4h', label: '4 hours' },
                { value: '24h', label: '24 hours' },
                { value: '168h', label: '7 days' },
              ]}
            />
          </Field.Root>
          <Field.Root>
            <Field.Label>Comment</Field.Label>
            <Textarea value={comment} onChange={(event) => setComment(event.target.value)} rows={3} />
          </Field.Root>
          {selectedAlert && Object.keys(selectedAlert.labels).length === 0 && (
            <Alert status="warning" title="This alert has no labels and cannot be safely silenced." />
          )}
        </Stack>
      </Modal>
    </div>
  )
}
