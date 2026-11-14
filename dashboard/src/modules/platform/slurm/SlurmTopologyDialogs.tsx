import { useState, type ReactNode } from 'react'
import { Button, Field, Input, Stack, Text } from '@chakra-ui/react'
import type { PlatformMemberRemoval, SlurmNodeGroupSpec, SlurmPartitionSpec } from '@/domain/platform/types'
import type { DeploymentTemplate } from '@/domain/provisioning/types'
import { serverDisplayName, type Server } from '@/domain/server/types'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import type { DisplayedFailure } from '@/presentation/components/failure'
import { emptyReleaseOptions, type ReleaseOptionsValue } from '@/presentation/components/releaseOptions'
import { ReleaseOptionsFields } from '@/presentation/components/ReleaseOptionsFields'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { NODE_GROUP_NAME, PARTITION_NAME, serverGPUSummary, SLURM_TIME } from './slurmTopology'

/** What every topology form dialog shares: its open state, the write in flight, and the write's failure. */
interface TopologyDialogBase {
  open: boolean
  busy: boolean
  error: DisplayedFailure | null
  onClose: () => void
}

/**
 * The frame of a topology form: a modal form whose submit button names the change, with the write's
 * failure shown above the footer so a refused change (`conflict`, `validation_error`) is read where
 * it was made. Dismissal is blocked while the write runs.
 */
function TopologyForm({ open, busy, error, onClose, title, submitLabel, canSubmit, destructive = false, onSubmit, children }: TopologyDialogBase & {
  title: string
  submitLabel: string
  canSubmit: boolean
  destructive?: boolean
  onSubmit: () => void
  children: ReactNode
}) {
  return (
    <Modal
      open={open}
      onClose={() => !busy && onClose()}
      closeOnInteractOutside={!busy}
      title={title}
      onSubmit={(event) => {
        event.preventDefault()
        if (canSubmit && !busy) onSubmit()
      }}
      footer={
        <>
          <Button variant="outline" onClick={onClose} disabled={busy}>Cancel</Button>
          <Button type="submit" colorPalette={destructive ? 'red' : 'brand'} loading={busy} disabled={!canSubmit || busy}>{submitLabel}</Button>
        </>
      }
    >
      <Stack gap="4">
        {children}
        {error && <Alert status="error" title="The change could not be started"><FailureMessage failure={error} /></Alert>}
      </Stack>
    </Modal>
  )
}

/**
 * Adds Servers of the Platform's Site to one compute group. `candidates` are the Site's Ready or
 * Deployed Servers that no Platform claims and that are not locked; each is installed from the group's
 * template (or reused when already deployed) and validated, and the existing nodes and their jobs are
 * not touched. The template's minimum hardware is checked by the API, which names a Server that falls
 * short.
 */
export function AddNodesDialog({ group, candidates, onSubmit, ...base }: TopologyDialogBase & {
  group: SlurmNodeGroupSpec
  candidates: Server[]
  onSubmit: (serverIds: string[]) => void
}) {
  const [chosen, setChosen] = useState<string[]>([])
  const toggle = (id: string, checked: boolean) => setChosen((current) => (checked ? [...current, id] : current.filter((value) => value !== id)))
  return (
    <TopologyForm
      {...base}
      title={`Add nodes to ${group.name}`}
      submitLabel={chosen.length > 1 ? `Add ${chosen.length} nodes` : 'Add node'}
      canSubmit={chosen.length > 0}
      onSubmit={() => onSubmit(chosen)}
    >
      <Text>
        Each new node is installed from the group&rsquo;s Deployment Template (a node that is already deployed keeps its OS), joins the cluster, and is
        validated on its own. Nodes already in the cluster keep running their jobs.
      </Text>
      {group.expectedGpusPerNode ? (
        <Text color="fg.muted" fontSize="sm">The group expects {group.expectedGpusPerNode} GPUs per node; a node with fewer is scheduled last and marked degraded.</Text>
      ) : null}
      {candidates.length === 0 ? (
        <Alert status="info" title="No Server can be added">
          Only Ready or Deployed Servers of this Site that no Platform uses and that are not locked can join.
        </Alert>
      ) : (
        <Stack gap="2" role="group" aria-label={`Servers to add to ${group.name}`}>
          {candidates.map((server) => (
            <Checkbox key={server.id} checked={chosen.includes(server.id)} onCheckedChange={(checked) => toggle(server.id, checked)} disabled={base.busy}>
              {serverDisplayName(server)} <Text as="span" color="fg.muted">({serverGPUSummary(server)}, {server.status.provisioning?.state ?? 'unknown'})</Text>
            </Checkbox>
          ))}
        </Stack>
      )}
    </TopologyForm>
  )
}

/**
 * Removes compute nodes from the Platform (platforms.md "Remove members"). The dialog states the
 * consequences before the confirmation: the nodes are drained and their jobs waited for up to the
 * drain timeout, then cancelled only when the user forces it; the cluster is reconfigured without them;
 * swallow's Slurm configuration is cleaned off them; and, when asked, the Servers are released. The
 * write approves its Change Plan (`approve=true`), so confirming is the approval.
 */
export function RemoveNodesDialog({ servers, onSubmit, ...base }: TopologyDialogBase & {
  servers: Array<{ id: string; name: string }>
  onSubmit: (removals: PlatformMemberRemoval[]) => void
}) {
  const [timeoutMinutes, setTimeoutMinutes] = useState('10')
  const [force, setForce] = useState(false)
  const [release, setRelease] = useState(false)
  const [releaseOptions, setReleaseOptions] = useState<ReleaseOptionsValue>(emptyReleaseOptions)
  const minutes = Number(timeoutMinutes)
  const timeoutValid = Number.isInteger(minutes) && minutes >= 0 && minutes <= 1440
  const names = servers.map((server) => server.name).join(', ')
  return (
    <TopologyForm
      {...base}
      title={servers.length > 1 ? `Remove ${servers.length} nodes` : `Remove ${names}`}
      submitLabel={servers.length > 1 ? 'Remove nodes' : 'Remove node'}
      destructive
      canSubmit={timeoutValid}
      onSubmit={() => onSubmit(servers.map((server) => ({
        serverId: server.id,
        drainTimeoutSeconds: minutes * 60,
        force,
        releaseServer: release,
        erase: release && releaseOptions.erase,
        secureErase: release && releaseOptions.secureErase,
        quickErase: release && releaseOptions.quickErase,
        unbindStaticIps: release && releaseOptions.unbindStaticIPs,
      })))}
    >
      <Text>
        {names} {servers.length > 1 ? 'leave' : 'leaves'} the Platform. Swallow drains {servers.length > 1 ? 'them' : 'it'} so no new job starts there, waits for
        the running jobs, reconfigures the cluster without {servers.length > 1 ? 'them' : 'it'}, and removes Slurm, the shared filesystem mount, and the GPU and
        monitoring configuration it installed. This cannot be undone; add the {servers.length > 1 ? 'Servers' : 'Server'} again to bring {servers.length > 1 ? 'them' : 'it'} back.
      </Text>
      <Field.Root invalid={!timeoutValid}>
        <Field.Label>Drain timeout (minutes)</Field.Label>
        <Input value={timeoutMinutes} inputMode="numeric" onChange={(event) => setTimeoutMinutes(event.target.value)} disabled={base.busy} />
        <Field.HelperText>How long running jobs are waited for, up to 1440 minutes; 0 uses the default of 10 minutes.</Field.HelperText>
        {!timeoutValid && <Field.ErrorText>Enter a whole number of minutes from 0 to 1440.</Field.ErrorText>}
      </Field.Root>
      <Checkbox checked={force} onCheckedChange={setForce} disabled={base.busy}>Cancel jobs still running at the timeout</Checkbox>
      <Text color="fg.muted" fontSize="sm">
        {force
          ? 'Jobs still running when the timeout passes are cancelled and the removal continues.'
          : 'If jobs are still running when the timeout passes, the removal stops with the node drained; retry it later or force it.'}
      </Text>
      <Checkbox checked={release} onCheckedChange={setRelease} disabled={base.busy}>Release the {servers.length > 1 ? 'Servers' : 'Server'} afterwards</Checkbox>
      <Text color="fg.muted" fontSize="sm">
        {release ? 'The provisioner takes the Server back and its OS is removed.' : 'The Server keeps its OS and returns to the Site as an unused Server.'}
      </Text>
      {release && <ReleaseOptionsFields idPrefix="slurm-remove" value={releaseOptions} onChange={setReleaseOptions} disabled={base.busy} />}
    </TopologyForm>
  )
}

/** The template choices of a group: the Platform's default first, then the Site's templates by name. */
function templateOptions(templates: DeploymentTemplate[], defaultLabel: string) {
  return [{ value: '', label: defaultLabel }, ...templates.map((template) => ({ value: template.id, label: template.name }))]
}

/**
 * Adds a compute group or changes one's template and expected GPUs. A group's template decides how its
 * new members are installed; changing it does not reinstall the members already in the group. The
 * expected GPUs per node marks a node with fewer GPUs `gpu_degraded` and schedules it last instead of
 * refusing it. A new group is placed in the partitions chosen here.
 */
export function NodeGroupDialog({ group, templates, partitions, existingNames, onSubmit, ...base }: TopologyDialogBase & {
  /** The group to change; undefined adds a compute group. */
  group?: SlurmNodeGroupSpec
  templates: DeploymentTemplate[]
  partitions: SlurmPartitionSpec[]
  existingNames: string[]
  onSubmit: (value: { name: string; templateId: string | null; expectedGpusPerNode: number | null; partitions: string[] }) => void
}) {
  const [name, setName] = useState(group?.name ?? '')
  const [templateId, setTemplateId] = useState(group?.templateId ?? '')
  const [expected, setExpected] = useState(group?.expectedGpusPerNode ? String(group.expectedGpusPerNode) : '')
  const [chosenPartitions, setChosenPartitions] = useState<string[]>([])
  const adding = !group
  const nameProblem = !adding ? '' : !NODE_GROUP_NAME.test(name) ? 'Use 1 to 32 lower-case letters, digits, or hyphens.' : existingNames.includes(name) ? 'Another group has this name.' : ''
  const expectedNumber = expected.trim() === '' ? null : Number(expected)
  const expectedValid = expectedNumber === null || (Number.isInteger(expectedNumber) && expectedNumber > 0)
  const partitionsValid = !adding || chosenPartitions.length > 0
  return (
    <TopologyForm
      {...base}
      title={adding ? 'Add compute group' : `Change ${group.name}`}
      submitLabel={adding ? 'Add group' : 'Save group'}
      canSubmit={!nameProblem && expectedValid && partitionsValid}
      onSubmit={() => onSubmit({ name, templateId: templateId || null, expectedGpusPerNode: expectedNumber, partitions: chosenPartitions })}
    >
      {adding && (
        <Field.Root required invalid={Boolean(nameProblem) && name !== ''}>
          <Field.Label>Group name <Field.RequiredIndicator /></Field.Label>
          <Input value={name} onChange={(event) => setName(event.target.value.toLowerCase())} placeholder="mi300x" disabled={base.busy} />
          <Field.HelperText>Also a Slurm feature of its nodes, for --constraint.</Field.HelperText>
          {nameProblem && name !== '' && <Field.ErrorText>{nameProblem}</Field.ErrorText>}
        </Field.Root>
      )}
      <Field.Root>
        <Field.Label>Deployment Template</Field.Label>
        <Select value={templateId} onChange={setTemplateId} aria-label="Deployment Template" options={templateOptions(templates, 'The Platform’s default template')} disabled={base.busy} />
        <Field.HelperText>New nodes of this group are installed from it. Nodes already in the group are not reinstalled.</Field.HelperText>
      </Field.Root>
      <Field.Root invalid={!expectedValid}>
        <Field.Label>Expected GPUs per node</Field.Label>
        <Input value={expected} inputMode="numeric" onChange={(event) => setExpected(event.target.value)} placeholder="Not set" disabled={base.busy} />
        <Field.HelperText>A node with fewer GPUs is marked gpu_degraded and scheduled last. Leave empty for none.</Field.HelperText>
        {!expectedValid && <Field.ErrorText>Enter a whole number greater than 0, or leave it empty.</Field.ErrorText>}
      </Field.Root>
      {adding && (
        <Stack gap="2" role="group" aria-label="Partitions for the new group">
          <Text fontWeight="medium">Partitions</Text>
          {partitions.map((partition) => (
            <Checkbox
              key={partition.name}
              checked={chosenPartitions.includes(partition.name)}
              onCheckedChange={(checked) => setChosenPartitions((current) => (checked ? [...current, partition.name] : current.filter((value) => value !== partition.name)))}
              disabled={base.busy}
            >
              {partition.name}
            </Checkbox>
          ))}
          {!partitionsValid && <Text color="fg.muted" fontSize="sm">Choose at least one partition, or add a partition for the group afterwards.</Text>}
        </Stack>
      )}
    </TopologyForm>
  )
}

const OVER_SUBSCRIBE_OPTIONS = [
  { value: 'NO', label: 'No (jobs share nodes, CPUs are not oversubscribed)' },
  { value: 'EXCLUSIVE', label: 'Exclusive (one job per node)' },
  { value: 'YES', label: 'Yes (on request)' },
  { value: 'FORCE', label: 'Force' },
]

/**
 * Adds or changes a partition: its compute groups, whether it is the default (making one default
 * clears the others), its time limit, default memory per CPU, sharing, and the Unix groups allowed to
 * use it. Applied in place with `scontrol reconfigure`; running jobs keep running.
 */
export function PartitionDialog({ partition, computeGroups, existingNames, omnistat, onSubmit, ...base }: TopologyDialogBase & {
  partition?: SlurmPartitionSpec
  computeGroups: string[]
  existingNames: string[]
  /** Omnistat makes every partition hand out whole nodes, so the sharing choice is shown as fixed. */
  omnistat: boolean
  onSubmit: (value: SlurmPartitionSpec) => void
}) {
  const [name, setName] = useState(partition?.name ?? '')
  const [isDefault, setDefault] = useState(partition?.default ?? false)
  const [groups, setGroups] = useState<string[]>(partition?.groups ?? [])
  const [maxTime, setMaxTime] = useState(partition?.maxTime ?? 'INFINITE')
  const [defMem, setDefMem] = useState(partition?.defMemPerCpu ? String(partition.defMemPerCpu) : '')
  const [overSubscribe, setOverSubscribe] = useState<string>(partition?.overSubscribe ?? 'NO')
  const [allowGroups, setAllowGroups] = useState((partition?.allowGroups ?? []).join(', '))
  const nameProblem = !PARTITION_NAME.test(name)
    ? 'Use up to 32 letters, digits, underscores, or hyphens.'
    : existingNames.filter((existing) => existing !== partition?.name).includes(name) ? 'Another partition has this name.' : ''
  const timeValid = SLURM_TIME.test(maxTime.trim())
  const defMemNumber = defMem.trim() === '' ? null : Number(defMem)
  const defMemValid = defMemNumber === null || (Number.isInteger(defMemNumber) && defMemNumber > 0)
  const allowed = allowGroups.split(',').map((value) => value.trim()).filter(Boolean)
  return (
    <TopologyForm
      {...base}
      title={partition ? `Change partition ${partition.name}` : 'Add partition'}
      submitLabel={partition ? 'Save partition' : 'Add partition'}
      canSubmit={!nameProblem && timeValid && defMemValid && groups.length > 0}
      onSubmit={() => onSubmit({
        name,
        default: isDefault,
        groups,
        maxTime: maxTime.trim(),
        ...(defMemNumber ? { defMemPerCpu: defMemNumber } : {}),
        ...(overSubscribe !== 'NO' ? { overSubscribe: overSubscribe as SlurmPartitionSpec['overSubscribe'] } : {}),
        ...(allowed.length > 0 ? { allowGroups: allowed } : {}),
      })}
    >
      <Field.Root required invalid={Boolean(nameProblem) && name !== ''}>
        <Field.Label>Partition name <Field.RequiredIndicator /></Field.Label>
        <Input value={name} onChange={(event) => setName(event.target.value)} placeholder="gpu" disabled={base.busy} />
        {nameProblem && name !== '' && <Field.ErrorText>{nameProblem}</Field.ErrorText>}
      </Field.Root>
      <Stack gap="2" role="group" aria-label="Compute groups in the partition">
        <Text fontWeight="medium">Compute groups</Text>
        {computeGroups.map((group) => (
          <Checkbox key={group} checked={groups.includes(group)} onCheckedChange={(checked) => setGroups((current) => (checked ? [...current, group] : current.filter((value) => value !== group)))} disabled={base.busy}>
            {group}
          </Checkbox>
        ))}
        {groups.length === 0 && <Text color="fg.muted" fontSize="sm">Choose at least one compute group.</Text>}
      </Stack>
      <Checkbox checked={isDefault} onCheckedChange={setDefault} disabled={base.busy || partition?.default}>Default partition</Checkbox>
      <Text color="fg.muted" fontSize="sm">
        {partition?.default ? 'This is the default partition; make another partition the default to change it.' : 'Jobs that name no partition run in the default one. Making this the default replaces the current default.'}
      </Text>
      <Field.Root invalid={!timeValid}>
        <Field.Label>Time limit</Field.Label>
        <Input value={maxTime} onChange={(event) => setMaxTime(event.target.value)} disabled={base.busy} />
        <Field.HelperText>Minutes, days-hours:minutes:seconds (for example 2-00:00:00), or INFINITE.</Field.HelperText>
        {!timeValid && <Field.ErrorText>Slurm does not read this time limit.</Field.ErrorText>}
      </Field.Root>
      <Field.Root invalid={!defMemValid}>
        <Field.Label>Default memory per CPU (MiB)</Field.Label>
        <Input value={defMem} inputMode="numeric" onChange={(event) => setDefMem(event.target.value)} placeholder="Smallest share of the partition’s nodes" disabled={base.busy} />
        {!defMemValid && <Field.ErrorText>Enter a whole number of MiB greater than 0, or leave it empty.</Field.ErrorText>}
      </Field.Root>
      <Field.Root>
        <Field.Label>Node sharing</Field.Label>
        <Select value={omnistat ? 'EXCLUSIVE' : overSubscribe} onChange={setOverSubscribe} aria-label="Node sharing" options={OVER_SUBSCRIBE_OPTIONS} disabled={base.busy || omnistat} />
        {omnistat && <Field.HelperText>Omnistat attributes a whole node to one job, so every partition hands out whole nodes.</Field.HelperText>}
      </Field.Root>
      <Field.Root>
        <Field.Label>Allowed Unix groups</Field.Label>
        <Input value={allowGroups} onChange={(event) => setAllowGroups(event.target.value)} placeholder="Everyone" disabled={base.busy} />
        <Field.HelperText>Comma-separated; leave empty to let every user submit.</Field.HelperText>
      </Field.Root>
    </TopologyForm>
  )
}
