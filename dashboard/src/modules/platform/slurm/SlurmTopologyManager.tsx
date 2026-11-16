import { useEffect, useMemo, useState } from 'react'
import { Badge, Button, HStack, Stack, Table, Text } from '@chakra-ui/react'
import { Plus, RefreshCw } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import type { Platform } from '@/domain/platform/types'
import { templatesForPlatformPool, type DeploymentNetworkMode, type DeploymentTemplate } from '@/domain/provisioning/types'
import { serverDisplayName, type Server } from '@/domain/server/types'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf } from '@/presentation/components/failure'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { useDeployableServers } from '@/presentation/pages/platforms/useDeployableServers'
import { useSlurmPlatform } from './SlurmModuleProvider'
import { AddNodesDialog, NodeGroupDialog, PartitionDialog, RemoveNodesDialog } from './SlurmTopologyDialogs'
import { computeGPUCount, readSlurmTopology, serverGPUSummary } from './slurmTopology'
import { useSlurmTopologyWrite } from './useSlurmTopologyWrite'

const CANDIDATE_STATES = ['ready', 'deployed'] as const

/** The dialog the manager has open; one at a time, so a write's failure belongs to exactly one. */
type OpenDialog =
  | { kind: 'add-nodes'; group: string }
  | { kind: 'remove-nodes'; serverIds: string[] }
  | { kind: 'group'; group?: string }
  | { kind: 'partition'; partition?: string }
  | { kind: 'remove-partition'; partition: string }
  | { kind: 'rescan'; serverId: string }

interface SlurmTopologyManagerProps {
  platform: Platform
  /** Servers the Platform reports or claims, for names and GPU inventory. */
  members: Server[]
  /** Whether the user may change the Platform (an admin or an owner of its Pool). */
  canOperate: boolean
  onChanged: () => void
  onOpenServer: (serverId: string) => void
}

/**
 * The Slurm workspace's management of node groups and partitions (platforms.md "Node groups and
 * partitions", "Remove members", "Rescan A Slurm Node"; decision 083), rendered by the Partitions
 * route above the live partition state.
 *
 * It shows the desired spec: each partition with its compute groups and limits, and each group with
 * its template, expected GPUs, and members, where a compute member shows its GPU count against the
 * group's expectation in text ("7 of 8 GPUs") and whether it was installed from the template's current
 * content. A user who may operate the Platform adds and removes compute nodes, adds and changes
 * compute groups and partitions, and rescans a node; controller and login groups say why they are not
 * scaled here. Every change is one spec write through the Slurm port, confirmed in a dialog that names
 * its consequences; whether it is destructive, and why a Change Plan waits, comes from the API. The
 * API's warnings stay above the sections until dismissed. Changes are offered only while the Platform
 * is active and no other change of it runs, with the reason in text otherwise.
 */
export function SlurmTopologyManager({ platform, members, canOperate, onChanged, onOpenServer }: SlurmTopologyManagerProps) {
  const { provisioning } = useApp()
  const slurm = useSlurmPlatform()
  const { showToast } = useToast()
  const write = useSlurmTopologyWrite(platform, onChanged)
  const [dialog, setDialog] = useState<OpenDialog | null>(null)
  const [templates, setTemplates] = useState<DeploymentTemplate[]>([])
  const [candidateRevision, setCandidateRevision] = useState(0)
  const deployable = useDeployableServers(platform.siteId, CANDIDATE_STATES, candidateRevision)

  // The Site's templates name each group's template and fill the group dialog. A failed read leaves
  // the ids visible instead of names; it does not block managing the topology.
  useEffect(() => {
    let cancelled = false
    provisioning.listTemplates({ siteId: platform.siteId })
      .then((items) => { if (!cancelled) setTemplates(items) })
      .catch(() => { if (!cancelled) setTemplates([]) })
    return () => { cancelled = true }
  }, [platform.siteId, provisioning])

  const spec = platform.spec
  const topology = useMemo(() => (spec ? readSlurmTopology(spec) : null), [spec])
  const sitesServers = deployable.status === 'ready' ? deployable.data.servers : []
  const serverById = (id: string) => members.find((server) => server.id === id) ?? sitesServers.find((server) => server.id === id)
  const serverName = (id: string) => {
    const server = serverById(id)
    return server ? serverDisplayName(server) : id
  }
  const templateName = (id: string | undefined) => {
    if (!id) return 'The Platform’s default'
    const generated = spec?.machinePreparation?.generatedTemplates?.find((template) => template.id === id)
    return templates.find((template) => template.id === id)?.name ?? (generated ? `${generated.name} (created with the next change)` : id)
  }

  if (!spec || !topology) {
    return <Alert status="info" title="Node groups appear once swallow adopts the Platform">Swallow reads the Platform&rsquo;s spec shortly after the API starts.</Alert>
  }

  const changing = Boolean(platform.status.realizingWorkflowId)
  const blockedReason = !canOperate
    ? 'Only an owner of the Platform’s Pool or an admin can change nodes, groups, and partitions.'
    : platform.lifecycleState !== 'active'
      ? 'Nodes, groups, and partitions can be changed once the Platform is active.'
      : changing ? 'A change of this Platform is being carried out; wait for it to finish.' : ''
  const editable = blockedReason === ''
  const computeGroups = topology.groups.filter((group) => group.role === 'compute')
  const computeMembers = computeGroups.flatMap((group) => group.members)
  const candidates = sitesServers.filter((server) => !server.status.platformClaim && !server.status.provisioning?.locked && !spec.members.some((member) => member.serverId === server.id))
  const omnistat = (() => {
    const value = spec.settings.omnistat
    return typeof value === 'object' && value !== null && (value as { enabled?: unknown }).enabled === true
  })()
  // The network mode a group's new nodes deploy with: its template's, else the machine preparation's
  // template's, else the machine preparation's own (a template the Platform generates copies it).
  // Null when swallow does not install the OS, so there is no network to override.
  const groupNetworkMode = (group: (typeof topology.groups)[number]): DeploymentNetworkMode | null => {
    const preparation = spec.machinePreparation
    if (!preparation || preparation.mode !== 'provision_os') return null
    const template = templates.find((candidate) => candidate.id === (group.templateId ?? preparation.templateId))
    const mode = template?.network.mode ?? preparation.network?.mode
    return mode === 'static' || mode === 'dhcp' ? mode : 'automatic'
  }
  const memberTemplate = (serverId: string) => platform.status.memberTemplates.find((record) => record.serverId === serverId)

  const closeDialog = () => {
    if (write.busy) return
    write.clearError()
    setDialog(null)
  }
  const done = (accepted: boolean) => {
    if (!accepted) return
    setDialog(null)
    setCandidateRevision((value) => value + 1)
  }

  const rescan = async (serverId: string) => {
    try {
      await slurm.rescanNode(platform.id, serverId)
      setDialog(null)
      showToast({ tone: 'success', title: `Re-detecting the GPUs of ${serverName(serverId)}`, description: 'The cluster is reconfigured with what the node reports; the node is validated on its own.' })
      onChanged()
    } catch (caught) {
      showToast({ tone: 'error', title: 'The rescan could not be started', description: failureOf(caught, 'Unknown error').message })
    }
  }

  // Cell renderers shared by the desktop tables and the mobile cards, so both say the same thing.
  const partitionTitle = (partition: (typeof topology.partitions)[number]) => (
    <HStack gap="2"><strong>{partition.name}</strong>{partition.default && <Badge variant="subtle" colorPalette="blue">Default</Badge>}</HStack>
  )
  const partitionMemory = (partition: (typeof topology.partitions)[number]) => (partition.defMemPerCpu ? `${partition.defMemPerCpu} MiB` : 'From its nodes')
  const partitionSharing = (partition: (typeof topology.partitions)[number]) => {
    if (omnistat) return 'Whole nodes (Omnistat)'
    if (partition.overSubscribe === 'EXCLUSIVE') return 'Whole nodes'
    return partition.overSubscribe && partition.overSubscribe !== 'NO' ? partition.overSubscribe : 'Shared nodes'
  }
  const partitionActions = (partition: (typeof topology.partitions)[number]) => (
    <HStack gap="1" justify="flex-end">
      <Button size="xs" variant="plain" colorPalette="brand" onClick={() => setDialog({ kind: 'partition', partition: partition.name })} aria-label={`Change partition ${partition.name}`}>Change</Button>
      {!partition.default && (
        <Button size="xs" variant="plain" colorPalette="red" onClick={() => setDialog({ kind: 'remove-partition', partition: partition.name })} aria-label={`Remove partition ${partition.name}`}>Remove</Button>
      )}
    </HStack>
  )
  const memberLink = (serverId: string) => (
    <Button variant="plain" size="sm" px="0" h="auto" colorPalette="brand" onClick={() => onOpenServer(serverId)}>{serverName(serverId)}</Button>
  )
  // A compute node's GPUs against its group's expectation, in text; the degraded badge carries words.
  const memberGPUs = (serverId: string, group: (typeof topology.groups)[number]) => {
    const server = serverById(serverId)
    if (!server) return 'Unknown'
    const count = computeGPUCount(server)
    const expected = group.expectedGpusPerNode
    if (group.role !== 'compute' || !expected) return serverGPUSummary(server)
    return (
      <HStack gap="2" flexWrap="wrap">
        <span>{count} of {expected} GPUs</span>
        {count < expected && <StatusBadge status="warning" label="Degraded: scheduled last" />}
      </HStack>
    )
  }
  const memberInstalledFrom = (serverId: string) => {
    const record = memberTemplate(serverId)
    if (!record) return 'Not recorded'
    return record.matchesTemplate ? templateName(record.templateId) : `${templateName(record.templateId)} (the template changed since)`
  }
  const memberActions = (serverId: string, lastCompute: boolean) => (
    <HStack gap="1" justify="flex-end">
      <Button size="xs" variant="plain" colorPalette="brand" onClick={() => setDialog({ kind: 'rescan', serverId })} aria-label={`Rescan GPUs of ${serverName(serverId)}`}>
        <RefreshCw size={12} />Rescan
      </Button>
      <Button
        size="xs"
        variant="plain"
        colorPalette="red"
        disabled={lastCompute}
        title={lastCompute ? 'The last compute node cannot be removed; delete the Platform instead.' : undefined}
        onClick={() => setDialog({ kind: 'remove-nodes', serverIds: [serverId] })}
        aria-label={lastCompute ? `Remove ${serverName(serverId)}, unavailable: the last compute node cannot be removed` : `Remove ${serverName(serverId)}`}
      >
        Remove
      </Button>
    </HStack>
  )

  const openDialog = dialog
  const dialogGroup = openDialog && (openDialog.kind === 'add-nodes' || openDialog.kind === 'group') ? topology.groups.find((group) => group.name === openDialog.group) : undefined
  const dialogPartition = openDialog && (openDialog.kind === 'partition' || openDialog.kind === 'remove-partition') ? topology.partitions.find((partition) => partition.name === openDialog.partition) : undefined

  return (
    <Stack gap="6">
      {topology.derived && (
        <Alert status="info" title="Groups from the Platform’s roles">
          This Platform was deployed before node groups. Its nodes are shown in one group per role and the partition main, the same partition it always had; the next
          change stores them.
        </Alert>
      )}
      {write.warnings.length > 0 && (
        <Alert status="warning" title="The change was accepted with warnings">
          <Stack gap="1">
            {write.warnings.map((warning) => <Text key={warning}>{warning}</Text>)}
            <Button size="sm" variant="plain" alignSelf="flex-start" onClick={write.dismissWarnings}>Dismiss warnings</Button>
          </Stack>
        </Alert>
      )}
      {blockedReason && <Text color="fg.muted" fontSize="sm">{blockedReason}</Text>}

      <section className="sw-section" aria-label="Partitions in the spec">
        <SectionHeader
          title="Partitions"
          description="The scheduling queues of the Platform's spec and the compute groups in each. Add or remove nodes on a group below."
          actions={editable ? <Button size="sm" variant="outline" onClick={() => setDialog({ kind: 'partition' })}><Plus size={14} />Add partition</Button> : undefined}
        />
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Partitions in the spec">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Partition</Table.ColumnHeader>
                    <Table.ColumnHeader>Compute groups</Table.ColumnHeader>
                    <Table.ColumnHeader>Time limit</Table.ColumnHeader>
                    <Table.ColumnHeader>Memory per CPU</Table.ColumnHeader>
                    <Table.ColumnHeader>Sharing</Table.ColumnHeader>
                    <Table.ColumnHeader>Access</Table.ColumnHeader>
                    {editable && <Table.ColumnHeader aria-label="Actions" />}
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {topology.partitions.map((partition) => (
                    <Table.Row key={partition.name}>
                      <Table.Cell>{partitionTitle(partition)}</Table.Cell>
                      <Table.Cell>{partition.groups.join(', ')}</Table.Cell>
                      <Table.Cell>{partition.maxTime ?? 'INFINITE'}</Table.Cell>
                      <Table.Cell>{partitionMemory(partition)}</Table.Cell>
                      <Table.Cell>{partitionSharing(partition)}</Table.Cell>
                      <Table.Cell>{partition.allowGroups?.join(', ') ?? 'Everyone'}</Table.Cell>
                      {editable && <Table.Cell textAlign="end">{partitionActions(partition)}</Table.Cell>}
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list" aria-label="Partitions in the spec">
              {topology.partitions.map((partition) => (
                <ResourceCard key={partition.name} title={partitionTitle(partition)} actions={editable ? partitionActions(partition) : undefined}>
                  <ResourceCardField label="Compute groups">{partition.groups.join(', ')}</ResourceCardField>
                  <ResourceCardField label="Time limit">{partition.maxTime ?? 'INFINITE'}</ResourceCardField>
                  <ResourceCardField label="Memory per CPU">{partitionMemory(partition)}</ResourceCardField>
                  <ResourceCardField label="Sharing">{partitionSharing(partition)}</ResourceCardField>
                  <ResourceCardField label="Access">{partition.allowGroups?.join(', ') ?? 'Everyone'}</ResourceCardField>
                </ResourceCard>
              ))}
            </div>
          }
        />
      </section>

      <section className="sw-section" aria-label="Node groups">
        <SectionHeader
          title="Node groups"
          description="Each group installs its new nodes from one Deployment Template. Only compute groups grow or shrink."
          actions={editable ? <Button size="sm" variant="outline" onClick={() => setDialog({ kind: 'group' })}><Plus size={14} />Add compute group</Button> : undefined}
        />
        <Stack gap="4">
          {topology.groups.map((group) => {
            const compute = group.role === 'compute'
            const lastCompute = computeMembers.length <= 1
            return (
              <div key={group.name} className="sw-slurm-group" role="group" aria-label={`Node group ${group.name}`}>
                <HStack justify="space-between" flexWrap="wrap" gap="2">
                  <Stack gap="0">
                    <HStack gap="2"><strong>{group.name}</strong><Badge variant="outline">{group.role}</Badge></HStack>
                    <Text color="fg.muted" fontSize="sm">
                      Template: {templateName(group.templateId)}
                      {group.expectedGpusPerNode ? ` · expects ${group.expectedGpusPerNode} GPUs per node` : ''}
                    </Text>
                  </Stack>
                  {editable && compute && (
                    <HStack gap="2">
                      <Button size="sm" variant="outline" onClick={() => setDialog({ kind: 'group', group: group.name })} aria-label={`Change group ${group.name}`}>Change</Button>
                      <Button size="sm" colorPalette="brand" onClick={() => setDialog({ kind: 'add-nodes', group: group.name })} aria-label={`Add nodes to ${group.name}`}><Plus size={14} />Add nodes</Button>
                    </HStack>
                  )}
                </HStack>
                {!compute && (
                  <Text color="fg.muted" fontSize="sm">
                    {group.role === 'controller' ? 'Controllers' : 'Login nodes'} are not added or removed here: swallow scales only compute nodes, because moving
                    the cluster&rsquo;s control plane needs its state and shared storage moved too.
                  </Text>
                )}
                {group.members.length === 0 ? (
                  <Text color="fg.muted" fontSize="sm">No nodes yet.</Text>
                ) : (
                  <ResponsiveDataView
                    desktop={
                      <Table.Root size="sm" aria-label={`Nodes in ${group.name}`}>
                        <Table.Header>
                          <Table.Row>
                            <Table.ColumnHeader>Node</Table.ColumnHeader>
                            <Table.ColumnHeader>GPUs</Table.ColumnHeader>
                            <Table.ColumnHeader>Installed from</Table.ColumnHeader>
                            {editable && compute && <Table.ColumnHeader aria-label="Actions" />}
                          </Table.Row>
                        </Table.Header>
                        <Table.Body>
                          {group.members.map((serverId) => (
                            <Table.Row key={serverId}>
                              <Table.Cell>{memberLink(serverId)}</Table.Cell>
                              <Table.Cell>{memberGPUs(serverId, group)}</Table.Cell>
                              <Table.Cell>{memberInstalledFrom(serverId)}</Table.Cell>
                              {editable && compute && <Table.Cell textAlign="end">{memberActions(serverId, lastCompute)}</Table.Cell>}
                            </Table.Row>
                          ))}
                        </Table.Body>
                      </Table.Root>
                    }
                    mobile={
                      <div className="sw-resource-card-list" aria-label={`Nodes in ${group.name}`}>
                        {group.members.map((serverId) => (
                          <ResourceCard key={serverId} title={memberLink(serverId)} actions={editable && compute ? memberActions(serverId, lastCompute) : undefined}>
                            <ResourceCardField label="GPUs">{memberGPUs(serverId, group)}</ResourceCardField>
                            <ResourceCardField label="Installed from">{memberInstalledFrom(serverId)}</ResourceCardField>
                          </ResourceCard>
                        ))}
                      </div>
                    }
                  />
                )}
              </div>
            )
          })}
        </Stack>
      </section>

      {openDialog?.kind === 'add-nodes' && dialogGroup && (
        <AddNodesDialog
          open
          busy={write.busy}
          error={write.error}
          onClose={closeDialog}
          group={dialogGroup}
          candidates={candidates}
          networkMode={groupNetworkMode(dialogGroup)}
          onSubmit={(serverIds, networks) => void write.submit({ kind: 'add-nodes', group: dialogGroup.name, serverIds, networks }, {
            started: serverIds.length > 1 ? `Adding ${serverIds.length} nodes to ${dialogGroup.name}` : `Adding ${serverName(serverIds[0])} to ${dialogGroup.name}`,
            detail: 'The new nodes are installed from the group’s template, join the cluster, and are validated on their own.',
            planAction: 'add the nodes',
          }).then(done)}
        />
      )}
      {openDialog?.kind === 'remove-nodes' && (
        <RemoveNodesDialog
          open
          busy={write.busy}
          error={write.error}
          onClose={closeDialog}
          servers={openDialog.serverIds.map((id) => ({ id, name: serverName(id) }))}
          onSubmit={(removals) => void write.submit({ kind: 'remove-nodes', removals }, {
            started: removals.length > 1 ? `Removing ${removals.length} nodes` : `Removing ${serverName(removals[0].serverId)}`,
            detail: 'The nodes are drained, the cluster is reconfigured without them, and they are cleaned up.',
            planAction: 'remove the nodes',
          }).then(done)}
        />
      )}
      {openDialog?.kind === 'group' && (
        <NodeGroupDialog
          open
          busy={write.busy}
          error={write.error}
          onClose={closeDialog}
          group={dialogGroup}
          templates={templatesForPlatformPool(templates, platform.status.poolId, [dialogGroup?.templateId])}
          partitions={topology.partitions}
          existingNames={topology.groups.map((group) => group.name)}
          onSubmit={(value) => void write.submit(
            dialogGroup
              ? { kind: 'set-group', name: dialogGroup.name, templateId: value.templateId, expectedGpusPerNode: value.expectedGpusPerNode }
              : { kind: 'add-group', group: { name: value.name, ...(value.templateId ? { templateId: value.templateId } : {}), ...(value.expectedGpusPerNode ? { expectedGpusPerNode: value.expectedGpusPerNode } : {}) }, partitions: value.partitions },
            {
              started: dialogGroup ? `Changing ${dialogGroup.name}` : `Adding compute group ${value.name}`,
              detail: dialogGroup ? 'New nodes of the group install with the new settings; its current nodes are not reinstalled.' : 'Add nodes to the group to grow it.',
              planAction: 'apply the change',
            },
          ).then(done)}
        />
      )}
      {openDialog?.kind === 'partition' && (
        <PartitionDialog
          open
          busy={write.busy}
          error={write.error}
          onClose={closeDialog}
          partition={dialogPartition}
          computeGroups={computeGroups.map((group) => group.name)}
          existingNames={topology.partitions.map((partition) => partition.name)}
          omnistat={omnistat}
          onSubmit={(partition) => void write.submit(
            dialogPartition ? { kind: 'set-partition', previousName: dialogPartition.name, partition } : { kind: 'add-partition', partition },
            {
              started: dialogPartition ? `Changing partition ${dialogPartition.name}` : `Adding partition ${partition.name}`,
              detail: 'Slurm is reconfigured in place; running jobs keep running.',
              planAction: 'apply the change',
            },
          ).then(done)}
        />
      )}
      {openDialog?.kind === 'remove-partition' && dialogPartition && (
        <ConfirmDialog
          open
          title={`Remove partition ${dialogPartition.name}`}
          confirmLabel="Remove partition"
          busy={write.busy}
          onCancel={closeDialog}
          onConfirm={() => void write.submit({ kind: 'remove-partition', name: dialogPartition.name }, {
            started: `Removing partition ${dialogPartition.name}`,
            detail: 'Slurm is reconfigured without it; its nodes stay in the cluster.',
            planAction: 'remove the partition',
          }).then(done)}
        >
          <Stack gap="3">
            <Text>
              Jobs can no longer be submitted to {dialogPartition.name}. Its nodes stay in the cluster and in their other partitions. Swallow refuses the removal while
              the partition still has queued or running jobs, and says how many.
            </Text>
            {write.error && <Alert status="error" title="The change could not be started"><FailureMessage failure={write.error} /></Alert>}
          </Stack>
        </ConfirmDialog>
      )}
      {openDialog?.kind === 'rescan' && (
        <ConfirmDialog
          open
          title={`Rescan the GPUs of ${serverName(openDialog.serverId)}`}
          confirmLabel="Rescan"
          destructive={false}
          onCancel={() => setDialog(null)}
          onConfirm={() => void rescan(openDialog.serverId)}
        >
          <Text>
            Swallow re-detects the node&rsquo;s GPUs and reconfigures the cluster so Slurm hands out the GPUs the node has now, for example after a GPU failed.
            Only this node is detected again and validated; no node is redeployed, and the others keep running their jobs.
          </Text>
        </ConfirmDialog>
      )}
    </Stack>
  )
}
