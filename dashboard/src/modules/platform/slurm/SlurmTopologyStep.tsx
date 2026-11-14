import { Badge, Field, Input, Stack, Table, Text } from '@chakra-ui/react'
import type { SlurmNodeGroupSpec, SlurmPartitionSpec } from '@/domain/platform/types'
import type { DeploymentTemplate } from '@/domain/provisioning/types'
import { serverDisplayName, type Server } from '@/domain/server/types'
import { StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Select } from '@/presentation/components/ui/select'
import { useMediaQuery } from '@/presentation/hooks/useMediaQuery'
import { serverGPUSummary } from './slurmTopology'
import type { SlurmTopologyEdits } from './slurmTopologyDraft'

interface SlurmTopologyStepProps {
  /** The suggested groups and partitions, before edits; their names key the edits. */
  suggestion: { groups: SlurmNodeGroupSpec[]; partitions: SlurmPartitionSpec[] }
  /** The groups and partitions after edits, as the request will send them. */
  effective: { groups: SlurmNodeGroupSpec[]; partitions: SlurmPartitionSpec[] }
  edits: SlurmTopologyEdits
  onEditsChange: (edits: SlurmTopologyEdits) => void
  servers: Server[]
  /** The Site's Deployment Templates; a group offers those of its members' provisioner Integration. */
  templates: DeploymentTemplate[]
}

/**
 * The deploy wizard's Groups and partitions step for a Slurm Platform (decision 083). Swallow suggests
 * a `controller` and a `login` group, one compute group per GPU product with its expected GPUs per
 * node, and a partition per compute group, the first the default; the user renames groups and
 * partitions, picks each group's Deployment Template or leaves it to the template swallow generates from
 * the OS settings, changes expectations and time limits, chooses the default, and may add a partition
 * `all` over every compute group. Groups follow the roles chosen in Nodes & Roles, so members are not
 * edited here. A problem with the result is read out by the wizard next to the disabled Next button.
 * Every control is labelled with its group or partition for assistive technology.
 */
export function SlurmTopologyStep({ suggestion, effective, edits, onEditsChange, servers, templates }: SlurmTopologyStepProps) {
  const serverOf = (id: string) => servers.find((server) => server.id === id)
  const integrationOf = (group: SlurmNodeGroupSpec) => serverOf(group.members[0] ?? '')?.status.provisioning?.integrationId ?? ''
  const setGroup = (key: string, change: Partial<SlurmTopologyEdits['groups'][string]>) =>
    onEditsChange({ ...edits, groups: { ...edits.groups, [key]: { ...edits.groups[key], ...change } } })
  const setPartition = (key: string, change: Partial<SlurmTopologyEdits['partitions'][string]>) =>
    onEditsChange({ ...edits, partitions: { ...edits.partitions, [key]: { ...edits.partitions[key], ...change } } })
  const defaultKey = suggestion.partitions.some((partition) => partition.name === edits.defaultPartition) ? edits.defaultPartition : suggestion.partitions[0]?.name ?? ''

  const wide = useMediaQuery('(min-width: 48em)')
  const effectiveName = (index: number, fallback: string) => effective.groups[index]?.name ?? fallback

  // Controls shared by the desktop tables and the narrow-screen cards; only one layout is rendered,
  // so every label stays unique on the page.
  const groupName = (group: SlurmNodeGroupSpec) => (
    <Input
      size="sm"
      aria-label={`Name of the ${group.role} group ${group.name}`}
      value={edits.groups[group.name]?.name ?? group.name}
      onChange={(event) => setGroup(group.name, { name: event.target.value.toLowerCase() })}
    />
  )
  const groupNodes = (group: SlurmNodeGroupSpec) => (
    <Stack gap="0">
      {group.members.map((id) => {
        const server = serverOf(id)
        return <Text key={id} fontSize="sm">{server ? `${serverDisplayName(server)} · ${serverGPUSummary(server)}` : id}</Text>
      })}
    </Stack>
  )
  const groupTemplate = (group: SlurmNodeGroupSpec, index: number) => {
    const integration = integrationOf(group)
    const options = [
      { value: '', label: 'Generate from the OS settings' },
      ...templates.filter((template) => !integration || template.integrationId === integration).map((template) => ({ value: template.id, label: template.name })),
    ]
    return (
      <Select
        size="sm"
        aria-label={`Deployment Template of ${effectiveName(index, group.name)}`}
        value={edits.groups[group.name]?.templateId ?? ''}
        onChange={(value) => setGroup(group.name, { templateId: value })}
        options={options}
      />
    )
  }
  const groupExpected = (group: SlurmNodeGroupSpec, index: number) => (group.role === 'compute' ? (
    <Input
      size="sm"
      inputMode="numeric"
      aria-label={`Expected GPUs per node in ${effectiveName(index, group.name)}`}
      placeholder="Not set"
      value={edits.groups[group.name]?.expectedGpusPerNode ?? (group.expectedGpusPerNode ? String(group.expectedGpusPerNode) : '')}
      onChange={(event) => setGroup(group.name, { expectedGpusPerNode: event.target.value })}
    />
  ) : <Text color="fg.muted" fontSize="sm">Not scheduled</Text>)
  const partitionName = (partition: SlurmPartitionSpec) => (
    <Input
      size="sm"
      aria-label={`Name of partition ${partition.name}`}
      value={edits.partitions[partition.name]?.name ?? partition.name}
      onChange={(event) => setPartition(partition.name, { name: event.target.value })}
    />
  )
  const partitionTime = (partition: SlurmPartitionSpec, index: number) => (
    <Input
      size="sm"
      aria-label={`Time limit of partition ${effective.partitions[index]?.name ?? partition.name}`}
      value={edits.partitions[partition.name]?.maxTime ?? partition.maxTime ?? 'INFINITE'}
      onChange={(event) => setPartition(partition.name, { maxTime: event.target.value })}
    />
  )
  const partitionDefault = (partition: SlurmPartitionSpec, index: number) => (
    <Checkbox
      checked={partition.name === defaultKey}
      onCheckedChange={(checked) => { if (checked) onEditsChange({ ...edits, defaultPartition: partition.name }) }}
      aria-label={`Make ${effective.partitions[index]?.name ?? partition.name} the default partition`}
    >
      {partition.name === defaultKey ? 'Default' : 'Make default'}
    </Checkbox>
  )

  return (
    <Stack gap="5">
      <Alert status="info" title="Groups are how the cluster grows">
        Each group installs its nodes from one Deployment Template. When Swallow installs the OS, a group without a template gets one generated from the OS settings
        and named after the Platform and the group, so nodes added later are installed the same way. Compute groups are suggested by GPU model; a node with fewer GPUs
        than its group expects is scheduled last instead of failing the deployment.
      </Alert>
      {wide ? (
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Node groups to deploy">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Group</Table.ColumnHeader>
                <Table.ColumnHeader>Nodes</Table.ColumnHeader>
                <Table.ColumnHeader>Deployment Template</Table.ColumnHeader>
                <Table.ColumnHeader>Expected GPUs per node</Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {suggestion.groups.map((group, index) => (
                <Table.Row key={group.name}>
                  <Table.Cell><Stack gap="1">{groupName(group)}<Badge variant="outline" alignSelf="flex-start">{group.role}</Badge></Stack></Table.Cell>
                  <Table.Cell>{groupNodes(group)}</Table.Cell>
                  <Table.Cell minW="220px">{groupTemplate(group, index)}</Table.Cell>
                  <Table.Cell>{groupExpected(group, index)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      ) : (
        <Stack gap="3" role="list" aria-label="Node groups to deploy">
          {suggestion.groups.map((group, index) => (
            <Stack key={group.name} gap="2" className="sw-slurm-group" role="listitem">
              <Badge variant="outline" alignSelf="flex-start">{group.role}</Badge>
              <Field.Root><Field.Label>Group</Field.Label>{groupName(group)}</Field.Root>
              {groupNodes(group)}
              <Field.Root><Field.Label>Deployment Template</Field.Label>{groupTemplate(group, index)}</Field.Root>
              {group.role === 'compute' && <Field.Root><Field.Label>Expected GPUs per node</Field.Label>{groupExpected(group, index)}</Field.Root>}
            </Stack>
          ))}
        </Stack>
      )}

      {wide ? (
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Partitions to deploy">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Partition</Table.ColumnHeader>
                <Table.ColumnHeader>Compute groups</Table.ColumnHeader>
                <Table.ColumnHeader>Time limit</Table.ColumnHeader>
                <Table.ColumnHeader>Default</Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {suggestion.partitions.map((partition, index) => (
                <Table.Row key={partition.name}>
                  <Table.Cell>{partitionName(partition)}</Table.Cell>
                  <Table.Cell>{effective.partitions[index]?.groups.join(', ')}</Table.Cell>
                  <Table.Cell>{partitionTime(partition, index)}</Table.Cell>
                  <Table.Cell>{partitionDefault(partition, index)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      ) : (
        <Stack gap="3" role="list" aria-label="Partitions to deploy">
          {suggestion.partitions.map((partition, index) => (
            <Stack key={partition.name} gap="2" className="sw-slurm-group" role="listitem">
              <Field.Root><Field.Label>Partition</Field.Label>{partitionName(partition)}</Field.Root>
              <Text fontSize="sm">Compute groups: {effective.partitions[index]?.groups.join(', ')}</Text>
              <Field.Root><Field.Label>Time limit</Field.Label>{partitionTime(partition, index)}</Field.Root>
              {partitionDefault(partition, index)}
            </Stack>
          ))}
        </Stack>
      )}
      {suggestion.partitions.length > 1 || suggestion.groups.filter((group) => group.role === 'compute').length > 1 ? (
        <Checkbox checked={edits.allPartition} onCheckedChange={(checked) => onEditsChange({ ...edits, allPartition: checked })}>
          Add a partition <strong>all</strong> over every compute group
        </Checkbox>
      ) : null}
      <Text color="fg.muted" fontSize="sm">Time limits are minutes, days-hours:minutes:seconds (for example 2-00:00:00), or INFINITE. Partitions and groups can be changed after deployment.</Text>
    </Stack>
  )
}
