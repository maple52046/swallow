import { useMemo } from 'react'
import { Table } from '@chakra-ui/react'
import type { Platform, SlurmClusterNode, SlurmController, SlurmPartition } from '@/domain/platform/types'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { SectionHeader, MetricGrid, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { platformLifecycleKpi, platformSyncKpis, topologyLabel } from './platformDetailShared'
import { useSlurmCluster } from './useSlurmCluster'

/** Row interaction props applied only when the row maps to a real Server (so it can be opened). */
function rowNav(server: Server | undefined, onSelect: (server: Server) => void) {
  return server ? { cursor: 'pointer' as const, _hover: { bg: 'bg.subtle' }, onClick: () => onSelect(server) } : {}
}

/**
 * The Slurm-specific body of the platform detail page.
 *
 * Slurm is structurally different from Kubernetes: slurmctld controllers are not scheduler
 * nodes, compute nodes carry Slurm scheduler state, and partitions are first-class. This view
 * reads that live state on demand from slurmrestd (controllers via ping, partitions, node
 * states) and renders each in its own section. When the live read is unavailable it degrades to
 * the deployment intent (controllers) plus the membership axis (compute), never borrowing the
 * monitoring health or host-power axes for a node's state.
 */
export function SlurmPlatformView({
  platform,
  members,
  loginNodes,
  onSelect,
}: {
  platform: Platform
  members: Server[]
  loginNodes: Server[]
  onSelect: (server: Server) => void
}) {
  const clusterState = useSlurmCluster(platform.id, platform.integrationId)
  const live = clusterState.status === 'ready' ? clusterState.cluster : null
  const serverIndex = useMemo(() => buildServerIndex(members), [members])

  const intendedControllers = platform.deployment?.roleAssignments.filter((assignment) => assignment.role === 'control-plane') ?? []
  const workloadCapable = platform.deployment?.roleAssignments.filter((assignment) => assignment.role === 'worker' || assignment.runWorkloads) ?? []

  const managerCount = live ? live.controllers.length : intendedControllers.length
  const computeCount = live ? live.nodes.length : workloadCapable.length
  const managersUp = live ? live.controllers.filter((c) => c.status === 'up').length : 0
  const nodesIdle = live ? live.nodes.filter((n) => n.state === 'idle').length : 0

  return (
    <>
      <MetricGrid
        items={[
          platformLifecycleKpi(platform),
          ...(platform.deployment ? [{ label: 'Topology', value: topologyLabel(platform.deployment.topology) }] : []),
          { label: 'Managers', value: managerCount, detail: live ? `${managersUp}/${managerCount} up` : 'From deploy intent' },
          { label: 'Compute nodes', value: computeCount, detail: live ? `${nodesIdle} idle` : 'From deploy intent' },
          ...platformSyncKpis(platform),
        ]}
      />

      <ClusterAccessSection loginNodes={loginNodes} onSelect={onSelect} />

      {clusterState.status === 'unavailable' && (
        <Alert status="info" title="Live Slurm state is unavailable">
          Showing the recorded deployment topology and last-synced membership. Live controllers, partitions, and node states appear once slurmrestd is
          reachable (the image must include slurm-smd-slurmrestd and the platform must have its Slurm integration recorded).
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

      <ComputeNodesSection live={live?.nodes ?? null} members={members} serverIndex={serverIndex} onSelect={onSelect} />
    </>
  )
}

/**
 * Cluster access: which host and IP to use to operate the cluster (submit jobs). A Slurm login
 * node is the submission host; it runs no cluster daemon and carries no membership, so it is not
 * in the controllers/compute tables. Shown only when the deployment has a login node.
 */
function ClusterAccessSection({ loginNodes, onSelect }: { loginNodes: Server[]; onSelect: (server: Server) => void }) {
  if (loginNodes.length === 0) return null
  return (
    <section className="sw-section">
      <SectionHeader title="Cluster access" description="Use a login node to operate the cluster: SSH in and submit jobs with sbatch/srun." />
      <StickyTableFrame>
        <Table.Root size="sm" aria-label="Slurm login nodes">
          <Table.Header>
            <Table.Row><Table.ColumnHeader>Login node</Table.ColumnHeader><Table.ColumnHeader>Access address</Table.ColumnHeader></Table.Row>
          </Table.Header>
          <Table.Body>
            {loginNodes.map((server) => {
              const address = serverPrimaryAddress(server)
              return (
                <Table.Row key={server.id} {...rowNav(server, onSelect)}>
                  <Table.Cell>
                    <strong>{serverDisplayName(server)}</strong>
                  </Table.Cell>
                  <Table.Cell className="mono">
                    <span className="sw-cell-inline">
                      {address ?? '-'}
                      {address && <CopyButton value={address} label="Copy login node address" />}
                    </span>
                  </Table.Cell>
                </Table.Row>
              )
            })}
          </Table.Body>
        </Table.Root>
      </StickyTableFrame>
    </section>
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
            <Table.Root size="sm" aria-label="Slurm controllers">
              <Table.Header>
                <Table.Row><Table.ColumnHeader>Controller</Table.ColumnHeader><Table.ColumnHeader>Role</Table.ColumnHeader><Table.ColumnHeader>Status</Table.ColumnHeader></Table.Row>
              </Table.Header>
              <Table.Body>
                {live.map((controller) => {
                  const server = matchServer(serverIndex, controller.hostname)
                  return (
                    <Table.Row key={controller.hostname} {...rowNav(server, onSelect)}>
                      <Table.Cell>
                        <span className="sw-cell-inline">
                          <strong>{controller.hostname}</strong>
                          <CopyButton value={controller.hostname} label="Copy controller name" />
                        </span>
                      </Table.Cell>
                      <Table.Cell>
                        <StatusBadge status="info" label={controller.primary ? 'primary' : 'backup'} />
                      </Table.Cell>
                      <Table.Cell>
                        <StatusBadge status={controller.status} />
                      </Table.Cell>
                    </Table.Row>
                  )
                })}
              </Table.Body>
            </Table.Root>
          </StickyTableFrame>
        )
      ) : intended.length === 0 ? (
        <EmptyState title="No controllers" message="This platform has no recorded controllers." />
      ) : (
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Slurm controllers">
            <Table.Header>
              <Table.Row><Table.ColumnHeader>Controller</Table.ColumnHeader><Table.ColumnHeader>Role</Table.ColumnHeader><Table.ColumnHeader>Status</Table.ColumnHeader></Table.Row>
            </Table.Header>
            <Table.Body>
              {intended.map((serverId, index) => {
                const server = members.find((candidate) => candidate.id === serverId)
                const name = server ? serverDisplayName(server) : serverId
                return (
                  <Table.Row key={serverId} {...rowNav(server, onSelect)}>
                    <Table.Cell>
                      <span className="sw-cell-inline">
                        <strong>{name}</strong>
                        <CopyButton value={name} label="Copy controller name" />
                      </span>
                    </Table.Cell>
                    <Table.Cell>
                      <StatusBadge status="info" label={index === 0 ? 'primary' : 'backup'} />
                    </Table.Cell>
                    <Table.Cell>
                      <StatusBadge status="unknown" label="not read" />
                    </Table.Cell>
                  </Table.Row>
                )
              })}
            </Table.Body>
          </Table.Root>
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
        <Table.Root size="sm" aria-label="Slurm partitions">
          <Table.Header>
            <Table.Row><Table.ColumnHeader>Partition</Table.ColumnHeader><Table.ColumnHeader>State</Table.ColumnHeader><Table.ColumnHeader>Nodes</Table.ColumnHeader><Table.ColumnHeader>Total</Table.ColumnHeader></Table.Row>
          </Table.Header>
          <Table.Body>
            {partitions.map((partition) => (
              <Table.Row key={partition.name}>
                <Table.Cell>
                  <strong>{partition.name}</strong>
                </Table.Cell>
                <Table.Cell>
                  <StatusBadge status={partition.state === 'up' ? 'up' : 'warning'} label={partition.state || 'unknown'} />
                </Table.Cell>
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
            <Table.Root size="sm" aria-label="Slurm compute nodes">
              <Table.Header>
                <Table.Row><Table.ColumnHeader>Node</Table.ColumnHeader><Table.ColumnHeader>State</Table.ColumnHeader><Table.ColumnHeader>CPUs</Table.ColumnHeader><Table.ColumnHeader>Memory</Table.ColumnHeader><Table.ColumnHeader>GRES</Table.ColumnHeader><Table.ColumnHeader>Partitions</Table.ColumnHeader><Table.ColumnHeader>Address</Table.ColumnHeader></Table.Row>
              </Table.Header>
              <Table.Body>
                {live.map((node) => {
                  const server = matchServer(serverIndex, node.name, node.address)
                  return (
                    <Table.Row key={node.name} {...rowNav(server, onSelect)}>
                      <Table.Cell>
                        <span className="sw-cell-inline">
                          <strong>{node.name}</strong>
                          <CopyButton value={node.name} label="Copy node name" />
                        </span>
                      </Table.Cell>
                      <Table.Cell>
                        <StatusBadge status={slurmNodeStateTone(node.state)} label={node.state || 'unknown'} />
                      </Table.Cell>
                      <Table.Cell>{node.cpus || '-'}</Table.Cell>
                      <Table.Cell>{formatMemory(node.realMemoryMiB)}</Table.Cell>
                      <Table.Cell className="mono">{node.gres || '-'}</Table.Cell>
                      <Table.Cell>{node.partitions.length > 0 ? node.partitions.join(', ') : '-'}</Table.Cell>
                      <Table.Cell className="mono">{node.address || '-'}</Table.Cell>
                    </Table.Row>
                  )
                })}
              </Table.Body>
            </Table.Root>
          </StickyTableFrame>
        )
      ) : computeMembers.length === 0 ? (
        <EmptyState title="No compute nodes" message="No Server currently reports Slurm membership." />
      ) : (
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Slurm compute nodes">
            <Table.Header>
              <Table.Row><Table.ColumnHeader>Node</Table.ColumnHeader><Table.ColumnHeader>State</Table.ColumnHeader><Table.ColumnHeader>Partition</Table.ColumnHeader><Table.ColumnHeader>Address</Table.ColumnHeader></Table.Row>
            </Table.Header>
            <Table.Body>
              {computeMembers.map((server) => {
                const nodeName = server.membership?.nodeName || serverDisplayName(server)
                const address = serverPrimaryAddress(server)
                const state = server.membership?.state ?? 'unknown'
                return (
                  <Table.Row key={server.id} {...rowNav(server, onSelect)}>
                    <Table.Cell>
                      <span className="sw-cell-inline">
                        <strong>{nodeName}</strong>
                        <CopyButton value={nodeName} label="Copy node name" />
                      </span>
                    </Table.Cell>
                    <Table.Cell>
                      <StatusBadge status={slurmNodeStateTone(state)} label={state} />
                    </Table.Cell>
                    <Table.Cell>{server.membership?.role || '-'}</Table.Cell>
                    <Table.Cell className="mono">
                      <span className="sw-cell-inline">
                        {address ?? '-'}
                        <CopyButton value={address ?? ''} label="Copy address" />
                      </span>
                    </Table.Cell>
                  </Table.Row>
                )
              })}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      )}
    </section>
  )
}

/**
 * slurmNodeStateTone maps a Slurm node scheduler state to a StatusBadge colour family (the label
 * always shows the real word). This is scheduler state, not monitoring health: idle is a healthy
 * available node, allocated/mixed are running, drain is maintenance, down/fail is a problem.
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
