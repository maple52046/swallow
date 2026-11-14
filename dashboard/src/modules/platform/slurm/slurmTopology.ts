import type { PlatformMemberRemoval, PlatformSpec, SlurmNodeGroupSpec, SlurmPartitionSpec } from '@/domain/platform/types'
import type { Server, ServerGPU } from '@/domain/server/types'

/**
 * A Slurm Platform's node groups and partitions as the spec stores them (platforms.md "Node groups
 * and partitions", decision 083), and the spec edits the Slurm workspace and the CLI make. Pure: the
 * API validates every write and classifies it; nothing here decides whether a change is destructive.
 */
export interface SlurmTopology {
  groups: SlurmNodeGroupSpec[]
  partitions: SlurmPartitionSpec[]
  /**
   * The spec had no groups or partitions and these follow from its members' roles, as the API reads
   * a Platform deployed before groups existed (one group per role, the one default partition
   * `main`). Writing the spec stores them.
   */
  derived: boolean
}

/** The roles the Slurm module scales; only `compute` groups grow or shrink. */
export type SlurmRole = SlurmNodeGroupSpec['role']

const ROLES: readonly SlurmRole[] = ['controller', 'login', 'compute']

/** A group name the API accepts: 1–32 lower-case letters, digits, or hyphens, not starting or ending with a hyphen. */
export const NODE_GROUP_NAME = /^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$/

/** A partition name the API accepts: up to 32 letters, digits, underscores, or hyphens. */
export const PARTITION_NAME = /^[A-Za-z0-9_][A-Za-z0-9_-]{0,31}$/

/** A Slurm time limit the API accepts: minutes, `[days-]hours:minutes[:seconds]`, or `INFINITE`. */
export const SLURM_TIME = /^(INFINITE|UNLIMITED|\d+|\d+:\d{2}(:\d{2})?|\d+-\d+(:\d{2}(:\d{2})?)?)$/

function slurmSettings(spec: PlatformSpec): Record<string, unknown> {
  const slurm = spec.settings.slurm
  return slurm !== null && typeof slurm === 'object' && !Array.isArray(slurm) ? (slurm as Record<string, unknown>) : {}
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
}

/**
 * Reads the groups and partitions from a Slurm spec. A spec without them reads as what the API
 * derives for it, so the workspace shows the same groups the next write stores.
 */
export function readSlurmTopology(spec: PlatformSpec): SlurmTopology {
  const settings = slurmSettings(spec)
  const rawGroups = settings.nodeGroups
  const rawPartitions = settings.partitions
  let derived = false
  let groups: SlurmNodeGroupSpec[]
  if (Array.isArray(rawGroups)) {
    groups = rawGroups.filter(isRecord).map((group) => ({
      name: typeof group.name === 'string' ? group.name : '',
      role: ROLES.includes(group.role as SlurmRole) ? (group.role as SlurmRole) : 'compute',
      ...(typeof group.templateId === 'string' && group.templateId ? { templateId: group.templateId } : {}),
      ...(typeof group.expectedGpusPerNode === 'number' && group.expectedGpusPerNode > 0 ? { expectedGpusPerNode: group.expectedGpusPerNode } : {}),
      members: strings(group.members),
    }))
  } else {
    derived = true
    groups = ROLES.map((role) => ({ name: role, role, members: spec.members.filter((member) => member.roles.includes(role)).map((member) => member.serverId) }))
      .filter((group) => group.members.length > 0 || group.role !== 'login')
  }
  let partitions: SlurmPartitionSpec[]
  if (Array.isArray(rawPartitions)) {
    partitions = rawPartitions.filter(isRecord).map((partition) => ({
      name: typeof partition.name === 'string' ? partition.name : '',
      default: partition.default === true,
      groups: strings(partition.groups),
      ...(typeof partition.maxTime === 'string' && partition.maxTime ? { maxTime: partition.maxTime } : {}),
      ...(typeof partition.defMemPerCpu === 'number' && partition.defMemPerCpu > 0 ? { defMemPerCpu: partition.defMemPerCpu } : {}),
      ...(typeof partition.overSubscribe === 'string' && partition.overSubscribe ? { overSubscribe: partition.overSubscribe.toUpperCase() as SlurmPartitionSpec['overSubscribe'] } : {}),
      ...(strings(partition.allowGroups).length > 0 ? { allowGroups: strings(partition.allowGroups) } : {}),
    }))
  } else {
    derived = true
    partitions = [{ name: 'main', default: true, groups: groups.filter((group) => group.role === 'compute').map((group) => group.name), maxTime: 'INFINITE' }]
  }
  return { groups, partitions, derived }
}

/** Writes groups and partitions into a copy of the spec's Slurm settings, leaving every other setting as stored. */
export function withSlurmTopology(spec: PlatformSpec, topology: Pick<SlurmTopology, 'groups' | 'partitions'>): PlatformSpec {
  return {
    ...spec,
    settings: { ...spec.settings, slurm: { ...slurmSettings(spec), nodeGroups: topology.groups, partitions: topology.partitions } },
  }
}

/**
 * A new node's own network, overriding its group template's (platforms.md "Node groups and
 * partitions": a member keeps only its network assignment from the machine preparation): the
 * interface to deploy on, its subnet, and, for a template whose mode is static, the node's address.
 * Written as the Server's `machinePreparation.network.assignments` entry.
 */
export interface SlurmNodeNetwork {
  serverId: string
  interfaceId: string
  subnetId?: string
  ipAddress?: string
}

/**
 * One edit of a Slurm Platform's members, groups, or partitions; each becomes one spec write.
 *
 * - `add-nodes`: the Servers join a compute group (and the members, with role `compute`), each with
 *   an optional network override.
 * - `remove-nodes`: the Servers leave the Platform with their removal choices.
 * - `add-group`: a new compute group, placed in the named partitions.
 * - `set-group`: a group's template (null to use the Platform's default) and expected GPUs per node (null for none).
 * - `add-partition` / `set-partition` (by `previousName`) / `remove-partition`.
 */
export type SlurmTopologyChange =
  | { kind: 'add-nodes'; group: string; serverIds: string[]; networks?: SlurmNodeNetwork[] }
  | { kind: 'remove-nodes'; removals: PlatformMemberRemoval[] }
  | { kind: 'add-group'; group: { name: string; templateId?: string; expectedGpusPerNode?: number }; partitions: string[] }
  | { kind: 'set-group'; name: string; templateId: string | null; expectedGpusPerNode: number | null }
  | { kind: 'add-partition'; partition: SlurmPartitionSpec }
  | { kind: 'set-partition'; previousName: string; partition: SlurmPartitionSpec }
  | { kind: 'remove-partition'; name: string }

/** A partition as the spec stores it: exactly one default, so making one default clears the others. */
function placePartition(partitions: SlurmPartitionSpec[], partition: SlurmPartitionSpec, previousName?: string): SlurmPartitionSpec[] {
  const others = partitions.filter((candidate) => candidate.name !== (previousName ?? partition.name))
  const cleared = partition.default ? others.map((candidate) => ({ ...candidate, default: false })) : others
  const index = previousName ? partitions.findIndex((candidate) => candidate.name === previousName) : -1
  const next = [...cleared]
  next.splice(index >= 0 ? index : next.length, 0, partition)
  return next
}

/**
 * Applies one change to a spec and returns the spec to write. Members join and leave together with
 * their group, so the members and the groups never disagree; a removal's choices go to
 * `memberRemovals`, replacing earlier choices for the same Server.
 */
export function applySlurmTopologyChange(spec: PlatformSpec, change: SlurmTopologyChange): PlatformSpec {
  const topology = readSlurmTopology(spec)
  let { groups, partitions } = topology
  let members = spec.members
  let memberRemovals = spec.memberRemovals ?? []
  let machinePreparation = spec.machinePreparation
  switch (change.kind) {
    case 'add-nodes': {
      const adding = change.serverIds.filter((id) => !members.some((member) => member.serverId === id))
      members = [...members, ...adding.map((serverId) => ({ serverId, roles: ['compute'] }))]
      groups = groups.map((group) => (group.name === change.group ? { ...group, members: [...group.members, ...adding] } : group))
      memberRemovals = memberRemovals.filter((removal) => !adding.includes(removal.serverId))
      machinePreparation = withNodeNetworks(machinePreparation, change.networks ?? [])
      break
    }
    case 'remove-nodes': {
      const leaving = change.removals.map((removal) => removal.serverId)
      members = members.filter((member) => !leaving.includes(member.serverId))
      groups = groups.map((group) => ({ ...group, members: group.members.filter((id) => !leaving.includes(id)) }))
      memberRemovals = [...memberRemovals.filter((removal) => !leaving.includes(removal.serverId)), ...change.removals]
      break
    }
    case 'add-group':
      groups = [...groups, {
        name: change.group.name,
        role: 'compute',
        ...(change.group.templateId ? { templateId: change.group.templateId } : {}),
        ...(change.group.expectedGpusPerNode ? { expectedGpusPerNode: change.group.expectedGpusPerNode } : {}),
        members: [],
      }]
      partitions = partitions.map((partition) => (change.partitions.includes(partition.name) ? { ...partition, groups: [...partition.groups, change.group.name] } : partition))
      break
    case 'set-group':
      groups = groups.map((group) => (group.name !== change.name ? group : {
        name: group.name,
        role: group.role,
        ...(change.templateId ? { templateId: change.templateId } : {}),
        ...(change.expectedGpusPerNode ? { expectedGpusPerNode: change.expectedGpusPerNode } : {}),
        members: group.members,
      }))
      break
    case 'add-partition':
      partitions = placePartition(partitions, change.partition)
      break
    case 'set-partition':
      partitions = placePartition(partitions, change.partition, change.previousName)
      break
    case 'remove-partition':
      partitions = partitions.filter((partition) => partition.name !== change.name)
      break
  }
  return { ...withSlurmTopology({ ...spec, members, machinePreparation }, { groups, partitions }), memberRemovals }
}

/**
 * The machine preparation with each override as its Server's network assignment, replacing an
 * earlier one for the same Server and keeping its template reference; unchanged without overrides
 * or without a machine preparation (members that keep their OS have no network to override).
 */
function withNodeNetworks(preparation: PlatformSpec['machinePreparation'], networks: SlurmNodeNetwork[]): PlatformSpec['machinePreparation'] {
  if (!preparation || networks.length === 0) return preparation
  const assignments = [...(preparation.network?.assignments ?? [])]
  for (const network of networks) {
    const index = assignments.findIndex((assignment) => assignment.serverId === network.serverId)
    const entry = {
      ...(index >= 0 ? assignments[index] : {}),
      serverId: network.serverId,
      interfaceId: network.interfaceId,
      ...(network.subnetId ? { subnetId: network.subnetId } : {}),
      ...(network.ipAddress ? { ipAddress: network.ipAddress } : {}),
    }
    if (index >= 0) assignments[index] = entry
    else assignments.push(entry)
  }
  return { ...preparation, network: { ...preparation.network, assignments } }
}

/** The compute GPU groups of a Server's GPU Inventory, display controllers excluded. */
function computeGPUs(gpus: readonly ServerGPU[]): ServerGPU[] {
  return gpus.filter((gpu) => gpu.kind === 'compute')
}

/** A Server's compute GPU count from its inventory; 0 without compute GPUs. */
export function computeGPUCount(server: Server): number {
  return computeGPUs(server.status.gpus).reduce((total, gpu) => total + gpu.count, 0)
}

/**
 * A short product name for a GPU Inventory model, for group names and labels: the product inside
 * brackets ("Aldebaran/MI200 [Instinct MI250X/MI250]" is "MI250X"), without the "AMD" and "Instinct"
 * brand words ("AMD Instinct MI308X" is "MI308X").
 * Empty when the inventory names no model (a virtual function may not).
 */
export function gpuProductName(model: string): string {
  let name = model.trim()
  const open = name.indexOf('[')
  const close = name.lastIndexOf(']')
  if (open >= 0 && close > open) name = name.slice(open + 1, close)
  name = name.split('/')[0].trim().replace(/^(AMD\s+)?(Instinct\s+)?/i, '').trim()
  return name
}

/** A Server's first compute GPU product, empty without one. */
export function serverGPUProduct(server: Server): string {
  const gpu = computeGPUs(server.status.gpus).find((candidate) => candidate.model.trim())
  return gpu ? gpuProductName(gpu.model) : ''
}

/** A Server's compute GPUs in words for a candidate or member list ("8× MI300X", "No compute GPUs"). */
export function serverGPUSummary(server: Server): string {
  const count = computeGPUCount(server)
  if (count === 0) return 'No compute GPUs'
  const product = serverGPUProduct(server)
  return product ? `${count}× ${product}` : `${count} compute GPUs`
}

/**
 * A group name from a node's GPU product ("MI300X" is "mi300x"). Without a product it is `gpu` for a
 * node that still has compute GPUs (a virtual function its inventory names no model for) and `cpu`
 * for one without any, so a GPU node is never proposed into a group that reads as CPU-only.
 */
export function groupNameForProduct(product: string, computeGPUs = 0): string {
  const name = product.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 32).replace(/-+$/g, '')
  return name || (computeGPUs > 0 ? 'gpu' : 'cpu')
}

/**
 * The groups and partitions the deploy wizard proposes for the selected Servers and their roles
 * (decision 083): `controller` and `login` groups, one compute group per GPU product (`gpu` for GPU
 * nodes whose inventory names no model, `cpu` for nodes without GPUs), each in a partition of the same name, the first of them the default. The GPU product
 * only suggests the first grouping; the user may rename groups and partitions before deploying.
 */
export function suggestSlurmTopology(nodes: ReadonlyArray<{ server: Server; roles: readonly SlurmRole[] }>): Pick<SlurmTopology, 'groups' | 'partitions'> {
  const groups: SlurmNodeGroupSpec[] = []
  for (const role of ['controller', 'login'] as const) {
    const members = nodes.filter((node) => node.roles.includes(role)).map((node) => node.server.id)
    if (members.length > 0) groups.push({ name: role, role, members })
  }
  const compute = new Map<string, { members: string[]; gpus: number[] }>()
  for (const node of nodes) {
    if (!node.roles.includes('compute')) continue
    const name = groupNameForProduct(serverGPUProduct(node.server), computeGPUCount(node.server))
    const entry = compute.get(name) ?? { members: [], gpus: [] }
    entry.members.push(node.server.id)
    entry.gpus.push(computeGPUCount(node.server))
    compute.set(name, entry)
  }
  for (const [name, entry] of compute) {
    // A group is named after its GPUs; it expects the most GPUs any of its nodes reports, so a node
    // that already lost one is marked degraded rather than lowering what the group expects.
    const expected = Math.max(0, ...entry.gpus)
    groups.push({ name: groups.some((group) => group.name === name) ? `${name}-nodes` : name, role: 'compute', ...(expected > 0 ? { expectedGpusPerNode: expected } : {}), members: entry.members })
  }
  const computeGroups = groups.filter((group) => group.role === 'compute')
  const partitions = computeGroups.map((group, index) => ({ name: group.name.replace(/-/g, '_'), default: index === 0, groups: [group.name], maxTime: 'INFINITE' }))
  return { groups, partitions }
}

/**
 * Checks groups and partitions the way the API will, so the wizard and dialogs name a problem before
 * submitting; returns the first problem, or an empty string. The API's check remains the authority.
 */
export function slurmTopologyProblem(topology: Pick<SlurmTopology, 'groups' | 'partitions'>): string {
  const names = new Set<string>()
  for (const group of topology.groups) {
    if (!NODE_GROUP_NAME.test(group.name)) return `Group name "${group.name}" must be 1 to 32 lower-case letters, digits, or hyphens.`
    if (names.has(group.name)) return `Group "${group.name}" is named twice.`
    names.add(group.name)
  }
  const partitionNames = new Set<string>()
  let defaults = 0
  const partitioned = new Set<string>()
  for (const partition of topology.partitions) {
    if (!PARTITION_NAME.test(partition.name)) return `Partition name "${partition.name}" must be up to 32 letters, digits, underscores, or hyphens.`
    if (partitionNames.has(partition.name)) return `Partition "${partition.name}" is named twice.`
    partitionNames.add(partition.name)
    if (partition.default) defaults++
    if (partition.groups.length === 0) return `Partition "${partition.name}" needs at least one compute group.`
    if (partition.maxTime && !SLURM_TIME.test(partition.maxTime)) return `Partition "${partition.name}" has a time limit Slurm does not read; use minutes, days-hours:minutes, or INFINITE.`
    partition.groups.forEach((group) => partitioned.add(group))
  }
  if (defaults !== 1) return 'Exactly one partition must be the default.'
  const orphan = topology.groups.find((group) => group.role === 'compute' && group.members.length > 0 && !partitioned.has(group.name))
  if (orphan) return `Compute group "${orphan.name}" has nodes but is in no partition.`
  return ''
}
