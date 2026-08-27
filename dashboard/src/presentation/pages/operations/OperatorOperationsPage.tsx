import { useMemo } from 'react'
import { FormSelect, FormSelectOption, SearchInput, ToolbarItem } from '@patternfly/react-core'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate, useSearchParams } from 'react-router-dom'
import type { ListOperationsFilters, OperationStatus } from '@/domain/operation/types'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { Pagination } from '@/presentation/components/Pagination'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatRelative } from '@/shared/utils/time'
import { useOperations } from './useOperations'

const PAGE_SIZE = 30
const STATUSES = ['all', 'pending', 'running', 'succeeded', 'failed', 'canceled', 'indeterminate']

/** AWX-style compact Operation list whose complete filtering state remains in the URL. */
export function OperatorOperationsPage() {
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const { siteId, scopedHref } = useSiteScope()
  const status = params.get('status') ?? 'all'
  const kind = params.get('kind') ?? ''
  const active = params.get('active') ?? 'all'
  const page = Math.max(1, Number(params.get('page')) || 1)
  const filters = useMemo<ListOperationsFilters>(() => ({ siteId, status: status === 'all' ? undefined : status as OperationStatus, kind: kind || undefined, active: active === 'active' ? true : undefined, page, pageSize: PAGE_SIZE }), [active, kind, page, siteId, status])
  const { state } = useOperations(filters)

  const setFilter = (key: string, value?: string) => {
    const next = new URLSearchParams(params)
    if (value && value !== 'all') next.set(key, value)
    else next.delete(key)
    if (key !== 'page') next.delete('page')
    setParams(next)
  }

  return <div className="operator-page">
    <PageHeader title="Operations" subtitle="Retained automation runs for audit, debugging, and explicit retry." />
    <DataToolbar>
      <ToolbarItem><FormSelect value={status} onChange={(_event, value) => setFilter('status', value)} aria-label="Filter by status">{STATUSES.map((value) => <FormSelectOption key={value} value={value} label={value === 'all' ? 'All statuses' : value} />)}</FormSelect></ToolbarItem>
      <ToolbarItem><FormSelect value={active} onChange={(_event, value) => setFilter('active', value)} aria-label="Filter active operations"><FormSelectOption value="all" label="All activity" /><FormSelectOption value="active" label="Active only" /></FormSelect></ToolbarItem>
      <ToolbarItem><SearchInput value={kind} onChange={(_event, value) => setFilter('kind', value)} onClear={() => setFilter('kind')} placeholder="Filter by kind" aria-label="Filter by operation kind" /></ToolbarItem>
    </DataToolbar>
    {state.status === 'loading' && <LoadingState rows={7} />}
    {state.status === 'error' && <ErrorState message={state.message} />}
    {state.status === 'ready' && state.operations.length === 0 && <EmptyState title="No operations" message="Nothing matches the current URL filters." />}
    {state.status === 'ready' && state.operations.length > 0 && <>
      <StickyTableFrame><Table aria-label="Operations" variant="compact" isStriped><Thead><Tr><Th>Status</Th><Th>Operation</Th><Th>Kind</Th><Th>Targets</Th><Th>Requested by</Th><Th>Age</Th></Tr></Thead><Tbody>{state.operations.map((operation) => <Tr key={operation.id} isClickable onRowClick={() => navigate(scopedHref(`/operations/${operation.id}`))}><Td dataLabel="Status"><StatusBadge status={operation.execution.status} /></Td><Td dataLabel="Operation"><strong>{operation.intent || operation.execution.playbook}</strong><small className="mono">{operation.id}</small></Td><Td dataLabel="Kind">{operation.kind}</Td><Td dataLabel="Targets">{operation.targetServerIds.length}</Td><Td dataLabel="Requested by">{operation.requestedBy || 'system'}</Td><Td dataLabel="Age">{formatRelative(operation.requestedAt)}</Td></Tr>)}</Tbody></Table></StickyTableFrame>
      <div className="sw-pagination"><Pagination value={page} total={Math.max(1, Math.ceil(state.total / PAGE_SIZE))} onChange={(value) => setFilter('page', String(value))} /></div>
    </>}
  </div>
}
