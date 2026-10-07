import type { OperationStatus } from '@/domain/operation/types'
import type { Site } from '@/domain/site/types'
import type { OSImageCatalogFailure, OSImageCatalogRow } from '@/application/usecases/provisioning/loadOSImageCatalog'

export type OSImageView = 'all' | 'verification_failed'
export type OSImageTarget = 'disk' | 'ram'
export type OSImageDeployModeFilter = 'any' | 'available' | 'verifying' | 'requires_attention' | 'failed' | 'not_verified'
export type OSImageGroup = 'none' | 'os' | 'provider'
export type OSImageSort = 'os' | 'name' | 'release' | 'size' | 'refreshed'
export type OSImageDirection = 'asc' | 'desc'

/** Shareable discovery and display state for the OS Image catalog. */
export interface OSImageInventoryQuery {
  q: string
  view: OSImageView
  os: readonly string[]
  architectures: readonly string[]
  tags: readonly string[]
  integrations: readonly string[]
  target: OSImageTarget | 'any'
  readiness: OSImageDeployModeFilter
  group: OSImageGroup
  sort: OSImageSort
  direction: OSImageDirection
  page: number
}

/** One active or parked verification Workflow projected onto its exact image target. */
export interface OSImageVerificationActivity {
  operationId: string
  integrationId: string
  imageId: string
  architecture: string
  target: OSImageTarget
  status: OperationStatus
  statusReason: string | null
  requestedAt: string
}

export type OSImageVerificationActivityMap = ReadonlyMap<string, OSImageVerificationActivity>

export interface OSImageDeployModeStatus {
  kind: 'supported' | 'failed' | 'not_tested' | 'testing' | 'requires_attention'
  label: 'Supported' | 'Failed' | 'Not tested' | 'Testing' | 'Needs attention'
  detail: string
  operationId?: string
  previousOutcome?: 'supported' | 'failed' | 'not_tested'
}

export type OSImageContextAction =
  | { kind: 'workflow'; label: 'View workflow' | 'Review workflow'; operationId: string }
  | { kind: 'test'; label: 'Test deployment'; target: OSImageTarget }
  | { kind: 'deploy'; label: 'Deploy OS' }

export interface OSImageCatalogFacts {
  loaded: number
  diskSupported: number
  ramSupported: number
  failedTargets: number
  unavailableProviders: number
}

export interface OSImageFacetOption {
  value: string
  label: string
  count: number
}

export interface OSImageFacetOptions {
  os: OSImageFacetOption[]
  architectures: OSImageFacetOption[]
  tags: OSImageFacetOption[]
  integrations: OSImageFacetOption[]
}

export interface OSImageRenderGroup {
  key: string
  label: string
  items: OSImageCatalogRow[]
}

const VIEWS = new Set<OSImageView>(['all', 'verification_failed'])
const TARGETS = new Set<OSImageTarget>(['disk', 'ram'])
const READINESS = new Set<OSImageDeployModeFilter>(['any', 'available', 'verifying', 'requires_attention', 'failed', 'not_verified'])
const GROUPS = new Set<OSImageGroup>(['none', 'os', 'provider'])
const SORTS = new Set<OSImageSort>(['os', 'name', 'release', 'size', 'refreshed'])
const DIRECTIONS = new Set<OSImageDirection>(['asc', 'desc'])

function enumValue<T extends string>(value: string | null, values: ReadonlySet<T>, fallback: T): T {
  return value !== null && values.has(value as T) ? value as T : fallback
}

function distinctValues(params: URLSearchParams, key: string): string[] {
  return [...new Set(params.getAll(key).map((value) => value.trim()).filter(Boolean))]
}

/** Provider subarchitectures identify the same catalog architecture by their first segment. */
export function primaryImageArchitecture(architecture: string): string {
  return architecture.split('/', 1)[0]
}

/** Stable identity for selection, rendering, and provider-scoped workflow activity. */
export function osImageKey(image: Pick<OSImageCatalogRow, 'integrationId' | 'id' | 'architecture'>): string {
  return `${image.integrationId}\u0000${image.id}\u0000${image.architecture}`
}

/** Exact target identity; integration scope prevents equal provider IDs from sharing activity. */
export function osImageVerificationKey(
  integrationId: string,
  imageId: string,
  architecture: string,
  target: string,
): string {
  return `${integrationId}\u0000${imageId}\u0000${primaryImageArchitecture(architecture)}\u0000${target}`
}

/** Whether Swallow owns any presentation or login metadata layered over the provider artifact. */
export function hasOSImageOverride(image: OSImageCatalogRow): boolean {
  return Boolean(
    image.customName
      || image.customOsSystem
      || image.customRelease
      || image.customDefaultUser
      || image.tags.length,
  )
}

/** Parses malformed and legacy URL state into safe catalog defaults. */
export function parseOSImageInventoryQuery(params: URLSearchParams): OSImageInventoryQuery {
  const target = enumValue(params.get('target'), TARGETS, 'any' as OSImageTarget | 'any')
  const requestedReadiness = enumValue(params.get('readiness'), READINESS, 'any')
  const pageValue = Number(params.get('page'))
  return {
    q: params.get('q') ?? params.get('query') ?? '',
    view: enumValue(params.get('view'), VIEWS, 'all'),
    os: distinctValues(params, 'os'),
    architectures: distinctValues(params, 'architecture'),
    tags: distinctValues(params, 'tag'),
    integrations: distinctValues(params, 'integration'),
    target,
    readiness: target === 'any' ? 'any' : requestedReadiness,
    group: enumValue(params.get('group'), GROUPS, 'none'),
    sort: enumValue(params.get('sort'), SORTS, 'os'),
    direction: enumValue(params.get('dir'), DIRECTIONS, 'asc'),
    page: Number.isInteger(pageValue) && pageValue > 1 ? pageValue : 1,
  }
}

function normalizeRepeated(next: URLSearchParams, key: string): void {
  const values = distinctValues(next, key)
  next.delete(key)
  values.forEach((value) => next.append(key, value))
}

/** Canonicalizes catalog state while leaving Site scope and exact deep-link parameters intact. */
export function normalizeOSImageInventoryParams(params: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(params)
  if (!next.has('q') && next.get('query')) next.set('q', next.get('query') ?? '')
  next.delete('query')
  if (!next.get('q')?.trim()) next.delete('q')

  const view = enumValue(next.get('view'), VIEWS, 'all')
  if (view === 'all') next.delete('view')
  else next.set('view', view)

  for (const key of ['os', 'architecture', 'tag', 'integration']) normalizeRepeated(next, key)

  const target = enumValue(next.get('target'), TARGETS, 'any' as OSImageTarget | 'any')
  if (target === 'any') {
    next.delete('target')
    next.delete('readiness')
  } else {
    next.set('target', target)
    const readiness = enumValue(next.get('readiness'), READINESS, 'any')
    if (readiness === 'any') next.delete('readiness')
    else next.set('readiness', readiness)
  }

  const group = enumValue(next.get('group'), GROUPS, 'none')
  if (group === 'none') next.delete('group')
  else next.set('group', group)

  const sort = enumValue(next.get('sort'), SORTS, 'os')
  if (sort === 'os') next.delete('sort')
  else next.set('sort', sort)
  const direction = enumValue(next.get('dir'), DIRECTIONS, 'asc')
  if (direction === 'asc') next.delete('dir')
  else next.set('dir', direction)

  const page = Number(next.get('page'))
  if (!Number.isInteger(page) || page <= 1) next.delete('page')
  return next
}

/** Exact Server-detail focus match, preserving the current compatibility link contract. */
export function matchesOSImageFocus(image: OSImageCatalogRow, params: URLSearchParams): boolean {
  const integrationId = params.get('integrationId') ?? ''
  const imageName = params.get('imageName') ?? ''
  if (!integrationId || !imageName || image.integrationId !== integrationId) return !integrationId || !imageName

  const architecture = primaryImageArchitecture(params.get('architecture') ?? '')
  if (architecture && primaryImageArchitecture(image.architecture) !== architecture) return false
  const osSystem = params.get('osSystem') ?? ''
  const release = params.get('release') ?? ''
  const expectedID = osSystem && release ? `${osSystem}/${release}` : ''
  return Boolean(
    (expectedID && image.id === expectedID)
      || (osSystem && release && image.osSystem === osSystem && image.release === release)
      || image.name === imageName,
  )
}

function storedTargetOutcome(image: OSImageCatalogRow, target: OSImageTarget): 'supported' | 'failed' | 'not_tested' {
  if (image.providerOsSystem !== 'custom' || image.verifiedDeployTargets.includes(target)) return 'supported'
  if (image.failedDeployTargets.includes(target)) return 'failed'
  return 'not_tested'
}

function outcomeLabel(outcome: 'supported' | 'failed' | 'not_tested'): string {
  if (outcome === 'supported') return 'Supported'
  if (outcome === 'failed') return 'Failed'
  return 'Not tested'
}

/** Projects one deploy target into user-facing support and deployment-test state. */
export function osImageDeployModeStatus(
  image: OSImageCatalogRow,
  target: OSImageTarget,
  activities: OSImageVerificationActivityMap,
): OSImageDeployModeStatus {
  const mode = target === 'disk' ? 'Disk' : 'RAM'
  const outcome = storedTargetOutcome(image, target)
  const activity = activities.get(osImageVerificationKey(image.integrationId, image.id, image.architecture, target))
  if (activity?.status === 'requires_attention') {
    return {
      kind: 'requires_attention',
      label: 'Needs attention',
      detail: activity.statusReason || `The ${mode} deployment test Workflow needs operator attention. Previous result: ${outcomeLabel(outcome)}.`,
      operationId: activity.operationId,
      previousOutcome: outcome,
    }
  }
  if (activity) {
    return {
      kind: 'testing',
      label: 'Testing',
      detail: activity.statusReason || `A ${mode} deployment test is active. Previous result: ${outcomeLabel(outcome)}.`,
      operationId: activity.operationId,
      previousOutcome: outcome,
    }
  }
  if (outcome === 'supported') return { kind: 'supported', label: 'Supported', detail: `This image can be deployed in ${mode} mode.` }
  if (outcome === 'failed') return { kind: 'failed', label: 'Failed', detail: `The latest ${mode} deployment test failed; this mode remains unavailable.` }
  return { kind: 'not_tested', label: 'Not tested', detail: `Run a deployment test before using ${mode} mode.` }
}

function activitiesForImage(image: OSImageCatalogRow, activities: OSImageVerificationActivityMap): OSImageVerificationActivity[] {
  return (['disk', 'ram'] as const)
    .map((target) => activities.get(osImageVerificationKey(image.integrationId, image.id, image.architecture, target)))
    .filter((activity): activity is OSImageVerificationActivity => Boolean(activity))
    .sort((left, right) => right.requestedAt.localeCompare(left.requestedAt))
}

/** Returns the single read-only or dialog-opening next step shown at the end of an image row. */
export function osImageContextAction(
  image: OSImageCatalogRow,
  activities: OSImageVerificationActivityMap,
): OSImageContextAction {
  const current = activitiesForImage(image, activities)
  const changing = current.find((activity) => activity.status !== 'requires_attention')
  if (changing) return { kind: 'workflow', label: 'View workflow', operationId: changing.operationId }
  const attention = current.find((activity) => activity.status === 'requires_attention')
  if (attention) return { kind: 'workflow', label: 'Review workflow', operationId: attention.operationId }
  if (image.providerOsSystem === 'custom' && image.verifiedDeployTargets.length === 0) {
    const target: OSImageTarget = image.failedDeployTargets.includes('disk')
      ? 'disk'
      : image.failedDeployTargets.includes('ram') ? 'ram' : 'disk'
    return { kind: 'test', label: 'Test deployment', target }
  }
  return { kind: 'deploy', label: 'Deploy OS' }
}

function textIncludes(value: string | undefined, needle: string): boolean {
  return Boolean(value?.toLocaleLowerCase().includes(needle))
}

/** Applies conjunctive dimensions and OR-within-facet semantics to one catalog row. */
export function matchesOSImageInventory(
  image: OSImageCatalogRow,
  query: OSImageInventoryQuery,
  activities: OSImageVerificationActivityMap,
  sites: readonly Site[],
): boolean {
  if (query.view === 'verification_failed' && image.failedDeployTargets.length === 0) return false
  if (query.os.length > 0 && !query.os.includes(image.osSystem)) return false
  if (query.architectures.length > 0 && !query.architectures.includes(image.architecture)) return false
  if (query.tags.length > 0 && !query.tags.some((tag) => image.tags.includes(tag))) return false
  if (query.integrations.length > 0 && !query.integrations.includes(image.integrationId)) return false

  if (query.target !== 'any' && query.readiness !== 'any') {
    const status = osImageDeployModeStatus(image, query.target, activities)
    const normalized = status.kind === 'supported'
      ? 'available'
      : status.kind === 'testing'
        ? 'verifying'
        : status.kind === 'not_tested'
          ? 'not_verified'
          : status.kind
    if (normalized !== query.readiness) return false
  }

  const needle = query.q.trim().toLocaleLowerCase()
  if (!needle) return true
  const siteName = sites.find((site) => site.id === image.siteId)?.name ?? image.siteId
  return [
    image.name,
    image.id,
    image.osSystem,
    image.release,
    image.defaultUser,
    image.architecture,
    image.integrationName,
    siteName,
    ...image.tags,
  ].some((value) => textIncludes(value, needle))
}

function compareText(left: string, right: string): number {
  return left.localeCompare(right, undefined, { numeric: true, sensitivity: 'base' })
}

/** Stable catalog ordering; provider identity breaks ties between equal display metadata. */
export function compareOSImages(left: OSImageCatalogRow, right: OSImageCatalogRow, query: OSImageInventoryQuery): number {
  let result = 0
  if (query.sort === 'name') result = compareText(left.name || left.id, right.name || right.id)
  else if (query.sort === 'release') result = compareText(left.release, right.release)
  else if (query.sort === 'size') result = (left.sizeBytes ?? Number.MAX_SAFE_INTEGER) - (right.sizeBytes ?? Number.MAX_SAFE_INTEGER)
  else if (query.sort === 'refreshed') result = left.refreshedAt.localeCompare(right.refreshedAt)
  else {
    result = compareText(left.osSystem, right.osSystem)
      || compareText(left.release, right.release)
      || compareText(left.name || left.id, right.name || right.id)
  }
  if (result === 0) result = compareText(left.architecture, right.architecture)
  if (result === 0) result = compareText(left.integrationName, right.integrationName)
  return query.direction === 'desc' ? -result : result
}

/** User-facing OS family; `custom` is a valid provider-reported family name. */
export function osImageOSFamilyLabel(value: string): string {
  const normalized = value.trim()
  return normalized || 'Unknown OS'
}

function countedOptions(values: readonly string[], selected: readonly string[], label: (value: string) => string = (value) => value): OSImageFacetOption[] {
  const counts = new Map<string, number>()
  values.filter(Boolean).forEach((value) => counts.set(value, (counts.get(value) ?? 0) + 1))
  selected.forEach((value) => {
    if (!counts.has(value)) counts.set(value, 0)
  })
  return [...counts.entries()]
    .sort(([left], [right]) => compareText(left, right))
    .map(([value, count]) => ({ value, label: label(value), count }))
}

/** Full-scope facets; filtering never makes a selected value disappear from its control. */
export function osImageFacetOptions(images: readonly OSImageCatalogRow[], query: OSImageInventoryQuery): OSImageFacetOptions {
  const integrationCounts = new Map<string, { label: string; count: number }>()
  images.forEach((image) => {
    const current = integrationCounts.get(image.integrationId)
    integrationCounts.set(image.integrationId, { label: image.integrationName, count: (current?.count ?? 0) + 1 })
  })
  query.integrations.forEach((id) => {
    if (!integrationCounts.has(id)) integrationCounts.set(id, { label: id, count: 0 })
  })
  return {
    os: countedOptions(images.map((image) => image.osSystem), query.os, osImageOSFamilyLabel),
    architectures: countedOptions(images.map((image) => image.architecture), query.architectures),
    tags: countedOptions(images.flatMap((image) => image.tags), query.tags),
    integrations: [...integrationCounts.entries()]
      .sort(([, left], [, right]) => compareText(left.label, right.label))
      .map(([value, item]) => ({ value, label: item.label, count: item.count })),
  }
}

/** Exact scope-wide deploy eligibility and latest completed test outcomes. */
export function osImageCatalogFacts(
  images: readonly OSImageCatalogRow[],
  failures: readonly OSImageCatalogFailure[],
): OSImageCatalogFacts {
  const supports = (image: OSImageCatalogRow, target: OSImageTarget) =>
    image.providerOsSystem !== 'custom' || image.verifiedDeployTargets.includes(target)
  return {
    loaded: images.length,
    diskSupported: images.filter((image) => supports(image, 'disk')).length,
    ramSupported: images.filter((image) => supports(image, 'ram')).length,
    failedTargets: images.reduce((total, image) => total + image.failedDeployTargets.length, 0),
    unavailableProviders: failures.length,
  }
}

/** Groups only the current page, keeping each image in exactly one section. */
export function groupOSImages(images: readonly OSImageCatalogRow[], group: OSImageGroup): OSImageRenderGroup[] {
  if (group === 'none') return [{ key: '', label: '', items: [...images] }]
  const groups = new Map<string, OSImageCatalogRow[]>()
  images.forEach((image) => {
    const key = group === 'os' ? osImageOSFamilyLabel(image.osSystem) : image.integrationName || image.integrationId
    groups.set(key, [...(groups.get(key) ?? []), image])
  })
  return [...groups.entries()].map(([key, items]) => ({ key, label: key, items }))
}

/** Whether discovery state, excluding display order and pagination, narrows the catalog. */
export function hasOSImageFilters(query: OSImageInventoryQuery, params: URLSearchParams): boolean {
  return Boolean(
    query.q
      || query.view !== 'all'
      || query.os.length
      || query.architectures.length
      || query.tags.length
      || query.integrations.length
      || query.target !== 'any'
      || params.get('integrationId')
      || params.get('imageName'),
  )
}
