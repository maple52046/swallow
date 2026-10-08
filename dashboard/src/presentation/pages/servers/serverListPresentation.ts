import {
  isProvisioningInProgress,
  type DeploymentAxis,
  type ProvisioningAxis,
  type ProvisioningState,
  type Server,
} from '@/domain/server/types'
import { gpuInventoryCount, preferredServerGPUs } from '@/domain/server/gpu'
import { resolveDeploymentPhase, type DeploymentPhase } from '@/presentation/components/deploymentPhase'
import {
  gpuInventoryCompactSummary,
  gpuInventoryProfile,
} from '@/presentation/components/serverGpuPresentation'
import { serverActivityPath, type ServerActivitySection } from './serverActivitySections'

/** Quick operational lens applied before the advanced Server facets. */
export type ServerView = 'all' | 'ready' | 'changing' | 'issues'

/** Grouping modes that keep every Server in exactly one visible group. */
export type ServerInventoryGroup =
  | 'none'
  | 'provisioning'
  | 'zone'
  | 'pool'
  | 'architecture'
  | 'power'
  | 'gpu-profile'
  | 'system-model'

/** Sort keys exposed by the compact Server inventory display controls. */
export type ServerInventorySort =
  | 'priority'
  | 'name'
  | 'provisioning'
  | 'power'
  | 'cores'
  | 'memory'
  | 'storage'
  | 'gpus'
  | 'zone'
  | 'pool'

export type ServerInventoryDirection = 'asc' | 'desc'
export type ServerHealthFilter = 'any' | 'up' | 'down' | 'unobserved'
export type ServerMembershipFilter = 'any' | 'assigned' | 'unassigned'
export type ServerGpuFilter = 'any' | 'present' | 'none'
export type ServerLockFilter = 'any' | 'locked' | 'unlocked'

/** Parsed, shareable discovery state for the Server inventory. */
export interface ServerInventoryQuery {
  q: string
  view: ServerView
  provisioning: readonly ProvisioningState[]
  health: ServerHealthFilter
  membership: ServerMembershipFilter
  gpu: ServerGpuFilter
  gpuVendors: readonly string[]
  gpuModels: readonly string[]
  architectures: readonly string[]
  systemVendors: readonly string[]
  systemProducts: readonly string[]
  zones: readonly string[]
  pools: readonly string[]
  tags: readonly string[]
  lock: ServerLockFilter
  includeAbsent: boolean
  group: ServerInventoryGroup
  sort: ServerInventorySort
  direction: ServerInventoryDirection
  page: number
}

/** Scope-wide facts rendered without collapsing the independent Server axes. */
export interface ServerFleetFacts {
  total: number
  absent: number
  deploymentVerified: number
  deploymentActive: number
  deploymentAttention: number
  assigned: number
  unassigned: number
  healthUp: number
  healthDown: number
  healthUnobserved: number
}

/**
 * The one contextual next step a Server list row offers, shown as a frameless icon-and-text link in
 * the Deployment cell because every choice follows the OS deployment axis that cell presents.
 * None of them performs a mutation; mutations stay in the row's Actions menu.
 *
 * `label` is the visible purpose and also anchors the tooltip and row-specific accessible name.
 */
export type ServerContextAction =
  | { kind: 'deploy'; label: 'Deploy OS' }
  | { kind: 'workflow'; label: 'View workflow'; operationId: string }
  | { kind: 'activity'; label: 'View activity' | 'Review activity'; section: ServerActivitySection }

const PROVISIONING_STATES: readonly ProvisioningState[] = [
  'new',
  'inspecting',
  'ready',
  'allocated',
  'deploying',
  'deployed',
  'releasing',
  'testing',
  'rescue',
  'broken',
  'failed',
  'retired',
  'unknown',
]

const PROVISIONING_STATE_SET = new Set<string>(PROVISIONING_STATES)
const ISSUE_PROVIDER_STATES = new Set<ProvisioningState>(['failed', 'broken', 'rescue'])
const VIEWS = new Set<ServerView>(['all', 'ready', 'changing', 'issues'])
const HEALTH_FILTERS = new Set<ServerHealthFilter>(['any', 'up', 'down', 'unobserved'])
const MEMBERSHIP_FILTERS = new Set<ServerMembershipFilter>(['any', 'assigned', 'unassigned'])
const GPU_FILTERS = new Set<ServerGpuFilter>(['any', 'present', 'none'])
const LOCK_FILTERS = new Set<ServerLockFilter>(['any', 'locked', 'unlocked'])
const GROUPS = new Set<ServerInventoryGroup>([
  'none',
  'provisioning',
  'zone',
  'pool',
  'architecture',
  'power',
  'gpu-profile',
  'system-model',
])
const SORTS = new Set<ServerInventorySort>([
  'priority',
  'name',
  'provisioning',
  'power',
  'cores',
  'memory',
  'storage',
  'gpus',
  'zone',
  'pool',
])

function enumValue<T extends string>(value: string | null, values: ReadonlySet<T>, fallback: T): T {
  return value !== null && values.has(value as T) ? value as T : fallback
}

function distinctValues(params: URLSearchParams, key: string): string[] {
  return [...new Set(params.getAll(key).map((value) => value.trim()).filter(Boolean))]
}

/**
 * Discovery filters that depend on an in-development feature. A filter that is not
 * offered is ignored when parsing and removed when canonicalizing, so a bookmarked
 * `?health=down` can never narrow the list through a control the operator cannot see.
 */
export interface ServerInventoryFilterAvailability {
  /** False while monitoring is in development (always, in release builds). */
  readonly health: boolean
}

const ALL_FILTERS_AVAILABLE: ServerInventoryFilterAvailability = { health: true }

/** Parses canonical URL discovery state; malformed values fail to least-surprising defaults. */
export function parseServerInventoryQuery(
  params: URLSearchParams,
  availability: ServerInventoryFilterAvailability = ALL_FILTERS_AVAILABLE,
): ServerInventoryQuery {
  const view = enumValue(params.get('view'), VIEWS, 'all')
  const provisioning = view === 'all'
    ? distinctValues(params, 'provisioning').filter((value): value is ProvisioningState => PROVISIONING_STATE_SET.has(value))
    : []
  const pageValue = Number(params.get('page'))
  const sort = enumValue(params.get('sort'), SORTS, 'priority')

  return {
    q: params.get('q') ?? '',
    view,
    provisioning,
    health: availability.health ? enumValue(params.get('health'), HEALTH_FILTERS, 'any') : 'any',
    membership: enumValue(params.get('membership'), MEMBERSHIP_FILTERS, 'any'),
    gpu: enumValue(params.get('gpu'), GPU_FILTERS, 'any'),
    gpuVendors: distinctValues(params, 'gpuVendor'),
    gpuModels: distinctValues(params, 'gpuModel'),
    architectures: distinctValues(params, 'architecture'),
    systemVendors: distinctValues(params, 'systemVendor'),
    systemProducts: distinctValues(params, 'systemProduct'),
    zones: distinctValues(params, 'zone'),
    pools: distinctValues(params, 'pool'),
    tags: distinctValues(params, 'tag'),
    lock: enumValue(params.get('lock'), LOCK_FILTERS, 'any'),
    includeAbsent: params.get('includeAbsent') === 'true',
    group: enumValue(params.get('group'), GROUPS, 'none'),
    sort,
    direction: sort === 'priority'
      ? 'asc'
      : enumValue(params.get('dir'), new Set<ServerInventoryDirection>(['asc', 'desc']), 'asc'),
    page: Number.isInteger(pageValue) && pageValue > 1 ? pageValue : 1,
  }
}

function normalizeRepeated(next: URLSearchParams, key: string): void {
  const values = distinctValues(next, key)
  next.delete(key)
  values.forEach((value) => next.append(key, value))
}

/** Canonicalizes Server-list parameters while preserving Site scope and unrelated route state. */
export function normalizeServerInventoryParams(
  params: URLSearchParams,
  availability: ServerInventoryFilterAvailability = ALL_FILTERS_AVAILABLE,
): URLSearchParams {
  const next = new URLSearchParams(params)
  if (!availability.health) next.delete('health')
  const view = enumValue(next.get('view'), VIEWS, 'all')
  if (view === 'all') next.delete('view')
  else next.set('view', view)

  normalizeRepeated(next, 'provisioning')
  const provisioning = next.getAll('provisioning').filter((value) => PROVISIONING_STATE_SET.has(value))
  next.delete('provisioning')
  if (view === 'all') provisioning.forEach((value) => next.append('provisioning', value))

  const singleEnums: ReadonlyArray<[string, ReadonlySet<string>, string]> = [
    ['health', HEALTH_FILTERS, 'any'],
    ['membership', MEMBERSHIP_FILTERS, 'any'],
    ['gpu', GPU_FILTERS, 'any'],
    ['lock', LOCK_FILTERS, 'any'],
    ['group', GROUPS, 'none'],
    ['sort', SORTS, 'priority'],
  ]
  singleEnums.forEach(([key, values, fallback]) => {
    const value = next.get(key)
    if (!value || !values.has(value) || value === fallback) next.delete(key)
  })

  for (const key of ['gpuVendor', 'gpuModel', 'architecture', 'systemVendor', 'systemProduct', 'zone', 'pool', 'tag']) {
    normalizeRepeated(next, key)
  }

  if (next.get('includeAbsent') !== 'true') next.delete('includeAbsent')
  const page = Number(next.get('page'))
  if (!Number.isInteger(page) || page <= 1) next.delete('page')

  if (!next.has('sort')) next.delete('dir')
  else if (next.get('dir') !== 'desc') next.delete('dir')
  return next
}

/**
 * True while OS provisioning work is running on the Server: an in-progress OS Provisioning State
 * (Releasing, Inspecting, Testing, Deploying) or a Swallow deployment that is deploying or
 * verifying. This is what the Deployment cell marks with a spinner, so the Active deployments
 * view, the Active count, the default order, the row tone, and the refresh poll all use it.
 */
export function isServerChanging(server: Server): boolean {
  return isProvisioningInProgress(server.provisioning?.state) || isServerDeploymentChanging(server)
}

/**
 * True only while a Swallow-owned OS deployment is changing. Narrower than
 * {@link isServerChanging} on purpose: it gates links to the deployment's Workflow, which would
 * point at an unrelated, finished Operation while the provider is, say, releasing.
 */
export function isServerDeploymentChanging(server: Server): boolean {
  return server.deployment?.state === 'deploying' || server.deployment?.state === 'verifying'
}

/**
 * Provisioning or deployment conditions that need operator review, without reading Health: a
 * failed, broken, or rescue OS Provisioning State, or a failed or attention-needing Swallow
 * deployment. These are the states the Deployment cell shows as a problem, so the Needs attention
 * view, the Attention count, the default order, and the row tone use it.
 */
export function hasServerProvisioningIssue(server: Server): boolean {
  return Boolean(
    (server.provisioning && ISSUE_PROVIDER_STATES.has(server.provisioning.state)) ||
    hasServerDeploymentIssue(server),
  )
}

/** True only when the latest Swallow-owned deployment requires review; gates its Workflow link. */
export function hasServerDeploymentIssue(server: Server): boolean {
  return server.deployment?.state === 'failed' || server.deployment?.state === 'requires_attention'
}

/**
 * The facts the list's Deployment cell is resolved from. An absent Server's provisioning axis is
 * a stale observation, so it is not passed. A `succeeded` Swallow result is current only while the
 * provider still reports the OS installed; afterwards it is history, and passing it would paint a
 * stale "Deployed" over the Server's real state (for example Releasing, then Ready).
 */
export function serverDeploymentCellInputs(server: Server): {
  axis: DeploymentAxis | null
  provider: ProvisioningAxis | null
} {
  const provider = server.absent ? null : server.provisioning
  const installed = provider?.state === 'deployed'
  const axis = server.deployment?.state === 'succeeded' && !installed ? null : server.deployment
  return { axis, provider }
}

/**
 * The `data-tone` of a list row or card (styled in `src/index.css`), in precedence order:
 * absent, running OS provisioning work, then a provisioning problem. It follows the same rules
 * as the Deployment cell so a row never shows Releasing or Failed without the matching tone.
 */
export function serverRowTone(server: Server): 'absent' | 'changing' | 'issue' | undefined {
  if (server.absent) return 'absent'
  if (isServerChanging(server)) return 'changing'
  if (hasServerProvisioningIssue(server)) return 'issue'
  return undefined
}

/** The Deployment cell's resolved phase, shared by the cell, its grouping, and its sorting. */
export function serverDeploymentPhase(server: Server): DeploymentPhase {
  const { axis, provider } = serverDeploymentCellInputs(server)
  return resolveDeploymentPhase(axis, provider)
}

/**
 * Deterministic profile for the GPU kind represented by the Server List: compute accelerators
 * when present, otherwise display controllers. Classification comes from the API, never tags.
 */
export function serverGpuProfile(server: Server): string {
  const gpus = preferredServerGPUs(server)
  return gpus.length > 0 ? gpuInventoryProfile(gpus) : 'CPU only'
}

/**
 * Short GPU inventory label for dense Server rows and cards.
 *
 * Compute inventory outranks display inventory. Within that selected kind, the profile with the
 * largest count is shown using its full vendor/model name. Callers retain `serverGpuProfile` for
 * exact tooltip/detail text and grouping, where hiding the remaining profiles would lose
 * operator-visible inventory meaning.
 */
export function serverGpuCompactSummary(server: Server): string {
  return gpuInventoryCompactSummary(preferredServerGPUs(server))
}

/** Physical GPU count represented by the Server List's compute-first inventory selection. */
export function serverGpuCount(server: Server): number {
  return gpuInventoryCount(preferredServerGPUs(server))
}

/** Stable effective tags; provenance is intentionally unavailable in the current projection. */
export function sortedServerTags(server: Server): string[] {
  return [...server.tags].sort((left, right) => left.localeCompare(right))
}

/** Resource-discovery search over identity, effective tags, and accelerator inventory. */
export function matchesServerDiscovery(server: Server, query: string): boolean {
  const needle = query.trim().toLocaleLowerCase()
  if (!needle) return true
  const searchable = [
    server.hostname,
    server.fqdn,
    ...server.addresses,
    server.hardware.serialNumber,
    server.hardware.systemUuid,
    server.source.providerMachineId,
    ...server.tags,
    ...server.gpus.flatMap((gpu) => [gpu.vendor, gpu.model]),
  ]
  return searchable.some((value) => value?.toLocaleLowerCase().includes(needle))
}

/** Applies every URL-owned facet. Dimensions AND together; repeated values OR within one facet. */
export function matchesServerInventoryQuery(server: Server, query: ServerInventoryQuery): boolean {
  if (!query.includeAbsent && server.absent) return false
  if (!matchesServerDiscovery(server, query.q)) return false
  if (query.view === 'ready' && (server.absent || server.provisioning?.locked || server.provisioning?.state !== 'ready')) return false
  if (query.view === 'changing' && !isServerChanging(server)) return false
  if (query.view === 'issues' && !hasServerProvisioningIssue(server)) return false
  if (query.provisioning.length > 0 && (!server.provisioning || !query.provisioning.includes(server.provisioning.state))) return false
  if (query.health === 'unobserved' && server.health !== null) return false
  if ((query.health === 'up' || query.health === 'down') && server.health?.state !== query.health) return false
  if (query.membership === 'assigned' && server.membership === null) return false
  if (query.membership === 'unassigned' && server.membership !== null) return false
  if (query.gpu === 'present' && server.gpus.length === 0) return false
  if (query.gpu === 'none' && server.gpus.length > 0) return false
  if (query.gpuVendors.length > 0 && !server.gpus.some((gpu) => query.gpuVendors.includes(gpu.vendor))) return false
  if (query.gpuModels.length > 0 && !server.gpus.some((gpu) => query.gpuModels.includes(gpu.model))) return false
  if (query.architectures.length > 0 && !query.architectures.includes(server.architecture || 'unknown')) return false
  if (query.systemVendors.length > 0 && !query.systemVendors.includes(server.systemVendor || 'unknown')) return false
  if (query.systemProducts.length > 0 && !query.systemProducts.includes(server.systemProduct || 'unknown')) return false
  if (query.zones.length > 0 && !query.zones.includes(server.providerZone || 'unknown')) return false
  if (query.pools.length > 0 && !query.pools.includes(server.providerResourcePool || 'unknown')) return false
  if (query.tags.length > 0 && !query.tags.some((tag) => server.tags.includes(tag))) return false

  const locked = server.provisioning?.locked === true
  if (query.lock === 'locked' && !locked) return false
  if (query.lock === 'unlocked' && locked) return false
  return true
}

/** Stable group label for the selected one-to-one grouping dimension. */
export function serverInventoryGroupValue(server: Server, group: ServerInventoryGroup): string {
  switch (group) {
    case 'provisioning': return serverDeploymentPhase(server).label
    case 'zone': return server.providerZone || 'unknown'
    case 'pool': return server.providerResourcePool || 'unknown'
    case 'architecture': return server.architecture || 'unknown'
    case 'power': return server.provisioning?.powerState ?? 'unknown'
    case 'gpu-profile': return serverGpuProfile(server)
    case 'system-model': return [server.systemVendor, server.systemProduct].filter(Boolean).join(' ') || 'unknown'
    case 'none':
    default: return ''
  }
}

/** Operational rank used by the default list order: running work, then problems, then idle. */
export function serverOperationalPriority(server: Server): number {
  if (server.absent) return 5
  if (isServerChanging(server)) return 0
  if (hasServerProvisioningIssue(server)) return 1
  if (server.provisioning?.state === 'ready') return 2
  if (server.deployment?.state === 'succeeded') return 3
  return 4
}

function displayName(server: Server): string {
  return server.hostname ?? server.fqdn ?? server.source.providerMachineId ?? server.id
}

function sortValue(server: Server, sort: Exclude<ServerInventorySort, 'priority'>): string | number {
  switch (sort) {
    case 'name': return displayName(server).toLocaleLowerCase()
    case 'provisioning': return serverDeploymentPhase(server).label
    case 'power': return server.provisioning?.powerState ?? ''
    case 'cores': return server.cpuCores
    case 'memory': return server.memoryMiB
    case 'storage': return server.storageGB
    case 'gpus': return serverGpuCount(server)
    case 'zone': return server.providerZone.toLocaleLowerCase()
    case 'pool': return server.providerResourcePool.toLocaleLowerCase()
  }
}

/** Compares rows by explicit sort or operational priority, then by display name for stability. */
export function compareServerInventory(
  left: Server,
  right: Server,
  sort: ServerInventorySort,
  direction: ServerInventoryDirection,
): number {
  let result = 0
  if (sort === 'priority') {
    result = serverOperationalPriority(left) - serverOperationalPriority(right)
  } else {
    const leftValue = sortValue(left, sort)
    const rightValue = sortValue(right, sort)
    result = typeof leftValue === 'number' && typeof rightValue === 'number'
      ? leftValue - rightValue
      : String(leftValue).localeCompare(String(rightValue))
    if (direction === 'desc') result *= -1
  }
  return result || displayName(left).localeCompare(displayName(right))
}

/** Computes scope-wide facts; absent projections are excluded from live external axes. */
export function serverFleetFacts(servers: readonly Server[]): ServerFleetFacts {
  const observed = servers.filter((server) => !server.absent)
  return {
    total: servers.length,
    absent: servers.length - observed.length,
    deploymentVerified: observed.filter((server) => server.deployment?.state === 'succeeded').length,
    deploymentActive: observed.filter(isServerChanging).length,
    deploymentAttention: observed.filter(hasServerProvisioningIssue).length,
    assigned: observed.filter((server) => server.membership !== null).length,
    unassigned: observed.filter((server) => server.membership === null).length,
    healthUp: observed.filter((server) => server.health?.state === 'up').length,
    healthDown: observed.filter((server) => server.health?.state === 'down').length,
    healthUnobserved: observed.filter((server) => server.health === null).length,
  }
}

/**
 * Chooses a row's contextual next step, highest precedence first:
 *
 * 1. Review activity (Provider events) for an absent Server. Its provisioning axis is a stale
 *    observation, which is also why the Deployment cell ignores it.
 * 2. Deploy OS for an unlocked `ready` Server.
 * 3. While work runs: the deployment's Workflow when Swallow is deploying or verifying, otherwise
 *    View activity on the section that records the provider work (see
 *    {@link inProgressActivitySection}).
 * 4. A Swallow deployment that needs review opens its Workflow, which holds the failed Step.
 * 5. Review activity (Provider events) for a failed, broken, or rescue Server.
 *
 * Returns `null` when nothing needs the operator, for example an idle deployed or locked Server;
 * its host name already links to the Summary, so the row gets no icon rather than a redundant
 * "open" link. Workflow links are gated on Swallow's own deployment state so they never point at
 * an unrelated, finished Operation while the provider is, say, releasing.
 */
export function serverContextAction(server: Server): ServerContextAction | null {
  if (server.absent) {
    return { kind: 'activity', label: 'Review activity', section: 'provider-events' }
  }
  if (!server.provisioning?.locked && server.provisioning?.state === 'ready') {
    return { kind: 'deploy', label: 'Deploy OS' }
  }
  const operationId = server.deployment?.operationId
  if (isServerChanging(server)) {
    if (isServerDeploymentChanging(server) && operationId) {
      return { kind: 'workflow', label: 'View workflow', operationId }
    }
    return { kind: 'activity', label: 'View activity', section: inProgressActivitySection(server) }
  }
  if (hasServerDeploymentIssue(server) && operationId) {
    return { kind: 'workflow', label: 'View workflow', operationId }
  }
  if (hasServerProvisioningIssue(server)) {
    return { kind: 'activity', label: 'Review activity', section: 'provider-events' }
  }
  return null
}

/**
 * The Activity section that records running work Swallow has no Workflow link for. The Server
 * list carries no Provisioning Task or Operation id, so this targets a section, not one record:
 *
 * - Releasing: Provisioning tasks, where post-Release network cleanup is tracked.
 * - Inspecting, or a Swallow deployment without an Operation id: Related Operations, which lists
 *   the inspect-hardware or deployment Workflow.
 * - Other provider work (Deploying, Testing): Provider events, the provisioner's own history.
 */
function inProgressActivitySection(server: Server): ServerActivitySection {
  if (server.provisioning?.state === 'releasing') return 'provisioning-tasks'
  if (server.provisioning?.state === 'inspecting' || isServerDeploymentChanging(server)) return 'related-operations'
  return 'provider-events'
}

/**
 * Site-unscoped destination of a contextual step; callers pass it through `scopedHref`.
 * Deploy is handled in-page, while this fallback remains a safe Server destination.
 */
export function serverContextActionPath(server: Server, action: ServerContextAction): string {
  switch (action.kind) {
    case 'deploy':
      return `/servers/${encodeURIComponent(server.id)}/summary`
    case 'workflow':
      return `/workflows/${action.operationId}`
    case 'activity':
      return serverActivityPath(server.id, action.section)
  }
}

/** Whether any discovery facet, search, or quick lens constrains the visible working set. */
export function hasServerInventoryFilters(query: ServerInventoryQuery): boolean {
  return Boolean(
    query.q || query.view !== 'all' || query.provisioning.length || query.health !== 'any' ||
    query.membership !== 'any' || query.gpu !== 'any' || query.gpuVendors.length ||
    query.gpuModels.length || query.architectures.length || query.systemVendors.length ||
    query.systemProducts.length || query.zones.length || query.pools.length || query.tags.length ||
    query.lock !== 'any' || query.includeAbsent
  )
}
