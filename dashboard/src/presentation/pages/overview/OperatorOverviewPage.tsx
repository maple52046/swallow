import { useEffect, useMemo, useState } from 'react'
import { Badge, Table, Text } from '@chakra-ui/react'
import { BellRing, Cpu, Server, Workflow } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Overview } from '@/domain/overview/types'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { MetricGrid, SectionSurface, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatRelative } from '@/shared/utils/time'

type OverviewState =
  | { status: 'loading' }
  | { status: 'ready'; data: Overview }
  | { status: 'error'; message: string }

interface AttentionItem {
  key: string
  priority: number
  kind: string
  title: string
  detail: string
  href: string
  action: string
}

/** Builds one severity-ordered action queue without inventing state outside the overview aggregate. */
function attentionItems(data: Overview): AttentionItem[] {
  const items: AttentionItem[] = data.monitoring.firing.items.map((alert) => ({
    key: `alert-${alert.fingerprint}`,
    priority: alert.severity === 'critical' ? 0 : 1,
    kind: alert.severity,
    title: alert.summary || alert.description || alert.name,
    detail: alert.name,
    href: alert.serverId ? `/servers/${alert.serverId}/monitoring` : '/monitoring',
    action: alert.serverId ? 'Open server' : 'View alert',
  }))

  data.integrations.items
    .filter((item) => item.lastError)
    .forEach((item) => items.push({
      key: `integration-${item.id}`,
      priority: 2,
      kind: 'integration',
      title: `${item.name} sync failed`,
      detail: item.lastError ?? 'Refresh failed',
      href: `/infrastructure/integrations#${item.id}`,
      action: 'Manage',
    }))

  if (data.platforms.unreachable) {
    items.push({
      key: 'platform-unreachable',
      priority: 2,
      kind: 'platform',
      title: `${data.platforms.unreachable} platform${data.platforms.unreachable === 1 ? '' : 's'} unreachable`,
      detail: 'Membership unavailable',
      href: '/platforms',
      action: 'Review',
    })
  }
  if (data.platforms.unmatchedMembers) {
    items.push({
      key: 'platform-unmatched',
      priority: 3,
      kind: 'membership',
      title: `${data.platforms.unmatchedMembers} unmatched member${data.platforms.unmatchedMembers === 1 ? '' : 's'}`,
      detail: 'Review membership mapping',
      href: '/platforms',
      action: 'Review',
    })
  }
  if (data.inventory.health.down) {
    items.push({
      key: 'servers-down',
      priority: 1,
      kind: 'health',
      title: `${data.inventory.health.down} server${data.inventory.health.down === 1 ? '' : 's'} down`,
      detail: 'Metrics report unavailable hosts',
      href: '/servers',
      action: 'Open servers',
    })
  }
  if (data.operations.failedLast24Hours) {
    items.push({
      key: 'operations-failed',
      priority: 2,
      kind: 'workflow',
      title: `${data.operations.failedLast24Hours} workflow${data.operations.failedLast24Hours === 1 ? '' : 's'} failed today`,
      detail: 'Review the failure before retrying',
      href: '/workflows?status=failed',
      action: 'Open workflow',
    })
  }

  return items.sort((left, right) => left.priority - right.priority).slice(0, 10)
}

/**
 * Operator landing page backed by the provider-owned overview aggregate.
 *
 * The page groups existing facts into four scan targets and derives only an
 * action queue; it never treats missing metrics as failure or invents trends.
 */
export function OperatorOverviewPage() {
  const { overview } = useApp()
  const { siteId, scopedHref } = useSiteScope()
  const [state, setState] = useState<OverviewState>({ status: 'loading' })

  // A Site change invalidates the aggregate. Ignore late responses so an older
  // scope cannot overwrite the active one after rapid menu navigation.
  useEffect(() => {
    let cancelled = false
    overview.getOverview(siteId)
      .then((data) => {
        if (!cancelled) setState({ status: 'ready', data })
      })
      .catch((error: Error) => {
        if (!cancelled) setState({ status: 'error', message: error.message })
      })
    return () => { cancelled = true }
  }, [overview, siteId])

  const attention = useMemo(() => state.status === 'ready' ? attentionItems(state.data) : [], [state])

  if (state.status === 'loading') return <LoadingState />
  if (state.status === 'error') return <ErrorState message={state.message} />

  const data = state.data
  const firingAlerts = data.monitoring.firing.critical + data.monitoring.firing.warning

  return (
    <div className="operator-page">
      <PageHeader
        title="Overview"
        metadata={<Text as="span" fontSize="sm" color="fg.muted">Updated {formatRelative(data.generatedAt)}</Text>}
      />
      {!data.monitoring.available && (
        <Alert status="warning" title="Monitoring unavailable">
          Inventory and workflows are still current.
        </Alert>
      )}
      <MetricGrid items={[
        {
          label: 'Fleet health',
          value: `${data.inventory.health.up} / ${data.inventory.servers}`,
          detail: `${data.inventory.health.down} down · ${data.inventory.health.unknown} unknown · ${data.inventory.absent} absent`,
          tone: data.inventory.health.down ? 'critical' : 'success',
          icon: <Server size={16} />,
        },
        {
          label: 'Capacity',
          value: `${data.inventory.gpuDevices} GPUs`,
          detail: `${data.inventory.deployed} deployed · ${data.inventory.platformed} platformed · ${data.inventory.sites} sites`,
          icon: <Cpu size={16} />,
        },
        {
          label: 'Workflows',
          value: `${data.operations.active} active`,
          detail: `${data.operations.failedLast24Hours} failed today`,
          tone: data.operations.failedLast24Hours ? 'warning' : 'neutral',
          icon: <Workflow size={16} />,
        },
        {
          label: 'Alerts',
          value: firingAlerts,
          detail: `${data.monitoring.firing.critical} critical`,
          tone: data.monitoring.firing.critical ? 'critical' : firingAlerts ? 'warning' : 'neutral',
          icon: <BellRing size={16} />,
        },
      ]} />

      <SectionSurface
        title="Needs attention"
        actions={<Text color="fg.muted" fontSize="sm">{attention.length} issues</Text>}
        flush
      >
        {attention.length === 0 ? (
          <div className="sw-section-empty"><strong>All clear</strong><span>No current issues need action.</span></div>
        ) : (
          <div className="sw-attention-list">
            {attention.map((item) => (
              <Link key={item.key} className="sw-attention-row" to={scopedHref(item.href)}>
                <Badge colorPalette={item.priority <= 1 ? 'red' : item.priority === 2 ? 'orange' : 'gray'} variant="subtle">
                  {item.kind}
                </Badge>
                <span><strong>{item.title}</strong><small>{item.detail}</small></span>
                <b>{item.action}</b>
              </Link>
            ))}
          </div>
        )}
      </SectionSurface>

      <div className="sw-two-column">
        <SectionSurface title="Recent workflows" actions={<Link to={scopedHref('/workflows')}>View all</Link>} flush>
          <StickyTableFrame>
            <Table.Root size="sm" aria-label="Recent workflows">
              <Table.Header>
                <Table.Row><Table.ColumnHeader>Workflow</Table.ColumnHeader><Table.ColumnHeader>Status</Table.ColumnHeader><Table.ColumnHeader>Requested</Table.ColumnHeader></Table.Row>
              </Table.Header>
              <Table.Body>
                {data.operations.recent.map((operation) => (
                  <Table.Row key={operation.id}>
                    <Table.Cell>
                      <Link to={scopedHref(`/workflows/${operation.id}`)}>{operation.intent || operation.kind}</Link>
                      <Text fontSize="xs" color="fg.muted">{operation.kind}</Text>
                    </Table.Cell>
                    <Table.Cell><StatusBadge status={operation.execution.status} /></Table.Cell>
                    <Table.Cell>{formatRelative(operation.requestedAt)}</Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Root>
          </StickyTableFrame>
          {data.operations.recent.length === 0 && <div className="sw-section-empty">No recent workflows.</div>}
        </SectionSurface>

        <SectionSurface title="Integrations" actions={<Link to={scopedHref('/infrastructure/integrations')}>Manage</Link>} flush>
          <StickyTableFrame>
            <Table.Root size="sm" aria-label="Integrations">
              <Table.Header>
                <Table.Row><Table.ColumnHeader>Name</Table.ColumnHeader><Table.ColumnHeader>Provider</Table.ColumnHeader><Table.ColumnHeader>Status</Table.ColumnHeader></Table.Row>
              </Table.Header>
              <Table.Body>
                {data.integrations.items.map((item) => (
                  <Table.Row key={item.id}>
                    <Table.Cell>{item.name}</Table.Cell>
                    <Table.Cell>{item.providerKind}</Table.Cell>
                    <Table.Cell>{item.lastError ? <StatusBadge status="failed" /> : item.lastSucceededAt ? formatRelative(item.lastSucceededAt) : 'Not synced'}</Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Root>
          </StickyTableFrame>
          {data.integrations.items.length === 0 && <div className="sw-section-empty">No integrations.</div>}
        </SectionSurface>
      </div>
    </div>
  )
}
