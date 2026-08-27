import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Content,
  FormGroup,
  FormSelect,
  FormSelectOption,
  Label,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  SearchInput,
  TextArea,
  ToolbarItem,
} from '@patternfly/react-core'
import { ExternalLinkAltIcon, SyncAltIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { Link, useSearchParams } from 'react-router-dom'
import { loadFleetMetrics, type FleetMetricsResult } from '@/application/usecases/monitoring/loadFleetMetrics'
import { loadServerWorkingSet } from '@/application/usecases/servers/loadServerWorkingSet'
import { useApp } from '@/di/AppProvider'
import {
  METRIC_DESCRIPTORS,
  type MonitoringAlert,
  type MonitoringAlertState,
  type ServerMetrics,
} from '@/domain/monitoring/types'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import { DataToolbar, SectionHeader, StatStrip, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
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

function severityColor(severity: string): 'red' | 'orange' | 'blue' | 'grey' {
  if (severity.toLowerCase() === 'critical') return 'red'
  if (severity.toLowerCase() === 'warning') return 'orange'
  if (severity.toLowerCase() === 'info') return 'blue'
  return 'grey'
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
    monitoring.listAlerts({ siteId, severity: severity || undefined, state: alertState })
      .then((alerts) => { if (!cancelled) { setAlertsState({ status: 'ready', alerts }); setRefreshedAt(currentIsoTime()) } })
      .catch((error: Error) => { if (!cancelled) setAlertsState({ status: 'error', message: error.message }) })
    return () => { cancelled = true }
  }, [alertState, monitoring, nonce, severity, siteId, siteScopeLoading])

  useEffect(() => {
    if (siteScopeLoading) return
    let cancelled = false
    setFleetState({ status: 'loading' })
    loadServerWorkingSet(serverRepository, { siteId, includeAbsent: false })
      .then(async (workingSet) => ({ servers: workingSet.servers, metrics: await loadFleetMetrics(monitoring, workingSet.servers.map((server) => server.id)) }))
      .then((data) => { if (!cancelled) { setFleetState({ status: 'ready', ...data }); setRefreshedAt(currentIsoTime()) } })
      .catch((error: Error) => { if (!cancelled) setFleetState({ status: 'error', message: error.message }) })
    return () => { cancelled = true }
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
    if (value) next.set(key, value); else next.delete(key)
    setSearchParams(next, { replace: true })
  }
  const refresh = () => setNonce((value) => value + 1)
  const openAcknowledge = (alert: MonitoringAlert) => { setSelectedAlert(alert); setDuration('4h'); setComment(''); setAckError(null) }
  const acknowledge = async () => {
    if (!selectedAlert || Object.keys(selectedAlert.labels).length === 0) return
    setAcknowledging(true); setAckError(null)
    try {
      const result = await monitoring.acknowledgeAlert({ fingerprint: selectedAlert.fingerprint, siteId: selectedAlert.siteId ?? siteId, matchers: selectedAlert.labels, duration, comment: comment.trim() || undefined })
      showToast({ title: 'Alert acknowledged', description: `Alertmanager silence ${result.silenceId} was created.`, tone: 'success' })
      setSelectedAlert(null); refresh()
    } catch (error) {
      setAckError(error instanceof Error ? error.message : 'Could not acknowledge alert')
    } finally { setAcknowledging(false) }
  }

  return <div className="operator-page">
    <PageHeader title="Monitoring" subtitle="Current fleet health, Alertmanager alerts, and instant server metrics." metadata={<Content component="small">Refreshed {formatRelative(refreshedAt)}</Content>} actions={<><Button variant="secondary" icon={<SyncAltIcon />} onClick={refresh}>Refresh</Button>{grafana && <Button component="a" href={grafana} target="_blank" rel="noreferrer" icon={<ExternalLinkAltIcon />} iconPosition="end">Open Grafana</Button>}</>} />
    <StatStrip items={[
      { label: 'Fleet servers', value: fleetState.status === 'ready' ? fleet.length : 'Unavailable', detail: `${healthUp} reporting up` },
      { label: 'Health down', value: fleetState.status === 'ready' ? healthDown : 'Unavailable', detail: `${Math.max(0, fleet.length - healthUp - healthDown)} unknown`, tone: healthDown ? 'critical' : 'neutral' },
      { label: 'Critical alerts', value: alertsState.status === 'ready' ? alerts.filter((alert) => alert.severity.toLowerCase() === 'critical' && alert.state === 'firing').length : 'Unavailable', tone: alerts.some((alert) => alert.severity.toLowerCase() === 'critical' && alert.state === 'firing') ? 'critical' : 'neutral' },
      { label: 'Warning alerts', value: alertsState.status === 'ready' ? alerts.filter((alert) => alert.severity.toLowerCase() === 'warning' && alert.state === 'firing').length : 'Unavailable', tone: alerts.some((alert) => alert.severity.toLowerCase() === 'warning' && alert.state === 'firing') ? 'warning' : 'neutral' },
    ]} />

    <section className="sw-section">
      <SectionHeader title="Alerts" description="Firing and suppressed alerts from Alertmanager, ordered by provider severity." />
      <DataToolbar><ToolbarItem><SearchInput value={alertQuery} onChange={(_event, value) => setAlertQuery(value)} onClear={() => setAlertQuery('')} placeholder="Search alerts or labels" aria-label="Search alerts" /></ToolbarItem><ToolbarItem><FormSelect value={severity} onChange={(_event, value) => setFilter('severity', value)} aria-label="Filter alert severity"><FormSelectOption value="" label="All severities" /><FormSelectOption value="critical" label="Critical" /><FormSelectOption value="warning" label="Warning" /><FormSelectOption value="info" label="Info" /></FormSelect></ToolbarItem><ToolbarItem><FormSelect value={alertState ?? ''} onChange={(_event, value) => setFilter('state', value)} aria-label="Filter alert state"><FormSelectOption value="" label="All states" /><FormSelectOption value="firing" label="Firing" /><FormSelectOption value="suppressed" label="Suppressed" /></FormSelect></ToolbarItem></DataToolbar>
      {alertsState.status === 'error' && <Alert variant={AlertVariant.warning} title="Alerts are unavailable" isInline>{alertsState.message}</Alert>}
      {alertsState.status === 'loading' && <div className="sw-section-empty">Loading alerts...</div>}
      {alertsState.status === 'ready' && visibleAlerts.length === 0 && <div className="sw-section-empty">No alerts match the current filters.</div>}
      {visibleAlerts.length > 0 && <StickyTableFrame><Table aria-label="Monitoring alerts" variant="compact"><Thead><Tr><Th>Alert</Th><Th>Severity</Th><Th>State</Th><Th>Resource</Th><Th>Started</Th><Th screenReaderText="Actions" /></Tr></Thead><Tbody>{visibleAlerts.map((alert) => <Tr key={alert.fingerprint}><Td dataLabel="Alert"><strong>{alert.name}</strong><small>{alert.summary || alert.description || 'No description'}</small></Td><Td dataLabel="Severity"><Label color={severityColor(alert.severity)}>{alert.severity || 'unknown'}</Label></Td><Td dataLabel="State"><StatusBadge status={alert.state} label={alert.state === 'suppressed' ? 'Acknowledged' : alert.state} /></Td><Td dataLabel="Resource">{alert.serverId ? <Link to={scopedHref(`/servers/${alert.serverId}/monitoring`)}>{alert.serverId}</Link> : alert.clusterId ? <Link to={scopedHref(`/clusters/${alert.clusterId}`)}>{alert.clusterId}</Link> : 'Fleet'}</Td><Td dataLabel="Started">{formatRelative(alert.startsAt ?? undefined)}</Td><Td isActionCell>{alert.state === 'firing' ? <Button variant="secondary" size="sm" onClick={() => openAcknowledge(alert)}>Acknowledge</Button> : <span className="sw-muted">Silenced</span>}</Td></Tr>)}</Tbody></Table></StickyTableFrame>}
    </section>

    <section className="sw-section">
      <SectionHeader title="Server metrics" description="Current named metrics only. Missing samples remain No data; history belongs in Grafana." />
      <DataToolbar><ToolbarItem><SearchInput value={serverQuery} onChange={(_event, value) => setServerQuery(value)} onClear={() => setServerQuery('')} placeholder="Search server or address" aria-label="Search server metrics" /></ToolbarItem></DataToolbar>
      {fleetState.status === 'error' && <Alert variant={AlertVariant.danger} title="Inventory is unavailable" isInline>{fleetState.message}</Alert>}
      {fleetState.status === 'loading' && <div className="sw-section-empty">Loading fleet metrics...</div>}
      {fleetState.status === 'ready' && fleetState.metrics.errors.length > 0 && <Alert variant={AlertVariant.warning} title="Some metric batches are unavailable" isInline>{`${fleetState.metrics.errors.length} batch request${fleetState.metrics.errors.length === 1 ? '' : 's'} failed. Other values remain current.`}</Alert>}
      {fleetState.status === 'ready' && visibleServers.length === 0 && <div className="sw-section-empty">No Servers match this scope or search.</div>}
      {visibleServers.length > 0 && <StickyTableFrame><Table aria-label="Server metrics" variant="compact" isStriped><Thead><Tr><Th>Server</Th><Th>Health</Th><Th>CPU usage</Th><Th>Memory used</Th><Th>GPU utilization</Th></Tr></Thead><Tbody>{visibleServers.map((server) => { const item = metricsByServer.get(server.id); return <Tr key={server.id}><Td dataLabel="Server"><Link to={scopedHref(`/servers/${server.id}/monitoring`)}><strong>{serverDisplayName(server)}</strong></Link><small>{serverPrimaryAddress(server) ?? server.id}</small></Td><Td dataLabel="Health"><StatusBadge status={server.health?.state ?? 'unknown'} /></Td><Td dataLabel="CPU usage">{metricValue(item, 'cpuUsagePercent')}</Td><Td dataLabel="Memory used">{metricValue(item, 'memoryUsedPercent')}</Td><Td dataLabel="GPU utilization">{server.gpus.length ? metricValue(item, 'gpuUtilizationPercent') : 'Not applicable'}</Td></Tr>})}</Tbody></Table></StickyTableFrame>}
    </section>

    <Modal isOpen={selectedAlert !== null} onClose={() => setSelectedAlert(null)} variant="small" aria-labelledby="acknowledge-alert-title">
      <ModalHeader title="Acknowledge alert" labelId="acknowledge-alert-title" description={selectedAlert ? `${selectedAlert.name}: ${selectedAlert.summary || selectedAlert.description}` : undefined} />
      <ModalBody>{ackError && <Alert variant={AlertVariant.danger} title="Acknowledgement failed" isInline>{ackError}</Alert>}<FormGroup label="Silence duration" isRequired fieldId="alert-duration"><FormSelect id="alert-duration" value={duration} onChange={(_event, value) => setDuration(value)}><FormSelectOption value="1h" label="1 hour" /><FormSelectOption value="4h" label="4 hours" /><FormSelectOption value="24h" label="24 hours" /><FormSelectOption value="168h" label="7 days" /></FormSelect></FormGroup><FormGroup label="Comment" fieldId="alert-comment"><TextArea id="alert-comment" value={comment} onChange={(_event, value) => setComment(value)} rows={3} /></FormGroup>{selectedAlert && Object.keys(selectedAlert.labels).length === 0 && <Alert variant={AlertVariant.warning} title="This alert has no labels and cannot be safely silenced." isInline />}</ModalBody>
      <ModalFooter><Button onClick={() => void acknowledge()} isLoading={acknowledging} isDisabled={acknowledging || !selectedAlert || Object.keys(selectedAlert.labels).length === 0}>Acknowledge</Button><Button variant="link" onClick={() => setSelectedAlert(null)}>Cancel</Button></ModalFooter>
    </Modal>
  </div>
}
