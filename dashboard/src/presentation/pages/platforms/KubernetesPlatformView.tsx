import { useMemo, useState } from 'react'
import { Flex, Label } from '@patternfly/react-core'
import { Table, Tbody, Td, Th, Thead, Tr, type ThProps } from '@patternfly/react-table'
import type { Platform } from '@/domain/platform/types'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { SectionHeader, StatStrip, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { platformLifecycleKpi, platformSyncKpis, topologyLabel } from './platformDetailShared'

/**
 * The Kubernetes-specific body of the platform detail page: a control-plane/worker topology
 * summary and a node member table. Kubernetes control-plane and worker nodes are both
 * scheduler nodes, so every member appears on the membership axis with a Ready/NotReady state;
 * there is no separate manager axis to reconcile as there is for Slurm.
 */
export function KubernetesPlatformView({
  platform,
  members,
  onSelect,
}: {
  platform: Platform
  members: Server[]
  onSelect: (server: Server) => void
}) {
  const intendedControllers = platform.deployment?.roleAssignments.filter(
    (assignment) => assignment.role === 'control-plane',
  ) ?? []
  const intendedWorkers = platform.deployment?.roleAssignments.filter(
    (assignment) => assignment.role === 'worker',
  ) ?? []
  const workloadCapable = platform.deployment?.roleAssignments.filter(
    (assignment) => assignment.role === 'worker' || assignment.runWorkloads,
  ) ?? []
  const workloadControllers = intendedControllers.filter((assignment) => assignment.runWorkloads)
  // Fallback counts for a registered platform with no deployment intent: derive from the live
  // membership axis instead of the recorded topology.
  const liveControllers = members.filter((server) => server.membership?.role === 'control-plane')
  const liveWorkers = members.filter((server) => server.membership?.role !== 'control-plane')

  return (
    <>
      <StatStrip
        items={[
          platformLifecycleKpi(platform),
          ...(platform.deployment
            ? [
                { label: 'Topology', value: topologyLabel(platform.deployment.topology) },
                {
                  label: 'Control-plane',
                  value: intendedControllers.length,
                  detail: workloadControllers.length > 0
                    ? workloadControllers.length + (workloadControllers.length === 1
                        ? ' also runs workloads'
                        : ' also run workloads')
                    : 'Dedicated control-plane',
                },
                {
                  label: 'Workload-capable',
                  value: workloadCapable.length,
                  detail: intendedWorkers.length === 0
                    ? 'No worker-only nodes'
                    : intendedWorkers.length + (intendedWorkers.length === 1
                        ? ' worker-only node'
                        : ' worker-only nodes'),
                },
              ]
            : [
                { label: 'Control-plane', value: liveControllers.length },
                { label: 'Worker nodes', value: liveWorkers.length },
              ]),
          ...platformSyncKpis(platform),
        ]}
      />
      <section className="sw-section">
        <SectionHeader title="Members" description="Kubernetes membership correlated to Server projections." />
        <KubernetesMemberTable members={members} deployment={platform.deployment} onSelect={onSelect} />
      </section>
    </>
  )
}

/** The Kubernetes node member table: role and Ready/NotReady state come from the membership axis. */
function KubernetesMemberTable({
  members,
  deployment,
  onSelect,
}: {
  members: Server[]
  deployment: Platform['deployment']
  onSelect: (server: Server) => void
}) {
  const [activeSortIndex, setActiveSortIndex] = useState(0)
  const [activeSortDirection, setActiveSortDirection] = useState<'asc' | 'desc'>('asc')

  const sortValue = (server: Server, columnIndex: number): string => {
    switch (columnIndex) {
      case 0:
        return (server.membership?.nodeName || serverDisplayName(server)).toLowerCase()
      case 1:
        return server.membership?.role ?? ''
      case 2:
        return server.membership?.state ?? ''
      case 3:
        return serverPrimaryAddress(server) ?? ''
      default:
        return ''
    }
  }
  const sortedMembers = useMemo(() => {
    const ordered = [...members].sort((a, b) =>
      sortValue(a, activeSortIndex).localeCompare(sortValue(b, activeSortIndex), undefined, { numeric: true }),
    )
    return activeSortDirection === 'asc' ? ordered : ordered.reverse()
  }, [members, activeSortIndex, activeSortDirection])
  const sortParams = (columnIndex: number): ThProps['sort'] => ({
    sortBy: { index: activeSortIndex, direction: activeSortDirection },
    onSort: (_event, index, direction) => {
      setActiveSortIndex(index)
      setActiveSortDirection(direction)
    },
    columnIndex,
  })

  if (members.length === 0) {
    return <EmptyState title="No members" message="No Server currently reports membership in this platform." />
  }
  return (
    <StickyTableFrame>
      <Table aria-label="Platform members" variant="compact">
        <Thead>
          <Tr>
            <Th sort={sortParams(0)}>Node</Th>
            <Th sort={sortParams(1)}>Role</Th>
            <Th sort={sortParams(2)}>State</Th>
            <Th sort={sortParams(3)}>Address</Th>
          </Tr>
        </Thead>
        <Tbody>
          {sortedMembers.map((server) => {
            const assignment = deployment?.roleAssignments.find((candidate) => candidate.serverId === server.id)
            const runsWorkloads = assignment?.role === 'control-plane' && assignment.runWorkloads
            const nodeName = server.membership?.nodeName || serverDisplayName(server)
            const address = serverPrimaryAddress(server)
            const role = server.membership?.role ?? assignment?.role ?? 'unknown'
            return (
              <Tr key={server.id} isClickable onRowClick={() => onSelect(server)}>
                <Td dataLabel="Node">
                  <span className="sw-cell-inline">
                    <strong>{nodeName}</strong>
                    <CopyButton value={nodeName} label="Copy node name" />
                  </span>
                </Td>
                <Td dataLabel="Role">
                  <Flex gap={{ default: 'gapSm' }} alignItems={{ default: 'alignItemsCenter' }}>
                    <StatusBadge status={role === 'control-plane' ? 'info' : 'neutral'} label={role} />
                    {runsWorkloads && <Label color="green">Runs workloads</Label>}
                  </Flex>
                </Td>
                <Td dataLabel="State">
                  {server.membership?.state ? (
                    <StatusBadge
                      status={server.membership.state === 'ready' ? 'succeeded' : 'warning'}
                      label={server.membership.state}
                    />
                  ) : (
                    <StatusBadge status="neutral" label="unknown" />
                  )}
                </Td>
                <Td dataLabel="Address" className="mono">
                  <span className="sw-cell-inline">
                    {address ?? '-'}
                    <CopyButton value={address ?? ''} label="Copy address" />
                  </span>
                </Td>
              </Tr>
            )
          })}
        </Tbody>
      </Table>
    </StickyTableFrame>
  )
}
