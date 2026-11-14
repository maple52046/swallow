import type { SlurmNodeGroupSpec, SlurmPartitionSpec } from '@/domain/platform/types'
import type { SlurmTopology } from './slurmTopology'

/** The deploy wizard's edits to one suggested group, keyed by the suggested name; empty fields keep the suggestion. */
export interface SlurmGroupEdit {
  name?: string
  /** A Deployment Template id; empty generates one from the OS settings (or uses the shell's template). */
  templateId?: string
  /** Typed text; empty sets no expectation. */
  expectedGpusPerNode?: string
}

/** The wizard's edits to one suggested partition, keyed by the suggested name. */
export interface SlurmPartitionEdit {
  name?: string
  maxTime?: string
}

/**
 * The wizard's edits over the suggested groups and partitions (decision 083). The suggestion follows
 * the selected nodes, so edits are kept by the suggested names and survive a change of selection that
 * keeps a group; `defaultPartition` names the suggested partition that is the default (empty keeps the
 * first), and `allPartition` adds a partition `all` over every compute group.
 */
export interface SlurmTopologyEdits {
  groups: Readonly<Record<string, SlurmGroupEdit>>
  partitions: Readonly<Record<string, SlurmPartitionEdit>>
  defaultPartition: string
  allPartition: boolean
}

/** No edits: deploy the suggestion as it is. */
export const NO_TOPOLOGY_EDITS: SlurmTopologyEdits = { groups: {}, partitions: {}, defaultPartition: '', allPartition: false }

/**
 * Applies the edits to the suggestion and returns the groups and partitions the deploy request sends.
 * A renamed group is renamed in every partition that holds it. An expectation that is not a whole
 * number above zero is sent as typed so the topology check names it.
 */
export function applyTopologyEdits(suggestion: Pick<SlurmTopology, 'groups' | 'partitions'>, edits: SlurmTopologyEdits): Pick<SlurmTopology, 'groups' | 'partitions'> & { invalidExpectation: string } {
  const renamed = new Map<string, string>()
  let invalidExpectation = ''
  const groups: SlurmNodeGroupSpec[] = suggestion.groups.map((group) => {
    const edit = edits.groups[group.name] ?? {}
    const name = edit.name?.trim() || group.name
    renamed.set(group.name, name)
    let expected = group.expectedGpusPerNode
    if (edit.expectedGpusPerNode !== undefined) {
      const text = edit.expectedGpusPerNode.trim()
      const number = Number(text)
      if (text === '') expected = undefined
      else if (Number.isInteger(number) && number > 0) expected = number
      else invalidExpectation = `Expected GPUs per node for ${name} must be a whole number greater than 0, or empty.`
    }
    return {
      name,
      role: group.role,
      ...(edit.templateId ? { templateId: edit.templateId } : {}),
      ...(expected ? { expectedGpusPerNode: expected } : {}),
      members: group.members,
    }
  })
  const defaultKey = suggestion.partitions.some((partition) => partition.name === edits.defaultPartition) ? edits.defaultPartition : suggestion.partitions[0]?.name
  const partitions: SlurmPartitionSpec[] = suggestion.partitions.map((partition) => {
    const edit = edits.partitions[partition.name] ?? {}
    return {
      name: edit.name?.trim() || partition.name,
      default: partition.name === defaultKey,
      groups: partition.groups.map((group) => renamed.get(group) ?? group),
      maxTime: edit.maxTime?.trim() || partition.maxTime || 'INFINITE',
    }
  })
  if (edits.allPartition) {
    const computeNames = groups.filter((group) => group.role === 'compute').map((group) => group.name)
    if (computeNames.length > 0) partitions.push({ name: 'all', default: false, groups: computeNames, maxTime: 'INFINITE' })
  }
  return { groups, partitions, invalidExpectation }
}
