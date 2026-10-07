import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Badge, Box, Button, Flex, Heading, HStack, IconButton, Popover, Portal, Table, Text } from '@chakra-ui/react'
import {
  ArrowUpRight,
  ChevronDown,
  ChevronRight,
  CircleCheckBig,
  Eye,
  Filter,
  Lock,
  MemoryStick,
  PenLine,
  Plus,
  RefreshCw,
  Rocket,
  SlidersHorizontal,
  Tags,
  TriangleAlert,
  UploadCloud,
  type LucideIcon,
} from 'lucide-react'
import { Link as RouterLink, useNavigate, useSearchParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { refreshServerProjections } from '@/application/usecases/servers/refreshServerProjections'
import { INSPECT_HARDWARE_WORKFLOW_KIND } from '@/domain/operation/types'
import type { Integration, Site } from '@/domain/site/types'
import type { ReleaseServerInput, Server } from '@/domain/server/types'
import { serverDisplayName, serverPrimaryAddress } from '@/domain/server/list'
import { PageHeader } from '@/presentation/components/PageHeader'
import { InventorySurface, SelectionToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { ResourceTag } from '@/presentation/components/ResourceTag'
import { LoadingState } from '@/presentation/components/LoadingState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { Pagination } from '@/presentation/components/Pagination'
import {
  DeploymentSummary,
  HealthBadge,
  PowerBadge,
} from '@/presentation/components/AxisBadge'
import { powerStateLabel } from '@/presentation/components/axisBadgeUtils'
import { CopyButton } from '@/presentation/components/CopyButton'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Select } from '@/presentation/components/ui/select'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useExperimentalFeature } from '@/presentation/contexts/ExperimentalFeaturesContext'
import { NOT_AVAILABLE_IN_RELEASE } from '@/presentation/components/releaseAvailability'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { actionLabel, isRamDeploy, serverActionAvailability, type ServerMenuAction } from './serverActions'
import { ServerLockDialog } from './ServerLockDialog'
import { ServerDeleteDialog } from './ServerDeleteDialog'
import { ServerReleaseDialog } from './ServerReleaseDialog'
import { ServerPowerDialog } from './ServerPowerDialog'
import { ServerPowerOffWarningDialog } from './ServerPowerOffWarningDialog'
import { useServerWorkingSet } from './useServerWorkingSet'
import { useServerBulkActions } from './useServerBulkActions'
import { ServerTagEditor } from './ServerTagEditor'
import { ServerActionResultDialog } from './ServerActionResultDialog'
import { ServerTakeActionMenu } from './ServerTakeActionMenu'
import { failedServerActionOutcomes, type ServerActionRunResult, type ServerActionTarget } from './serverActionResults'
import { AddServersDialog } from './AddServersDialog'
import { InspectionAttentionAlert } from './InspectionAttentionAlert'
import { useInspectionAttention } from './useInspectionAttention'
import { SERVER_ACTIVITY_SECTION_TITLES } from './serverActivitySections'
import {
  compareServerInventory,
  hasServerInventoryFilters,
  isServerChanging,
  matchesServerInventoryQuery,
  normalizeServerInventoryParams,
  parseServerInventoryQuery,
  serverContextAction,
  serverContextActionPath,
  serverDeploymentCellInputs,
  serverFleetFacts,
  serverGpuCompactSummary,
  serverGpuProfile,
  serverInventoryGroupValue,
  serverRowTone,
  sortedServerTags,
  type ServerContextAction,
  type ServerInventoryDirection,
  type ServerInventoryGroup,
  type ServerInventoryQuery,
  type ServerInventorySort,
  type ServerView,
} from './serverListPresentation'

/** Table density is a local readability preference, not shareable fleet state. */
type ServerDensity = 'compact' | 'comfortable'

interface FilterOption {
  value: string
  label: string
  count: number
}

interface ServerFacetOptions {
  provisioning: FilterOption[]
  gpuVendor: FilterOption[]
  gpuModel: FilterOption[]
  architecture: FilterOption[]
  systemVendor: FilterOption[]
  systemProduct: FilterOption[]
  zone: FilterOption[]
  pool: FilterOption[]
  tag: FilterOption[]
}

interface RenderGroup {
  key: string
  label: string
  items: Server[]
}

interface DetailFactGroup {
  title: string
  facts: Array<{ label: string; value: string }>
}

interface ServerSelectionState {
  filterKey: string
  ids: ReadonlySet<string>
}

const DEFAULT_PAGE_SIZE = 50
const EMPTY_SERVERS: Server[] = []
const EMPTY_SERVER_IDS: readonly string[] = []
const EMPTY_INTEGRATIONS: Integration[] = []
const LEGACY_GROUP_KEY = 'swallow.servers.group-by'
const DENSITY_KEY = 'swallow.servers.density'
const PAGE_SIZE_KEY = 'swallow.servers.page-size'
const DEPLOYMENT_POLL_INTERVAL_MS = 2_000
const MAX_DEPLOYMENT_POLL_ATTEMPTS = 150
const RELEASE_FOLLOW_WINDOW_MS = 180_000

const SERVER_VIEWS: ReadonlyArray<{ value: ServerView; label: string }> = [
  { value: 'all', label: 'All' },
  { value: 'ready', label: 'Deployable' },
  { value: 'changing', label: 'Active deployments' },
  { value: 'issues', label: 'Needs attention' },
]

const GROUP_OPTIONS: ReadonlyArray<{ value: ServerInventoryGroup; label: string }> = [
  { value: 'none', label: 'No grouping' },
  { value: 'provisioning', label: 'OS deployment' },
  { value: 'zone', label: 'Zone' },
  { value: 'pool', label: 'Pool' },
  { value: 'architecture', label: 'Architecture' },
  { value: 'power', label: 'Power' },
  { value: 'gpu-profile', label: 'GPU profile' },
  { value: 'system-model', label: 'System model' },
]

const SORT_OPTIONS: ReadonlyArray<{ value: ServerInventorySort; label: string }> = [
  { value: 'priority', label: 'Operational priority' },
  { value: 'name', label: 'Server name' },
  { value: 'provisioning', label: 'OS deployment' },
  { value: 'power', label: 'Power' },
  { value: 'cores', label: 'CPU cores' },
  { value: 'memory', label: 'Memory' },
  { value: 'storage', label: 'Storage' },
  { value: 'gpus', label: 'GPU count' },
  { value: 'zone', label: 'Zone' },
  { value: 'pool', label: 'Pool' },
]

function readPreference<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key)
    return raw === null ? fallback : JSON.parse(raw) as T
  } catch {
    return fallback
  }
}

function writePreference<T>(key: string, value: T): void {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Browser policy may block preferences; the current view remains fully usable.
  }
}
function readDensityPreference(): ServerDensity {
  const value = readPreference<string>(DENSITY_KEY, 'compact')
  return value === 'comfortable' ? value : 'compact'
}

function readPageSizePreference(): number {
  const value = readPreference<number>(PAGE_SIZE_KEY, DEFAULT_PAGE_SIZE)
  return [25, 50, 100].includes(value) ? value : DEFAULT_PAGE_SIZE
}


function textOrDash(value: string | null | undefined): string {
  return value?.trim() || '—'
}

function quantityOrDash(value: number, unit = '', divisor = 1): string {
  if (!Number.isFinite(value) || value <= 0) return '—'
  const quantity = Math.round(value / divisor)
  return unit ? `${quantity} ${unit}` : String(quantity)
}
/** Encodes arbitrary provider facts into stable, whitespace-free control ids. */
function controlId(value: string): string {
  return encodeURIComponent(value).replaceAll('%', '-')
}


function renderGroups(items: Server[], group: ServerInventoryGroup): RenderGroup[] {
  if (group === 'none') return [{ key: '', label: '', items }]
  const groups = new Map<string, Server[]>()
  items.forEach((server) => {
    const value = serverInventoryGroupValue(server, group)
    groups.set(value, [...(groups.get(value) ?? []), server])
  })
  return [...groups.entries()].map(([key, grouped]) => ({ key, label: key, items: grouped }))
}

function facetOptions(
  servers: readonly Server[],
  valuesOf: (server: Server) => readonly string[],
  selected: readonly string[],
): FilterOption[] {
  const counts = new Map<string, number>()
  servers.forEach((server) => {
    new Set(valuesOf(server).filter(Boolean)).forEach((value) => {
      counts.set(value, (counts.get(value) ?? 0) + 1)
    })
  })
  selected.forEach((value) => {
    if (!counts.has(value)) counts.set(value, 0)
  })
  return [...counts.entries()]
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([value, count]) => ({ value, label: value, count }))
}

function activeAdvancedFilterCount(query: ServerInventoryQuery): number {
  return [
    query.provisioning.length > 0,
    query.health !== 'any',
    query.membership !== 'any',
    query.gpu !== 'any',
    query.gpuVendors.length > 0,
    query.gpuModels.length > 0,
    query.architectures.length > 0,
    query.systemVendors.length > 0,
    query.systemProducts.length > 0,
    query.zones.length > 0,
    query.pools.length > 0,
    query.tags.length > 0,
    query.lock !== 'any',
    query.includeAbsent,
  ].filter(Boolean).length
}

function siteNameOf(server: Server, sites: readonly Site[]): string {
  return sites.find((site) => site.id === server.source.siteId)?.name ?? server.source.siteId
}

function integrationNameOf(server: Server, integrations: readonly Integration[]): string {
  return integrations.find((integration) => integration.id === server.source.integrationId)?.name ?? server.source.integrationId
}

/** Complete mirrored Server facts shared by desktop disclosure and mobile card details. */
function serverDetailFacts(
  server: Server,
  sites: readonly Site[],
  integrations: readonly Integration[],
  monitoring: boolean,
): DetailFactGroup[] {
  const provisioning = server.provisioning
  return [
    {
      title: 'Identity',
      facts: [
        { label: 'FQDN', value: textOrDash(server.fqdn) },
        { label: 'Server ID', value: server.id },
        { label: 'Site', value: siteNameOf(server, sites) },
        { label: 'Provisioner', value: integrationNameOf(server, integrations) },
        { label: 'Provider machine ID', value: server.source.providerMachineId },
        { label: 'Addresses', value: server.addresses.length ? server.addresses.join(', ') : '—' },
        { label: 'Serial number', value: textOrDash(server.hardware.serialNumber) },
        { label: 'System UUID', value: textOrDash(server.hardware.systemUuid) },
        { label: 'MAC addresses', value: server.hardware.macAddresses.length ? server.hardware.macAddresses.join(', ') : '—' },
      ],
    },
    {
      title: 'Hardware',
      facts: [
        { label: 'GPU inventory', value: serverGpuProfile(server) },
        { label: 'Architecture', value: textOrDash(server.architecture) },
        { label: 'CPU cores', value: quantityOrDash(server.cpuCores) },
        { label: 'CPU model', value: textOrDash(server.cpuModel) },
        { label: 'Memory', value: quantityOrDash(server.memoryMiB, 'GiB', 1024) },
        { label: 'Storage', value: quantityOrDash(server.storageGB, 'GB') },
        { label: 'System vendor', value: textOrDash(server.systemVendor) },
        { label: 'System product', value: textOrDash(server.systemProduct) },
      ],
    },
    {
      title: 'Resource organization',
      facts: [
        { label: 'Zone', value: textOrDash(server.providerZone) },
        { label: 'Pool', value: textOrDash(server.providerResourcePool) },
        { label: 'Pod', value: textOrDash(server.providerPod) },
        { label: 'Tags', value: sortedServerTags(server).join(', ') || '—' },
      ],
    },
    {
      title: 'Operating system',
      facts: [
        { label: 'Installed image', value: textOrDash(provisioning?.deployedImageName) },
        { label: 'Reported OS', value: textOrDash([provisioning?.osSystem, provisioning?.distroSeries].filter(Boolean).join(' ')) },
        { label: 'Kernel', value: textOrDash(provisioning?.hweKernel) },
        { label: 'Root filesystem', value: provisioning ? (provisioning.ephemeral ? 'RAM (ephemeral)' : 'Disk') : '—' },
      ],
    },
    {
      title: 'Operational context',
      facts: [
        { label: 'Power', value: powerStateLabel(provisioning?.powerState ?? null) },
        { label: 'Health', value: monitoring ? server.health?.state ?? 'Unobserved' : NOT_AVAILABLE_IN_RELEASE },
        { label: 'Platform ID', value: textOrDash(server.membership?.platformId) },
        { label: 'Platform node', value: textOrDash(server.membership?.nodeName) },
        { label: 'Platform role', value: textOrDash(server.membership?.role) },
        { label: 'Membership', value: server.membership?.state ?? 'Unassigned' },
      ],
    },
    {
      title: 'Freshness',
      facts: [
        { label: 'Last seen', value: server.lastSeenAt ? formatDateTime(server.lastSeenAt) : 'Never' },
        { label: 'Provisioning observed', value: provisioning?.observedAt ? formatDateTime(provisioning.observedAt) : 'Unobserved' },
        { label: 'Membership observed', value: server.membership?.observedAt ? formatDateTime(server.membership.observedAt) : 'Unobserved' },
        {
          label: 'Health observed',
          value: !monitoring
            ? NOT_AVAILABLE_IN_RELEASE
            : server.health?.observedAt ? formatDateTime(server.health.observedAt) : 'Unobserved',
        },
      ],
    },
  ]
}

/** One axis of the fleet summary; `note` replaces the counts when the axis is not offered. */
interface FleetAxisGroup {
  label: string
  values: ReadonlyArray<readonly [string, number]>
  note?: string
}

/**
 * Scope-wide fleet summary that stays independent from inventory discovery filters.
 * While monitoring is in development the Health axis keeps its place but shows the
 * "not available" note instead of counts, and down Servers no longer raise attention.
 */
function ServerFleetOverview({ servers }: { servers: readonly Server[] }) {
  const monitoring = useExperimentalFeature('monitoring')
  const facts = serverFleetFacts(servers)
  const hasAttention = facts.deploymentAttention > 0 || (monitoring && facts.healthDown > 0)
  const groups: FleetAxisGroup[] = [
    { label: 'Inventory', values: [['Total', facts.total], ['Absent', facts.absent]] },
    { label: 'OS deployment', values: [['Verified', facts.deploymentVerified], ['Active', facts.deploymentActive], ['Attention', facts.deploymentAttention]] },
    { label: 'Membership', values: [['Assigned', facts.assigned], ['Unassigned', facts.unassigned]] },
    monitoring
      ? { label: 'Health', values: [['Up', facts.healthUp], ['Down', facts.healthDown], ['Unobserved', facts.healthUnobserved]] }
      : { label: 'Health', values: [], note: NOT_AVAILABLE_IN_RELEASE },
  ]
  const OverviewIcon = hasAttention ? TriangleAlert : CircleCheckBig
  return (
    <Box as="section" className="sw-server-fleet-overview" data-tone={hasAttention ? 'attention' : 'normal'} aria-labelledby="server-fleet-title">
      <div className="sw-server-fleet-overview__lead">
        <span className="sw-server-fleet-overview__icon" aria-hidden><OverviewIcon size={20} /></span>
        <div>
          <Text className="sw-inventory-surface__eyebrow">Infrastructure fleet</Text>
          <Heading as="h2" id="server-fleet-title" size="lg">{facts.total} managed server{facts.total === 1 ? '' : 's'} in scope</Heading>
          <Text color="fg.muted">Independent inventory, Swallow deployment, membership, and health facts.</Text>
        </div>
      </div>
      <div className="sw-server-fleet-overview__axes">
        {groups.map((group) => (
          <section key={group.label} className="sw-server-fleet-axis" aria-label={group.label}>
            <Text className="sw-server-fleet-axis__label">{group.label}</Text>
            {group.note ? (
              <Text color="fg.muted" fontSize="sm">{group.note}</Text>
            ) : (
              <dl>
                {group.values.map(([label, value]) => (
                  <div key={label}>
                    <dt>{label}</dt>
                    <dd>{value}</dd>
                  </div>
                ))}
              </dl>
            )}
          </section>
        ))}
      </div>
    </Box>
  )
}

/**
 * GPU-datacenter fleet console. The complete Site snapshot owns overview counts while URL-owned
 * discovery state derives the visible inventory without additional API reads.
 */
export function ServersPage() {
  const { sites: siteRepository, servers: serverRepository } = useApp()
  const { sites, siteId, scopedHref } = useSiteScope()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const paramsKey = searchParams.toString()
  // The health filter is offered only while monitoring is shown; otherwise `?health=` is
  // ignored and dropped from the canonical URL.
  const monitoring = useExperimentalFeature('monitoring')
  const query = useMemo(() => parseServerInventoryQuery(searchParams, { health: monitoring }), [searchParams, monitoring])
  const normalizedParams = useMemo(
    () => normalizeServerInventoryParams(searchParams, { health: monitoring }),
    [searchParams, monitoring],
  )
  const [density, setDensity] = useState<ServerDensity>(readDensityPreference)
  const [pageSize, setPageSize] = useState(readPageSizePreference)
  const [selectionState, setSelectionState] = useState<ServerSelectionState>({ filterKey: '', ids: new Set() })
  const [collapsedGroups, setCollapsedGroups] = useState<ReadonlySet<string>>(new Set())
  // Keyed by the Site scope it was read for, so a scope change never shows the previous Site's
  // provisioners (or decides the empty state) before its own read returns.
  const [provisionerState, setProvisionerState] = useState<{ siteKey: string | null; items: Integration[] }>({ siteKey: null, items: [] })
  const [addingServers, setAddingServers] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<Server | null>(null)
  const [releaseTargets, setReleaseTargets] = useState<ServerActionTarget[] | null>(null)
  const [tagEditorTargets, setTagEditorTargets] = useState<Server[] | null>(null)
  const [lastActionResult, setLastActionResult] = useState<ServerActionRunResult | null>(null)
  const [resultDialogOpen, setResultDialogOpen] = useState(false)
  const [pendingLockAction, setPendingLockAction] = useState<{
    action: 'lock' | 'unlock'
    targets: readonly Server[]
    skipped: readonly Server[]
  } | null>(null)
  const [pendingPowerOff, setPendingPowerOff] = useState<{
    targets: readonly Server[]
    ramTargets: readonly Server[]
  } | null>(null)
  const siteKey = siteId ?? ''
  const provisionersLoaded = provisionerState.siteKey === siteKey
  const provisioners = provisionersLoaded ? provisionerState.items : EMPTY_INTEGRATIONS
  const inspectionAttention = useInspectionAttention({ siteId })
  const [followedServers, setFollowedServers] = useState<{ siteKey: string; ids: readonly string[] }>({ siteKey, ids: [] })
  const followedServerIds = followedServers.siteKey === siteKey ? followedServers.ids : EMPTY_SERVER_IDS
  const legacyGroupChecked = useRef(false)

  const updateParams = useCallback((
    update: (next: URLSearchParams) => void,
    options: { replace?: boolean; clearSelection?: boolean } = {},
  ) => {
    const next = new URLSearchParams(paramsKey)
    update(next)
    setSearchParams(next, { replace: options.replace })
    if (options.clearSelection) setSelectionState({ filterKey: '', ids: new Set() })
  }, [paramsKey, setSearchParams])

  useEffect(() => {
    if (normalizedParams.toString() === paramsKey) return
    setSearchParams(normalizedParams, { replace: true })
  }, [normalizedParams, paramsKey, setSearchParams])

  useEffect(() => {
    if (legacyGroupChecked.current) return
    if (searchParams.has('group')) {
      legacyGroupChecked.current = true
      return
    }
    const legacy = readPreference<string>(LEGACY_GROUP_KEY, 'none')
    if (!GROUP_OPTIONS.some((option) => option.value === legacy) || legacy === 'none') {
      legacyGroupChecked.current = true
      return
    }
    const handle = setTimeout(() => {
      legacyGroupChecked.current = true
      updateParams((next) => next.set('group', legacy), { replace: true })
    }, 0)
    return () => clearTimeout(handle)
  }, [searchParams, updateParams])

  const commitSearch = useCallback((value: string) => {
    const q = value.trim()
    updateParams((next) => {
      if (q) next.set('q', q)
      else next.delete('q')
      next.delete('page')
    }, { replace: true, clearSelection: true })
  }, [updateParams])

  const workingSetQuery = useMemo(() => ({ siteId, includeAbsent: true }), [siteId])
  const { state, reload, isRefreshing, streamStatus } = useServerWorkingSet(workingSetQuery)
  const workingSet = state.status === 'ready' ? state.data.servers : EMPTY_SERVERS
  const bulk = useServerBulkActions()

  useEffect(() => {
    let cancelled = false
    const key = siteId ?? ''
    siteRepository.listIntegrations({ siteId, kind: 'provisioner' })
      .then((items) => {
        if (!cancelled) setProvisionerState({ siteKey: key, items })
      })
      .catch(() => {
        // A failed read reads as "none known": sync warnings disappear and the empty state
        // offers to connect a provisioner, which the Integrations page then shows accurately.
        if (!cancelled) setProvisionerState({ siteKey: key, items: [] })
      })
    return () => {
      cancelled = true
    }
  }, [siteId, siteRepository])


  const activeProjectionTargetKey = useMemo(() => workingSet
    .filter((server) => isServerChanging(server))
    .map((server) => server.id)
    .sort()
    .join(','), [workingSet])
  const pollTargetKey = useMemo(() => {
    const active = activeProjectionTargetKey ? activeProjectionTargetKey.split(',') : []
    return Array.from(new Set([...active, ...followedServerIds])).sort().join(',')
  }, [activeProjectionTargetKey, followedServerIds])

  useEffect(() => {
    if (!pollTargetKey) return
    const targetIds = pollTargetKey.split(',')
    let cancelled = false
    let attempts = 0
    let timer: ReturnType<typeof setTimeout> | undefined
    const tick = async () => {
      await refreshServerProjections(serverRepository, targetIds)
      if (cancelled) return
      if (followedServerIds.length > 0) reload()
      attempts += 1
      if (attempts < MAX_DEPLOYMENT_POLL_ATTEMPTS) {
        timer = setTimeout(() => void tick(), DEPLOYMENT_POLL_INTERVAL_MS)
      }
    }
    void tick()
    return () => {
      cancelled = true
      if (timer !== undefined) clearTimeout(timer)
    }
  }, [followedServerIds.length, pollTargetKey, reload, serverRepository])

  useEffect(() => {
    if (followedServerIds.length === 0) return
    const handle = setTimeout(() => {
      setFollowedServers({ siteKey, ids: [] })
    }, RELEASE_FOLLOW_WINDOW_MS)
    return () => clearTimeout(handle)
  }, [followedServerIds, siteKey])

  const visibleServers = useMemo(() => workingSet.filter((server) => matchesServerInventoryQuery(server, query)), [query, workingSet])
  const sortedServers = useMemo(() => [...visibleServers].sort((left, right) => (
    compareServerInventory(left, right, query.sort, query.direction)
  )), [query.direction, query.sort, visibleServers])
  const totalPages = Math.max(1, Math.ceil(sortedServers.length / pageSize))
  const safePage = Math.min(query.page, totalPages)
  const pageItems = useMemo(() => sortedServers.slice((safePage - 1) * pageSize, safePage * pageSize), [pageSize, safePage, sortedServers])

  useEffect(() => {
    if (state.status !== 'ready' || query.page === safePage) return
    const handle = setTimeout(() => {
      updateParams((next) => {
        if (safePage > 1) next.set('page', String(safePage))
        else next.delete('page')
      }, { replace: true })
    }, 0)
    return () => clearTimeout(handle)
  }, [query.page, safePage, state.status, updateParams])

  const facets = useMemo<ServerFacetOptions>(() => ({
    provisioning: facetOptions(workingSet, (server) => server.provisioning ? [server.provisioning.state] : [], query.provisioning),
    gpuVendor: facetOptions(workingSet, (server) => server.gpus.map((gpu) => gpu.vendor), query.gpuVendors),
    gpuModel: facetOptions(workingSet, (server) => server.gpus.map((gpu) => gpu.model), query.gpuModels),
    architecture: facetOptions(workingSet, (server) => [server.architecture || 'unknown'], query.architectures),
    systemVendor: facetOptions(workingSet, (server) => [server.systemVendor || 'unknown'], query.systemVendors),
    systemProduct: facetOptions(workingSet, (server) => [server.systemProduct || 'unknown'], query.systemProducts),
    zone: facetOptions(workingSet, (server) => [server.providerZone || 'unknown'], query.zones),
    pool: facetOptions(workingSet, (server) => [server.providerResourcePool || 'unknown'], query.pools),
    tag: facetOptions(workingSet, (server) => server.tags, query.tags),
  }), [query, workingSet])

  const filterParams = new URLSearchParams(paramsKey)
  for (const key of ['group', 'sort', 'dir', 'page']) filterParams.delete(key)
  const filterKey = `${siteId ?? ''}|${filterParams.toString()}`
  const selected = selectionState.filterKey === filterKey ? selectionState.ids : new Set<string>()
  const updateSelection = useCallback((update: (current: ReadonlySet<string>) => ReadonlySet<string>) => {
    setSelectionState((previous) => ({
      filterKey,
      ids: update(previous.filterKey === filterKey ? previous.ids : new Set()),
    }))
  }, [filterKey])
  const toggleOne = useCallback((id: string) => {
    updateSelection((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [updateSelection])
  const setMany = useCallback((ids: string[], checked: boolean) => {
    updateSelection((current) => {
      const next = new Set(current)
      ids.forEach((id) => checked ? next.add(id) : next.delete(id))
      return next
    })
  }, [updateSelection])
  const clearSelection = useCallback(() => setSelectionState({ filterKey, ids: new Set() }), [filterKey])

  const pageIds = pageItems.map((server) => server.id)
  const allPageSelected = pageIds.length > 0 && pageIds.every((id) => selected.has(id))
  const somePageSelected = pageIds.some((id) => selected.has(id))
  const actionTargets = workingSet.filter((server) => selected.has(server.id))
  const targetIntegrations = new Set(actionTargets.map((server) => server.source.integrationId))
  const deployDisabledReason = selected.size > 100
    ? 'Deploy OS supports at most 100 Servers.'
    : actionTargets.some((server) => server.absent)
      ? 'Absent Servers cannot be deployed.'
      : actionTargets.some((server) => server.provisioning?.locked)
        ? 'Unlock every selected Server before deployment.'
        : actionTargets.some((server) => server.provisioning?.state !== 'ready')
          ? 'Every selected Server must be ready.'
          : targetIntegrations.size > 1
            ? 'Selected Servers must use the same provisioner integration.'
            : undefined

  const deploySelected = () => {
    const target = new URL(scopedHref('/provisioning/deploy'), window.location.origin)
    selected.forEach((id) => target.searchParams.append('serverId', id))
    navigate(`${target.pathname}${target.search}`)
  }

  const runAction = useCallback(async (action: ServerMenuAction, ids: string[], confirmed = false) => {
    if (!ids.length) return
    const targets = ids
      .map((id) => workingSet.find((server) => server.id === id))
      .filter((server): server is Server => Boolean(server))
    const availability = serverActionAvailability(action, targets)
    if (availability.disabledReason) return
    if ((action === 'lock' || action === 'unlock') && !confirmed) {
      setPendingLockAction({ action, targets: availability.eligible, skipped: availability.skipped })
      return
    }
    const eligibleIds = availability.eligible.map((server) => server.id)
    if (action === 'delete') {
      const target = workingSet.find((server) => server.id === eligibleIds[0])
      if (target) setDeleteTarget(target)
      return
    }
    const actionTargets = eligibleIds.map((id) => {
      const server = workingSet.find((item) => item.id === id)
      return { serverId: id, serverName: server ? serverDisplayName(server) : id }
    })
    if (action === 'release') {
      setReleaseTargets(actionTargets)
      return
    }
    if (action === 'recover') {
      await bulk.recover(actionTargets)
      clearSelection()
      setFollowedServers({ siteKey, ids: eligibleIds })
      reload()
      return
    }
    if (action === 'power-off' && !confirmed) {
      const ramTargets = availability.eligible.filter(isRamDeploy)
      if (ramTargets.length > 0) {
        setPendingPowerOff({ targets: availability.eligible, ramTargets })
        return
      }
    }
    const result = await bulk.run(action, actionTargets)
    setLastActionResult(result)
    setResultDialogOpen(failedServerActionOutcomes(result).length > 0)
    clearSelection()
    reload()
  }, [bulk, clearSelection, reload, siteKey, workingSet])

  const confirmRelease = useCallback(async (input: ReleaseServerInput) => {
    if (!releaseTargets?.length) return
    const releasedIds = releaseTargets.map((target) => target.serverId)
    await bulk.release(releaseTargets, input)
    clearSelection()
    setFollowedServers({ siteKey, ids: releasedIds })
    reload()
  }, [bulk, clearSelection, releaseTargets, reload, siteKey])

  const setLens = (view: ServerView) => {
    updateParams((next) => {
      if (view === 'all') next.delete('view')
      else next.set('view', view)
      next.delete('provisioning')
      next.delete('page')
    }, { clearSelection: true })
  }
  const setSingleFilter = (key: string, value: string, fallback = 'any') => {
    updateParams((next) => {
      if (value === fallback) next.delete(key)
      else next.set(key, value)
      next.delete('page')
    }, { clearSelection: true })
  }
  const setMultiFilter = (key: string, values: readonly string[]) => {
    updateParams((next) => {
      next.delete(key)
      values.forEach((value) => next.append(key, value))
      if (key === 'provisioning') next.delete('view')
      next.delete('page')
    }, { clearSelection: true })
  }
  const clearFilters = () => {
    updateParams((next) => {
      for (const key of [
        'q', 'view', 'provisioning', 'health', 'membership', 'gpu', 'gpuVendor', 'gpuModel',
        'architecture', 'systemVendor', 'systemProduct', 'zone', 'pool', 'tag', 'lock',
        'includeAbsent', 'page',
      ]) next.delete(key)
    }, { clearSelection: true })
  }

  const staleProvisioners = provisioners.filter((integration) => integration.sync.lastError !== null)
  const lastActionFailures = lastActionResult ? failedServerActionOutcomes(lastActionResult) : []
  const rangeStart = pageItems.length > 0 ? (safePage - 1) * pageSize + 1 : 0
  const rangeEnd = pageItems.length > 0 ? rangeStart + pageItems.length - 1 : 0
  const summary = state.status === 'ready' ? (
    <div className="sw-server-inventory-summary">
      <span>{sortedServers.length === 0 ? 'Showing 0 servers' : `Showing ${rangeStart}–${rangeEnd} of ${sortedServers.length} servers`}</span>
      <span className="sw-server-live-state" data-state={streamStatus}>
        <span className="sw-server-live-dot" aria-hidden />
        {streamStatus === 'connected'
          ? 'Live'
          : streamStatus === 'closed'
            ? `Live updates unavailable · refreshed ${formatRelative(state.refreshedAt)}`
            : `${streamStatus === 'connecting' ? 'Connecting' : 'Reconnecting'} · refreshed ${formatRelative(state.refreshedAt)}`}
      </span>
    </div>
  ) : state.status === 'error' ? 'Server inventory unavailable' : 'Loading Server inventory'

  if (state.status === 'error') {
    return (
      <div className="operator-page sw-servers-page">
        <PageHeader title="Servers" subtitle="Discover hardware inventory and operate Server lifecycle across the datacenter." />
        <ErrorState message={state.message} onRetry={reload} />
      </div>
    )
  }

  return (
    <div className="operator-page sw-servers-page">
      <PageHeader
        title="Servers"
        subtitle="Discover GPU and hardware inventory, then operate provisioning and runtime signals from one fleet control plane."
        metadata={activeProjectionTargetKey ? <Badge colorPalette="blue" variant="subtle">Updating servers…</Badge> : undefined}
        // Two header commands no longer fit beside the title on a phone.
        stackActionsOnMobile
        actions={
          <>
            <Button variant="outline" loading={isRefreshing} onClick={reload}>
              <RefreshCw size={16} />
              Refresh
            </Button>
            {provisioners.length > 0 && (
              <Button colorPalette="brand" onClick={() => setAddingServers(true)}>
                <Plus size={16} />
                Add servers
              </Button>
            )}
          </>
        }
      />
      {addingServers && <AddServersDialog provisioners={provisioners} servers={workingSet} onClose={() => setAddingServers(false)} />}
      <InspectionAttentionAlert
        operations={inspectionAttention}
        onView={(operationId) => navigate(scopedHref(operationId
          ? `/workflows/${operationId}`
          : `/workflows?kind=${INSPECT_HARDWARE_WORKFLOW_KIND}&status=requires_attention`))}
      />

      {state.status === 'ready' && state.refreshError && (
        <Alert status="warning" title="Couldn't refresh the Server inventory" actions={<Button variant="plain" size="sm" onClick={reload}>Retry</Button>}>
          Showing the last complete snapshot while live patches remain available.
        </Alert>
      )}
      {staleProvisioners.map((integration) => (
        <div key={integration.id} className="sw-inline-warning">
          <strong>{integration.name} sync failed</strong>
          <span>{integration.sync.lastError}</span>
        </div>
      ))}
      {lastActionResult && lastActionFailures.length > 0 && (
        <Alert
          status={lastActionResult.succeeded > 0 ? 'warning' : 'error'}
          title={actionLabel(lastActionResult.action) + (lastActionResult.succeeded > 0 ? ' partially accepted' : ' failed')}
          actions={
            <HStack gap="1">
              <Button variant="plain" size="sm" onClick={() => setResultDialogOpen(true)}>View details</Button>
              <Button variant="plain" size="sm" onClick={() => setLastActionResult(null)}>Dismiss</Button>
            </HStack>
          }
        >
          {lastActionFailures.length === 1
            ? `${lastActionFailures[0].serverName}: ${lastActionFailures[0].message}`
            : `${lastActionResult.succeeded} accepted; ${lastActionFailures.length} failed.`}
        </Alert>
      )}

      {(state.status === 'loading' || (state.status === 'ready' && workingSet.length === 0 && !provisionersLoaded)) && <LoadingState rows={8} />}
      {/* The empty inventory explains how to add Servers. Only a scope without any provisioner
          sends the operator to connect one, because Servers can only come from a provisioner. */}
      {state.status === 'ready' && workingSet.length === 0 && provisionersLoaded && (
        provisioners.length > 0 ? (
          <EmptyState
            title="No servers yet"
            action={{ label: 'Add servers', onClick: () => setAddingServers(true) }}
          />
        ) : (
          <EmptyState
            title="No provisioner connected"
            message="Servers come from a provisioner such as MAAS."
            action={{ label: 'Connect a provisioner', onClick: () => navigate(scopedHref('/infrastructure/integrations')) }}
          />
        )
      )}
      {state.status === 'ready' && workingSet.length > 0 && (
        <>
          <ServerFleetOverview servers={workingSet} />
          <InventorySurface
            headingId="server-inventory-title"
            eyebrow="Infrastructure inventory"
            title="Server fleet"
            summary={summary}
            className="sw-server-inventory"
            toolbar={
              <div className="sw-server-toolbar-layout">
                <ServerDiscoverySearch key={query.q} value={query.q} onCommit={commitSearch} />
                <Flex className="sw-server-view-filters" align="center" gap="2" wrap="wrap">
                  <Flex as="div" className="sw-platform-filter-group" role="group" aria-label="Filter Server operational view">
                    {SERVER_VIEWS.map((view) => (
                      <Button
                        key={view.value}
                        className="sw-platform-filter-chip"
                        variant="plain"
                        size="sm"
                        aria-pressed={query.view === view.value}
                        data-active={query.view === view.value || undefined}
                        onClick={() => setLens(view.value)}
                      >
                        {view.label}
                      </Button>
                    ))}
                  </Flex>
                  <PopoverButton
                    title="Server filters"
                    trigger={
                      <Button variant="outline" size="sm">
                        <Filter size={16} />
                        Filters
                        {activeAdvancedFilterCount(query) > 0 && <Badge variant="subtle">{activeAdvancedFilterCount(query)}</Badge>}
                      </Button>
                    }
                  >
                    <ServerFilterPanel
                      query={query}
                      options={facets}
                      onSingle={setSingleFilter}
                      onMulti={setMultiFilter}
                      onIncludeAbsent={(checked) => setSingleFilter('includeAbsent', checked ? 'true' : '', '')}
                      onClear={clearFilters}
                    />
                  </PopoverButton>
                  <PopoverButton
                    title="Display options"
                    trigger={<Button variant="outline" size="sm"><SlidersHorizontal size={16} />Display</Button>}
                  >
                    <DisplayPanel
                      group={query.group}
                      sort={query.sort}
                      direction={query.direction}
                      density={density}
                      pageSize={pageSize}
                      onGroup={(value) => {
                        updateParams((next) => {
                          if (value === 'none') next.delete('group')
                          else next.set('group', value)
                          next.delete('page')
                        })
                        setCollapsedGroups(new Set())
                      }}
                      onSort={(value) => updateParams((next) => {
                        if (value === 'priority') {
                          next.delete('sort')
                          next.delete('dir')
                        } else {
                          next.set('sort', value)
                        }
                        next.delete('page')
                      })}
                      onDirection={(value) => updateParams((next) => {
                        if (value === 'desc') next.set('dir', 'desc')
                        else next.delete('dir')
                        next.delete('page')
                      })}
                      onDensity={(value) => {
                        setDensity(value)
                        writePreference(DENSITY_KEY, value)
                      }}
                      onPageSize={(value) => {
                        setPageSize(value)
                        writePreference(PAGE_SIZE_KEY, value)
                        updateParams((next) => next.delete('page'))
                      }}
                    />
                  </PopoverButton>
                </Flex>
              </div>
            }
          >
            <SelectionToolbar count={selected.size} onClear={clearSelection}>
              <Tooltip content={deployDisabledReason ?? 'Deploy one OS configuration to the selected Servers'}>
                <span>
                  <Button colorPalette="brand" size="sm" disabled={Boolean(deployDisabledReason)} onClick={deploySelected}>
                    <UploadCloud size={16} />Deploy OS
                  </Button>
                </span>
              </Tooltip>
              <Button variant="outline" size="sm" onClick={() => setTagEditorTargets(actionTargets)}><Tags size={16} />Edit tags</Button>
              <ServerTakeActionMenu
                targets={actionTargets}
                includeSingleOnly={false}
                busy={bulk.running}
                trigger="take-action"
                size="sm"
                onAction={(action) => {
                  if (action !== 'delete') void runAction(action, [...selected])
                }}
              />
              {selected.size < visibleServers.length && (
                <Button variant="plain" size="sm" onClick={() => setMany(visibleServers.map((server) => server.id), true)}>
                  Select all {visibleServers.length} matches
                </Button>
              )}
              {deployDisabledReason && <span className="sw-action-reason">{deployDisabledReason}</span>}
            </SelectionToolbar>

            {sortedServers.length === 0 ? (
              <div className="sw-server-filter-empty">
                <EmptyState
                  title={!hasServerInventoryFilters(query) && workingSet.every((server) => server.absent) ? 'No current Servers' : 'No matching Servers'}
                  message={!hasServerInventoryFilters(query) && workingSet.every((server) => server.absent)
                    ? 'Every retained projection is currently absent from provider inventory.'
                    : 'Adjust the discovery search or operational facets to see other Servers.'}
                  action={!hasServerInventoryFilters(query) && workingSet.every((server) => server.absent)
                    ? { label: 'Include absent', onClick: () => setSingleFilter('includeAbsent', 'true', '') }
                    : { label: 'Clear filters', onClick: clearFilters }}
                />
              </div>
            ) : (
              <>
                <ResponsiveDataView
                  desktop={
                    <StickyTableFrame>
                      <Table.Root size={density === 'compact' ? 'sm' : 'md'} aria-label="Servers" className="sw-server-inventory-table">
                        <Table.Header>
                          <Table.Row>
                            <Table.ColumnHeader className="sw-cell-center sw-col-select" aria-label="Row selection">
                              <Checkbox
                                id="select-page"
                                aria-label="Select all on this page"
                                checked={allPageSelected ? true : somePageSelected ? 'indeterminate' : false}
                                onCheckedChange={() => setMany(pageIds, !allPageSelected)}
                              />
                            </Table.ColumnHeader>
                            <Table.ColumnHeader className="sw-cell-center sw-col-details" aria-label="Row details" />
                            <Table.ColumnHeader className="sw-server-col--identity">Server</Table.ColumnHeader>
                            <Table.ColumnHeader className="sw-server-col--power">Power</Table.ColumnHeader>
                            <Table.ColumnHeader className="sw-server-col--network">Network</Table.ColumnHeader>
                            <Table.ColumnHeader className="sw-server-col--deployment">Deployment</Table.ColumnHeader>
                            <Table.ColumnHeader className="sw-server-col--hardware">Hardware</Table.ColumnHeader>
                            <Table.ColumnHeader className="sw-server-col--zone">Zone</Table.ColumnHeader>
                            <Table.ColumnHeader className="sw-server-col--pool">Pool</Table.ColumnHeader>
                            <Table.ColumnHeader className="sw-server-col--health">Health</Table.ColumnHeader>
                            <Table.ColumnHeader className="sw-server-row-actions" aria-label="Server actions" />
                          </Table.Row>
                        </Table.Header>
                        <Table.Body>
                          {renderGroups(pageItems, query.group).map((group) => (
                            <ServerGroupRows
                              key={group.key || 'all'}
                              group={group}
                              grouped={query.group !== 'none'}
                              collapsed={collapsedGroups.has(group.key)}
                              selected={selected}
                              sites={sites}
                              integrations={provisioners}
                              scopedHref={scopedHref}
                              onCollapse={() => setCollapsedGroups((current) => {
                                const next = new Set(current)
                                if (next.has(group.key)) next.delete(group.key)
                                else next.add(group.key)
                                return next
                              })}
                              onToggleOne={toggleOne}
                              onToggleGroup={setMany}
                              onAction={(action, id) => void runAction(action, [id])}
                              onEditTags={(server) => setTagEditorTargets([server])}
                            />
                          ))}
                        </Table.Body>
                      </Table.Root>
                    </StickyTableFrame>
                  }
                  mobile={
                    <div className="sw-server-card-list" aria-label="Servers">
                      {renderGroups(pageItems, query.group).map((group) => (
                        <section key={group.key || 'all'}>
                          {query.group !== 'none' && (
                            <HStack className="sw-server-mobile-group" gap="2">
                              <Text fontWeight="semibold">{group.label}</Text><Badge variant="subtle">{group.items.length}</Badge>
                            </HStack>
                          )}
                          <div className="sw-resource-card-list">
                            {group.items.map((server) => (
                              <ServerMobileCard
                                key={server.id}
                                server={server}
                                checked={selected.has(server.id)}
                                sites={sites}
                                integrations={provisioners}
                                scopedHref={scopedHref}
                                onToggle={() => toggleOne(server.id)}
                                onAction={(action) => void runAction(action, [server.id])}
                                onEditTags={() => setTagEditorTargets([server])}
                              />
                            ))}
                          </div>
                        </section>
                      ))}
                    </div>
                  }
                />
                <div className="sw-server-pagination">
                  <Pagination
                    total={totalPages}
                    value={safePage}
                    onChange={(page) => updateParams((next) => {
                      if (page > 1) next.set('page', String(page))
                      else next.delete('page')
                    })}
                  />
                </div>
              </>
            )}
          </InventorySurface>
        </>
      )}

      {releaseTargets && <ServerReleaseDialog targets={releaseTargets} onClose={() => setReleaseTargets(null)} onRelease={confirmRelease} />}
      {tagEditorTargets && (
        <ServerTagEditor
          servers={tagEditorTargets}
          onClose={() => setTagEditorTargets(null)}
          onSaved={() => {
            clearSelection()
            reload()
          }}
        />
      )}
      {lastActionResult && resultDialogOpen && <ServerActionResultDialog result={lastActionResult} onClose={() => setResultDialogOpen(false)} />}
      {deleteTarget && (
        <ServerDeleteDialog
          serverId={deleteTarget.id}
          serverName={serverDisplayName(deleteTarget)}
          onClose={() => setDeleteTarget(null)}
          onDeleted={() => {
            setDeleteTarget(null)
            clearSelection()
            reload()
          }}
        />
      )}
      {pendingLockAction && (
        <ServerLockDialog
          action={pendingLockAction.action}
          targets={pendingLockAction.targets}
          skipped={pendingLockAction.skipped}
          busy={bulk.running}
          onClose={() => setPendingLockAction(null)}
          onConfirm={() => {
            const pending = pendingLockAction
            setPendingLockAction(null)
            void runAction(pending.action, pending.targets.map((server) => server.id), true)
          }}
        />
      )}
      {pendingPowerOff && (
        <ServerPowerOffWarningDialog
          ramTargets={pendingPowerOff.ramTargets}
          totalTargets={pendingPowerOff.targets.length}
          busy={bulk.running}
          onClose={() => setPendingPowerOff(null)}
          onConfirm={() => {
            const pending = pendingPowerOff
            setPendingPowerOff(null)
            void runAction('power-off', pending.targets.map((server) => server.id), true)
          }}
        />
      )}
    </div>
  )
}

/** Debounced dashboard-owned discovery search; URL changes remount it with canonical input. */
function ServerDiscoverySearch({ value, onCommit }: { value: string; onCommit: (value: string) => void }) {
  const [draft, setDraft] = useState(value)
  useEffect(() => {
    if (draft === value) return
    const handle = setTimeout(() => onCommit(draft), 300)
    return () => clearTimeout(handle)
  }, [draft, onCommit, value])
  return (
    <SearchInput
      value={draft}
      onChange={setDraft}
      placeholder="Search name, address, tag, or GPU"
      aria-label="Search Servers"
      maxW="28rem"
      size="md"
    />
  )
}

/** Popover-backed toolbar control shared by the filter and display panels. */
function PopoverButton({ title, trigger, children }: { title: string; trigger: ReactNode; children: ReactNode }) {
  return (
    <Popover.Root positioning={{ placement: 'bottom-end' }}>
      <Popover.Trigger asChild>{trigger}</Popover.Trigger>
      <Portal>
        <Popover.Positioner>
          <Popover.Content className="sw-server-popover">
            <Popover.Arrow />
            <Popover.Header fontWeight="semibold">{title}</Popover.Header>
            <Popover.Body>{children}</Popover.Body>
          </Popover.Content>
        </Popover.Positioner>
      </Portal>
    </Popover.Root>
  )
}

function updateValues(values: readonly string[], value: string, checked: boolean): string[] {
  return checked ? [...values, value] : values.filter((item) => item !== value)
}

/** One exact multi-select facet with stable occurrence counts. */
function FilterOptions({
  title,
  param,
  values,
  selected,
  onChange,
}: {
  title: string
  param: string
  values: FilterOption[]
  selected: readonly string[]
  onChange: (key: string, next: readonly string[]) => void
}) {
  if (values.length === 0) return null
  return (
    <fieldset className="sw-filter-group">
      <legend>{title}</legend>
      {values.map((option) => (
        <Checkbox
          key={option.value}
          id={`server-filter-${param}-${controlId(option.value)}`}
          checked={selected.includes(option.value)}
          onCheckedChange={(checked) => onChange(param, updateValues(selected, option.value, checked))}
        >
          {`${option.label} (${option.count})`}
        </Checkbox>
      ))}
    </fieldset>
  )
}

/** Advanced operational and hardware facets; quick lenses remain in the main toolbar. */
function ServerFilterPanel({
  query,
  options,
  onSingle,
  onMulti,
  onIncludeAbsent,
  onClear,
}: {
  query: ServerInventoryQuery
  options: ServerFacetOptions
  onSingle: (key: string, value: string, fallback?: string) => void
  onMulti: (key: string, values: readonly string[]) => void
  onIncludeAbsent: (checked: boolean) => void
  onClear: () => void
}) {
  // Health keeps its place while monitoring is in development but cannot be used.
  const monitoring = useExperimentalFeature('monitoring')
  return (
    <div className="sw-server-filter-panel">
      <div className="sw-server-filter-panel__selects">
        <fieldset className="sw-filter-group" aria-describedby={monitoring ? undefined : 'server-filter-health-note'}>
          <legend>Health</legend>
          <Select value={query.health} aria-label="Filter Server health" size="sm" disabled={!monitoring} onChange={(value) => onSingle('health', value)} options={[
            { value: 'any', label: 'Any health' },
            { value: 'up', label: 'Up' },
            { value: 'down', label: 'Down' },
            { value: 'unobserved', label: 'Unobserved' },
          ]} />
          {!monitoring && (
            <Text id="server-filter-health-note" color="fg.muted" fontSize="xs">{NOT_AVAILABLE_IN_RELEASE}</Text>
          )}
        </fieldset>
        <fieldset className="sw-filter-group">
          <legend>Membership</legend>
          <Select value={query.membership} aria-label="Filter Platform membership" size="sm" onChange={(value) => onSingle('membership', value)} options={[
            { value: 'any', label: 'Any membership' },
            { value: 'assigned', label: 'Assigned' },
            { value: 'unassigned', label: 'Unassigned' },
          ]} />
        </fieldset>
        <fieldset className="sw-filter-group">
          <legend>GPU presence</legend>
          <Select value={query.gpu} aria-label="Filter GPU presence" size="sm" onChange={(value) => onSingle('gpu', value)} options={[
            { value: 'any', label: 'Any hardware' },
            { value: 'present', label: 'GPU equipped' },
            { value: 'none', label: 'CPU only' },
          ]} />
        </fieldset>
        <fieldset className="sw-filter-group">
          <legend>Protection</legend>
          <Select value={query.lock} aria-label="Filter Server lock" size="sm" onChange={(value) => onSingle('lock', value)} options={[
            { value: 'any', label: 'Any protection' },
            { value: 'locked', label: 'Locked' },
            { value: 'unlocked', label: 'Unlocked' },
          ]} />
        </fieldset>
      </div>
      <Checkbox id="include-absent" checked={query.includeAbsent} onCheckedChange={onIncludeAbsent}>Include absent projections</Checkbox>
      <FilterOptions title="GPU vendor" param="gpuVendor" values={options.gpuVendor} selected={query.gpuVendors} onChange={onMulti} />
      <FilterOptions title="GPU model" param="gpuModel" values={options.gpuModel} selected={query.gpuModels} onChange={onMulti} />
      <FilterOptions title="Architecture" param="architecture" values={options.architecture} selected={query.architectures} onChange={onMulti} />
      <FilterOptions title="System vendor" param="systemVendor" values={options.systemVendor} selected={query.systemVendors} onChange={onMulti} />
      <FilterOptions title="System product" param="systemProduct" values={options.systemProduct} selected={query.systemProducts} onChange={onMulti} />
      <FilterOptions title="Zone" param="zone" values={options.zone} selected={query.zones} onChange={onMulti} />
      <FilterOptions title="Pool" param="pool" values={options.pool} selected={query.pools} onChange={onMulti} />
      <FilterOptions title="Tags" param="tag" values={options.tag} selected={query.tags} onChange={onMulti} />
      <Button variant="plain" size="sm" onClick={onClear}>Clear filters</Button>
    </div>
  )
}

/** Local readability and URL-owned grouping/sorting controls. */
function DisplayPanel({
  group,
  sort,
  direction,
  density,
  pageSize,
  onGroup,
  onSort,
  onDirection,
  onDensity,
  onPageSize,
}: {
  group: ServerInventoryGroup
  sort: ServerInventorySort
  direction: ServerInventoryDirection
  density: ServerDensity
  pageSize: number
  onGroup: (value: ServerInventoryGroup) => void
  onSort: (value: ServerInventorySort) => void
  onDirection: (value: ServerInventoryDirection) => void
  onDensity: (value: ServerDensity) => void
  onPageSize: (value: number) => void
}) {
  return (
    <div className="sw-server-display-panel">
      <Select value={group} aria-label="Group Servers" size="sm" onChange={(value) => onGroup(value as ServerInventoryGroup)} options={GROUP_OPTIONS} />
      <Select value={sort} aria-label="Sort Servers" size="sm" onChange={(value) => onSort(value as ServerInventorySort)} options={SORT_OPTIONS} />
      {sort !== 'priority' && (
        <Select value={direction} aria-label="Sort direction" size="sm" onChange={(value) => onDirection(value as ServerInventoryDirection)} options={[
          { value: 'asc', label: 'Ascending' },
          { value: 'desc', label: 'Descending' },
        ]} />
      )}
      <Select value={density} aria-label="Table density" size="sm" onChange={(value) => onDensity(value as ServerDensity)} options={[
        { value: 'compact', label: 'Compact rows' },
        { value: 'comfortable', label: 'Comfortable rows' },
      ]} />
      <Select value={String(pageSize)} aria-label="Rows per page" size="sm" onChange={(value) => onPageSize(Number(value))} options={
        [25, 50, 100].map((size) => ({ value: String(size), label: `${size} rows` }))
      } />
    </div>
  )
}

function ServerGroupRows({
  group,
  grouped,
  collapsed,
  selected,
  sites,
  integrations,
  scopedHref,
  onCollapse,
  onToggleOne,
  onToggleGroup,
  onAction,
  onEditTags,
}: {
  group: RenderGroup
  grouped: boolean
  collapsed: boolean
  selected: ReadonlySet<string>
  sites: readonly Site[]
  integrations: readonly Integration[]
  scopedHref: (path: string) => string
  onCollapse: () => void
  onToggleOne: (id: string) => void
  onToggleGroup: (ids: string[], checked: boolean) => void
  onAction: (action: ServerMenuAction, id: string) => void
  onEditTags: (server: Server) => void
}) {
  const ids = group.items.map((server) => server.id)
  const all = ids.length > 0 && ids.every((id) => selected.has(id))
  const some = ids.some((id) => selected.has(id))
  return (
    <>
      {grouped && (
        <Table.Row className="sw-server-group-row">
          <Table.Cell colSpan={11}>
            <HStack gap="2">
              <IconButton variant="ghost" size="xs" aria-label={`${collapsed ? 'Expand' : 'Collapse'} ${group.label}`} onClick={onCollapse}>
                {collapsed ? <ChevronRight size={16} /> : <ChevronDown size={16} />}
              </IconButton>
              <Checkbox id={`server-group-${controlId(group.key)}`} aria-label={`Select all in ${group.label}`} checked={all ? true : some ? 'indeterminate' : false} onCheckedChange={() => onToggleGroup(ids, !all)} />
              <strong>{group.label}</strong><Badge variant="subtle">{group.items.length}</Badge>
            </HStack>
          </Table.Cell>
        </Table.Row>
      )}
      {!collapsed && group.items.map((server) => (
        <ServerRow
          key={server.id}
          server={server}
          checked={selected.has(server.id)}
          sites={sites}
          integrations={integrations}
          scopedHref={scopedHref}
          onToggle={() => onToggleOne(server.id)}
          onAction={(action) => onAction(action, server.id)}
          onEditTags={() => onEditTags(server)}
        />
      ))}
    </>
  )
}

function ServerIdentity({
  server,
  scopedHref,
  onEditTags,
}: {
  server: Server
  scopedHref: (path: string) => string
  onEditTags: () => void
}) {
  const name = serverDisplayName(server)
  return (
    <div className="sw-server-identity">
      <div className="sw-server-identity__copy">
        <HStack gap="1" minW="0">
          {server.provisioning?.locked && (
            <Tooltip content="Locked">
              <span className="sw-server-lock-icon" role="img" aria-label="Locked"><Lock size={14} /></span>
            </Tooltip>
          )}
          {server.provisioning?.ephemeral && (
            <Tooltip content="RAM deployment · Data written to this Server is lost on power off or reboot">
              <span className="sw-server-ram-icon" role="img" aria-label="RAM deployment"><MemoryStick size={13} aria-hidden /></span>
            </Tooltip>
          )}
          <RouterLink className="sw-server-name" to={scopedHref(`/servers/${server.id}/summary`)}>{name}</RouterLink>
          <CopyButton value={name} label="Copy Server name" />
        </HStack>
        <TagSummary server={server} onEdit={onEditTags} />
        {server.absent && <Badge colorPalette="gray" variant="subtle">Absent</Badge>}
      </div>
    </div>
  )
}

/** High-value network identifiers kept together so they scan as one fact. */
function ServerNetworkIdentity({ server }: { server: Server }) {
  const primaryAddress = serverPrimaryAddress(server)
  const primaryMac = server.hardware.macAddresses[0]
  return (
    <div className="sw-server-network-identity">
      <div>
        <span>IP</span>
        <span className="sw-server-network-value">
          <span className="mono">{textOrDash(primaryAddress)}</span>
          {primaryAddress && <CopyButton value={primaryAddress} label="Copy IP address" />}
        </span>
      </div>
      <div>
        <span>MAC</span>
        <span className="sw-server-network-value">
          <span className="mono">{textOrDash(primaryMac)}</span>
          {primaryMac && <CopyButton value={primaryMac} label="Copy MAC address" />}
        </span>
      </div>
    </div>
  )
}

/** Power state only; RAM deployment risk is presented with Server identity. */
function ServerPowerIndicator({ server }: { server: Server }) {
  return <PowerBadge powerState={server.provisioning?.powerState ?? null} decorative={Boolean(server.provisioning)} />
}

/**
 * The Deployment cell of a list row and mobile card: the deployment state plus, when the state
 * calls for one, the row's single contextual next step (`serverContextAction`). The step lives
 * here rather than in the Actions column because it always follows this axis.
 */
function ServerDeployment({ server, scopedHref }: { server: Server; scopedHref: (path: string) => string }) {
  const action = serverContextAction(server)
  const { axis, provider } = serverDeploymentCellInputs(server)
  return <DeploymentSummary
    axis={axis}
    provider={provider}
    action={action && <ServerContextActionLink server={server} action={action} scopedHref={scopedHref} />}
  />
}

/**
 * Frameless icon-and-text link for a contextual next step. It sits in Deployment's secondary line
 * instead of competing with the state. The tooltip adds the destination section for Activity
 * links, and the accessible name includes the Server so screen-reader link lists still distinguish
 * otherwise repeated labels.
 */
const SERVER_CONTEXT_ACTION_ICONS: Record<ServerContextAction['label'], LucideIcon> = {
  'Deploy OS': Rocket,
  'View workflow': ArrowUpRight,
  'View activity': Eye,
  'Review activity': TriangleAlert,
}

function ServerContextActionLink({
  server,
  action,
  scopedHref,
}: {
  server: Server
  action: ServerContextAction
  scopedHref: (path: string) => string
}) {
  const tooltip = action.kind === 'activity'
    ? `${action.label} · ${SERVER_ACTIVITY_SECTION_TITLES[action.section]}`
    : action.label
  const Icon = SERVER_CONTEXT_ACTION_ICONS[action.label]
  return (
    <Tooltip content={tooltip}>
      <RouterLink
        className="sw-server-context-action"
        to={scopedHref(serverContextActionPath(server, action))}
        aria-label={`${action.label} for ${serverDisplayName(server)}`}
      >
        <Icon size={13} aria-hidden />
        <span className="sw-server-context-action__label">{action.label}</span>
      </RouterLink>
    </Tooltip>
  )
}

/** Compact discovery tags with a direct, keyboard-accessible editing shortcut. */
function TagSummary({ server, onEdit }: { server: Server; onEdit: () => void }) {
  const tags = sortedServerTags(server)
  return (
    <HStack className="sw-server-tag-summary" gap="1" wrap="wrap">
      {tags.length === 0 && <Text as="span" color="fg.muted">No tags</Text>}
      {tags.slice(0, 2).map((tag) => <ResourceTag key={tag}>{tag}</ResourceTag>)}
      {tags.length > 2 && (
        <Tooltip content={tags.join(', ')}><ResourceTag ariaLabel={`${tags.length - 2} more tags`}>+{tags.length - 2}</ResourceTag></Tooltip>
      )}
      <Tooltip content={`Edit tags for ${serverDisplayName(server)}`}>
        <IconButton
          variant="ghost"
          size="2xs"
          aria-label={`Edit tags for ${serverDisplayName(server)}`}
          onClick={(event) => {
            event.stopPropagation()
            onEdit()
          }}
        >
          <PenLine size={13} aria-hidden />
        </IconButton>
      </Tooltip>
    </HStack>
  )
}

/**
 * Dense Hardware summary with exactly one emphasized first fact: the primary GPU when present,
 * otherwise the CPU identity. Exact provider values remain keyboard-accessible through tooltips.
 */
function ServerHardware({ server }: { server: Server }) {
  const compactProfile = serverGpuCompactSummary(server)
  const fullProfile = serverGpuProfile(server)
  const hasGPU = Boolean(compactProfile)
  const cpuModel = server.cpuModel.trim()
  const cpuCores = server.cpuCores > 0 ? server.cpuCores.toLocaleString() : null
  const memory = server.memoryMiB > 0 ? quantityOrDash(server.memoryMiB, 'GiB', 1024) : null
  const hasHardwareInventory = hasGPU || Boolean(cpuModel) || Boolean(cpuCores) || Boolean(memory)

  if (!hasHardwareInventory) {
    return (
      <div className="sw-server-hardware-summary">
        <strong className="sw-server-hardware-empty">Not reported</strong>
      </div>
    )
  }

  const gpuSummary = hasGPU ? (
    <strong
      className="sw-server-hardware-gpu"
      tabIndex={0}
    >
      {compactProfile}
    </strong>
  ) : null
  const cpuSummary = cpuModel ? (
    <Tooltip content={cpuModel}>
      <span className="sw-server-hardware-cpu" tabIndex={0}>
        {cpuModel}
      </span>
    </Tooltip>
  ) : cpuCores || (!hasGPU && memory) ? (
    <span className="sw-server-hardware-cpu" data-state="unobserved">Model not reported</span>
  ) : null

  return (
    <div className="sw-server-hardware-summary">
      {gpuSummary && (
        <div className="sw-server-hardware-fact sw-server-hardware-fact--primary">
          <span className="sw-server-hardware-label">GPU</span>
          <Tooltip content={fullProfile}>{gpuSummary}</Tooltip>
        </div>
      )}
      {cpuSummary && (
        <div className={`sw-server-hardware-fact${hasGPU ? '' : ' sw-server-hardware-fact--primary'}`}>
          {!hasGPU && <span className="sw-server-hardware-label">CPU</span>}
          {cpuSummary}
        </div>
      )}
      {(cpuCores || memory) && (
        <div className="sw-server-hardware-capacity" aria-label="Server capacity">
          {cpuCores && <span><strong>{cpuCores}</strong> cores</span>}
          {memory && <span>RAM <strong>{memory}</strong></span>}
        </div>
      )}
    </div>
  )
}

function ServerDetailsPanel({ groups }: { groups: readonly DetailFactGroup[] }) {
  return (
    <div className="sw-server-details-panel">
      {groups.map((group) => (
        <section key={group.title}>
          <Heading as="h3" size="sm">{group.title}</Heading>
          <dl>
            {group.facts.map((fact) => <div key={fact.label}><dt>{fact.label}</dt><dd>{fact.value}</dd></div>)}
          </dl>
        </section>
      ))}
    </div>
  )
}

function ServerRow({
  server,
  checked,
  sites,
  integrations,
  scopedHref,
  onToggle,
  onAction,
  onEditTags,
}: {
  server: Server
  checked: boolean
  sites: readonly Site[]
  integrations: readonly Integration[]
  scopedHref: (path: string) => string
  onToggle: () => void
  onAction: (action: ServerMenuAction) => void
  onEditTags: () => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [powerDialogOpen, setPowerDialogOpen] = useState(false)
  const monitoring = useExperimentalFeature('monitoring')
  const detailId = `server-details-${server.id}`
  const tone = serverRowTone(server)
  return (
    <>
      <Table.Row data-selected={checked || undefined} data-tone={tone}>
        <Table.Cell className="sw-cell-center sw-col-select">
          <Checkbox id={`select-${server.id}`} aria-label={`Select ${serverDisplayName(server)}`} checked={checked} onCheckedChange={onToggle} />
        </Table.Cell>
        <Table.Cell className="sw-cell-center sw-col-details">
          <IconButton variant="ghost" size="xs" aria-label={`${expanded ? 'Hide' : 'Show'} details for ${serverDisplayName(server)}`} aria-expanded={expanded} aria-controls={detailId} onClick={() => setExpanded((value) => !value)}>
            {expanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
          </IconButton>
        </Table.Cell>
        <Table.Cell className="sw-server-col--identity"><ServerIdentity server={server} scopedHref={scopedHref} onEditTags={onEditTags} /></Table.Cell>
        <Table.Cell className="sw-server-col--power" onClick={(event) => event.stopPropagation()}>
          {server.provisioning ? (
            <Tooltip content={`Power actions (${powerStateLabel(server.provisioning.powerState)})${server.provisioning.ephemeral ? ' · RAM deployment' : ''}`}>
              <Button
                variant="plain"
                className="sw-power-button"
                aria-label={`Power actions for ${serverDisplayName(server)}${server.provisioning.ephemeral ? '; RAM deployment' : ''}`}
                onClick={() => setPowerDialogOpen(true)}
              >
                <ServerPowerIndicator server={server} />
              </Button>
            </Tooltip>
          ) : <ServerPowerIndicator server={server} />}
        </Table.Cell>
        <Table.Cell className="sw-server-col--network"><ServerNetworkIdentity server={server} /></Table.Cell>
        <Table.Cell className="sw-server-col--deployment"><ServerDeployment server={server} scopedHref={scopedHref} /></Table.Cell>
        <Table.Cell className="sw-server-col--hardware"><ServerHardware server={server} /></Table.Cell>
        <Table.Cell className="sw-server-col--zone">{textOrDash(server.providerZone)}</Table.Cell>
        <Table.Cell className="sw-server-col--pool">{textOrDash(server.providerResourcePool)}</Table.Cell>
        <Table.Cell className="sw-server-col--health"><HealthBadge axis={server.health} /></Table.Cell>
        <Table.Cell className="sw-server-row-actions">
          <ServerTakeActionMenu targets={[server]} trigger="actions" targetName={serverDisplayName(server)} onAction={onAction} />
        </Table.Cell>
      </Table.Row>
      {expanded && (
        <Table.Row id={detailId} className="sw-server-detail-row">
          <Table.Cell colSpan={11}><ServerDetailsPanel groups={serverDetailFacts(server, sites, integrations, monitoring)} /></Table.Cell>
        </Table.Row>
      )}
      {powerDialogOpen && (
        <ServerPowerDialog
          server={server}
          onClose={() => setPowerDialogOpen(false)}
          onSelect={(action) => {
            setPowerDialogOpen(false)
            onAction(action)
          }}
        />
      )}
    </>
  )
}

/** Mobile card with the same axes, discovery facts, details, and navigation as a desktop row. */
function ServerMobileCard({
  server,
  checked,
  sites,
  integrations,
  scopedHref,
  onToggle,
  onAction,
  onEditTags,
}: {
  server: Server
  checked: boolean
  sites: readonly Site[]
  integrations: readonly Integration[]
  scopedHref: (path: string) => string
  onToggle: () => void
  onAction: (action: ServerMenuAction) => void
  onEditTags: () => void
}) {
  const [powerDialogOpen, setPowerDialogOpen] = useState(false)
  const monitoring = useExperimentalFeature('monitoring')
  const details = serverDetailFacts(server, sites, integrations, monitoring).flatMap((group) => (
    group.facts.map((fact) => ({ ...fact, label: `${group.title} · ${fact.label}` }))
  ))
  return (
    <div className="sw-server-runtime-card" data-tone={serverRowTone(server)}>
      <ResourceCard
        title={<ServerIdentity server={server} scopedHref={scopedHref} onEditTags={onEditTags} />}
        selected={checked}
        status={server.absent ? <Badge variant="subtle">Absent</Badge> : undefined}
        details={details.map((fact) => <ResourceCardField key={fact.label} label={fact.label}>{fact.value}</ResourceCardField>)}
        actions={
          <>
            <Checkbox id={`server-mobile-${server.id}`} aria-label={`Mobile selection: ${serverDisplayName(server)}`} checked={checked} onCheckedChange={onToggle}>Select</Checkbox>
            {server.provisioning && (
              <Button
                variant="outline"
                size="sm"
                aria-label={`Power actions for ${serverDisplayName(server)}${server.provisioning.ephemeral ? '; RAM deployment' : ''}`}
                onClick={() => setPowerDialogOpen(true)}
              >
                <ServerPowerIndicator server={server} /> Power · {powerStateLabel(server.provisioning.powerState)}
              </Button>
            )}
            <ServerTakeActionMenu targets={[server]} trigger="actions" targetName={serverDisplayName(server)} onAction={onAction} />
          </>
        }
      >
        <ResourceCardField label="Network"><ServerNetworkIdentity server={server} /></ResourceCardField>
        <ResourceCardField label="Deployment"><ServerDeployment server={server} scopedHref={scopedHref} /></ResourceCardField>
        <ResourceCardField label="Hardware"><ServerHardware server={server} /></ResourceCardField>
        <ResourceCardField label="Zone">{textOrDash(server.providerZone)}</ResourceCardField>
        <ResourceCardField label="Pool">{textOrDash(server.providerResourcePool)}</ResourceCardField>
      </ResourceCard>
      {powerDialogOpen && (
        <ServerPowerDialog
          server={server}
          onClose={() => setPowerDialogOpen(false)}
          onSelect={(action) => {
            setPowerDialogOpen(false)
            onAction(action)
          }}
        />
      )}
    </div>
  )
}
