import { Button, Label } from '@patternfly/react-core'
import { PlusIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate } from 'react-router-dom'
import { clusterLifecycleLabel, clusterLifecycleStatus } from '@/domain/cluster/lifecycle'
import type { Cluster } from '@/domain/cluster/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StatStrip, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatRelative } from '@/shared/utils/time'
import { useClusters } from './useClusters'

function issueScore(cluster: Cluster): number {
  const lifecycleIssue =
    cluster.lifecycleState === 'deploy_failed' || cluster.lifecycleState === 'uninstall_failed'
      ? 6
      : cluster.lifecycleState === 'deploying' || cluster.lifecycleState === 'uninstalling'
        ? 2
        : 0
  const reachabilityIssue =
    cluster.lifecycleState !== 'uninstalled' && !cluster.integrationId ? 2 : 0
  return lifecycleIssue + reachabilityIssue + (cluster.sync.lastError ? 2 : 0)
    + Math.max(0, cluster.sync.memberCount - cluster.sync.matchedCount)
}

function needsAttention(cluster: Cluster): boolean {
  return issueScore(cluster) >= 4
}

/** Multi-cluster inventory ordered by operation lifecycle and membership issues. */
export function ClustersPage() {
  const navigate = useNavigate()
  const { siteId, scopedHref } = useSiteScope()
  const { state } = useClusters(siteId)
  const clusters = state.status === 'ready'
    ? [...state.clusters].sort(
      (a, b) => issueScore(b) - issueScore(a) || a.name.localeCompare(b.name),
    )
    : []
  const attention = clusters.filter(needsAttention).length
  const uninstalling = clusters.filter(
    (cluster) => cluster.lifecycleState === 'uninstalling',
  ).length
  const unmatched = clusters.reduce(
    (sum, cluster) => sum + Math.max(0, cluster.sync.memberCount - cluster.sync.matchedCount),
    0,
  )

  return (
    <div className="operator-page">
      <PageHeader
        title="Clusters"
        subtitle="Multi-cluster lifecycle, readiness, membership, and automation context."
        actions={
          <Button
            icon={<PlusIcon />}
            onClick={() => navigate(scopedHref('/clusters/deploy'))}
          >
            Deploy cluster
          </Button>
        }
      />
      {state.status === 'loading' && <LoadingState rows={4} />}
      {state.status === 'error' && <ErrorState message={state.message} />}
      {state.status === 'ready' && clusters.length === 0 && (
        <EmptyState
          title="No clusters"
          message="Deploy a k0s cluster onto deployed Servers to get started."
          action={{
            label: 'Deploy cluster',
            onClick: () => navigate(scopedHref('/clusters/deploy')),
          }}
        />
      )}
      {clusters.length > 0 && (
        <>
          <StatStrip
            items={[
              { label: 'Clusters', value: clusters.length },
              { label: 'Needs attention', value: attention, tone: attention ? 'warning' : 'neutral' },
              { label: 'Uninstalling', value: uninstalling },
              {
                label: 'Unmatched members',
                value: unmatched,
                tone: unmatched ? 'warning' : 'neutral',
              },
            ]}
          />
          <StickyTableFrame>
            <Table aria-label="Clusters" variant="compact" isStriped>
              <Thead>
                <Tr>
                  <Th>Name</Th>
                  <Th>Type</Th>
                  <Th>Lifecycle</Th>
                  <Th>Connectivity</Th>
                  <Th>Members</Th>
                  <Th>Membership freshness</Th>
                </Tr>
              </Thead>
              <Tbody>
                {clusters.map((cluster) => {
                  const unmanaged = Math.max(
                    0,
                    cluster.sync.memberCount - cluster.sync.matchedCount,
                  )
                  return (
                    <Tr
                      key={cluster.id}
                      isClickable
                      onRowClick={() => navigate(scopedHref(`/clusters/${cluster.id}`))}
                    >
                      <Td dataLabel="Name">
                        <strong>{cluster.name}</strong>
                        <small>{cluster.origin === 'deployed' ? 'Swallow deployed' : 'Registered'}</small>
                      </Td>
                      <Td dataLabel="Type">
                        <Label color={cluster.type === 'kubernetes' ? 'blue' : 'grey'}>
                          {cluster.type}
                        </Label>
                      </Td>
                      <Td dataLabel="Lifecycle">
                        <StatusBadge
                          status={clusterLifecycleStatus(cluster.lifecycleState)}
                          label={clusterLifecycleLabel(cluster.lifecycleState)}
                        />
                      </Td>
                      <Td dataLabel="Connectivity">
                        {cluster.lifecycleState === 'uninstalled' ? (
                          '-'
                        ) : !cluster.integrationId ? (
                          <StatusBadge status="pending" label="Not reachable" />
                        ) : cluster.sync.lastError ? (
                          <StatusBadge status="failed" label="Sync failing" />
                        ) : (
                          <StatusBadge status="ready" label="Connected" />
                        )}
                      </Td>
                      <Td dataLabel="Members">
                        {cluster.integrationId ? (
                          <>
                            {cluster.sync.matchedCount}/{cluster.sync.memberCount}
                            {unmanaged > 0 && <Label color="orange">{unmanaged} unmatched</Label>}
                          </>
                        ) : '-'}
                      </Td>
                      <Td dataLabel="Membership freshness">
                        {cluster.sync.lastSucceededAt
                          ? formatRelative(cluster.sync.lastSucceededAt)
                          : '-'}
                      </Td>
                    </Tr>
                  )
                })}
              </Tbody>
            </Table>
          </StickyTableFrame>
        </>
      )}
    </div>
  )
}
