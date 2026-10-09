import { useCallback, useEffect, useMemo, useState } from 'react'
import { Badge, Box, Button, Flex, HStack, Table, Text } from '@chakra-ui/react'
import {
  ArrowUpRight,
  CircleX,
  Clock3,
  LoaderCircle,
  RefreshCw,
  Server,
  TriangleAlert,
  Workflow as WorkflowIcon,
} from 'lucide-react'
import { Link as RouterLink, useSearchParams } from 'react-router-dom'
import type { ListOperationsFilters, Operation, OperationStatus } from '@/domain/operation/types'
import { operationStatus } from '@/domain/operation/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { InProgressSpinner } from '@/presentation/components/InProgressSpinner'
import { LoadingState } from '@/presentation/components/LoadingState'
import { InventorySurface, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { Pagination } from '@/presentation/components/Pagination'
import { ResponsiveDataView, ResourceCard, ResourceCardField } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { Select } from '@/presentation/components/ui/select'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDuration, formatRelative } from '@/shared/utils/time'
import { useOperations } from './useOperations'
import {
  isWorkflowChangingStatus,
  isWorkflowReviewStatus,
  workflowActionLabel,
  workflowDisplayIntent,
  workflowActivitySummary,
  type WorkflowActivitySummary,
} from './workflowListPresentation'

const PAGE_SIZE = 30

const WORKFLOW_STATUSES: readonly OperationStatus[] = [
  'pending',
  'running',
  'waiting_external',
  'waiting_dependency',
  'canceling',
  'succeeded',
  'partially_succeeded',
  'failed',
  'requires_attention',
  'canceled',
  'indeterminate',
]

const OTHER_STATUS_OPTIONS = [
  { value: 'pending', label: 'Pending' },
  { value: 'running', label: 'Running' },
  { value: 'waiting_external', label: 'Waiting external' },
  { value: 'waiting_dependency', label: 'Waiting dependency' },
  { value: 'canceling', label: 'Canceling' },
  { value: 'succeeded', label: 'Succeeded' },
  { value: 'partially_succeeded', label: 'Partially succeeded' },
  { value: 'canceled', label: 'Canceled' },
  { value: 'indeterminate', label: 'Indeterminate' },
] as const

type WorkflowLens = 'all' | 'active' | 'requires_attention' | 'failed'

const QUICK_LENSES: ReadonlyArray<{ value: WorkflowLens; label: string }> = [
  { value: 'all', label: 'All' },
  { value: 'active', label: 'Active' },
  { value: 'requires_attention', label: 'Needs attention' },
  { value: 'failed', label: 'Failed' },
]

/** Unknown URL status values fail closed to the unfiltered list. */
function parseWorkflowStatus(value: string | null): OperationStatus | undefined {
  return WORKFLOW_STATUSES.find((status) => status === value)
}

/** Status family used only for subtle row treatment; the visible badge carries meaning. */
function workflowTone(status: OperationStatus): 'changing' | 'attention' | 'history' {
  if (isWorkflowChangingStatus(status)) return 'changing'
  if (isWorkflowReviewStatus(status)) return 'attention'
  return 'history'
}

/** Human elapsed time using canonical Workflow timestamps with legacy execution fallback. */
function workflowDuration(operation: Operation): string {
  const startedAt = operation.startedAt ?? operation.execution.startedAt
  if (!startedAt) return 'Not started'
  const finishedAt = operation.finishedAt ?? operation.execution.finishedAt
  const started = new Date(startedAt).getTime()
  const finished = finishedAt ? new Date(finishedAt).getTime() : Date.now()
  if (Number.isNaN(started) || Number.isNaN(finished)) return 'Unavailable'
  return formatDuration(Math.max(0, finished - started))
}

/** Native Workflow identity used by both table rows and mobile cards. */
function WorkflowIdentity({ operation, href }: { operation: Operation; href: string }) {
  return (
    <div className="sw-workflow-identity">
      <span className="sw-workflow-identity__icon" aria-hidden>
        <WorkflowIcon size={18} />
      </span>
      <span className="sw-workflow-identity__copy">
        <RouterLink className="sw-workflow-name" to={href}>
          {workflowDisplayIntent(operation)}
        </RouterLink>
        <HStack as="span" gap="2" minW="0">
          <Badge variant="subtle" colorPalette="purple">{operation.kind}</Badge>
          <Text as="span" className="mono" color="fg.muted" fontSize="xs" truncate>{operation.id}</Text>
        </HStack>
      </span>
    </div>
  )
}

/**
 * Textual Workflow status with the shared in-progress cue used on Server Deployment
 * cells: label first, then `InProgressSpinner` while the execution can still advance.
 * Status meaning stays on the badge text; the spinner is decorative only.
 */
function WorkflowStatusLine({ status }: { status: OperationStatus }) {
  return (
    <div className="sw-workflow-status-line">
      <StatusBadge status={status} />
      {isWorkflowChangingStatus(status) && <InProgressSpinner />}
    </div>
  )
}

/** Current Task or legacy runner context without inventing percentage progress. */
function WorkflowActivityCopy({ summary }: { summary: WorkflowActivitySummary }) {
  return (
    <div className="sw-workflow-activity-copy">
      <Text as="span" fontWeight="medium" lineClamp={1}>{summary.primary}</Text>
      {summary.secondary && (
        <Text as="span" color="fg.muted" fontSize="xs" lineClamp={1}>{summary.secondary}</Text>
      )}
    </div>
  )
}

/** Verifiable Task totals and status counts from the existing Workflow projection. */
function WorkflowTaskContext({ summary }: { summary: WorkflowActivitySummary }) {
  return (
    <div className="sw-workflow-task-context">
      <Text as="span" fontWeight="medium">
        {summary.taskTotal > 0 ? summary.taskTotal + (summary.taskTotal === 1 ? ' Task' : ' Tasks') : 'Legacy'}
      </Text>
      <Text as="span" color="fg.muted" fontSize="xs" lineClamp={1}>{summary.taskCounts}</Text>
    </div>
  )
}

/** Platform relation and frozen Server target count without additional lookups. */
function WorkflowTargets({ operation, platformHref }: { operation: Operation; platformHref: string | null }) {
  const serverCount = operation.targetServerIds.length
  return (
    <div className="sw-workflow-targets">
      {platformHref && operation.platformId && (
        <RouterLink
          className="sw-workflow-target-link"
          to={platformHref}
          aria-label={'Open Platform ' + operation.platformId}
        >
          Platform
          <ArrowUpRight size={12} aria-hidden />
        </RouterLink>
      )}
      <HStack as="span" gap="1.5" color="fg.muted">
        <Server size={14} aria-hidden />
        <span>{serverCount} Server{serverCount === 1 ? '' : 's'}</span>
      </HStack>
    </div>
  )
}

/** Request and elapsed-time facts shared by desktop and mobile renderers. */
function WorkflowTiming({ operation }: { operation: Operation }) {
  return (
    <div className="sw-workflow-timing">
      <HStack as="span" gap="1.5">
        <Clock3 size={14} aria-hidden />
        <span>{workflowDuration(operation)}</span>
      </HStack>
      <Text as="span" color="fg.muted" fontSize="xs">Updated {formatRelative(operation.updatedAt)}</Text>
    </div>
  )
}

/** Inputs for the mobile representation of one Workflow row. */
interface WorkflowRuntimeCardProps {
  operation: Operation
  workflowHref: string
  platformHref: string | null
}

/** Mobile Workflow card with information and navigation parity with the desktop row. */
function WorkflowRuntimeCard({ operation, workflowHref, platformHref }: WorkflowRuntimeCardProps) {
  const status = operationStatus(operation)
  const summary = workflowActivitySummary(operation)
  const actionLabel = workflowActionLabel(status)

  return (
    <div className="sw-workflow-runtime-card" data-tone={workflowTone(status)}>
      <ResourceCard
        title={<WorkflowIdentity operation={operation} href={workflowHref} />}
        status={<WorkflowStatusLine status={status} />}
        actions={
          <Button
            asChild
            size="sm"
            variant={isWorkflowReviewStatus(status) ? 'solid' : 'outline'}
            colorPalette={isWorkflowReviewStatus(status) ? 'brand' : undefined}
          >
            <RouterLink to={workflowHref}>
              {actionLabel}
              <ArrowUpRight size={14} aria-hidden />
            </RouterLink>
          </Button>
        }
      >
        <ResourceCardField label="Execution"><WorkflowActivityCopy summary={summary} /></ResourceCardField>
        <ResourceCardField label="Tasks"><WorkflowTaskContext summary={summary} /></ResourceCardField>
        <ResourceCardField label="Targets">
          <WorkflowTargets operation={operation} platformHref={platformHref} />
        </ResourceCardField>
        <ResourceCardField label="Requested">
          <div className="sw-workflow-requested">
            <span>{operation.requestedBy || 'system'}</span>
            <Text as="span" color="fg.muted" fontSize="xs">{formatRelative(operation.requestedAt)}</Text>
          </div>
        </ResourceCardField>
        <ResourceCardField label="Timing"><WorkflowTiming operation={operation} /></ResourceCardField>
      </ResourceCard>
    </div>
  )
}

/**
 * URL-owned, server-paginated Workflow orchestration console.
 *
 * The list preserves provider ordering and exact filters, adapts between a
 * semantic table and equivalent cards, and keeps execution controls in the
 * detail route. Live rows update through useOperations without additional API
 * fan-out or client-side status aggregation.
 */
export function OperatorOperationsPage() {
  const [params, setParams] = useSearchParams()
  const { siteId, scopedHref } = useSiteScope()
  const paramsKey = params.toString()
  const rawStatus = params.get('status')
  const status = parseWorkflowStatus(rawStatus)
  const active = params.get('active') === 'true' || params.get('active') === 'active'
  const kind = params.get('kind') ?? ''
  const page = Math.max(1, Number(params.get('page')) || 1)
  const [kindDraft, setKindDraft] = useState(kind)

  const updateParams = useCallback((
    update: (next: URLSearchParams) => void,
    replace = false,
  ) => {
    const next = new URLSearchParams(paramsKey)
    update(next)
    setParams(next, { replace })
  }, [paramsKey, setParams])

  useEffect(() => {
    setKindDraft(kind)
  }, [kind])

  useEffect(() => {
    if (kindDraft === kind) return
    const timer = setTimeout(() => {
      updateParams((next) => {
        if (kindDraft) next.set('kind', kindDraft)
        else next.delete('kind')
        next.delete('page')
      }, true)
    }, 300)
    return () => clearTimeout(timer)
  }, [kind, kindDraft, updateParams])

  useEffect(() => {
    if ((!rawStatus || status) && !(active && rawStatus)) return
    updateParams((next) => {
      next.delete('status')
      next.delete('page')
    }, true)
  }, [active, rawStatus, status, updateParams])

  const filters = useMemo<ListOperationsFilters>(() => ({
    siteId,
    status: active ? undefined : status,
    kind: kind || undefined,
    active: active || undefined,
    page,
    pageSize: PAGE_SIZE,
  }), [active, kind, page, siteId, status])
  const { state, reload } = useOperations(filters)

  useEffect(() => {
    if (state.status !== 'ready' || state.total === 0 || state.operations.length > 0) return
    const lastPage = Math.max(1, Math.ceil(state.total / PAGE_SIZE))
    if (page <= lastPage) return
    updateParams((next) => next.set('page', String(lastPage)), true)
  }, [page, state, updateParams])

  const setLens = (lens: WorkflowLens) => {
    updateParams((next) => {
      next.delete('page')
      if (lens === 'all') {
        next.delete('active')
        next.delete('status')
      } else if (lens === 'active') {
        next.set('active', 'true')
        next.delete('status')
      } else {
        next.set('status', lens)
        next.delete('active')
      }
    })
  }

  const setOtherStatus = (value: string) => {
    const nextStatus = parseWorkflowStatus(value)
    if (!nextStatus) return
    updateParams((next) => {
      next.set('status', nextStatus)
      next.delete('active')
      next.delete('page')
    })
  }

  const clearFilters = () => {
    updateParams((next) => {
      next.delete('active')
      next.delete('status')
      next.delete('kind')
      next.delete('page')
    })
  }

  const setPage = (nextPage: number) => {
    updateParams((next) => {
      if (nextPage > 1) next.set('page', String(nextPage))
      else next.delete('page')
    })
  }

  const quickLens: WorkflowLens | null = active
    ? 'active'
    : status === 'requires_attention' || status === 'failed'
      ? status
      : status ? null : 'all'
  const otherStatus = status && status !== 'requires_attention' && status !== 'failed' ? status : ''
  const hasFilters = active || Boolean(status) || Boolean(kind)
  const hasLiveWork = state.status === 'ready' &&
    state.operations.some((operation) => isWorkflowChangingStatus(operationStatus(operation)))
  const totalPages = state.status === 'ready' ? Math.max(1, Math.ceil(state.total / PAGE_SIZE)) : 1
  const rangeStart = state.status === 'ready' && state.operations.length > 0 ? (page - 1) * PAGE_SIZE + 1 : 0
  const rangeEnd = state.status === 'ready' ? rangeStart + state.operations.length - (state.operations.length > 0 ? 1 : 0) : 0

  const summary = state.status === 'ready' ? (
    <div className="sw-workflow-result-summary">
      <span>
        {state.total === 0
          ? 'Showing 0 workflows'
          : 'Showing ' + rangeStart + '–' + rangeEnd + ' of ' + state.total + ' workflows'}
      </span>
      <span className="sw-workflow-refresh-state" data-live={hasLiveWork || undefined}>
        {hasLiveWork && <span className="sw-workflow-live-dot" aria-hidden />}
        {hasLiveWork ? 'Live updates every 5s' : 'Updated ' + formatRelative(state.refreshedAt)}
      </span>
    </div>
  ) : state.status === 'error' ? 'Workflow activity unavailable' : 'Loading workflows'

  return (
    <div className="operator-page sw-workflows-page">
      <PageHeader
        title="Workflows"
        subtitle="Observe durable automation, identify stalled work, and inspect execution history."
        actions={
          <Button variant="outline" onClick={reload} disabled={state.status === 'loading'}>
            <RefreshCw size={16} />
            Refresh
          </Button>
        }
      />

      <InventorySurface
        headingId="workflow-inventory-title"
        eyebrow="Automation orchestration"
        title="Workflow activity"
        summary={summary}
        className="sw-workflow-inventory"
        toolbar={
          <>
            <Flex className="sw-workflow-filter-groups" align="center" gap="2">
              <Flex
                as="div"
                className="sw-workflow-filter-group"
                role="group"
                aria-label="Workflow operational view"
              >
                {QUICK_LENSES.map((lens) => (
                  <Button
                    key={lens.value}
                    className="sw-workflow-filter-chip"
                    variant="plain"
                    size="sm"
                    aria-pressed={quickLens === lens.value}
                    data-active={quickLens === lens.value || undefined}
                    onClick={() => setLens(lens.value)}
                  >
                    {lens.value === 'active' && <LoaderCircle size={14} aria-hidden />}
                    {lens.value === 'requires_attention' && <TriangleAlert size={14} aria-hidden />}
                    {lens.value === 'failed' && <CircleX size={14} aria-hidden />}
                    {lens.label}
                  </Button>
                ))}
              </Flex>
              <Select
                value={otherStatus}
                onChange={setOtherStatus}
                options={OTHER_STATUS_OPTIONS}
                placeholder="More statuses"
                aria-label="Filter by another Workflow status"
                size="sm"
                width="auto"
              />
            </Flex>
            <Box className="sw-workflow-kind-filter">
              <SearchInput
                value={kindDraft}
                onChange={setKindDraft}
                placeholder="Exact Workflow kind"
                aria-label="Filter by Workflow kind"
                maxW="100%"
                size="md"
              />
            </Box>
          </>
        }
      >
        {state.status === 'ready' && state.refreshError && (
          <div className="sw-workflow-refresh-error">
            <Alert status="warning" title="Live updates interrupted">
              {state.refreshError} — showing the last successful result and retrying automatically.
            </Alert>
          </div>
        )}

        {state.status === 'loading' && <div className="sw-workflow-state"><LoadingState rows={7} /></div>}
        {state.status === 'error' && (
          <div className="sw-workflow-state">
            <ErrorState message={state.message} onRetry={reload} />
          </div>
        )}
        {state.status === 'ready' && state.total === 0 && !hasFilters && (
          <div className="sw-workflow-state">
            <EmptyState
              title="No workflows yet"
              message="Durable automation launched from Servers, Platforms, and provisioning flows will appear here."
            />
          </div>
        )}
        {state.status === 'ready' && state.total === 0 && hasFilters && (
          <div className="sw-workflow-state">
            <EmptyState
              title="No workflows match this view"
              message="Adjust the operational view, status, or exact Workflow kind."
              action={{ label: 'Clear filters', onClick: clearFilters }}
            />
          </div>
        )}
        {state.status === 'ready' && state.total > 0 && state.operations.length === 0 && (
          <div className="sw-workflow-state"><LoadingState rows={7} /></div>
        )}
        {state.status === 'ready' && state.operations.length > 0 && (
          <>
            <ResponsiveDataView
              desktop={
                <StickyTableFrame>
                  <Table.Root className="sw-workflow-table" size="sm" aria-label="Workflows">
                    <Table.Header>
                      <Table.Row>
                        <Table.ColumnHeader>Workflow</Table.ColumnHeader>
                        <Table.ColumnHeader>Execution</Table.ColumnHeader>
                        <Table.ColumnHeader>Tasks</Table.ColumnHeader>
                        <Table.ColumnHeader>Targets</Table.ColumnHeader>
                        <Table.ColumnHeader>Requested</Table.ColumnHeader>
                        <Table.ColumnHeader>Timing</Table.ColumnHeader>
                        <Table.ColumnHeader className="sw-workflow-table__action">Action</Table.ColumnHeader>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {state.operations.map((operation) => {
                        const currentStatus = operationStatus(operation)
                        const activity = workflowActivitySummary(operation)
                        const workflowHref = scopedHref('/workflows/' + operation.id)
                        const platformHref = operation.platformId
                          ? scopedHref('/platforms/' + operation.platformId)
                          : null
                        const actionLabel = workflowActionLabel(currentStatus)
                        return (
                          <Table.Row key={operation.id} data-tone={workflowTone(currentStatus)}>
                            <Table.Cell>
                              <WorkflowIdentity operation={operation} href={workflowHref} />
                            </Table.Cell>
                            <Table.Cell>
                              <div className="sw-workflow-execution">
                                <WorkflowStatusLine status={currentStatus} />
                                <WorkflowActivityCopy summary={activity} />
                              </div>
                            </Table.Cell>
                            <Table.Cell><WorkflowTaskContext summary={activity} /></Table.Cell>
                            <Table.Cell>
                              <WorkflowTargets operation={operation} platformHref={platformHref} />
                            </Table.Cell>
                            <Table.Cell>
                              <div className="sw-workflow-requested">
                                <span>{operation.requestedBy || 'system'}</span>
                                <Text as="span" color="fg.muted" fontSize="xs">
                                  {formatRelative(operation.requestedAt)}
                                </Text>
                              </div>
                            </Table.Cell>
                            <Table.Cell><WorkflowTiming operation={operation} /></Table.Cell>
                            <Table.Cell className="sw-workflow-table__action">
                              <Button
                                asChild
                                size="sm"
                                variant={isWorkflowReviewStatus(currentStatus) ? 'solid' : 'outline'}
                                colorPalette={isWorkflowReviewStatus(currentStatus) ? 'brand' : undefined}
                              >
                                <RouterLink to={workflowHref}>
                                  {actionLabel}
                                  <ArrowUpRight size={14} aria-hidden />
                                </RouterLink>
                              </Button>
                            </Table.Cell>
                          </Table.Row>
                        )
                      })}
                    </Table.Body>
                  </Table.Root>
                </StickyTableFrame>
              }
              mobile={
                <div className="sw-resource-card-list sw-workflow-card-list" aria-label="Workflows">
                  {state.operations.map((operation) => (
                    <WorkflowRuntimeCard
                      key={operation.id}
                      operation={operation}
                      workflowHref={scopedHref('/workflows/' + operation.id)}
                      platformHref={operation.platformId ? scopedHref('/platforms/' + operation.platformId) : null}
                    />
                  ))}
                </div>
              }
            />
            <div className="sw-workflow-pagination">
              <Pagination value={page} total={totalPages} onChange={setPage} />
            </div>
          </>
        )}
      </InventorySurface>
    </div>
  )
}
