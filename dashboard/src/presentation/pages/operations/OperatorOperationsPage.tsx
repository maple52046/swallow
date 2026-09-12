import { useMemo } from 'react'
import { Table, Text } from '@chakra-ui/react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import type { ListOperationsFilters, OperationStatus } from '@/domain/operation/types'
import { operationStatus } from '@/domain/operation/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { Pagination } from '@/presentation/components/Pagination'
import { ResponsiveDataView, ResourceCard, ResourceCardField } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { Select } from '@/presentation/components/ui/select'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatRelative } from '@/shared/utils/time'
import { useOperations } from './useOperations'

const PAGE_SIZE = 30
const STATUSES = [
  'all', 'pending', 'running', 'waiting_external', 'waiting_dependency', 'canceling',
  'succeeded', 'partially_succeeded', 'failed', 'requires_attention', 'canceled', 'indeterminate',
]

/**
 * URL-filtered list of durable Workflows.
 *
 * The same result set renders as a dense table on desktop and cards on mobile;
 * both routes preserve Site scope, pagination, audit identity, and native links.
 */
export function OperatorOperationsPage() {
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const { siteId, scopedHref } = useSiteScope()
  const status = params.get('status') ?? 'all'
  const kind = params.get('kind') ?? ''
  const active = params.get('active') ?? 'all'
  const page = Math.max(1, Number(params.get('page')) || 1)
  const filters = useMemo<ListOperationsFilters>(() => ({
    siteId,
    status: status === 'all' ? undefined : status as OperationStatus,
    kind: kind || undefined,
    active: active === 'active' ? true : undefined,
    page,
    pageSize: PAGE_SIZE,
  }), [active, kind, page, siteId, status])
  const { state } = useOperations(filters)

  const setFilter = (key: string, value?: string) => {
    const next = new URLSearchParams(params)
    if (value && value !== 'all') next.set(key, value)
    else next.delete(key)
    if (key !== 'page') next.delete('page')
    setParams(next)
  }

  return (
    <div className="operator-page">
      <PageHeader title="Workflows" />
      <DataToolbar variant="plain">
        <Select
          value={status}
          onChange={(value) => setFilter('status', value)}
          aria-label="Filter by status"
          size="sm"
          width="auto"
          options={STATUSES.map((value) => ({ value, label: value === 'all' ? 'All statuses' : value.replaceAll('_', ' ') }))}
        />
        <Select
          value={active}
          onChange={(value) => setFilter('active', value)}
          aria-label="Filter active workflows"
          size="sm"
          width="auto"
          options={[
            { value: 'all', label: 'All activity' },
            { value: 'active', label: 'Active only' },
          ]}
        />
        <SearchInput value={kind} onChange={(value) => setFilter('kind', value || undefined)} placeholder="Workflow kind" aria-label="Filter by workflow kind" />
      </DataToolbar>

      {state.status === 'loading' && <LoadingState rows={7} />}
      {state.status === 'error' && <ErrorState message={state.message} />}
      {state.status === 'ready' && state.operations.length === 0 && (
        <EmptyState title="No workflows" message="No results match these filters." />
      )}
      {state.status === 'ready' && state.operations.length > 0 && (
        <>
          <ResponsiveDataView
            desktop={
              <StickyTableFrame>
                <Table.Root size="sm" aria-label="Workflows">
                  <Table.Header>
                    <Table.Row><Table.ColumnHeader>Status</Table.ColumnHeader><Table.ColumnHeader>Workflow</Table.ColumnHeader><Table.ColumnHeader>Kind</Table.ColumnHeader><Table.ColumnHeader>Targets</Table.ColumnHeader><Table.ColumnHeader>Requested by</Table.ColumnHeader><Table.ColumnHeader>Age</Table.ColumnHeader></Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {state.operations.map((operation) => (
                      <Table.Row key={operation.id} cursor="pointer" onClick={() => navigate(scopedHref(`/workflows/${operation.id}`))}>
                        <Table.Cell><StatusBadge status={operationStatus(operation)} /></Table.Cell>
                        <Table.Cell>
                          <Link to={scopedHref(`/workflows/${operation.id}`)} onClick={(event) => event.stopPropagation()}><strong>{operation.intent || operation.execution.playbook}</strong></Link>
                          <Text fontSize="xs" color="fg.muted" className="mono">{operation.id}</Text>
                        </Table.Cell>
                        <Table.Cell>{operation.kind}</Table.Cell>
                        <Table.Cell>{operation.targetServerIds.length}</Table.Cell>
                        <Table.Cell>{operation.requestedBy || 'system'}</Table.Cell>
                        <Table.Cell>{formatRelative(operation.requestedAt)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table.Root>
              </StickyTableFrame>
            }
            mobile={
              <div className="sw-resource-card-list" aria-label="Workflows">
                {state.operations.map((operation) => (
                  <ResourceCard
                    key={operation.id}
                    title={<Link to={scopedHref(`/workflows/${operation.id}`)}>{operation.intent || operation.execution.playbook}</Link>}
                    description={operation.id}
                    status={<StatusBadge status={operationStatus(operation)} />}
                  >
                    <ResourceCardField label="Kind">{operation.kind}</ResourceCardField>
                    <ResourceCardField label="Targets">{operation.targetServerIds.length}</ResourceCardField>
                    <ResourceCardField label="Requested by">{operation.requestedBy || 'system'}</ResourceCardField>
                    <ResourceCardField label="Age">{formatRelative(operation.requestedAt)}</ResourceCardField>
                  </ResourceCard>
                ))}
              </div>
            }
          />
          <div className="sw-pagination">
            <Pagination value={page} total={Math.max(1, Math.ceil(state.total / PAGE_SIZE))} onChange={(value) => setFilter('page', String(value))} />
          </div>
        </>
      )}
    </div>
  )
}
