import { useMemo } from 'react'
import { Alert, AlertVariant } from '@patternfly/react-core'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import type {
  Platform,
  SlurmClusterNode,
  SlurmController,
  SlurmPartition,
} from '@/domain/platform/types'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { SectionHeader, StatStrip, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { platformLifecycleKpi, platformSyncKpis, topologyLabel } from './platformDetailShared'
import { useSlurmCluster } from './useSlurmCluster'

/**
 * The Slurm-specific body of the platform detail page.
 *
 * Slurm is structurally different from Kubernetes: slurmctld controllers are not scheduler
 * nodes, compute nodes carry Slurm scheduler state, and partitions are first-class. This view
 * reads that live state on demand from slurmrestd (controllers via ping, partitions, node
 * states) and renders each in its own section. When the live read is unavailable (no
 * slurmrestd integration recorded yet, or slurmrestd unreachable) it degrades to the
 * deployment intent (controllers) plus the membership axis (compute), never borrowing the
 * monitoring health or host-power axes for a node's state.
 */
export function SlurmPlatformView({
  platform,
  members,
  onSelect,
}: {
  platform: Platform
  members: Server[]
  onSelect: (server: Server) => void
}) {
  const clusterState = useSlurmCluster(platform.id, platform.integrationId)
  const live = clusterState.status === 'ready' ? clusterState.cluster : null
  const serverIndex = useMemo(() => buildServerIndex(members), [members])

  const intendedControllers = platform.deployment?.roleAssignments.filter(
    (assignment) => assignment.role === 'control-plane',
  ) ?? []
  const workloadCapable = platform.deployment?.roleAssignments.filter(
    (assignment) => assignment.role === 'worker' || assignment.runWorkloads,
  ) ?? []

  const managerCount = live ? live.controllers.length : intendedControllers.length
  const computeCount = live ? live.nodes.length : workloadCapable.length
  const managersUp = live ? live.controllers.filter((c) => c.status === 'up').length : 0
  const nodesIdle = live ? live.nodes.filter((n) => n.state === 'idle').length : 0

  return (
    <>
      <StatStrip
        items={[
          platformLifecycleKpi(platform),
          ...(platform.deployment
            ? [{ label: 'Topology', value: topologyLabel(platform.deployment.topology) }]
            : []),
          {
            label: 'Managers',
            value: managerCount,
            detail: live ? `${managersUp}/${managerCount} up` : 'From deploy intent',
          },
          {
            label: 'Compute nodes',
            value: computeCount,
            detail: live ? `${nodesIdle} idle` : 'From deploy intent',
          },
          ...platformSyncKpis(platform),
        ]}
      />

      {clusterState.status === 'unavailable' && (
        <Alert variant={AlertVariant.info} title="Live Slurm state is unavailable" isInline>
          Showing the recorded deployment topology and last-synced membership. Live controllers,
          partitions, and node states appear once slurmrestd is reachable (the image must include
          slurm-smd-slurmrestd and the platform must have its Slurm integration recorded).
        </Alert>
      )}

      <ControllersSection
        live={live?.controllers ?? null}
        intended={intendedControllers.map((assignment) => assignment.serverId)}
        serverIndex={serverIndex}
        members={members}
        onSelect={onSelect}
      />

      {live && live.partitions.length > 0 && <PartitionsSection partitions={live.partitions} />}

      <ComputeNodesSection
        live={live?.nodes ?? null}
        members={members}
        serverIndex={serverIndex}
        onSelect={onSelect}
      />
    </>
  )
}

/** Controllers: live slurmctld ping status, or the recorded controller intent when degraded. */
function ControllersSection({
  live,
  intended,
  serverIndex,
  members,
  onSelect,
}: {
  live: SlurmController[] | null
  intended: string[]
  serverIndex: Map<string, Server>
  members: Server[]
  onSelect: (server: Server) => void
}) {
  const description = live
    ? 'Live slurmctld controllers in failover order, from slurmrestd ping.'
    : 'Recorded controllers from the deployment. Live status appears once slurmrestd is reachable.'
  return (
    <section className="sw-section">
      <SectionHeader title="Controllers" description={description} />
      {live ? (
        live.length === 0 ? (
          <EmptyState title="No controllers" message="slurmrestd reported no controllers." />
        ) : (
          <StickyTableFrame>
            <Table aria-label="Slurm controllers" variant="compact">
              <Thead>
                <Tr><Th>Controller</Th><Th>Role</Th><Th>Status</Th></Tr>
              </Thead>
              <Tbody>
                {live.map((controller) => {
                  const server = matchServer(serverIndex, controller.hostname)
                  return (
                    <Tr
                      key={controller.hostname}
                      isClickable={Boolean(server)}
                      onRowClick={server ? () => onSelect(server) : undefined}
                    >
                      <Td dataLabel="Controller">
                        <span className="sw-cell-inline">
                          <strong>{controller.hostname}</strong>
                          <CopyButton value={controller.hostname} label="Copy controller name" />
                        </span>
                      </Td>
                      <Td dataLabel="Role">
                        <StatusBadge status="info" label={controller.primary ? 'primary' : 'backup'} />
                      </Td>
                      <Td dataLabel="Status"><StatusBadge status={controller.status} /></Td>
                    </Tr>
                  )
                })}
              </Tbody>
            </Table>
          </StickyTableFrame>
        )
      ) : intended.length === 0 ? (
        <EmptyState title="No controllers" message="This platform has no recorded controllers." />
      ) : (
        <StickyTableFrame>
          <Table aria-label="Slurm controllers" variant="compact">
            <Thead>
              <Tr><Th>Controller</Th><Th>Role</Th><Th>Status</Th></Tr>
            </Thead>
            <Tbody>
              {intended.map((serverId, index) => {
                const server = members.find((candidate) => candidate.id === serverId)
                const name = server ? serverDisplayName(server) : serverId
                return (
                  <Tr
                    key={serverId}
                    isClickable={Boolean(server)}
                    onRowClick={server ? () => onSelect(server) : undefined}
                  >
                    <Td dataLabel="Controller">
                      <span className="sw-cell-inline">
                        <strong>{name}</strong>
                        <CopyButton value={name} label="Copy controller name" />
                      </span>
                    </Td>
                    <Td dataLabel="Role">
                      <StatusBadge status="info" label={index === 0 ? 'primary' : 'backup'} />
                    </Td>
                    <Td dataLabel="Status"><StatusBadge status="unknown" label="not read" /></Td>
                  </Tr>
                )
              })}
            </Tbody>
          </Table>
        </StickyTableFrame>
      )}
    </section>
  )
}

/** Partitions: live scheduling partitions and their configured node sets. */
function PartitionsSection({ partitions }: { partitions: SlurmPartition[] }) {
  return (
    <section className="sw-section">
      <SectionHeader title="Partitions" description="Scheduling partitions reported by slurmrestd." />
      <StickyTableFrame>
        <Table aria-label="Slurm partitions" variant="compact">
          <Thead>
            <Tr><Th>Partition</Th><Th>State</Th><Th>Nodes</Th><Th>Total</Th></Tr>
          </Thead>
          <Tbody>
            {partitions.map((partition) => (
              <Tr key={partition.name}>
                <Td dataLabel="Partition"><strong>{partition.name}</strong></Td>
                <Td dataLabel="State"><StatusBadge status={partition.state === 'up' ? 'up' : 'warning'} label={partition.state || 'unknown'} /></Td>
                <Td dataLabel="Nodes" className="mono">{partition.nodeSpec || '-'}</Td>
                <Td dataLabel="Total">{partition.totalNodes || 0}</Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      </StickyTableFrame>
    </section>
  )
}

/** Compute nodes: live Slurm scheduler state, or the membership axis when degraded. */
function ComputeNodesSection({
  live,
  members,
  serverIndex,
  onSelect,
}: {
  live: SlurmClusterNode[] | null
  members: Server[]
  serverIndex: Map<string, Server>
  onSelect: (server: Server) => void
}) {
  // In degrade mode the compute nodes are the members carrying the membership axis (slurmrestd
  // reports only compute as members); controllers appear in their own section above.
  const computeMembers = members.filter((server) => server.membership)
  const description = live
    ? 'Live compute node scheduler state from slurmrestd.'
    : 'Compute members from the last membership sync. Live state appears once slurmrestd is reachable.'

  return (
    <section className="sw-section">
      <SectionHeader title="Compute nodes" description={description} />
      {live ? (
        live.length === 0 ? (
          <EmptyState title="No compute nodes" message="slurmrestd reported no nodes." />
        ) : (
          <StickyTableFrame>
            <Table aria-label="Slurm compute nodes" variant="compact">
              <Thead>
                <Tr><Th>Node</Th><Th>State</Th><Th>CPUs</Th><Th>Memory</Th><Th>GRES</Th><Th>Partitions</Th><Th>Address</Th></Tr>
              </Thead>
              <Tbody>
                {live.map((node) => {
                  const server = matchServer(serverIndex, node.name, node.address)
                  return (
                    <Tr key={node.name} isClickable={Boolean(server)} onRowClick={server ? () => onSelect(server) : undefined}>
                      <Td dataLabel="Node">
                        <span className="sw-cell-inline">
                          <strong>{node.name}</strong>
                          <CopyButton value={node.name} label="Copy node name" />
                        </span>
                      </Td>
                      <Td dataLabel="State"><StatusBadge status={slurmNodeStateTone(node.state)} label={node.state || 'unknown'} /></Td>
                      <Td dataLabel="CPUs">{node.cpus || '-'}</Td>
                      <Td dataLabel="Memory">{formatMemory(node.realMemoryMiB)}</Td>
                      <Td dataLabel="GRES" className="mono">{node.gres || '-'}</Td>
                      <Td dataLabel="Partitions">{node.partitions.length > 0 ? node.partitions.join(', ') : '-'}</Td>
                      <Td dataLabel="Address" className="mono">{node.address || '-'}</Td>
                    </Tr>
                  )
                })}
              </Tbody>
            </Table>
          </StickyTableFrame>
        )
      ) : computeMembers.length === 0 ? (
        <EmptyState title="No compute nodes" message="No Server currently reports Slurm membership." />
      ) : (
        <StickyTableFrame>
          <Table aria-label="Slurm compute nodes" variant="compact">
            <Thead>
              <Tr><Th>Node</Th><Th>State</Th><Th>Partition</Th><Th>Address</Th></Tr>
            </Thead>
            <Tbody>
              {computeMembers.map((server) => {
                const nodeName = server.membership?.nodeName || serverDisplayName(server)
                const address = serverPrimaryAddress(server)
                const state = server.membership?.state ?? 'unknown'
                return (
                  <Tr key={server.id} isClickable onRowClick={() => onSelect(server)}>
                    <Td dataLabel="Node">
                      <span className="sw-cell-inline">
                        <strong>{nodeName}</strong>
                        <CopyButton value={nodeName} label="Copy node name" />
                      </span>
                    </Td>
                    <Td dataLabel="State"><StatusBadge status={slurmNodeStateTone(state)} label={state} /></Td>
                    <Td dataLabel="Partition">{server.membership?.role || '-'}</Td>
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
      )}
    </section>
  )
}

/**
 * slurmNodeStateTone maps a Slurm node scheduler state to a StatusBadge colour family (the
 * label always shows the real word). This is scheduler state, not monitoring health: idle is a
 * healthy available node, allocated/mixed are running, drain is maintenance, down/fail is a
 * problem.
 */
function slurmNodeStateTone(state: string): string {
  switch (state.toLowerCase()) {
    case 'idle':
      return 'succeeded'
    case 'allocated':
    case 'mixed':
    case 'completing':
      return 'info'
    case 'drain':
    case 'draining':
    case 'drained':
      return 'warning'
    case 'down':
    case 'fail':
    case 'failing':
    case 'invalid':
      return 'failed'
    default:
      return 'unknown'
  }
}

/** formatMemory renders MiB as GiB for readability, falling back to a dash when unknown. */
function formatMemory(mib: number): string {
  if (!mib || mib <= 0) return '-'
  const gib = mib / 1024
  return gib >= 10 ? `${Math.round(gib)} GiB` : `${gib.toFixed(1)} GiB`
}

/** Indexes member servers by every name/address a Slurm node or controller might use. */
function buildServerIndex(members: Server[]): Map<string, Server> {
  const index = new Map<string, Server>()
  const add = (key: string | undefined | null, server: Server) => {
    if (key) index.set(key.toLowerCase(), server)
  }
  for (const server of members) {
    add(server.hostname, server)
    add(server.fqdn, server)
    add(server.membership?.nodeName, server)
    add(serverDisplayName(server), server)
    for (const address of server.addresses ?? []) add(address, server)
  }
  return index
}

/** Finds the member server for a Slurm node/controller by any of the given keys. */
function matchServer(index: Map<string, Server>, ...keys: (string | undefined)[]): Server | undefined {
  for (const key of keys) {
    if (key) {
      const server = index.get(key.toLowerCase())
      if (server) return server
    }
  }
  return undefined
}
