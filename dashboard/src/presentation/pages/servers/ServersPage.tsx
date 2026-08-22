import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Badge, Box, Callout, Card, Flex, Table, Text } from '@radix-ui/themes'
import { ExclamationTriangleIcon } from '@radix-ui/react-icons'
import { useApp } from '@/di/AppProvider'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { Pagination } from '@/presentation/components/radix/Pagination'
import { DoubleRow } from '@/presentation/components/table/DoubleRow'
import { SortableTh } from '@/presentation/components/table/SortableTh'
import { GroupHeaderRow } from '@/presentation/components/table/GroupHeaderRow'
import { RowActionMenu, type RowActionGroup } from '@/presentation/components/table/RowActionMenu'
import { TableSelectionCheckbox } from '@/presentation/components/table/TableSelectionCheckbox'
import {
  HealthBadge,
  MembershipBadge,
  ProvisioningBadge,
} from '@/presentation/components/AxisBadge'
import type { FilterOption } from '@/presentation/components/radix/CheckboxFilterList'
import type { Integration } from '@/domain/site/types'
import type { Server } from '@/domain/server/types'
// UI preferences persist through the same browser-storage helper the theme uses; these
// are per-device view settings, not platform state.
import { lsGet, lsSet } from '@/infrastructure/persistence/localStorage'
import {
  EMPTY_SERVER_FILTERS,
  compareServers,
  countByDimension,
  groupValueOf,
  matchesServerFilters,
  serverDisplayName,
  serverGpuCount,
  serverMacAddress,
  serverPrimaryAddress,
  type ServerDimension,
  type ServerFilters,
  type ServerGroupBy,
  type ServerSortKey,
  type SortDirection,
} from '@/domain/server/list'
import {
  ServerListControls,
  type ColumnToggle,
  type ServerFilterOptions,
} from './ServerListControls'
import { SERVER_ACTION_GROUPS, type BulkAction } from './serverActions'
import { useServerWorkingSet } from './useServerWorkingSet'
import { useServerBulkActions } from './useServerBulkActions'

/** Optional (toggleable) columns in display order; the name and actions columns are fixed. */
const OPTIONAL_COLUMNS: ColumnToggle[] = [
  { key: 'power', label: 'Power' },
  { key: 'status', label: 'Status' },
  { key: 'address', label: 'Address' },
  { key: 'tags', label: 'Tags' },
  { key: 'pool', label: 'Pool' },
  { key: 'zone', label: 'Zone' },
  { key: 'cores', label: 'Cores' },
  { key: 'memory', label: 'RAM' },
  { key: 'storage', label: 'Storage' },
  { key: 'gpus', label: 'GPUs' },
  { key: 'cluster', label: 'Cluster' },
  { key: 'health', label: 'Health' },
]

const DEFAULT_PAGE_SIZE = 50

/** Stable empty reference so the working-set memos do not see a new [] every render. */
const EMPTY_SERVERS: Server[] = []

/** localStorage keys for the durable view preferences (grouping, columns, page size). */
const GROUP_KEY = 'servers.groupBy'
const COLUMNS_KEY = 'servers.hiddenColumns'
const PAGE_SIZE_KEY = 'servers.pageSize'

/** Builds a sorted filter-option list with counts from a dimension tally. */
function toFilterOptions(counts: Map<string, number>): FilterOption[] {
  return [...counts.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([value, count]) => ({ value, label: value, count }))
}

/**
 * The servers list, modelled on MAAS's machines list but on Radix and the gdcm API.
 *
 * A working set is loaded once (coarse `keyword`/`includeAbsent` are server-side) and then
 * filtered, sorted, grouped, and paginated on the client, because the API does none of
 * those. Rows carry multi-select with a per-group and select-all affordance; the toolbar
 * becomes a bulk-action bar when anything is selected, fanning actions out over the
 * per-server API. Every column can be sorted (where meaningful) and hidden. The three
 * status axes (provisioning, cluster membership, health) are shown as separate badges,
 * never a combined status.
 */
export function ServersPage() {
  const { sites } = useApp()
  const navigate = useNavigate()
  const bulk = useServerBulkActions()

  // Search input is debounced into the coarse query so typing does not refetch per key.
  const [searchInput, setSearchInput] = useState('')
  const [coarseKeyword, setCoarseKeyword] = useState('')
  const [includeAbsent, setIncludeAbsent] = useState(false)

  const [filters, setFilters] = useState<ServerFilters>(EMPTY_SERVER_FILTERS)
  // Grouping, page size, and hidden columns are seeded from and written back to
  // localStorage, so a reload or remount keeps the operator's view. Filters, search,
  // sort, and selection are transient by design.
  const [groupBy, setGroupBy] = useState<ServerGroupBy>(() => lsGet<ServerGroupBy>(GROUP_KEY, 'none'))
  const [sortKey, setSortKey] = useState<ServerSortKey>('name')
  const [sortDir, setSortDir] = useState<SortDirection>('asc')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(() => lsGet<number>(PAGE_SIZE_KEY, DEFAULT_PAGE_SIZE))

  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [collapsedGroups, setCollapsedGroups] = useState<ReadonlySet<string>>(new Set())
  const [hiddenColumns, setHiddenColumns] = useState<ReadonlySet<string>>(
    () => new Set(lsGet<string[]>(COLUMNS_KEY, [])),
  )

  const [provisioners, setProvisioners] = useState<Integration[]>([])

  useEffect(() => {
    const id = setTimeout(() => {
      setCoarseKeyword(searchInput)
      setPage(1)
    }, 300)
    return () => clearTimeout(id)
  }, [searchInput])

  const query = useMemo(
    () => ({ keyword: coarseKeyword || undefined, includeAbsent }),
    [coarseKeyword, includeAbsent],
  )
  const { state, reload } = useServerWorkingSet(query)

  // Provisioner freshness, so an empty or stale list has a visible reason.
  useEffect(() => {
    let cancelled = false
    sites
      .listIntegrations({ kind: 'provisioner' })
      .then((result) => {
        if (!cancelled) setProvisioners(result)
      })
      .catch(() => {
        // Non-essential context; the list stands on its own.
      })
    return () => {
      cancelled = true
    }
  }, [sites])

  const workingSet = state.status === 'ready' ? state.data.servers : EMPTY_SERVERS

  // Derivations run over the whole working set; grouping/paging is applied last.
  const filtered = useMemo(
    () => workingSet.filter((server) => matchesServerFilters(server, filters)),
    [workingSet, filters],
  )
  const sorted = useMemo(
    () => [...filtered].sort((a, b) => compareServers(a, b, sortKey, sortDir)),
    [filtered, sortKey, sortDir],
  )

  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize))
  const safePage = Math.min(page, totalPages)
  const pageItems = useMemo(
    () => sorted.slice((safePage - 1) * pageSize, safePage * pageSize),
    [sorted, safePage, pageSize],
  )

  const filterOptions: ServerFilterOptions = useMemo(
    () => ({
      provisioningState: toFilterOptions(countByDimension(workingSet, 'provisioningState')),
      zone: toFilterOptions(countByDimension(workingSet, 'zone')),
      pool: toFilterOptions(countByDimension(workingSet, 'pool')),
      tag: toFilterOptions(countByDimension(workingSet, 'tag' as ServerDimension)),
    }),
    [workingSet],
  )

  // --- selection helpers ---

  const toggleOne = useCallback((id: string) => {
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const setMany = useCallback((ids: string[], selectedState: boolean) => {
    setSelected((current) => {
      const next = new Set(current)
      for (const id of ids) {
        if (selectedState) next.add(id)
        else next.delete(id)
      }
      return next
    })
  }, [])

  const clearSelection = useCallback(() => setSelected(new Set()), [])

  const pageIds = pageItems.map((server) => server.id)
  const allPageSelected = pageIds.length > 0 && pageIds.every((id) => selected.has(id))
  const somePageSelected = pageIds.some((id) => selected.has(id))
  const headerChecked: boolean | 'indeterminate' = allPageSelected
    ? true
    : somePageSelected
      ? 'indeterminate'
      : false

  const handleSort = useCallback(
    (key: string) => {
      const typed = key as ServerSortKey
      if (typed === sortKey) {
        setSortDir((dir) => (dir === 'asc' ? 'desc' : 'asc'))
      } else {
        setSortKey(typed)
        setSortDir('asc')
      }
    },
    [sortKey],
  )

  const changeFilters = useCallback((next: ServerFilters) => {
    setFilters(next)
    setPage(1)
    setSelected(new Set())
  }, [])

  const toggleColumn = useCallback((key: string) => {
    setHiddenColumns((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      lsSet(COLUMNS_KEY, [...next])
      return next
    })
  }, [])

  const toggleGroupCollapse = useCallback((key: string) => {
    setCollapsedGroups((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }, [])

  const runAction = useCallback(
    async (action: BulkAction, ids: string[]) => {
      if (ids.length === 0) return
      await bulk.run(action, ids)
      clearSelection()
      reload()
    },
    [bulk, clearSelection, reload],
  )

  const visible = useCallback((key: string) => !hiddenColumns.has(key), [hiddenColumns])
  // One leading name cell, one trailing actions cell, plus every shown optional column.
  const columnSpan = 2 + OPTIONAL_COLUMNS.filter((column) => visible(column.key)).length

  const staleProvisioners = provisioners.filter((integration) => integration.sync.lastError !== null)

  if (state.status === 'error') {
    return (
      <>
        <PageHeader title="Servers" subtitle="Projected from each site's provisioner." />
        <ErrorState message={state.message} onRetry={reload} />
      </>
    )
  }

  return (
    <>
      <PageHeader
        title="Servers"
        subtitle="Projected from each site's provisioner. Servers are not created here."
      />

      {provisioners.length === 0 && state.status === 'ready' && (
        <Callout.Root color="amber" mb="4">
          <Callout.Icon>
            <ExclamationTriangleIcon />
          </Callout.Icon>
          <Callout.Text>
            No provisioner registered. Servers appear once a provisioner integration is
            registered and reconciled. Register one through the API; the dashboard does not
            handle credentials.
          </Callout.Text>
        </Callout.Root>
      )}

      {staleProvisioners.map((integration) => (
        <Callout.Root key={integration.id} color="orange" mb="4">
          <Callout.Icon>
            <ExclamationTriangleIcon />
          </Callout.Icon>
          <Callout.Text>
            {integration.name} last sync failed: {integration.sync.lastError}
          </Callout.Text>
        </Callout.Root>
      ))}

      <Card mb="4">
        <ServerListControls
          keyword={searchInput}
          onKeyword={setSearchInput}
          includeAbsent={includeAbsent}
          onIncludeAbsent={(value) => {
            setIncludeAbsent(value)
            setPage(1)
            setSelected(new Set())
          }}
          groupBy={groupBy}
          onGroupBy={(value) => {
            setGroupBy(value)
            lsSet(GROUP_KEY, value)
            setCollapsedGroups(new Set())
          }}
          filters={filters}
          onFilters={changeFilters}
          filterOptions={filterOptions}
          columns={OPTIONAL_COLUMNS}
          hiddenColumns={hiddenColumns}
          onToggleColumn={toggleColumn}
          pageSize={pageSize}
          onPageSize={(size) => {
            setPageSize(size)
            lsSet(PAGE_SIZE_KEY, size)
            setPage(1)
          }}
          selectedCount={selected.size}
          onClearSelection={clearSelection}
          onBulkAction={(action) => void runAction(action, [...selected])}
          bulkRunning={bulk.running}
        />
      </Card>

      {state.status === 'loading' && <LoadingState />}

      {state.status === 'ready' && sorted.length === 0 && (
        <EmptyState
          title="No servers"
          message="Nothing matches these filters."
          action={
            !filtersAreEmpty(filters, coarseKeyword)
              ? { label: 'Clear filters', onClick: () => changeFilters(EMPTY_SERVER_FILTERS) }
              : undefined
          }
        />
      )}

      {state.status === 'ready' && sorted.length > 0 && (
        <Card>
          {selected.size > 0 && selected.size < filtered.length && (
            <Box mb="2">
              <Text
                size="1"
                color="blue"
                style={{ cursor: 'pointer' }}
                role="button"
                tabIndex={0}
                onClick={() => setMany(filtered.map((server) => server.id), true)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') setMany(filtered.map((server) => server.id), true)
                }}
              >
                Select all {filtered.length} matching servers
              </Text>
            </Box>
          )}

          <Table.Root variant="surface">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeaderCell>
                  <Flex align="center" gap="2">
                    <TableSelectionCheckbox
                      checked={headerChecked}
                      onToggle={() => setMany(pageIds, !allPageSelected)}
                      ariaLabel="Select all on this page"
                    />
                    <SortableThInline
                      label="FQDN"
                      secondary="MAC"
                      sortKey="name"
                      activeKey={sortKey}
                      direction={sortDir}
                      onSort={handleSort}
                    />
                  </Flex>
                </Table.ColumnHeaderCell>
                {visible('power') && (
                  <SortableTh label="Power" sortKey="power" activeKey={sortKey} direction={sortDir} onSort={handleSort} />
                )}
                {visible('status') && (
                  <SortableTh label="Status" sortKey="provisioning" activeKey={sortKey} direction={sortDir} onSort={handleSort} />
                )}
                {visible('address') && <Table.ColumnHeaderCell>Address</Table.ColumnHeaderCell>}
                {visible('tags') && <Table.ColumnHeaderCell>Tags</Table.ColumnHeaderCell>}
                {visible('pool') && (
                  <SortableTh label="Pool" sortKey="pool" activeKey={sortKey} direction={sortDir} onSort={handleSort} />
                )}
                {visible('zone') && (
                  <SortableTh label="Zone" sortKey="zone" activeKey={sortKey} direction={sortDir} onSort={handleSort} />
                )}
                {visible('cores') && (
                  <SortableTh label="Cores" secondary="Arch" sortKey="cores" activeKey={sortKey} direction={sortDir} onSort={handleSort} align="end" />
                )}
                {visible('memory') && (
                  <SortableTh label="RAM" sortKey="memory" activeKey={sortKey} direction={sortDir} onSort={handleSort} align="end" />
                )}
                {visible('storage') && (
                  <SortableTh label="Storage" sortKey="storage" activeKey={sortKey} direction={sortDir} onSort={handleSort} align="end" />
                )}
                {visible('gpus') && <Table.ColumnHeaderCell>GPUs</Table.ColumnHeaderCell>}
                {visible('cluster') && <Table.ColumnHeaderCell>Cluster</Table.ColumnHeaderCell>}
                {visible('health') && <Table.ColumnHeaderCell>Health</Table.ColumnHeaderCell>}
                <Table.ColumnHeaderCell />
              </Table.Row>
            </Table.Header>

            <Table.Body>
              {renderGroups(pageItems, groupBy).map((group) => (
                <GroupSection
                  key={group.key || 'all'}
                  group={group}
                  grouped={groupBy !== 'none'}
                  columnSpan={columnSpan}
                  collapsed={collapsedGroups.has(group.key)}
                  onToggleCollapse={() => toggleGroupCollapse(group.key)}
                  selected={selected}
                  onToggleOne={toggleOne}
                  onToggleGroup={(ids, next) => setMany(ids, next)}
                  onNavigate={(id) => navigate(`/servers/${id}`)}
                  onRowAction={(action, id) => void runAction(action, [id])}
                  visible={visible}
                />
              ))}
            </Table.Body>
          </Table.Root>
        </Card>
      )}

      {state.status === 'ready' && totalPages > 1 && (
        <Flex justify="center" mt="4">
          <Pagination total={totalPages} value={safePage} onChange={setPage} />
        </Flex>
      )}
    </>
  )
}

/** True when neither the search box nor any filter dimension is constraining the list. */
function filtersAreEmpty(filters: ServerFilters, keyword: string): boolean {
  return (
    keyword === '' &&
    filters.provisioningStates.length === 0 &&
    filters.zones.length === 0 &&
    filters.pools.length === 0 &&
    filters.tags.length === 0 &&
    filters.hasGpu === null
  )
}

/** A rendered group: a stable key/label and its member servers. `none` yields one group. */
interface RenderGroup {
  key: string
  label: string
  items: Server[]
}

/** Splits the page's servers into display groups, preserving sorted order within each. */
function renderGroups(items: Server[], groupBy: ServerGroupBy): RenderGroup[] {
  if (groupBy === 'none') {
    return [{ key: '', label: '', items }]
  }
  const order: string[] = []
  const byKey = new Map<string, Server[]>()
  for (const server of items) {
    const key = groupValueOf(server, groupBy)
    if (!byKey.has(key)) {
      byKey.set(key, [])
      order.push(key)
    }
    byKey.get(key)!.push(server)
  }
  return order.map((key) => ({ key, label: key, items: byKey.get(key)! }))
}

interface GroupSectionProps {
  group: RenderGroup
  grouped: boolean
  columnSpan: number
  collapsed: boolean
  onToggleCollapse: () => void
  selected: ReadonlySet<string>
  onToggleOne: (id: string) => void
  onToggleGroup: (ids: string[], next: boolean) => void
  onNavigate: (id: string) => void
  onRowAction: (action: BulkAction, id: string) => void
  visible: (key: string) => boolean
}

/**
 * One group's header (when grouped) plus its rows.
 *
 * Kept as a component so the group's select-all checkbox state (all/some/none of its
 * members) is computed close to where it renders. When ungrouped, only the rows render.
 */
function GroupSection({
  group,
  grouped,
  columnSpan,
  collapsed,
  onToggleCollapse,
  selected,
  onToggleOne,
  onToggleGroup,
  onNavigate,
  onRowAction,
  visible,
}: GroupSectionProps) {
  const ids = group.items.map((server) => server.id)
  const allSelected = ids.length > 0 && ids.every((id) => selected.has(id))
  const someSelected = ids.some((id) => selected.has(id))
  const groupChecked: boolean | 'indeterminate' = allSelected
    ? true
    : someSelected
      ? 'indeterminate'
      : false

  return (
    <>
      {grouped && (
        <GroupHeaderRow
          colSpan={columnSpan}
          label={group.label}
          count={group.items.length}
          collapsed={collapsed}
          onToggleCollapse={onToggleCollapse}
          checkbox={
            <TableSelectionCheckbox
              checked={groupChecked}
              onToggle={() => onToggleGroup(ids, !allSelected)}
              ariaLabel={`Select all in ${group.label}`}
            />
          }
        />
      )}

      {!collapsed &&
        group.items.map((server) => (
          <ServerRow
            key={server.id}
            server={server}
            selected={selected.has(server.id)}
            onToggle={() => onToggleOne(server.id)}
            onNavigate={() => onNavigate(server.id)}
            onRowAction={(action) => onRowAction(action, server.id)}
            visible={visible}
          />
        ))}
    </>
  )
}

interface ServerRowProps {
  server: Server
  selected: boolean
  onToggle: () => void
  onNavigate: () => void
  onRowAction: (action: BulkAction) => void
  visible: (key: string) => boolean
}

/**
 * A single server row.
 *
 * Clicking the row opens the server detail; the checkbox and action-menu cells stop that
 * propagation so selecting or acting does not also navigate. Numeric columns are
 * right-aligned to match MAAS. GPUs are shown as "count x model" because a GPU fleet is
 * the point of this platform.
 */
function ServerRow({ server, selected, onToggle, onNavigate, onRowAction, visible }: ServerRowProps) {
  const address = serverPrimaryAddress(server)
  const mac = serverMacAddress(server)
  const gpuCount = serverGpuCount(server)
  const gpuLabel =
    server.gpus.length === 0
      ? '—'
      : server.gpus.map((gpu) => `${gpu.count}× ${gpu.model || gpu.vendor}`).join(', ')

  const rowActionGroups: RowActionGroup[] = SERVER_ACTION_GROUPS.map((group) => ({
    label: group.label,
    actions: group.actions.map((entry) => ({
      label: entry.label,
      color: entry.destructive ? ('red' as const) : undefined,
      onSelect: () => onRowAction(entry.action),
    })),
  }))

  return (
    <Table.Row style={{ cursor: 'pointer' }} onClick={onNavigate}>
      <Table.Cell>
        <Flex align="center" gap="2">
          <Box onClick={(event) => event.stopPropagation()}>
            <TableSelectionCheckbox
              checked={selected}
              onToggle={onToggle}
              ariaLabel={`Select ${serverDisplayName(server)}`}
            />
          </Box>
          <DoubleRow
            primary={
              <Flex align="center" gap="2">
                {serverDisplayName(server)}
                {server.absent && (
                  <Badge color="gray" variant="outline">
                    absent
                  </Badge>
                )}
              </Flex>
            }
            secondary={mac ?? undefined}
          />
        </Flex>
      </Table.Cell>

      {visible('power') && (
        <Table.Cell>
          <Text size="2" color={server.provisioning ? undefined : 'gray'}>
            {server.provisioning?.powerState ?? 'unknown'}
          </Text>
        </Table.Cell>
      )}
      {visible('status') && (
        <Table.Cell>
          <ProvisioningBadge axis={server.provisioning} />
        </Table.Cell>
      )}
      {visible('address') && (
        <Table.Cell>
          <Text size="2" color={address ? undefined : 'gray'}>
            {address ?? 'not assigned'}
          </Text>
        </Table.Cell>
      )}
      {visible('tags') && (
        <Table.Cell>
          <Text size="2" color={server.tags.length ? undefined : 'gray'}>
            {server.tags.length ? server.tags.join(', ') : '—'}
          </Text>
        </Table.Cell>
      )}
      {visible('pool') && (
        <Table.Cell>
          <Text size="2">{server.providerResourcePool || '—'}</Text>
        </Table.Cell>
      )}
      {visible('zone') && (
        <Table.Cell>
          <Text size="2">{server.providerZone || '—'}</Text>
        </Table.Cell>
      )}
      {visible('cores') && (
        <Table.Cell align="right">
          <DoubleRow primary={server.cpuCores || '—'} secondary={server.architecture} align="end" />
        </Table.Cell>
      )}
      {visible('memory') && (
        <Table.Cell align="right">
          <Text size="2">{server.memoryMiB ? `${Math.round(server.memoryMiB / 1024)} GiB` : '—'}</Text>
        </Table.Cell>
      )}
      {visible('storage') && (
        <Table.Cell align="right">
          <Text size="2">{server.storageGB ? `${Math.round(server.storageGB)} GB` : '—'}</Text>
        </Table.Cell>
      )}
      {visible('gpus') && (
        <Table.Cell>
          <Text size="2" color={gpuCount ? undefined : 'gray'}>
            {gpuLabel}
          </Text>
        </Table.Cell>
      )}
      {visible('cluster') && (
        <Table.Cell>
          <MembershipBadge axis={server.membership} />
        </Table.Cell>
      )}
      {visible('health') && (
        <Table.Cell>
          <HealthBadge axis={server.health} />
        </Table.Cell>
      )}

      <Table.Cell>
        <Box onClick={(event) => event.stopPropagation()}>
          <RowActionMenu groups={rowActionGroups} ariaLabel={`Actions for ${serverDisplayName(server)}`} />
        </Box>
      </Table.Cell>
    </Table.Row>
  )
}

/**
 * The FQDN/MAC sortable header, rendered inside the name column's cell (which also holds
 * the select-all checkbox), so it cannot be the standalone `SortableTh` that emits its own
 * header cell.
 */
function SortableThInline({
  label,
  secondary,
  sortKey,
  activeKey,
  direction,
  onSort,
}: {
  label: string
  secondary: string
  sortKey: string
  activeKey: string
  direction: SortDirection
  onSort: (key: string) => void
}) {
  const isActive = activeKey === sortKey
  return (
    <button
      type="button"
      onClick={() => onSort(sortKey)}
      aria-label={`Sort by ${label}`}
      style={{ background: 'transparent', border: 'none', padding: 0, cursor: 'pointer', color: 'inherit', font: 'inherit' }}
    >
      <DoubleRow
        primary={
          <Text size="2" weight="medium">
            {label} {isActive ? (direction === 'asc' ? '↑' : '↓') : ''}
          </Text>
        }
        secondary={secondary}
      />
    </button>
  )
}
