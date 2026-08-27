import { Button, Label } from '@patternfly/react-core'
import { PlusIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate } from 'react-router-dom'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatStrip, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { formatRelative } from '@/shared/utils/time'
import type { Cluster } from '@/domain/cluster/types'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useClusters } from './useClusters'

function issueScore(cluster: Cluster): number {
  return (cluster.integrationId ? 0 : 2) + (cluster.sync.lastError ? 2 : 0) + Math.max(0, cluster.sync.memberCount - cluster.sync.matchedCount)
}

/** Rancher-style multi-cluster inventory ordered by reachability and membership issues. */
export function ClustersPage() {
  const navigate = useNavigate()
  const { siteId, scopedHref } = useSiteScope()
  const { state } = useClusters(siteId)
  const clusters = state.status === 'ready' ? [...state.clusters].sort((a, b) => issueScore(b) - issueScore(a) || a.name.localeCompare(b.name)) : []
  const unreachable = clusters.filter((cluster) => !cluster.integrationId).length
  const unmatched = clusters.reduce((sum, cluster) => sum + Math.max(0, cluster.sync.memberCount - cluster.sync.matchedCount), 0)

  return (
    <div className="operator-page">
      <PageHeader title="Clusters" subtitle="Multi-cluster readiness, membership, and automation context." actions={<Button icon={<PlusIcon />} onClick={() => navigate(scopedHref('/clusters/deploy'))}>Deploy cluster</Button>} />
      {state.status === 'loading' && <LoadingState rows={4} />}
      {state.status === 'error' && <ErrorState message={state.message} />}
      {state.status === 'ready' && clusters.length === 0 && <EmptyState title="No clusters" message="Deploy a k0s cluster onto deployed Servers to get started." action={{ label: 'Deploy cluster', onClick: () => navigate(scopedHref('/clusters/deploy')) }} />}
      {clusters.length > 0 && <>
        <StatStrip items={[{ label: 'Clusters', value: clusters.length }, { label: 'Unreachable', value: unreachable, tone: unreachable ? 'warning' : 'neutral' }, { label: 'Unmatched members', value: unmatched, tone: unmatched ? 'warning' : 'neutral' }]} />
        <StickyTableFrame>
          <Table aria-label="Clusters" variant="compact" isStriped>
            <Thead><Tr><Th>Name</Th><Th>Type</Th><Th>Readiness</Th><Th>Members</Th><Th>Membership freshness</Th></Tr></Thead>
            <Tbody>{clusters.map((cluster) => {
              const unmanaged = Math.max(0, cluster.sync.memberCount - cluster.sync.matchedCount)
              return <Tr key={cluster.id} isClickable onRowClick={() => navigate(scopedHref(`/clusters/${cluster.id}`))}>
                <Td dataLabel="Name"><strong>{cluster.name}</strong><small>GPU stack: {cluster.gpuStackOwner}</small></Td>
                <Td dataLabel="Type"><Label color={cluster.type === 'kubernetes' ? 'blue' : 'grey'}>{cluster.type}</Label></Td>
                <Td dataLabel="Readiness">{!cluster.integrationId ? <StatusBadge status="pending" label="Not reachable" /> : cluster.sync.lastError ? <StatusBadge status="failed" label="Sync failing" /> : <StatusBadge status="ready" />}</Td>
                <Td dataLabel="Members">{cluster.integrationId ? <>{cluster.sync.matchedCount}/{cluster.sync.memberCount}{unmanaged > 0 && <Label color="orange">{unmanaged} unmanaged</Label>}</> : 'No data'}</Td>
                <Td dataLabel="Membership freshness">{cluster.sync.lastSucceededAt ? formatRelative(cluster.sync.lastSucceededAt) : 'Never synced'}</Td>
              </Tr>
            })}</Tbody>
          </Table>
        </StickyTableFrame>
      </>}
    </div>
  )
}
