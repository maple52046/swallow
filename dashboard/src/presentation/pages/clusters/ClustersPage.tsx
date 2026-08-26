import { useNavigate } from 'react-router-dom'
import { Badge, Button, Callout, Card, Flex, Table, Text } from '@radix-ui/themes'
import { ExclamationTriangleIcon, PlusIcon } from '@radix-ui/react-icons'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { formatRelative } from '@/shared/utils/time'
import type { Cluster } from '@/domain/cluster/types'
import { useClusters } from './useClusters'

/**
 * Lists the clusters swallow knows about, with a link to deploy a new one.
 *
 * Each row shows the membership freshness and the matched/total member counts, so a stale
 * or partially matched cluster is visible at a glance rather than looking healthy. A
 * cluster with no integration yet is shown as "not reachable", which is the normal state
 * while one is being built.
 */
export function ClustersPage() {
  const navigate = useNavigate()
  const { state } = useClusters()

  return (
    <Flex direction="column">
      <PageHeader
        title="Clusters"
        subtitle="Kubernetes and Slurm clusters swallow knows about, and the k0s clusters it deploys."
        actions={
          <Button onClick={() => navigate('/clusters/deploy')}>
            <PlusIcon />
            Deploy cluster
          </Button>
        }
      />

      {state.status === 'loading' && <LoadingState rows={4} />}
      {state.status === 'error' && <ErrorState message={state.message} />}

      {state.status === 'ready' && state.clusters.length === 0 && (
        <EmptyState
          title="No clusters"
          message="Deploy a k0s cluster onto your deployed servers to get started."
          action={{ label: 'Deploy cluster', onClick: () => navigate('/clusters/deploy') }}
        />
      )}

      {state.status === 'ready' && state.clusters.length > 0 && (
        <Card>
          <Table.Root variant="ghost">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeaderCell>Name</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Type</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>GPU stack</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Members</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Membership</Table.ColumnHeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {state.clusters.map((cluster) => (
                <Table.Row
                  key={cluster.id}
                  onClick={() => navigate(`/clusters/${cluster.id}`)}
                  style={{ cursor: 'pointer' }}
                >
                  <Table.Cell>
                    <Text size="2" weight="medium">
                      {cluster.name}
                    </Text>
                  </Table.Cell>
                  <Table.Cell>
                    <Text size="2">{cluster.type}</Text>
                  </Table.Cell>
                  <Table.Cell>
                    <Text size="2">{cluster.gpuStackOwner}</Text>
                  </Table.Cell>
                  <Table.Cell>
                    <MemberCount cluster={cluster} />
                  </Table.Cell>
                  <Table.Cell>
                    <MembershipFreshness cluster={cluster} />
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </Card>
      )}
    </Flex>
  )
}

/** Matched-of-total members; a gap means the cluster has machines swallow does not manage. */
function MemberCount({ cluster }: { cluster: Cluster }) {
  if (!cluster.integrationId) {
    return (
      <Text size="2" color="gray">
        —
      </Text>
    )
  }
  const matched = cluster.sync.matchedCount
  const total = cluster.sync.memberCount
  return (
    <Flex gap="1" align="center">
      <Text size="2">
        {matched}/{total}
      </Text>
      {total > matched && (
        <Badge color="yellow" variant="soft">
          {total - matched} unmanaged
        </Badge>
      )}
    </Flex>
  )
}

/** Sync freshness: not reachable, an error callout, or how long ago the last read succeeded. */
function MembershipFreshness({ cluster }: { cluster: Cluster }) {
  if (!cluster.integrationId) {
    return <StatusBadge status="pending" label="not reachable" />
  }
  if (cluster.sync.lastError) {
    return (
      <Callout.Root color="red" size="1" role="status">
        <Callout.Icon>
          <ExclamationTriangleIcon />
        </Callout.Icon>
        <Callout.Text>sync failing</Callout.Text>
      </Callout.Root>
    )
  }
  if (!cluster.sync.lastSucceededAt) {
    return (
      <Text size="2" color="gray">
        never synced
      </Text>
    )
  }
  return (
    <Text size="2" color="gray">
      synced {formatRelative(cluster.sync.lastSucceededAt)}
    </Text>
  )
}
