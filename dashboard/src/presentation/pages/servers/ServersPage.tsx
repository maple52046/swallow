import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  Badge,
  Button,
  Checkbox,
  Dropdown,
  DropdownItem,
  DropdownList,
  FormSelect,
  FormSelectOption,
  Label,
  MenuToggle,
  Popover,
  SearchInput,
  ToolbarGroup,
  ToolbarItem,
} from '@patternfly/react-core'
import {
  AngleDownIcon,
  AngleRightIcon,
  ColumnsIcon,
  EllipsisVIcon,
  FilterIcon,
  SortAmountDownIcon,
  SortAmountUpIcon,
} from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Integration } from '@/domain/site/types'
import type { Server } from '@/domain/server/types'
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
import { PageHeader } from '@/presentation/components/PageHeader'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { LoadingState } from '@/presentation/components/LoadingState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { Pagination } from '@/presentation/components/Pagination'
import { HealthBadge, MembershipBadge, ProvisioningBadge } from '@/presentation/components/AxisBadge'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { SERVER_ACTION_GROUPS, type BulkAction } from './serverActions'
import { useServerWorkingSet } from './useServerWorkingSet'
import { useServerBulkActions } from './useServerBulkActions'
import { ServerSavedViews, type SavedServerViewState, type ServerDensity } from './ServerSavedViews'

interface ColumnToggle { key: string; label: string }
interface FilterOption { value: string; label: string; count: number }
interface ServerFilterOptions { provisioningState: FilterOption[]; zone: FilterOption[]; pool: FilterOption[]; tag: FilterOption[] }

const OPTIONAL_COLUMNS: ColumnToggle[] = [
  { key: 'power', label: 'Power' }, { key: 'status', label: 'Provisioning' },
  { key: 'address', label: 'Address' }, { key: 'placement', label: 'Zone / pool' },
  { key: 'tags', label: 'Tags' }, { key: 'hardware', label: 'Hardware' },
  { key: 'gpus', label: 'GPUs' }, { key: 'cluster', label: 'Cluster' },
  { key: 'health', label: 'Health' },
]
const DEFAULT_PAGE_SIZE = 50
const EMPTY_SERVERS: Server[] = []
const GROUP_KEY = 'swallow.servers.group-by'
const COLUMNS_KEY = 'swallow.servers.hidden-columns'
const PAGE_SIZE_KEY = 'swallow.servers.page-size'

function readPreference<T>(key: string, fallback: T): T {
  try { const raw = localStorage.getItem(key); return raw === null ? fallback : JSON.parse(raw) as T } catch { return fallback }
}
function writePreference<T>(key: string, value: T): void {
  try { localStorage.setItem(key, JSON.stringify(value)) } catch {
    // Browser policy can block persistence; the active view remains fully usable.
  }
}
function toFilterOptions(counts: Map<string, number>): FilterOption[] {
  return [...counts.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([value, count]) => ({ value, label: value, count }))
}
function filtersAreEmpty(filters: ServerFilters, keyword: string): boolean {
  return keyword === '' && filters.provisioningStates.length === 0 && filters.zones.length === 0 && filters.pools.length === 0 && filters.tags.length === 0 && filters.hasGpu === null
}

interface RenderGroup { key: string; label: string; items: Server[] }
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
  const { sites } = useApp()
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
  const [hiddenColumns, setHiddenColumns] = useState<ReadonlySet<string>>(() => new Set(readPreference<string[]>(COLUMNS_KEY, [])))
  const [provisioners, setProvisioners] = useState<Integration[]>([])

  const savedViewState = useMemo<SavedServerViewState>(() => ({ filters, keyword: searchInput, includeAbsent, groupBy, sortKey, sortDirection: sortDir, hiddenColumns: [...hiddenColumns], density, pageSize }), [density, filters, groupBy, hiddenColumns, includeAbsent, pageSize, searchInput, sortDir, sortKey])
  const applySavedView = useCallback((view: SavedServerViewState) => {
    setFilters(view.filters); setSearchInput(view.keyword); setCoarseKeyword(view.keyword); setIncludeAbsent(view.includeAbsent); setGroupBy(view.groupBy); setSortKey(view.sortKey); setSortDir(view.sortDirection); setHiddenColumns(new Set(view.hiddenColumns)); setDensity(view.density); setPageSize(view.pageSize); setSelected(new Set()); setCollapsedGroups(new Set()); setPage(1)
  }, [])

  useEffect(() => { const id = setTimeout(() => { setCoarseKeyword(searchInput); setPage(1) }, 300); return () => clearTimeout(id) }, [searchInput])
  const query = useMemo(() => ({ siteId, keyword: coarseKeyword || undefined, includeAbsent }), [coarseKeyword, includeAbsent, siteId])
  const { state, reload } = useServerWorkingSet(query)
  useEffect(() => {
    let cancelled = false
    sites.listIntegrations({ siteId, kind: 'provisioner' }).then((items) => { if (!cancelled) setProvisioners(items) }).catch(() => undefined)
    return () => { cancelled = true }
  }, [siteId, sites])

  const workingSet = state.status === 'ready' ? state.data.servers : EMPTY_SERVERS
  const filtered = useMemo(() => workingSet.filter((server) => matchesServerFilters(server, filters)), [filters, workingSet])
  const sorted = useMemo(() => [...filtered].sort((a, b) => compareServers(a, b, sortKey, sortDir)), [filtered, sortDir, sortKey])
  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize))
  const safePage = Math.min(page, totalPages)
  const pageItems = useMemo(() => sorted.slice((safePage - 1) * pageSize, safePage * pageSize), [pageSize, safePage, sorted])
  const filterOptions = useMemo<ServerFilterOptions>(() => ({ provisioningState: toFilterOptions(countByDimension(workingSet, 'provisioningState')), zone: toFilterOptions(countByDimension(workingSet, 'zone')), pool: toFilterOptions(countByDimension(workingSet, 'pool')), tag: toFilterOptions(countByDimension(workingSet, 'tag' as ServerDimension)) }), [workingSet])

  const toggleOne = useCallback((id: string) => setSelected((current) => { const next = new Set(current); if (next.has(id)) next.delete(id); else next.add(id); return next }), [])
  const setMany = useCallback((ids: string[], checked: boolean) => setSelected((current) => { const next = new Set(current); ids.forEach((id) => { if (checked) next.add(id); else next.delete(id) }); return next }), [])
  const clearSelection = useCallback(() => setSelected(new Set()), [])
  const pageIds = pageItems.map((server) => server.id)
  const allPageSelected = pageIds.length > 0 && pageIds.every((id) => selected.has(id))
  const somePageSelected = pageIds.some((id) => selected.has(id))
  const changeFilters = useCallback((next: ServerFilters) => { setFilters(next); setPage(1); setSelected(new Set()) }, [])
  const toggleColumn = useCallback((key: string) => setHiddenColumns((current) => { const next = new Set(current); if (next.has(key)) next.delete(key); else next.add(key); writePreference(COLUMNS_KEY, [...next]); return next }), [])
  const runAction = useCallback(async (action: BulkAction, ids: string[]) => { if (!ids.length) return; await bulk.run(action, ids); clearSelection(); reload() }, [bulk, clearSelection, reload])
  const visible = useCallback((key: string) => !hiddenColumns.has(key), [hiddenColumns])
  const columnSpan = 2 + OPTIONAL_COLUMNS.filter((column) => visible(column.key)).length
  const staleProvisioners = provisioners.filter((item) => item.sync.lastError !== null)

  const sort = (key: ServerSortKey) => {
    if (key === sortKey) setSortDir((direction) => direction === 'asc' ? 'desc' : 'asc')
    else { setSortKey(key); setSortDir('asc') }
  }

  if (state.status === 'error') return <><PageHeader title="Servers" subtitle="Projected from each Site's provisioner." /><ErrorState message={state.message} onRetry={reload} /></>
  return <div className="operator-page">
    <PageHeader title="Servers" subtitle="Fleet inventory projected from Site provisioners; machines are not created here." actions={<ServerSavedViews current={savedViewState} onApply={applySavedView} />} />
    {staleProvisioners.map((item) => <div key={item.id} className="sw-inline-warning"><strong>{item.name} sync failed</strong><span>{item.sync.lastError}</span></div>)}
    <DataToolbar>
      <ToolbarItem><SearchInput value={searchInput} onChange={(_event, value) => setSearchInput(value)} onClear={() => setSearchInput('')} placeholder="Search hostname, serial, address, or ID" aria-label="Search Servers" /></ToolbarItem>
      <ToolbarItem><Checkbox id="include-absent" label="Include absent" isChecked={includeAbsent} onChange={(_event, checked) => { setIncludeAbsent(checked); setPage(1); setSelected(new Set()) }} /></ToolbarItem>
      <ToolbarItem><FormSelect value={groupBy} onChange={(_event, value) => { setGroupBy(value as ServerGroupBy); writePreference(GROUP_KEY, value); setCollapsedGroups(new Set()) }} aria-label="Group Servers"><FormSelectOption value="none" label="No grouping" /><FormSelectOption value="provisioning" label="Provisioning" /><FormSelectOption value="zone" label="Zone" /><FormSelectOption value="pool" label="Pool" /><FormSelectOption value="architecture" label="Architecture" /><FormSelectOption value="power" label="Power" /></FormSelect></ToolbarItem>
      <ToolbarItem><Popover headerContent="Filters" bodyContent={<FilterPanel filters={filters} options={filterOptions} onChange={changeFilters} />}><Button variant="secondary" icon={<FilterIcon />}>Filters {!filtersAreEmpty(filters, '') && <Badge isRead>Active</Badge>}</Button></Popover></ToolbarItem>
      <ToolbarItem><Popover headerContent="Visible columns" bodyContent={<ColumnPanel columns={OPTIONAL_COLUMNS} hidden={hiddenColumns} onToggle={toggleColumn} />}><Button variant="secondary" icon={<ColumnsIcon />} aria-label="Configure columns" /></Popover></ToolbarItem>
      <ToolbarItem><FormSelect value={density} onChange={(_event, value) => setDensity(value as ServerDensity)} aria-label="Table density"><FormSelectOption value="compact" label="Compact" /><FormSelectOption value="comfortable" label="Comfortable" /></FormSelect></ToolbarItem>
      <ToolbarItem><FormSelect value={String(pageSize)} onChange={(_event, value) => { const size = Number(value); setPageSize(size); writePreference(PAGE_SIZE_KEY, size); setPage(1) }} aria-label="Rows per page">{[25, 50, 100].map((size) => <FormSelectOption key={size} value={String(size)} label={`${size} rows`} />)}</FormSelect></ToolbarItem>
      {selected.size > 0 && <ToolbarGroup variant="action-group"><ToolbarItem><strong>{selected.size} selected</strong></ToolbarItem><ToolbarItem><BulkActionMenu running={bulk.running} onAction={(action) => void runAction(action, [...selected])} /></ToolbarItem><ToolbarItem><Button variant="link" onClick={clearSelection}>Clear</Button></ToolbarItem></ToolbarGroup>}
    </DataToolbar>
    {state.status === 'loading' && <LoadingState rows={8} />}
    {state.status === 'ready' && sorted.length === 0 && <EmptyState title="No Servers" message="Nothing matches this working view." action={!filtersAreEmpty(filters, coarseKeyword) ? { label: 'Clear filters', onClick: () => { changeFilters(EMPTY_SERVER_FILTERS); setSearchInput('') } } : undefined} />}
    {state.status === 'ready' && sorted.length > 0 && <>
      {selected.size > 0 && selected.size < filtered.length && <Button variant="link" onClick={() => setMany(filtered.map((server) => server.id), true)}>Select all {filtered.length} matching Servers</Button>}
      <StickyTableFrame><Table aria-label="Servers" variant={density === 'compact' ? 'compact' : undefined} isStriped className="sw-server-table" gridBreakPoint=""><Thead><Tr>
        <Th className="sw-sticky-name"><span className="sw-select-name"><Checkbox id="select-page" aria-label="Select all on this page" isChecked={allPageSelected ? true : somePageSelected ? null : false} onChange={() => setMany(pageIds, !allPageSelected)} /><SortableHeader label="Machine" active={sortKey === 'name'} direction={sortDir} onClick={() => sort('name')} /></span></Th>
        {visible('power') && <Th><SortableHeader label="Power" active={sortKey === 'power'} direction={sortDir} onClick={() => sort('power')} /></Th>}
        {visible('status') && <Th><SortableHeader label="Provisioning" active={sortKey === 'provisioning'} direction={sortDir} onClick={() => sort('provisioning')} /></Th>}
        {visible('address') && <Th>Address</Th>}{visible('placement') && <Th>Zone / pool</Th>}{visible('tags') && <Th>Tags</Th>}{visible('hardware') && <Th>Hardware</Th>}{visible('gpus') && <Th>GPUs</Th>}{visible('cluster') && <Th>Cluster</Th>}{visible('health') && <Th>Health</Th>}<Th className="sw-sticky-actions" screenReaderText="Actions" />
      </Tr></Thead><Tbody>{renderGroups(pageItems, groupBy).map((group) => <GroupRows key={group.key || 'all'} group={group} grouped={groupBy !== 'none'} columnSpan={columnSpan} collapsed={collapsedGroups.has(group.key)} onCollapse={() => setCollapsedGroups((current) => { const next = new Set(current); if (next.has(group.key)) next.delete(group.key); else next.add(group.key); return next })} selected={selected} onToggleOne={toggleOne} onToggleGroup={setMany} onNavigate={(id) => navigate(scopedHref(`/servers/${id}`))} onAction={(action, id) => void runAction(action, [id])} visible={visible} />)}</Tbody></Table></StickyTableFrame>
      <div className="sw-pagination"><Pagination total={totalPages} value={safePage} onChange={setPage} /></div>
    </>}
  </div>
}

function SortableHeader({ label, active, direction, onClick }: { label: string; active: boolean; direction: SortDirection; onClick: () => void }) {
  return <Button variant="plain" className="sw-sort-button" icon={active ? direction === 'asc' ? <SortAmountUpIcon /> : <SortAmountDownIcon /> : undefined} iconPosition="end" onClick={onClick} aria-label={`Sort by ${label}`}>{label}</Button>
}

function updateValues(values: readonly string[], value: string, checked: boolean): string[] {
  return checked ? [...values, value] : values.filter((item) => item !== value)
}

function FilterOptions({ title, values, selected, onChange }: { title: string; values: FilterOption[]; selected: readonly string[]; onChange: (next: string[]) => void }) {
  return <fieldset className="sw-filter-group"><legend>{title}</legend>{values.map((option) => <Checkbox key={option.value} id={`filter-${title}-${option.value}`} label={`${option.label} (${option.count})`} isChecked={selected.includes(option.value)} onChange={(_event, checked) => onChange(updateValues(selected, option.value, checked))} />)}</fieldset>
}

function FilterPanel({ filters, options, onChange }: { filters: ServerFilters; options: ServerFilterOptions; onChange: (next: ServerFilters) => void }) {
  return <div className="sw-filter-panel"><FilterOptions title="Provisioning" values={options.provisioningState} selected={filters.provisioningStates} onChange={(values) => onChange({ ...filters, provisioningStates: values })} /><FilterOptions title="Zone" values={options.zone} selected={filters.zones} onChange={(values) => onChange({ ...filters, zones: values })} /><FilterOptions title="Pool" values={options.pool} selected={filters.pools} onChange={(values) => onChange({ ...filters, pools: values })} /><FilterOptions title="Tags" values={options.tag} selected={filters.tags} onChange={(values) => onChange({ ...filters, tags: values })} /><fieldset className="sw-filter-group"><legend>GPU</legend><FormSelect value={filters.hasGpu === null ? 'any' : filters.hasGpu ? 'yes' : 'no'} onChange={(_event, value) => onChange({ ...filters, hasGpu: value === 'any' ? null : value === 'yes' })} aria-label="Filter GPU presence"><FormSelectOption value="any" label="Any" /><FormSelectOption value="yes" label="Has GPU" /><FormSelectOption value="no" label="No GPU" /></FormSelect></fieldset><Button variant="link" onClick={() => onChange(EMPTY_SERVER_FILTERS)}>Clear filters</Button></div>
}

function ColumnPanel({ columns, hidden, onToggle }: { columns: ColumnToggle[]; hidden: ReadonlySet<string>; onToggle: (key: string) => void }) {
  return <div className="sw-column-panel">{columns.map((column) => <Checkbox key={column.key} id={`column-${column.key}`} label={column.label} isChecked={!hidden.has(column.key)} onChange={() => onToggle(column.key)} />)}</div>
}

function ActionDropdown({ label, icon, running, onAction }: { label: string; icon?: ReactNode; running?: boolean; onAction: (action: BulkAction) => void }) {
  const [open, setOpen] = useState(false)
  return <Dropdown isOpen={open} onOpenChange={setOpen} toggle={(ref) => <MenuToggle ref={ref} icon={icon} variant={label ? 'default' : 'plain'} aria-label={label || 'Actions'} isExpanded={open} isDisabled={running} onClick={() => setOpen((value) => !value)}>{label || null}</MenuToggle>}><DropdownList>{SERVER_ACTION_GROUPS.flatMap((group) => [<DropdownItem key={`${group.label}-label`} isDisabled>{group.label}</DropdownItem>, ...group.actions.map((entry) => <DropdownItem key={entry.action} value={entry.action} onClick={() => { setOpen(false); onAction(entry.action) }}>{entry.label}</DropdownItem>)])}</DropdownList></Dropdown>
}
function BulkActionMenu({ running, onAction }: { running: boolean; onAction: (action: BulkAction) => void }) { return <ActionDropdown label={running ? 'Working...' : 'Take action'} running={running} onAction={onAction} /> }

function GroupRows({ group, grouped, columnSpan, collapsed, onCollapse, selected, onToggleOne, onToggleGroup, onNavigate, onAction, visible }: { group: RenderGroup; grouped: boolean; columnSpan: number; collapsed: boolean; onCollapse: () => void; selected: ReadonlySet<string>; onToggleOne: (id: string) => void; onToggleGroup: (ids: string[], checked: boolean) => void; onNavigate: (id: string) => void; onAction: (action: BulkAction, id: string) => void; visible: (key: string) => boolean }) {
  const ids = group.items.map((item) => item.id)
  const all = ids.length > 0 && ids.every((id) => selected.has(id))
  const some = ids.some((id) => selected.has(id))
  return <>{grouped && <Tr className="sw-group-row"><Td colSpan={columnSpan}><span><Button variant="plain" icon={collapsed ? <AngleRightIcon /> : <AngleDownIcon />} aria-label={`${collapsed ? 'Expand' : 'Collapse'} ${group.label}`} onClick={onCollapse} /><Checkbox id={`group-${group.key}`} aria-label={`Select all in ${group.label}`} isChecked={all ? true : some ? null : false} onChange={() => onToggleGroup(ids, !all)} /><strong>{group.label}</strong><Badge isRead>{group.items.length}</Badge></span></Td></Tr>}{!collapsed && group.items.map((server) => <ServerRow key={server.id} server={server} checked={selected.has(server.id)} onToggle={() => onToggleOne(server.id)} onNavigate={() => onNavigate(server.id)} onAction={(action) => onAction(action, server.id)} visible={visible} />)}</>
}

function ServerRow({ server, checked, onToggle, onNavigate, onAction, visible }: { server: Server; checked: boolean; onToggle: () => void; onNavigate: () => void; onAction: (action: BulkAction) => void; visible: (key: string) => boolean }) {
  const gpuCount = serverGpuCount(server)
  return <Tr isClickable onRowClick={onNavigate}>
    <Td dataLabel="Machine" className="sw-sticky-name"><span className="sw-select-name" onClick={(event) => event.stopPropagation()}><Checkbox id={`server-${server.id}`} aria-label={`Select ${serverDisplayName(server)}`} isChecked={checked} onChange={onToggle} /><span><strong>{serverDisplayName(server)}</strong>{server.absent && <Label color="grey">absent</Label>}<small className="mono">{serverMacAddress(server) ?? server.id}</small></span></span></Td>
    {visible('power') && <Td dataLabel="Power">{server.provisioning?.powerState ?? 'unknown'}</Td>}
    {visible('status') && <Td dataLabel="Provisioning"><ProvisioningBadge axis={server.provisioning} /></Td>}
    {visible('address') && <Td dataLabel="Address" className="mono">{serverPrimaryAddress(server) ?? 'Not assigned'}</Td>}
    {visible('placement') && <Td dataLabel="Zone / pool"><strong>{server.providerZone || 'No zone'}</strong><small>{server.providerResourcePool || 'No pool'}</small></Td>}
    {visible('tags') && <Td dataLabel="Tags">{server.tags.length ? server.tags.slice(0, 3).map((tag) => <Label key={tag} color="blue" isCompact>{tag}</Label>) : 'No tags'}</Td>}
    {visible('hardware') && <Td dataLabel="Hardware"><strong>{server.cpuCores || 'No data'} cores, {server.memoryMiB ? `${Math.round(server.memoryMiB / 1024)} GiB` : 'No RAM data'}</strong><small>{server.systemVendor || server.architecture || 'Hardware not observed'}; {server.storageGB ? `${Math.round(server.storageGB)} GB` : 'No storage data'}</small></Td>}
    {visible('gpus') && <Td dataLabel="GPUs">{gpuCount ? server.gpus.map((gpu) => `${gpu.count}x ${gpu.model || gpu.vendor}`).join(', ') : 'None'}</Td>}
    {visible('cluster') && <Td dataLabel="Cluster"><MembershipBadge axis={server.membership} /></Td>}
    {visible('health') && <Td dataLabel="Health"><HealthBadge axis={server.health} /></Td>}
    <Td isActionCell className="sw-sticky-actions" onClick={(event) => event.stopPropagation()}><ActionDropdown label="" icon={<EllipsisVIcon />} onAction={onAction} /></Td>
  </Tr>
}
