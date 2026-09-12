import { useMemo, useState } from 'react'
import { Badge, Button, HStack, Table } from '@chakra-ui/react'
import { ArrowDown, ArrowUp } from 'lucide-react'
import type { Platform } from '@/domain/platform/types'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { SectionHeader, MetricGrid, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
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
  const intendedControllers = platform.deployment?.roleAssignments.filter((assignment) => assignment.role === 'control-plane') ?? []
  const intendedWorkers = platform.deployment?.roleAssignments.filter((assignment) => assignment.role === 'worker') ?? []
  const workloadCapable = platform.deployment?.roleAssignments.filter((assignment) => assignment.role === 'worker' || assignment.runWorkloads) ?? []
  const workloadControllers = intendedControllers.filter((assignment) => assignment.runWorkloads)
  // Fallback counts for a registered platform with no deployment intent: derive from the live
  // membership axis instead of the recorded topology.
  const liveControllers = members.filter((server) => server.membership?.role === 'control-plane')
  const liveWorkers = members.filter((server) => server.membership?.role !== 'control-plane')

  return (
    <>
      <MetricGrid
        items={[
          platformLifecycleKpi(platform),
          ...(platform.deployment
            ? [
                { label: 'Topology', value: topologyLabel(platform.deployment.topology) },
                {
                  label: 'Control-plane',
                  value: intendedControllers.length,
                  detail:
                    workloadControllers.length > 0
                      ? workloadControllers.length + (workloadControllers.length === 1 ? ' also runs workloads' : ' also run workloads')
                      : 'Dedicated control-plane',
                },
                {
                  label: 'Workload-capable',
                  value: workloadCapable.length,
                  detail:
                    intendedWorkers.length === 0
                      ? 'No worker-only nodes'
                      : intendedWorkers.length + (intendedWorkers.length === 1 ? ' worker-only node' : ' worker-only nodes'),
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
    const ordered = [...members].sort((a, b) => sortValue(a, activeSortIndex).localeCompare(sortValue(b, activeSortIndex), undefined, { numeric: true }))
    return activeSortDirection === 'asc' ? ordered : ordered.reverse()
  }, [members, activeSortIndex, activeSortDirection])

  const sort = (index: number) => {
    if (index === activeSortIndex) setActiveSortDirection((direction) => (direction === 'asc' ? 'desc' : 'asc'))
    else {
      setActiveSortIndex(index)
      setActiveSortDirection('asc')
    }
  }

  if (members.length === 0) {
    return <EmptyState title="No members" message="No Server currently reports membership in this platform." />
  }

  const rows = sortedMembers.map((server) => {
    const assignment = deployment?.roleAssignments.find((candidate) => candidate.serverId === server.id)
    return {
      server,
      nodeName: server.membership?.nodeName || serverDisplayName(server),
      address: serverPrimaryAddress(server),
      role: server.membership?.role ?? assignment?.role ?? 'unknown',
      runsWorkloads: assignment?.role === 'control-plane' && assignment.runWorkloads,
      membershipState: server.membership?.state,
    }
  })

  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Platform members">
            <Table.Header>
              <Table.Row>
                {['Node', 'Role', 'State', 'Address'].map((label, index) => (
                  <Table.ColumnHeader key={label}>
                    <Button variant="plain" size="sm" className="sw-sort-button" onClick={() => sort(index)} aria-label={`Sort by ${label}`}>
                      {label}
                      {activeSortIndex === index && (activeSortDirection === 'asc' ? <ArrowUp size={14} /> : <ArrowDown size={14} />)}
                    </Button>
                  </Table.ColumnHeader>
                ))}
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {rows.map(({ server, nodeName, address, role, runsWorkloads, membershipState }) => (
                <Table.Row key={server.id} cursor="pointer" _hover={{ bg: 'bg.subtle' }} onClick={() => onSelect(server)}>
                  <Table.Cell>
                    <span className="sw-cell-inline">
                      <strong>{nodeName}</strong>
                      <CopyButton value={nodeName} label="Copy node name" />
                    </span>
                  </Table.Cell>
                  <Table.Cell>
                    <HStack gap="2">
                      <StatusBadge status={role === 'control-plane' ? 'info' : 'neutral'} label={role} />
                      {runsWorkloads && (
                        <Badge colorPalette="green" variant="subtle">
                          Runs workloads
                        </Badge>
                      )}
                    </HStack>
                  </Table.Cell>
                  <Table.Cell>
                    {membershipState ? (
                      <StatusBadge status={membershipState === 'ready' ? 'succeeded' : 'warning'} label={membershipState} />
                    ) : (
                      <StatusBadge status="neutral" label="unknown" />
                    )}
                  </Table.Cell>
                  <Table.Cell className="mono">
                    <span className="sw-cell-inline">
                      {address ?? '-'}
                      <CopyButton value={address ?? ''} label="Copy address" />
                    </span>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      }
      mobile={
        <div className="sw-resource-card-list">
          {rows.map(({ server, nodeName, address, role, runsWorkloads, membershipState }) => (
            <ResourceCard
              key={server.id}
              title={
                <span className="sw-cell-inline">
                  <strong>{nodeName}</strong>
                  <CopyButton value={nodeName} label="Copy node name" />
                </span>
              }
              status={
                membershipState ? (
                  <StatusBadge status={membershipState === 'ready' ? 'succeeded' : 'warning'} label={membershipState} />
                ) : (
                  <StatusBadge status="neutral" label="unknown" />
                )
              }
              actions={
                <Button variant="outline" size="sm" onClick={() => onSelect(server)}>
                  Open server
                </Button>
              }
            >
              <ResourceCardField label="Role">
                <StatusBadge status={role === 'control-plane' ? 'info' : 'neutral'} label={role} />
              </ResourceCardField>
              <ResourceCardField label="Address">
                <span className="sw-cell-inline">
                  {address ?? '-'}
                  <CopyButton value={address ?? ''} label="Copy address" />
                </span>
              </ResourceCardField>
              {runsWorkloads && (
                <ResourceCardField label="Scheduling">
                  <Badge colorPalette="green" variant="subtle">
                    Runs workloads
                  </Badge>
                </ResourceCardField>
              )}
            </ResourceCard>
          ))}
        </div>
      }
    />
  )
}
