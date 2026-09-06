import { useEffect, useMemo, useState } from 'react'
import { Alert, AlertVariant, Content, Label } from '@patternfly/react-core'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { Link } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Overview } from '@/domain/overview/types'
import { PageHeader } from '@/presentation/components/PageHeader'
import { SectionHeader, StatStrip, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatRelative } from '@/shared/utils/time'

type OverviewState = { status: 'loading' } | { status: 'ready'; data: Overview } | { status: 'error'; message: string }
interface AttentionItem { key: string; priority: number; kind: string; title: string; detail: string; href?: string }

function attentionItems(data: Overview): AttentionItem[] {
  const items: AttentionItem[] = data.monitoring.firing.items.map((alert) => ({
    key: `alert-${alert.fingerprint}`,
    priority: alert.severity === 'critical' ? 0 : 1,
    kind: alert.severity,
    title: alert.name,
    detail: alert.summary || alert.description || 'Firing alert',
    href: alert.serverId ? `/servers/${alert.serverId}/monitoring` : '/monitoring',
  }))
  data.integrations.items.filter((item) => item.lastError).forEach((item) => items.push({ key: `integration-${item.id}`, priority: 2, kind: 'integration', title: `${item.name} is failing`, detail: item.lastError ?? 'The last refresh failed.' }))
  if (data.platforms.unreachable) items.push({ key: 'platform-unreachable', priority: 2, kind: 'platform', title: `${data.platforms.unreachable} platforms unreachable`, detail: 'Membership cannot currently be read.', href: '/platforms' })
  if (data.platforms.unmatchedMembers) items.push({ key: 'platform-unmatched', priority: 3, kind: 'membership', title: `${data.platforms.unmatchedMembers} unmatched members`, detail: 'Reported nodes do not correlate to current Servers.', href: '/platforms' })
  if (data.inventory.health.down) items.push({ key: 'servers-down', priority: 1, kind: 'health', title: `${data.inventory.health.down} servers down`, detail: 'Metrics report these hosts as unavailable.', href: '/servers' })
  if (data.operations.failedLast24Hours) items.push({ key: 'operations-failed', priority: 2, kind: 'operation', title: `${data.operations.failedLast24Hours} failed operations in 24h`, detail: 'Review retained events and stdout before retrying.', href: '/workflows?status=failed' })
  return items.sort((left, right) => left.priority - right.priority).slice(0, 10)
}

/** PatternFly operator landing page backed by one coherent provider-owned aggregate. */
export function OperatorOverviewPage() {
  const { overview } = useApp()
  const { siteId, scopedHref } = useSiteScope()
  const [state, setState] = useState<OverviewState>({ status: 'loading' })
  useEffect(() => {
    let cancelled = false
    overview.getOverview(siteId).then((data) => { if (!cancelled) setState({ status: 'ready', data }) }).catch((error: Error) => { if (!cancelled) setState({ status: 'error', message: error.message }) })
    return () => { cancelled = true }
  }, [overview, siteId])
  const attention = useMemo(() => state.status === 'ready' ? attentionItems(state.data) : [], [state])
  if (state.status === 'loading') return <LoadingState />
  if (state.status === 'error') return <ErrorState message={state.message} />
  const data = state.data
  return (
    <div className="operator-page">
      <PageHeader title="Overview" subtitle="Current infrastructure, automation, and monitoring posture." metadata={<Content component="small">Generated {formatRelative(data.generatedAt)}</Content>} />
      {!data.monitoring.available && <Alert variant={AlertVariant.warning} title="Monitoring is temporarily unavailable" isInline>Inventory and operations remain current.</Alert>}
      <StatStrip items={[
        { label: 'Servers', value: data.inventory.servers, detail: `${data.inventory.absent} absent` },
        { label: 'Health up', value: data.inventory.health.up, detail: `${data.inventory.health.down} down, ${data.inventory.health.unknown} unknown`, tone: data.inventory.health.down ? 'critical' : 'success' },
        { label: 'Deployed', value: data.inventory.deployed, detail: `${data.inventory.platformed} platformed` },
        { label: 'GPU devices', value: data.inventory.gpuDevices, detail: `Across ${data.inventory.sites} sites` },
        { label: 'Active operations', value: data.operations.active, detail: `${data.operations.failedLast24Hours} failed in 24h`, tone: data.operations.failedLast24Hours ? 'warning' : 'neutral' },
        { label: 'Firing alerts', value: data.monitoring.firing.critical + data.monitoring.firing.warning, detail: `${data.monitoring.firing.critical} critical`, tone: data.monitoring.firing.critical ? 'critical' : 'neutral' },
      ]} />
      <section className="sw-section">
        <SectionHeader title="Operator attention" description="Issues ordered by severity and immediate operational impact." />
        {attention.length === 0 ? <div className="sw-section-empty"><strong>No immediate issues</strong><span>Observed systems are within their current operating signals.</span></div> : (
          <div className="sw-attention-list">{attention.map((item) => {
            const href = item.href ? scopedHref(item.href) : undefined
            const content = <><Label color={item.priority <= 1 ? 'red' : item.priority === 2 ? 'orange' : 'grey'}>{item.kind}</Label><span><strong>{item.title}</strong><small>{item.detail}</small></span><b>{href ? 'Open' : 'Inspect'}</b></>
            return href ? <Link key={item.key} className="sw-attention-row" to={href}>{content}</Link> : <div key={item.key} className="sw-attention-row">{content}</div>
          })}</div>
        )}
      </section>
      <div className="sw-two-column">
        <section className="sw-section"><SectionHeader title="Recent operations" description="Newest accepted work in this scope." actions={<Link to={scopedHref('/workflows')}>View all</Link>} /><StickyTableFrame><Table aria-label="Recent operations" variant="compact"><Thead><Tr><Th>Operation</Th><Th>Status</Th><Th>Requested</Th></Tr></Thead><Tbody>{data.operations.recent.map((operation) => <Tr key={operation.id}><Td dataLabel="Operation"><Link to={scopedHref(`/workflows/${operation.id}`)}>{operation.intent || operation.kind}</Link><small>{operation.kind}</small></Td><Td dataLabel="Status"><StatusBadge status={operation.execution.status} /></Td><Td dataLabel="Requested">{formatRelative(operation.requestedAt)}</Td></Tr>)}</Tbody></Table></StickyTableFrame>{data.operations.recent.length === 0 && <div className="sw-section-empty">No operations in this scope.</div>}</section>
        <section className="sw-section"><SectionHeader title="Integrations" description="Provider freshness and ingestion health." /><StickyTableFrame><Table aria-label="Integrations" variant="compact"><Thead><Tr><Th>Name</Th><Th>Kind</Th><Th>Freshness</Th></Tr></Thead><Tbody>{data.integrations.items.map((item) => <Tr key={item.id}><Td dataLabel="Name">{item.name}</Td><Td dataLabel="Kind">{item.providerKind}</Td><Td dataLabel="Freshness">{item.lastError ? <StatusBadge status="failed" /> : item.lastSucceededAt ? formatRelative(item.lastSucceededAt) : 'Not synced'}</Td></Tr>)}</Tbody></Table></StickyTableFrame>{data.integrations.items.length === 0 && <div className="sw-section-empty">No integrations in this scope.</div>}</section>
      </div>
    </div>
  )
}
