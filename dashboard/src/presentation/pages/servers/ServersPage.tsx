import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Badge, Box, Button, HStack, IconButton, Menu, Popover, Portal, Table, Text } from '@chakra-ui/react'
import { ArrowDown, ArrowUp, ChevronDown, ChevronRight, Columns3, Filter, Lock, MoreVertical, Tags, UploadCloud } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { refreshServerProjections } from '@/application/usecases/servers/refreshServerProjections'
import type { Integration } from '@/domain/site/types'
import type { ReleaseServerInput, Server } from '@/domain/server/types'
import {
  EMPTY_SERVER_FILTERS,
  compareServers,
  countByDimension,
  groupValueOf,
  matchesServerFilters,
  serverDisplayName,
  serverMacAddress,
  serverPrimaryAddress,
  type ServerDimension,
  type ServerFilters,
  type ServerGroupBy,
  type ServerSortKey,
  type SortDirection,
} from '@/domain/server/list'
import { PageHeader } from '@/presentation/components/PageHeader'
import { DataToolbar, SelectionToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { GpuVendorLogo } from '@/presentation/components/GpuVendorLogo'
import { LoadingState } from '@/presentation/components/LoadingState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { Pagination } from '@/presentation/components/Pagination'
import { DeploymentBadge, HealthBadge, MembershipBadge, PowerBadge } from '@/presentation/components/AxisBadge'
import { powerStateLabel } from '@/presentation/components/axisBadgeUtils'
import { CopyButton } from '@/presentation/components/CopyButton'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Select } from '@/presentation/components/ui/select'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { SERVER_ACTION_GROUPS, actionLabel, serverActionAvailability, type BulkAction, type ServerMenuAction } from './serverActions'
import { ServerLockDialog } from './ServerLockDialog'
import { ServerDeleteDialog } from './ServerDeleteDialog'
import { ServerReleaseDialog } from './ServerReleaseDialog'
import { ServerPowerDialog } from './ServerPowerDialog'
import { useServerWorkingSet } from './useServerWorkingSet'
import { useServerBulkActions } from './useServerBulkActions'
import { ServerTagEditor } from './ServerTagEditor'
import { ServerActionResultDialog } from './ServerActionResultDialog'
import { failedServerActionOutcomes, type ServerActionRunResult, type ServerActionTarget } from './serverActionResults'

/** Table row density for the server list; controls compact vs comfortable row spacing. */
type ServerDensity = 'compact' | 'comfortable'

interface ColumnToggle {
  key: string
  label: string
}
interface FilterOption {
  value: string
  label: string
  count: number
}
interface ServerFilterOptions {
  provisioningState: FilterOption[]
  zone: FilterOption[]
  pool: FilterOption[]
  tag: FilterOption[]
}

/** Independent provider observations that replace the former composed Hardware cell. */
const HARDWARE_COLUMNS: ColumnToggle[] = [
  { key: 'architecture', label: 'Architecture' },
  { key: 'cpuCores', label: 'CPU cores' },
  { key: 'cpuModel', label: 'CPU model' },
  { key: 'memory', label: 'Memory' },
  { key: 'storage', label: 'Storage' },
  { key: 'systemVendor', label: 'System vendor' },
  { key: 'systemProduct', label: 'System product' },
]
const OPTIONAL_COLUMNS: ColumnToggle[] = [
  { key: 'power', label: 'Power' },
  { key: 'status', label: 'Deployment' },
  { key: 'address', label: 'Address' },
  { key: 'mac', label: 'MAC address' },
  { key: 'zone', label: 'Zone' },
  { key: 'pool', label: 'Pool' },
  { key: 'tags', label: 'Tags' },
  ...HARDWARE_COLUMNS,
  { key: 'gpus', label: 'GPUs' },
  { key: 'platform', label: 'Platform' },
  { key: 'health', label: 'Health' },
]
const DEFAULT_PAGE_SIZE = 50
const EMPTY_SERVERS: Server[] = []
const GROUP_KEY = 'swallow.servers.group-by'
const COLUMNS_KEY = 'swallow.servers.hidden-columns'
const PAGE_SIZE_KEY = 'swallow.servers.page-size'
const DEPLOYMENT_POLL_INTERVAL_MS = 2_000
const MAX_DEPLOYMENT_POLL_ATTEMPTS = 150
// How long to keep polling a just-released Server that is not yet in an active provisioning
// axis, so the list reflects the release once the durable Operation dispatches.
const RELEASE_FOLLOW_WINDOW_MS = 180_000

function readPreference<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key)
    return raw === null ? fallback : (JSON.parse(raw) as T)
  } catch {
    return fallback
  }
}
function writePreference<T>(key: string, value: T): void {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Browser policy can block persistence; the active view remains fully usable.
  }
}
function toFilterOptions(counts: Map<string, number>): FilterOption[] {
  return [...counts.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([value, count]) => ({ value, label: value, count }))
}
function filtersAreEmpty(filters: ServerFilters, keyword: string): boolean {
  return (
    keyword === '' &&
    filters.provisioningStates.length === 0 &&
    filters.zones.length === 0 &&
    filters.pools.length === 0 &&
    filters.tags.length === 0 &&
    filters.hasGpu === null &&
    filters.lockState === 'any'
  )
}

/** Preserves visibility choices when a composed legacy column becomes independent fields. */
function normalizeHiddenColumns(columns: readonly string[]): string[] {
  const normalized = new Set(columns)
  if (normalized.delete('placement')) {
    normalized.add('zone')
    normalized.add('pool')
  }
  if (normalized.delete('hardware')) {
    HARDWARE_COLUMNS.forEach((column) => normalized.add(column.key))
  }
  return [...normalized]
}

/** Uses one quiet placeholder for provider text that has not been observed. */
function textOrDash(value: string | null | undefined): string {
  return value?.trim() || '-'
}

/** Formats positive hardware quantities while treating zero as an absent observation. */
function quantityOrDash(value: number, unit = '', divisor = 1): string {
  if (!Number.isFinite(value) || value <= 0) return '-'
  const quantity = Math.round(value / divisor)
  return unit ? `${quantity} ${unit}` : String(quantity)
}

interface RenderGroup {
  key: string
  label: string
  items: Server[]
}
function renderGroups(items: Server[], groupBy: ServerGroupBy): RenderGroup[] {
  if (groupBy === 'none') return [{ key: '', label: '', items }]
  const groups = new Map<string, Server[]>()
  for (const server of items) {
    const key = groupValueOf(server, groupBy)
    groups.set(key, [...(groups.get(key) ?? []), server])
  }
  return [...groups.entries()].map(([key, grouped]) => ({ key, label: key, items: grouped }))
}

/**
 * NetBox-style fleet inventory with MAAS lifecycle axes and selection-driven actions.
 * All API pages are loaded before client grouping/filtering, so counts, saved views, and
 * select-all operate over the complete scoped working set rather than the first 100 rows.
 */
export function ServersPage() {
  const { sites, servers } = useApp()
  const navigate = useNavigate()
  const { siteId, scopedHref } = useSiteScope()
  const bulk = useServerBulkActions()
  const [searchInput, setSearchInput] = useState('')
  const [coarseKeyword, setCoarseKeyword] = useState('')
  const [includeAbsent, setIncludeAbsent] = useState(false)
  const [filters, setFilters] = useState<ServerFilters>(EMPTY_SERVER_FILTERS)
  const [groupBy, setGroupBy] = useState<ServerGroupBy>(() => readPreference(GROUP_KEY, 'none'))
  const [sortKey, setSortKey] = useState<ServerSortKey>('name')
  const [sortDir, setSortDir] = useState<SortDirection>('asc')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(() => readPreference(PAGE_SIZE_KEY, DEFAULT_PAGE_SIZE))
  const [density, setDensity] = useState<ServerDensity>('compact')
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [collapsedGroups, setCollapsedGroups] = useState<ReadonlySet<string>>(new Set())
  const [hiddenColumns, setHiddenColumns] = useState<ReadonlySet<string>>(
    () => new Set(normalizeHiddenColumns(readPreference<string[]>(COLUMNS_KEY, []))),
  )
  const [provisioners, setProvisioners] = useState<Integration[]>([])
  const [deleteTarget, setDeleteTarget] = useState<Server | null>(null)
  const [releaseTargets, setReleaseTargets] = useState<ServerActionTarget[] | null>(null)
  // The Servers whose tags are being edited in batch; null while the editor is closed.
  const [tagEditorTargets, setTagEditorTargets] = useState<Server[] | null>(null)
  const [lastActionResult, setLastActionResult] = useState<ServerActionRunResult | null>(null)
  const [resultDialogOpen, setResultDialogOpen] = useState(false)
  const [pendingLockAction, setPendingLockAction] = useState<{
    action: 'lock' | 'unlock'
    targets: readonly Server[]
    skipped: readonly Server[]
  } | null>(null)

  useEffect(() => {
    const id = setTimeout(() => {
      setCoarseKeyword(searchInput)
      setPage(1)
    }, 300)
    return () => clearTimeout(id)
  }, [searchInput])
  const query = useMemo(() => ({ siteId, keyword: coarseKeyword || undefined, includeAbsent }), [coarseKeyword, includeAbsent, siteId])
  const { state, reload } = useServerWorkingSet(query)
  // Servers whose release we just accepted. A durable release Operation runs asynchronously,
  // so the Server is not yet in an active provisioning axis and the active-projection poll
  // below will not pick it up. Follow these Servers for a bounded window so the list reflects
  // the release (deployed -> releasing -> ready) in place, without a manual refresh.
  const [followedServerIds, setFollowedServerIds] = useState<readonly string[]>([])
  useEffect(() => {
    let cancelled = false
    sites
      .listIntegrations({ siteId, kind: 'provisioner' })
      .then((items) => {
        if (!cancelled) setProvisioners(items)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [siteId, sites])

  const workingSet = state.status === 'ready' ? state.data.servers : EMPTY_SERVERS
  const activeProjectionTargetKey = useMemo(
    () =>
      workingSet
        .filter(
          (server) =>
            ['deploying', 'releasing', 'commissioning', 'testing'].includes(server.provisioning?.state ?? '') ||
            ['deploying', 'verifying'].includes(server.deployment?.state ?? ''),
        )
        .map((server) => server.id)
        .sort()
        .join(','),
    [workingSet],
  )
  // The set of Servers to poll: those already in an active axis, plus recently released
  // Servers we are following until the durable Operation moves them into one. Deduped so a
  // Server that becomes active while followed is polled once, not twice.
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
      // Nudge the backend to re-observe each active Server live; the resulting projection
      // write is delivered back through the SSE stream and patched into the list by
      // useServerWorkingSet, so this no longer refetches the whole list.
      await refreshServerProjections(servers, targetIds)
      if (cancelled) return
      // SSE is the primary update path. A recently accepted Release also reloads the
      // working set so the row still converges if stream delivery is delayed or offline.
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
  }, [followedServerIds.length, pollTargetKey, reload, servers])
  // Stop following released Servers after a bounded window so the list does not poll forever.
  useEffect(() => {
    if (followedServerIds.length === 0) return
    const handle = setTimeout(() => setFollowedServerIds([]), RELEASE_FOLLOW_WINDOW_MS)
    return () => clearTimeout(handle)
  }, [followedServerIds])
  const filtered = useMemo(() => workingSet.filter((server) => matchesServerFilters(server, filters)), [filters, workingSet])
  const sorted = useMemo(() => [...filtered].sort((a, b) => compareServers(a, b, sortKey, sortDir)), [filtered, sortDir, sortKey])
  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize))
  const safePage = Math.min(page, totalPages)
  const pageItems = useMemo(() => sorted.slice((safePage - 1) * pageSize, safePage * pageSize), [pageSize, safePage, sorted])
  const filterOptions = useMemo<ServerFilterOptions>(
    () => ({
      provisioningState: toFilterOptions(countByDimension(workingSet, 'provisioningState')),
      zone: toFilterOptions(countByDimension(workingSet, 'zone')),
      pool: toFilterOptions(countByDimension(workingSet, 'pool')),
      tag: toFilterOptions(countByDimension(workingSet, 'tag' as ServerDimension)),
    }),
    [workingSet],
  )

  const toggleOne = useCallback(
    (id: string) =>
      setSelected((current) => {
        const next = new Set(current)
        if (next.has(id)) next.delete(id)
        else next.add(id)
        return next
      }),
    [],
  )
  const setMany = useCallback(
    (ids: string[], checked: boolean) =>
      setSelected((current) => {
        const next = new Set(current)
        ids.forEach((id) => {
          if (checked) next.add(id)
          else next.delete(id)
        })
        return next
      }),
    [],
  )
  const clearSelection = useCallback(() => setSelected(new Set()), [])
  const pageIds = pageItems.map((server) => server.id)
  const allPageSelected = pageIds.length > 0 && pageIds.every((id) => selected.has(id))
  const somePageSelected = pageIds.some((id) => selected.has(id))
  const changeFilters = useCallback((next: ServerFilters) => {
    setFilters(next)
    setPage(1)
    setSelected(new Set())
  }, [])
  const toggleColumn = useCallback(
    (key: string) =>
      setHiddenColumns((current) => {
        const next = new Set(current)
        if (next.has(key)) next.delete(key)
        else next.add(key)
        writePreference(COLUMNS_KEY, [...next])
        return next
      }),
    [],
  )
  const runAction = useCallback(
    async (action: ServerMenuAction, ids: string[], confirmed = false) => {
      if (!ids.length) return
      const selectedServers = ids.map((id) => workingSet.find((server) => server.id === id)).filter((server): server is Server => Boolean(server))
      const availability = serverActionAvailability(action, selectedServers)
      if (availability.disabledReason) return
      if ((action === 'lock' || action === 'unlock') && !confirmed) {
        setPendingLockAction({ action, targets: availability.eligible, skipped: availability.skipped })
        return
      }
      ids = availability.eligible.map((server) => server.id)
      if (action === 'delete') {
        const target = workingSet.find((server) => server.id === ids[0])
        if (target) setDeleteTarget(target)
        return
      }
      const targets = ids.map((id) => {
        const server = workingSet.find((item) => item.id === id)
        return { serverId: id, serverName: server ? serverDisplayName(server) : id }
      })
      if (action === 'release') {
        setReleaseTargets(targets)
        return
      }
      const result = await bulk.run(action, targets)
      setLastActionResult(result)
      setResultDialogOpen(failedServerActionOutcomes(result).length > 0)
      clearSelection()
      reload()
    },
    [bulk, clearSelection, reload, workingSet],
  )
  const confirmRelease = useCallback(
    async (input: ReleaseServerInput) => {
      if (!releaseTargets?.length) return
      const releasedIds = releaseTargets.map((target) => target.serverId)
      // Stay on the Server list after accepting the release; the toast confirms the durable
      // Operation. Follow the released Servers so the list converges in place.
      await bulk.release(releaseTargets, input)
      clearSelection()
      setFollowedServerIds(releasedIds)
      reload()
    },
    [bulk, clearSelection, releaseTargets, reload],
  )
  const visible = useCallback((key: string) => !hiddenColumns.has(key), [hiddenColumns])
  const columnSpan = 3 + OPTIONAL_COLUMNS.filter((column) => visible(column.key)).length
  const staleProvisioners = provisioners.filter((item) => item.sync.lastError !== null)
  const lastActionFailures = lastActionResult ? failedServerActionOutcomes(lastActionResult) : []
  const actionTargets = workingSet.filter((server) => selected.has(server.id))
  const targetIntegrations = new Set(actionTargets.map((server) => server.source.integrationId))
  const deployDisabledReason =
    selected.size > 100
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
    ;[...selected].forEach((id) => target.searchParams.append('serverId', id))
    navigate(`${target.pathname}${target.search}`)
  }

  const sort = (key: ServerSortKey) => {
    if (key === sortKey) setSortDir((direction) => (direction === 'asc' ? 'desc' : 'asc'))
    else {
      setSortKey(key)
      setSortDir('asc')
    }
  }

  if (state.status === 'error') {
    return (
      <>
        <PageHeader title="Servers" />
        <ErrorState message={state.message} onRetry={reload} />
      </>
    )
  }
  return (
    <div className="operator-page">
      <PageHeader
        title="Servers"
        metadata={activeProjectionTargetKey ? <Badge colorPalette="blue" variant="subtle">Updating servers…</Badge> : undefined}
      />
      {staleProvisioners.map((item) => (
        <div key={item.id} className="sw-inline-warning">
          <strong>{item.name} sync failed</strong>
          <span>{item.sync.lastError}</span>
        </div>
      ))}
      {lastActionResult && lastActionFailures.length > 0 && (
        <Alert
          status={lastActionResult.succeeded > 0 ? 'warning' : 'error'}
          title={actionLabel(lastActionResult.action) + (lastActionResult.succeeded > 0 ? ' partially accepted' : ' failed')}
          actions={
            <HStack gap="1">
              <Button variant="plain" size="sm" onClick={() => setResultDialogOpen(true)}>
                View details
              </Button>
              <Button variant="plain" size="sm" onClick={() => setLastActionResult(null)}>
                Dismiss
              </Button>
            </HStack>
          }
        >
          {lastActionFailures.length === 1
            ? lastActionFailures[0].serverName + ': ' + lastActionFailures[0].message
            : lastActionResult.succeeded + ' accepted; ' + lastActionFailures.length + ' failed.'}
        </Alert>
      )}
      <DataToolbar variant="plain">
        <SearchInput
          value={searchInput}
          onChange={setSearchInput}
          placeholder="Search hostname, serial, address, or ID"
          aria-label="Search Servers"
        />
        <Checkbox
          id="include-absent"
          checked={includeAbsent}
          onCheckedChange={(checked) => {
            setIncludeAbsent(checked)
            setPage(1)
            setSelected(new Set())
          }}
        >
          Include absent
        </Checkbox>
        <Select
          value={groupBy}
          aria-label="Group Servers"
          size="sm"
          width="auto"
          onChange={(value) => {
            setGroupBy(value as ServerGroupBy)
            writePreference(GROUP_KEY, value)
            setCollapsedGroups(new Set())
          }}
          options={[
            { value: 'none', label: 'No grouping' },
            { value: 'provisioning', label: 'Provisioning' },
            { value: 'zone', label: 'Zone' },
            { value: 'pool', label: 'Pool' },
            { value: 'architecture', label: 'Architecture' },
            { value: 'power', label: 'Power' },
          ]}
        />
        <PopoverButton
          title="Filters"
          trigger={
            <Button variant="outline" size="sm">
              <Filter size={16} />
              Filters {!filtersAreEmpty(filters, '') && <Badge variant="subtle">Active</Badge>}
            </Button>
          }
        >
          <FilterPanel filters={filters} options={filterOptions} onChange={changeFilters} />
        </PopoverButton>
        <PopoverButton
          title="Visible columns"
          trigger={
            <IconButton variant="outline" size="sm" aria-label="Configure columns">
              <Columns3 size={16} />
            </IconButton>
          }
        >
          <ColumnPanel columns={OPTIONAL_COLUMNS} hidden={hiddenColumns} onToggle={toggleColumn} />
        </PopoverButton>
        <Select
          value={density}
          aria-label="Table density"
          size="sm"
          width="auto"
          onChange={(value) => setDensity(value as ServerDensity)}
          options={[
            { value: 'compact', label: 'Compact' },
            { value: 'comfortable', label: 'Comfortable' },
          ]}
        />
        <Select
          value={String(pageSize)}
          aria-label="Rows per page"
          size="sm"
          width="auto"
          onChange={(value) => {
            const size = Number(value)
            setPageSize(size)
            writePreference(PAGE_SIZE_KEY, size)
            setPage(1)
          }}
          options={[25, 50, 100].map((size) => ({ value: String(size), label: `${size} rows` }))}
        />
        <SelectionToolbar count={selected.size} onClear={clearSelection}>
          <Tooltip content={deployDisabledReason ?? 'Deploy one OS configuration to the selected Servers'}>
            <span>
              <Button colorPalette="brand" size="sm" disabled={Boolean(deployDisabledReason)} onClick={deploySelected}>
                <UploadCloud size={16} />
                Deploy OS
              </Button>
            </span>
          </Tooltip>
          <Tooltip content="Edit tags across the selected Servers">
            <Button variant="outline" size="sm" onClick={() => setTagEditorTargets(actionTargets)}>
              <Tags size={16} />
              Edit tags
            </Button>
          </Tooltip>
          <BulkActionMenu targets={actionTargets} running={bulk.running} onAction={(action) => void runAction(action, [...selected])} />
          {selected.size < filtered.length && (
            <Button variant="plain" size="sm" onClick={() => setMany(filtered.map((server) => server.id), true)}>
              Select all {filtered.length} matches
            </Button>
          )}
          {deployDisabledReason && <span className="sw-action-reason">{deployDisabledReason}</span>}
        </SelectionToolbar>
      </DataToolbar>
      {state.status === 'loading' && <LoadingState rows={8} />}
      {state.status === 'ready' && sorted.length === 0 && (
        <EmptyState
          title="No Servers"
          message="Nothing matches this working view."
          action={
            !filtersAreEmpty(filters, coarseKeyword)
              ? {
                  label: 'Clear filters',
                  onClick: () => {
                    changeFilters(EMPTY_SERVER_FILTERS)
                    setSearchInput('')
                  },
                }
              : undefined
          }
        />
      )}
      {state.status === 'ready' && sorted.length > 0 && (
        <>
          <ResponsiveDataView
            desktop={
              <StickyTableFrame>
            <Table.Root size={density === 'compact' ? 'sm' : 'md'} aria-label="Servers" className="sw-server-table">
              <Table.Header>
                <Table.Row>
                  <Table.ColumnHeader className="sw-sticky-selection sw-cell-center">
                    <Checkbox
                      id="select-page"
                      aria-label="Select all on this page"
                      checked={allPageSelected ? true : somePageSelected ? 'indeterminate' : false}
                      onCheckedChange={() => setMany(pageIds, !allPageSelected)}
                    />
                  </Table.ColumnHeader>
                  <Table.ColumnHeader className="sw-sticky-name">
                    <SortableHeader label="Machine" active={sortKey === 'name'} direction={sortDir} onClick={() => sort('name')} />
                  </Table.ColumnHeader>
                  {visible('power') && (
                    <Table.ColumnHeader className="sw-cell-center sw-power-cell">
                      <SortableHeader label="Power" active={sortKey === 'power'} direction={sortDir} onClick={() => sort('power')} />
                    </Table.ColumnHeader>
                  )}
                  {visible('status') && (
                    <Table.ColumnHeader>
                      <SortableHeader label="Deployment" active={sortKey === 'provisioning'} direction={sortDir} onClick={() => sort('provisioning')} />
                    </Table.ColumnHeader>
                  )}
                  {visible('address') && <Table.ColumnHeader>Address</Table.ColumnHeader>}
                  {visible('mac') && <Table.ColumnHeader>MAC address</Table.ColumnHeader>}
                  {visible('zone') && <Table.ColumnHeader>Zone</Table.ColumnHeader>}
                  {visible('pool') && <Table.ColumnHeader>Pool</Table.ColumnHeader>}
                  {visible('tags') && <Table.ColumnHeader className="sw-column-tags">Tags</Table.ColumnHeader>}
                  {visible('architecture') && <Table.ColumnHeader className="sw-hardware-column sw-column-architecture">Architecture</Table.ColumnHeader>}
                  {visible('cpuCores') && <Table.ColumnHeader className="sw-hardware-column sw-column-cpu-cores sw-cell-center">CPU cores</Table.ColumnHeader>}
                  {visible('cpuModel') && <Table.ColumnHeader className="sw-hardware-column sw-column-cpu-model">CPU model</Table.ColumnHeader>}
                  {visible('memory') && <Table.ColumnHeader className="sw-hardware-column sw-column-memory">Memory</Table.ColumnHeader>}
                  {visible('storage') && <Table.ColumnHeader className="sw-hardware-column sw-column-storage">Storage</Table.ColumnHeader>}
                  {visible('systemVendor') && <Table.ColumnHeader className="sw-hardware-column sw-column-system-vendor">System vendor</Table.ColumnHeader>}
                  {visible('systemProduct') && <Table.ColumnHeader className="sw-hardware-column sw-column-system-product">System product</Table.ColumnHeader>}
                  {visible('gpus') && <Table.ColumnHeader>GPUs</Table.ColumnHeader>}
                  {visible('platform') && <Table.ColumnHeader>Platform</Table.ColumnHeader>}
                  {visible('health') && <Table.ColumnHeader>Health</Table.ColumnHeader>}
                  <Table.ColumnHeader className="sw-sticky-actions" />
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {renderGroups(pageItems, groupBy).map((group) => (
                  <GroupRows
                    key={group.key || 'all'}
                    group={group}
                    grouped={groupBy !== 'none'}
                    columnSpan={columnSpan}
                    collapsed={collapsedGroups.has(group.key)}
                    onCollapse={() =>
                      setCollapsedGroups((current) => {
                        const next = new Set(current)
                        if (next.has(group.key)) next.delete(group.key)
                        else next.add(group.key)
                        return next
                      })
                    }
                    selected={selected}
                    onToggleOne={toggleOne}
                    onToggleGroup={setMany}
                    onNavigate={(id) => navigate(scopedHref(`/servers/${id}`))}
                    onAction={(action, id) => void runAction(action, [id])}
                    visible={visible}
                  />
                ))}
              </Table.Body>
            </Table.Root>
              </StickyTableFrame>
            }
            mobile={
              <Box className="sw-resource-card-list">
                {renderGroups(pageItems, groupBy).map((group) => (
                  <Box key={group.key || 'all'}>
                    {groupBy !== 'none' && (
                      <HStack mb="2" gap="2">
                        <Text fontWeight="semibold">{group.label}</Text>
                        <Badge variant="subtle">{group.items.length}</Badge>
                      </HStack>
                    )}
                    <Box className="sw-resource-card-list">
                      {group.items.map((server) => (
                        <ServerMobileCard
                          key={server.id}
                          server={server}
                          checked={selected.has(server.id)}
                          onToggle={() => toggleOne(server.id)}
                          onNavigate={() => navigate(scopedHref(`/servers/${server.id}`))}
                          onAction={(action) => void runAction(action, [server.id])}
                          visible={visible}
                        />
                      ))}
                    </Box>
                  </Box>
                ))}
              </Box>
            }
          />
          <div className="sw-pagination">
            <Pagination total={totalPages} value={safePage} onChange={setPage} />
          </div>
        </>
      )}
      {releaseTargets && <ServerReleaseDialog targets={releaseTargets} onClose={() => setReleaseTargets(null)} onRelease={confirmRelease} />}
      {tagEditorTargets && (
        <ServerTagEditor
          servers={tagEditorTargets}
          onClose={() => setTagEditorTargets(null)}
          onSaved={() => {
            // Reload so the tags column and Server Type-derived badges reflect the edit; the SSE
            // stream also patches the affected rows, but reloading converges even if it is offline.
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
    </div>
  )
}

/** Popover-backed toolbar button used for the Filters and Columns panels. */
function PopoverButton({ title, trigger, children }: { title: string; trigger: ReactNode; children: ReactNode }) {
  return (
    <Popover.Root positioning={{ placement: 'bottom-start' }}>
      <Popover.Trigger asChild>{trigger}</Popover.Trigger>
      <Portal>
        <Popover.Positioner>
          <Popover.Content>
            <Popover.Arrow />
            <Popover.Header fontWeight="semibold">{title}</Popover.Header>
            <Popover.Body>{children}</Popover.Body>
          </Popover.Content>
        </Popover.Positioner>
      </Portal>
    </Popover.Root>
  )
}

function SortableHeader({
  label,
  active,
  direction,
  onClick,
}: {
  label: string
  active: boolean
  direction: SortDirection
  onClick: () => void
}) {
  return (
    <Button variant="plain" size="sm" className="sw-sort-button" onClick={onClick} aria-label={`Sort by ${label}`}>
      {label}
      {active && (direction === 'asc' ? <ArrowUp size={14} /> : <ArrowDown size={14} />)}
    </Button>
  )
}

function updateValues(values: readonly string[], value: string, checked: boolean): string[] {
  return checked ? [...values, value] : values.filter((item) => item !== value)
}

function FilterOptions({
  title,
  values,
  selected,
  onChange,
}: {
  title: string
  values: FilterOption[]
  selected: readonly string[]
  onChange: (next: string[]) => void
}) {
  return (
    <fieldset className="sw-filter-group">
      <legend>{title}</legend>
      {values.map((option) => (
        <Checkbox
          key={option.value}
          id={`filter-${title}-${option.value}`}
          checked={selected.includes(option.value)}
          onCheckedChange={(checked) => onChange(updateValues(selected, option.value, checked))}
        >
          {`${option.label} (${option.count})`}
        </Checkbox>
      ))}
    </fieldset>
  )
}

function FilterPanel({
  filters,
  options,
  onChange,
}: {
  filters: ServerFilters
  options: ServerFilterOptions
  onChange: (next: ServerFilters) => void
}) {
  return (
    <div className="sw-filter-panel">
      <fieldset className="sw-filter-group">
        <legend>Protection</legend>
        <Select
          value={filters.lockState}
          aria-label="Filter Server lock"
          size="sm"
          onChange={(value) => onChange({ ...filters, lockState: value as ServerFilters['lockState'] })}
          options={[
            { value: 'any', label: 'Any' },
            { value: 'locked', label: 'Locked' },
            { value: 'unlocked', label: 'Unlocked' },
          ]}
        />
      </fieldset>
      <FilterOptions title="Provisioning" values={options.provisioningState} selected={filters.provisioningStates} onChange={(values) => onChange({ ...filters, provisioningStates: values })} />
      <FilterOptions title="Zone" values={options.zone} selected={filters.zones} onChange={(values) => onChange({ ...filters, zones: values })} />
      <FilterOptions title="Pool" values={options.pool} selected={filters.pools} onChange={(values) => onChange({ ...filters, pools: values })} />
      <FilterOptions title="Tags" values={options.tag} selected={filters.tags} onChange={(values) => onChange({ ...filters, tags: values })} />
      <fieldset className="sw-filter-group">
        <legend>GPU</legend>
        <Select
          value={filters.hasGpu === null ? 'any' : filters.hasGpu ? 'yes' : 'no'}
          aria-label="Filter GPU presence"
          size="sm"
          onChange={(value) => onChange({ ...filters, hasGpu: value === 'any' ? null : value === 'yes' })}
          options={[
            { value: 'any', label: 'Any' },
            { value: 'yes', label: 'Has GPU' },
            { value: 'no', label: 'No GPU' },
          ]}
        />
      </fieldset>
      <Button variant="plain" size="sm" onClick={() => onChange(EMPTY_SERVER_FILTERS)}>
        Clear filters
      </Button>
    </div>
  )
}

function ColumnPanel({
  columns,
  hidden,
  onToggle,
}: {
  columns: ColumnToggle[]
  hidden: ReadonlySet<string>
  onToggle: (key: string) => void
}) {
  return (
    <div className="sw-column-panel">
      {columns.map((column) => (
        <Checkbox key={column.key} id={`column-${column.key}`} checked={!hidden.has(column.key)} onCheckedChange={() => onToggle(column.key)}>
          {column.label}
        </Checkbox>
      ))}
    </div>
  )
}

function ActionMenu({
  label,
  icon,
  targets,
  running,
  includeSingleOnly = true,
  onAction,
}: {
  label: string
  icon?: ReactNode
  targets: readonly Server[]
  running?: boolean
  includeSingleOnly?: boolean
  onAction: (action: ServerMenuAction) => void
}) {
  const groups = SERVER_ACTION_GROUPS.map((group) => ({
    ...group,
    actions: group.actions.filter((entry) => includeSingleOnly || entry.bulk !== false),
  })).filter((group) => group.actions.length > 0)
  return (
    <Menu.Root>
      <Menu.Trigger asChild>
        {label ? (
          <Button variant="outline" size="sm" disabled={running}>
            {label}
            <ChevronDown size={16} />
          </Button>
        ) : (
          <IconButton variant="ghost" size="sm" aria-label="Actions" disabled={running}>
            {icon ?? <MoreVertical size={16} />}
          </IconButton>
        )}
      </Menu.Trigger>
      <Portal>
        <Menu.Positioner>
          <Menu.Content minW="12rem">
            {groups.map((group) => (
              <Menu.ItemGroup key={group.label}>
                <Menu.ItemGroupLabel>{group.label}</Menu.ItemGroupLabel>
                {group.actions.map((entry) => {
                  const availability = serverActionAvailability(entry.action, targets)
                  return (
                    <Menu.Item
                      key={entry.action}
                      value={entry.action}
                      color={entry.destructive ? 'red.fg' : undefined}
                      disabled={Boolean(availability.disabledReason)}
                      onClick={() => onAction(entry.action)}
                    >
                      <Box>
                        <Text>{entry.label}</Text>
                        {availability.disabledReason && (
                          <Text fontSize="xs" color="fg.muted">
                            {availability.disabledReason}
                          </Text>
                        )}
                      </Box>
                    </Menu.Item>
                  )
                })}
              </Menu.ItemGroup>
            ))}
          </Menu.Content>
        </Menu.Positioner>
      </Portal>
    </Menu.Root>
  )
}

function BulkActionMenu({
  targets,
  running,
  onAction,
}: {
  targets: readonly Server[]
  running: boolean
  onAction: (action: BulkAction) => void
}) {
  return (
    <ActionMenu
      label={running ? 'Working...' : 'Take action'}
      targets={targets}
      running={running}
      includeSingleOnly={false}
      onAction={(action) => {
        if (action !== 'delete') onAction(action)
      }}
    />
  )
}

function GroupRows({
  group,
  grouped,
  columnSpan,
  collapsed,
  onCollapse,
  selected,
  onToggleOne,
  onToggleGroup,
  onNavigate,
  onAction,
  visible,
}: {
  group: RenderGroup
  grouped: boolean
  columnSpan: number
  collapsed: boolean
  onCollapse: () => void
  selected: ReadonlySet<string>
  onToggleOne: (id: string) => void
  onToggleGroup: (ids: string[], checked: boolean) => void
  onNavigate: (id: string) => void
  onAction: (action: ServerMenuAction, id: string) => void
  visible: (key: string) => boolean
}) {
  const ids = group.items.map((item) => item.id)
  const all = ids.length > 0 && ids.every((id) => selected.has(id))
  const some = ids.some((id) => selected.has(id))
  return (
    <>
      {grouped && (
        <Table.Row className="sw-group-row">
          <Table.Cell colSpan={columnSpan}>
            <span>
              <IconButton variant="ghost" size="xs" aria-label={`${collapsed ? 'Expand' : 'Collapse'} ${group.label}`} onClick={onCollapse}>
                {collapsed ? <ChevronRight size={16} /> : <ChevronDown size={16} />}
              </IconButton>
              <Checkbox
                id={`group-${group.key}`}
                aria-label={`Select all in ${group.label}`}
                checked={all ? true : some ? 'indeterminate' : false}
                onCheckedChange={() => onToggleGroup(ids, !all)}
              />
              <strong>{group.label}</strong>
              <Badge variant="subtle">{group.items.length}</Badge>
            </span>
          </Table.Cell>
        </Table.Row>
      )}
      {!collapsed &&
        group.items.map((server) => (
          <ServerRow
            key={server.id}
            server={server}
            checked={selected.has(server.id)}
            onToggle={() => onToggleOne(server.id)}
            onNavigate={() => onNavigate(server.id)}
            onAction={(action) => onAction(action, server.id)}
            visible={visible}
          />
        ))}
    </>
  )
}

/** Mobile server row with the same state axes, selection, and actions as the desktop table. */
function ServerMobileCard({
  server,
  checked,
  onToggle,
  onNavigate,
  onAction,
  visible,
}: {
  server: Server
  checked: boolean
  onToggle: () => void
  onNavigate: () => void
  onAction: (action: ServerMenuAction) => void
  visible: (key: string) => boolean
}) {
  const [powerDialogOpen, setPowerDialogOpen] = useState(false)
  const name = serverDisplayName(server)
  return (
    <>
      <ResourceCard
        title={name}
        description={server.hardware.serialNumber || server.source.providerMachineId}
        status={
          <HStack gap="1" wrap="wrap" justify="flex-end">
            {server.provisioning?.locked && <Badge colorPalette="orange" variant="subtle"><Lock size={12} /> Locked</Badge>}
            {server.absent && <Badge colorPalette="gray" variant="subtle">Absent</Badge>}
          </HStack>
        }
        selected={checked}
        actions={
          <>
            <Checkbox id={'server-mobile-' + server.id} aria-label={'Mobile selection: ' + name} checked={checked} onCheckedChange={onToggle}>
              Select
            </Checkbox>
            <Button colorPalette="brand" variant="outline" size="sm" onClick={onNavigate}>Open server</Button>
            {visible('power') && server.provisioning && (
              <Button variant="outline" size="sm" onClick={() => setPowerDialogOpen(true)}>
                Power · {powerStateLabel(server.provisioning.powerState)}
              </Button>
            )}
            <ActionMenu label="Actions" targets={[server]} onAction={onAction} />
          </>
        }
        details={
          <>
            {visible('mac') && <ResourceCardField label="MAC address"><span className="mono">{textOrDash(serverMacAddress(server))}</span></ResourceCardField>}
            {visible('pool') && <ResourceCardField label="Pool">{textOrDash(server.providerResourcePool)}</ResourceCardField>}
            {visible('tags') && <ResourceCardField label="Tags">{server.tags.length ? server.tags.join(', ') : '-'}</ResourceCardField>}
            {visible('architecture') && <ResourceCardField label="Architecture">{textOrDash(server.architecture)}</ResourceCardField>}
            {visible('cpuCores') && <ResourceCardField label="CPU cores">{quantityOrDash(server.cpuCores)}</ResourceCardField>}
            {visible('cpuModel') && <ResourceCardField label="CPU model">{textOrDash(server.cpuModel)}</ResourceCardField>}
            {visible('memory') && <ResourceCardField label="Memory">{quantityOrDash(server.memoryMiB, 'GiB', 1024)}</ResourceCardField>}
            {visible('storage') && <ResourceCardField label="Storage">{quantityOrDash(server.storageGB, 'GB')}</ResourceCardField>}
            {visible('systemVendor') && <ResourceCardField label="System vendor">{textOrDash(server.systemVendor)}</ResourceCardField>}
            {visible('systemProduct') && <ResourceCardField label="System product">{textOrDash(server.systemProduct)}</ResourceCardField>}
            {visible('gpus') && <ResourceCardField label="GPUs"><GpuInventory server={server} /></ResourceCardField>}
          </>
        }
      >
        {visible('status') && <ResourceCardField label="Deployment"><DeploymentBadge axis={server.deployment} provider={server.provisioning} /></ResourceCardField>}
        {visible('health') && <ResourceCardField label="Health">{server.health ? <HealthBadge axis={server.health} /> : '-'}</ResourceCardField>}
        {visible('platform') && <ResourceCardField label="Platform">{server.membership ? <MembershipBadge axis={server.membership} /> : '-'}</ResourceCardField>}
        {visible('address') && <ResourceCardField label="Address"><span className="mono">{textOrDash(serverPrimaryAddress(server))}</span></ResourceCardField>}
        {visible('zone') && <ResourceCardField label="Zone">{textOrDash(server.providerZone)}</ResourceCardField>}
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
    </>
  )
}

function ServerRow({
  server,
  checked,
  onToggle,
  onNavigate,
  onAction,
  visible,
}: {
  server: Server
  checked: boolean
  onToggle: () => void
  onNavigate: () => void
  onAction: (action: ServerMenuAction) => void
  visible: (key: string) => boolean
}) {
  // The power cell doubles as a shortcut to power actions; the dialog lives on the row so each
  // row owns its own open state without lifting it into the (already large) page component.
  const [powerDialogOpen, setPowerDialogOpen] = useState(false)
  return (
    <>
      <Table.Row cursor="pointer" _hover={{ bg: 'bg.subtle' }} onClick={onNavigate}>
        <Table.Cell data-label="Selection" className="sw-sticky-selection sw-cell-center" onClick={(event) => event.stopPropagation()}>
          <Checkbox id={'server-' + server.id} aria-label={'Select ' + serverDisplayName(server)} checked={checked} onCheckedChange={onToggle} />
        </Table.Cell>
        <Table.Cell data-label="Machine" className="sw-sticky-name">
          <span className="sw-machine-name">
            {(server.provisioning?.locked ?? false) && (
              <Tooltip content="This Server is protected. Unlock it before making changes.">
                <span className="sw-lock-indicator" role="img" aria-label="Locked">
                  <Lock size={14} />
                </span>
              </Tooltip>
            )}
            <strong>{serverDisplayName(server)}</strong>
            <CopyButton value={serverDisplayName(server)} label="Copy hostname" />
            {server.absent && <Badge colorPalette="gray" variant="subtle">absent</Badge>}
          </span>
        </Table.Cell>
        {visible('power') && (
          <Table.Cell data-label="Power" className="sw-cell-center sw-power-cell" onClick={(event) => event.stopPropagation()}>
            {server.provisioning ? (
              <Tooltip content={`Power actions (${powerStateLabel(server.provisioning.powerState)})`}>
                <Button variant="plain" className="sw-power-button" aria-label={`Power actions for ${serverDisplayName(server)}`} onClick={() => setPowerDialogOpen(true)}>
                  <PowerBadge powerState={server.provisioning.powerState} decorative />
                </Button>
              </Tooltip>
            ) : (
              <PowerBadge powerState={null} />
            )}
          </Table.Cell>
        )}
        {visible('status') && (
          <Table.Cell data-label="Deployment">
            <DeploymentBadge axis={server.deployment} provider={server.provisioning} />
          </Table.Cell>
        )}
        {visible('address') && (
          <Table.Cell data-label="Address" className="mono">
            <span className="sw-copyable">
              {textOrDash(serverPrimaryAddress(server))}
              <CopyButton value={serverPrimaryAddress(server) ?? ''} label="Copy IP address" />
            </span>
          </Table.Cell>
        )}
        {visible('mac') && (
          <Table.Cell data-label="MAC address" className="mono">
            <span className="sw-copyable">
              {textOrDash(serverMacAddress(server))}
              <CopyButton value={serverMacAddress(server) ?? ''} label="Copy MAC address" />
            </span>
          </Table.Cell>
        )}
        {visible('zone') && <Table.Cell data-label="Zone">{textOrDash(server.providerZone)}</Table.Cell>}
        {visible('pool') && <Table.Cell data-label="Pool">{textOrDash(server.providerResourcePool)}</Table.Cell>}
        {visible('tags') && (
          <Table.Cell data-label="Tags" className="sw-column-tags">
            {server.tags.length ? (
              <span className="sw-tag-list">
                {server.tags.slice(0, 3).map((tag) => (
                  <Badge key={tag} colorPalette="blue" variant="subtle">
                    {tag}
                  </Badge>
                ))}
              </span>
            ) : (
              '-'
            )}
          </Table.Cell>
        )}
        {visible('architecture') && <Table.Cell data-label="Architecture" className="sw-hardware-column sw-column-architecture">{textOrDash(server.architecture)}</Table.Cell>}
        {visible('cpuCores') && <Table.Cell data-label="CPU cores" className="sw-hardware-column sw-column-cpu-cores sw-cell-center">{quantityOrDash(server.cpuCores)}</Table.Cell>}
        {visible('cpuModel') && <Table.Cell data-label="CPU model" className="sw-hardware-column sw-column-cpu-model">{textOrDash(server.cpuModel)}</Table.Cell>}
        {visible('memory') && <Table.Cell data-label="Memory" className="sw-hardware-column sw-column-memory">{quantityOrDash(server.memoryMiB, 'GiB', 1024)}</Table.Cell>}
        {visible('storage') && <Table.Cell data-label="Storage" className="sw-hardware-column sw-column-storage">{quantityOrDash(server.storageGB, 'GB')}</Table.Cell>}
        {visible('systemVendor') && <Table.Cell data-label="System vendor" className="sw-hardware-column sw-column-system-vendor">{textOrDash(server.systemVendor)}</Table.Cell>}
        {visible('systemProduct') && <Table.Cell data-label="System product" className="sw-hardware-column sw-column-system-product">{textOrDash(server.systemProduct)}</Table.Cell>}
        {visible('gpus') && (
          <Table.Cell data-label="GPUs">
            <GpuInventory server={server} />
          </Table.Cell>
        )}
        {visible('platform') && <Table.Cell data-label="Platform">{server.membership ? <MembershipBadge axis={server.membership} /> : '-'}</Table.Cell>}
        {visible('health') && <Table.Cell data-label="Health">{server.health ? <HealthBadge axis={server.health} /> : '-'}</Table.Cell>}
        <Table.Cell data-label="Actions" className="sw-sticky-actions" onClick={(event) => event.stopPropagation()}>
          <ActionMenu label="" icon={<MoreVertical size={16} />} targets={[server]} onAction={onAction} />
        </Table.Cell>
      </Table.Row>
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

/**
 * Presents physical GPU inventory by vendor without implying utilization or health.
 * Model details remain available through each accessible vendor-mark tooltip.
 */
function GpuInventory({ server }: { server: Server }) {
  if (server.gpus.length === 0) return <>-</>
  return (
    <span className="sw-gpu-inventory">
      {server.gpus.map((gpu, index) => (
        <span key={[gpu.vendor, gpu.model, index].join('-')}>
          <span className="sw-gpu-count">{gpu.count} x</span>
          <GpuVendorLogo vendor={gpu.vendor} model={gpu.model} />
        </span>
      ))}
    </span>
  )
}
