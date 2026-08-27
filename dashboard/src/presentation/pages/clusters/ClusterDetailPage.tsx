import { useCallback, useState } from 'react'
import { Alert, AlertVariant, Button, Label } from '@patternfly/react-core'
import { SyncAltIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate, useParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionHeader, StatStrip, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useToast } from '@/presentation/components/toast/toastContext'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useClusterDetail } from './useClusterDetail'

/**
 * Cluster drill-down combining Rancher-style readiness with Headlamp-style member rows.
 * Kubernetes receives role/readiness language; Slurm keeps neutral member terminology and
 * never invents Kubernetes resource concepts that are absent from the API.
 */
export function ClusterDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const { clusters } = useApp()
  const { showToast } = useToast()
  const state = useClusterDetail(id)
  const [syncing, setSyncing] = useState(false)

  const sync = useCallback(async () => {
    if (!id) return
    setSyncing(true)
    try {
      const report = await clusters.syncCluster(id)
      showToast({ tone: report.error ? 'error' : 'success', title: report.error ? 'Sync failed' : 'Membership synced', description: report.error ?? `${report.matched} of ${report.members} members matched to Servers.` })
      if (state.status === 'ready') state.data.reload()
    } catch (error) {
      showToast({ tone: 'error', title: 'Sync failed', description: error instanceof Error ? error.message : 'Could not sync membership.' })
    } finally { setSyncing(false) }
  }, [clusters, id, showToast, state])

  if (state.status === 'loading') return <><PageHeader title="Cluster" /><LoadingState rows={6} /></>
  if (state.status === 'not-found') return <><PageHeader title="Cluster" /><EmptyState title="Cluster not found" message="This cluster no longer exists." /></>
  if (state.status === 'error') return <><PageHeader title="Cluster" /><ErrorState message={state.message} /></>
  const { cluster, members, operations, reload } = state.data
  const controllers = members.filter((server) => server.membership?.role === 'control-plane')
  const workers = members.filter((server) => server.membership?.role !== 'control-plane')
  const isKubernetes = cluster.type === 'kubernetes'

  return <div className="operator-page">
    <PageHeader title={cluster.name} subtitle={`${cluster.type} cluster, GPU stack owned by ${cluster.gpuStackOwner}, exporters managed by ${cluster.exporterOwner}`} breadcrumbs={[{ label: 'Clusters', href: scopedHref('/clusters') }, { label: cluster.name }]} metadata={<Label color={isKubernetes ? 'blue' : 'grey'}>{cluster.type}</Label>} actions={<Button icon={<SyncAltIcon />} onClick={() => void sync()} isLoading={syncing} isDisabled={syncing || !cluster.integrationId}>Sync now</Button>} />
    {!cluster.integrationId && <Alert variant={AlertVariant.info} title="Cluster is not reachable yet" isInline>Membership becomes available after deployment finishes and Swallow records a read credential.</Alert>}
    {cluster.sync.lastError && <Alert variant={AlertVariant.danger} title="Membership sync is failing" isInline>{cluster.sync.lastError}. The member list may be stale; last success {formatRelative(cluster.sync.lastSucceededAt ?? undefined)}.</Alert>}
    <StatStrip items={[{ label: isKubernetes ? 'Control-plane' : 'Managers', value: controllers.length }, { label: isKubernetes ? 'Workers' : 'Compute members', value: workers.length }, { label: 'Matched members', value: cluster.sync.matchedCount, detail: `${cluster.sync.memberCount} reported`, tone: cluster.sync.matchedCount < cluster.sync.memberCount ? 'warning' : 'neutral' }, { label: 'Last synced', value: cluster.sync.lastSucceededAt ? formatRelative(cluster.sync.lastSucceededAt) : 'No data' }]} />
    <section className="sw-section"><SectionHeader title="Members" description={isKubernetes ? 'Kubernetes membership correlated to Server projections.' : 'Cluster membership correlated to Server projections.'} /><MemberTable members={members} isKubernetes={isKubernetes} onSelect={(server) => navigate(scopedHref(`/servers/${server.id}`))} /></section>
    <section className="sw-section"><SectionHeader title="Related operations" description="Automation requested against this Cluster." actions={<Button variant="link" icon={<SyncAltIcon />} onClick={reload}>Refresh</Button>} />{operations.length === 0 ? <EmptyState title="No operations" message="No automation has run against this cluster." /> : <StickyTableFrame><Table aria-label="Related operations" variant="compact"><Thead><Tr><Th>Intent</Th><Th>Status</Th><Th>Requested</Th></Tr></Thead><Tbody>{operations.map((operation) => <Tr key={operation.id} isClickable onRowClick={() => navigate(scopedHref(`/operations/${operation.id}`))}><Td dataLabel="Intent">{operation.intent || operation.execution.playbook}</Td><Td dataLabel="Status"><StatusBadge status={operation.execution.status} /></Td><Td dataLabel="Requested">{formatDateTime(operation.requestedAt)}</Td></Tr>)}</Tbody></Table></StickyTableFrame>}</section>
  </div>
}

/** Member table whose labels remain type-aware instead of treating Slurm as Kubernetes. */
function MemberTable({ members, isKubernetes, onSelect }: { members: Server[]; isKubernetes: boolean; onSelect: (server: Server) => void }) {
  if (members.length === 0) return <EmptyState title="No members yet" message="No Server currently reports membership in this cluster." />
  return <StickyTableFrame><Table aria-label="Cluster members" variant="compact"><Thead><Tr><Th>{isKubernetes ? 'Node' : 'Member'}</Th><Th>Role</Th><Th>State</Th><Th>Address</Th></Tr></Thead><Tbody>{members.map((server) => <Tr key={server.id} isClickable onRowClick={() => onSelect(server)}><Td dataLabel={isKubernetes ? 'Node' : 'Member'}><strong>{server.membership?.nodeName || serverDisplayName(server)}</strong></Td><Td dataLabel="Role"><StatusBadge status={server.membership?.role === 'control-plane' ? 'info' : 'neutral'} label={server.membership?.role || 'unknown'} /></Td><Td dataLabel="State"><StatusBadge status={server.membership?.state === 'ready' ? 'succeeded' : 'warning'} label={server.membership?.state || 'unknown'} /></Td><Td dataLabel="Address" className="mono">{serverPrimaryAddress(server) ?? 'No data'}</Td></Tr>)}</Tbody></Table></StickyTableFrame>
}
