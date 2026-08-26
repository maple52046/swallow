import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Card, Flex, Select, Table, Text } from '@radix-ui/themes'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Pagination } from '@/presentation/components/radix/Pagination'
import { formatRelative } from '@/shared/utils/time'
import type { ListOperationsFilters, OperationStatus } from '@/domain/operation/types'
import { useOperations } from './useOperations'

const PAGE_SIZE = 20

/** The status filter values, plus an "all" sentinel that clears the filter. */
const STATUS_OPTIONS: Array<{ value: string; label: string }> = [
  { value: 'all', label: 'All statuses' },
  { value: 'pending', label: 'Pending' },
  { value: 'running', label: 'Running' },
  { value: 'succeeded', label: 'Succeeded' },
  { value: 'failed', label: 'Failed' },
  { value: 'canceled', label: 'Canceled' },
  { value: 'indeterminate', label: 'Indeterminate' },
]

/**
 * Lists swallow-owned operations with a status filter and pagination.
 *
 * The backend owns filtering and paging; this screen only chooses the query and renders the
 * result, then links each row to its detail view where progress and logs live. Status is
 * shown with the shared badge so it never reads as colour alone.
 */
export function OperationsPage() {
  const navigate = useNavigate()
  const [statusFilter, setStatusFilter] = useState<string>('all')
  const [page, setPage] = useState(1)

  const filters = useMemo<ListOperationsFilters>(
    () => ({
      status: statusFilter === 'all' ? undefined : (statusFilter as OperationStatus),
      page,
      pageSize: PAGE_SIZE,
    }),
    [statusFilter, page],
  )

  const { state } = useOperations(filters)

  return (
    <Flex direction="column">
      <PageHeader
        title="Operations"
        subtitle="Automation swallow ran against your servers: cluster deployments, driver installs, and diagnostics."
        actions={
          <Select.Root
            value={statusFilter}
            onValueChange={(value) => {
              setStatusFilter(value)
              setPage(1)
            }}
          >
            <Select.Trigger aria-label="Filter by status" />
            <Select.Content>
              {STATUS_OPTIONS.map((option) => (
                <Select.Item key={option.value} value={option.value}>
                  {option.label}
                </Select.Item>
              ))}
            </Select.Content>
          </Select.Root>
        }
      />

      {state.status === 'loading' && <LoadingState rows={6} />}
      {state.status === 'error' && <ErrorState message={state.message} />}

      {state.status === 'ready' && state.operations.length === 0 && (
        <EmptyState
          title="No operations"
          message={
            statusFilter === 'all'
              ? 'Nothing has run yet. Deploying a cluster is the first thing that creates one.'
              : 'No operations match this status filter.'
          }
        />
      )}

      {state.status === 'ready' && state.operations.length > 0 && (
        <Card>
          <Table.Root variant="ghost">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeaderCell>Intent</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Kind</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Targets</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Status</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Requested</Table.ColumnHeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {state.operations.map((operation) => (
                <Table.Row
                  key={operation.id}
                  onClick={() => navigate(`/operations/${operation.id}`)}
                  style={{ cursor: 'pointer' }}
                >
                  <Table.Cell>
                    <Flex direction="column">
                      <Text size="2" weight="medium">
                        {operation.intent || operation.execution.playbook}
                      </Text>
                      {operation.retryOfOperationId && (
                        <Text size="1" color="gray">
                          retry of an earlier operation
                        </Text>
                      )}
                    </Flex>
                  </Table.Cell>
                  <Table.Cell>
                    <Text size="2">{operation.kind}</Text>
                  </Table.Cell>
                  <Table.Cell>
                    <Text size="2">{operation.targetServerIds.length}</Text>
                  </Table.Cell>
                  <Table.Cell>
                    <StatusBadge status={operation.execution.status} />
                  </Table.Cell>
                  <Table.Cell>
                    <Text size="2" color="gray">
                      {formatRelative(operation.requestedAt)}
                    </Text>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>

          <Flex justify="end" mt="3">
            <Pagination
              value={page}
              total={Math.max(1, Math.ceil(state.total / PAGE_SIZE))}
              onChange={setPage}
            />
          </Flex>
        </Card>
      )}
    </Flex>
  )
}
