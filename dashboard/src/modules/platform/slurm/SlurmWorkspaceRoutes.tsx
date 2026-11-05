import { Button, Table } from '@chakra-ui/react'
import { RefreshCw } from 'lucide-react'
import type { PlatformTabProps } from '@/modules/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { SlurmPlatformView } from './SlurmPlatformView'
import { useSlurmCluster } from './useSlurmCluster'

/** Slurm login access, controller state, and compute-node scheduler state. */
export function SlurmNodesRoute({ platform, members, loginNodes, onOpenServer }: PlatformTabProps) {
  return (
    <SlurmPlatformView
      platform={platform}
      members={members}
      loginNodes={loginNodes}
      onSelect={(server) => onOpenServer(server.id)}
      showPartitions={false}
    />
  )
}

/** Live Slurm partitions remain a first-class route instead of sharing Kubernetes vocabulary. */
export function SlurmPartitionsRoute({ platform }: PlatformTabProps) {
  const state = useSlurmCluster(platform.id, platform.integrationId)
  if (state.status === 'loading') {
    return <EmptyState title="Loading partitions" message="Reading live scheduler state from slurmrestd." />
  }
  if (state.status === 'unavailable') {
    return (
      <Alert status="info" title="Live partitions are unavailable">
        A reachable slurmrestd Integration is required. This does not change the Platform lifecycle
        or the recorded node-role assignments.
      </Alert>
    )
  }
  if (state.status === 'error') {
    return (
      <Alert status="warning" title="Live partitions could not be loaded">
        {state.message}{state.requestId ? ' Request ID: ' + state.requestId + '.' : ''}
        <Button variant="plain" size="sm" onClick={state.reload}><RefreshCw size={14} />Retry</Button>
      </Alert>
    )
  }
  if (state.status === 'warning') {
    return (
      <>
        <Alert status="warning" title="Live Slurm state could not be refreshed">
          {state.message}{state.requestId ? ` Request ID: ${state.requestId}.` : ''} Showing the last successful result.
          <Button variant="plain" size="sm" onClick={state.reload}><RefreshCw size={14} />Retry</Button>
        </Alert>
        <PartitionTable partitions={state.cluster.partitions} />
      </>
    )
  }
  return <PartitionTable partitions={state.cluster.partitions} />
}

function PartitionTable({ partitions }: { partitions: Array<{ name: string; state: string; nodeSpec: string; totalNodes: number }> }) {
  if (partitions.length === 0) {
    return <EmptyState title="No partitions" message="slurmrestd reported no scheduling partitions." />
  }
  return (
    <section className="sw-section">
      <SectionHeader title="Partitions" description="Live scheduling partitions reported by slurmrestd." />
      <StickyTableFrame>
        <Table.Root size="sm" aria-label="Slurm partitions">
          <Table.Header>
            <Table.Row>
              <Table.ColumnHeader>Partition</Table.ColumnHeader>
              <Table.ColumnHeader>State</Table.ColumnHeader>
              <Table.ColumnHeader>Nodes</Table.ColumnHeader>
              <Table.ColumnHeader>Total</Table.ColumnHeader>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {partitions.map((partition) => (
              <Table.Row key={partition.name}>
                <Table.Cell><strong>{partition.name}</strong></Table.Cell>
                <Table.Cell><StatusBadge status={partition.state === 'up' ? 'up' : 'warning'} label={partition.state || 'unknown'} /></Table.Cell>
                <Table.Cell className="mono">{partition.nodeSpec || '-'}</Table.Cell>
                <Table.Cell>{partition.totalNodes || 0}</Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Root>
      </StickyTableFrame>
    </section>
  )
}
