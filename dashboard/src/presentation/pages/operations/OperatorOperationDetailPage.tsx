import { useCallback, useState } from 'react'
import { Button, Tabs } from '@chakra-ui/react'
import { Redo, RefreshCw } from 'lucide-react'
import { useNavigate, useParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { isOrchestrationOperation, isTerminalStatus, type Operation } from '@/domain/operation/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { KeyValueGrid, SectionHeader } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useTargetLockProtection } from '@/presentation/hooks/useTargetLockProtection'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime } from '@/shared/utils/time'
import { OperationEventWorkspace } from './OperationEventWorkspace'
import { OperationLogWorkspace } from './OperationLogWorkspace'
import { DurableOperationDetail } from './DurableOperationDetail'
import { useOperation } from './useOperation'

/** AWX-style operation debugger with stdout first, filtered events, and immutable details. */
export function OperatorOperationDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { operations } = useApp()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const state = useOperation(id)
  const [retrying, setRetrying] = useState(false)
  const [tab, setTab] = useState('stdout')

  const retry = useCallback(async () => {
    if (!id) return
    setRetrying(true)
    try {
      const created = await operations.retryOperation(id)
      showToast({
        title: 'Retry started',
        description: 'A new Operation was created with the same intent and targets.',
        tone: 'success',
      })
      navigate(scopedHref(`/workflows/${created.id}`))
    } catch (error) {
      showToast({
        title: 'Retry failed',
        description: error instanceof Error ? error.message : 'Could not create the retry.',
        tone: 'error',
      })
    } finally {
      setRetrying(false)
    }
  }, [id, navigate, operations, scopedHref, showToast])

  if (state.status === 'loading') return <LoadingState rows={7} />
  if (state.status === 'error') return <ErrorState message={state.message} />
  if (state.status === 'not-found') return <EmptyState title="Operation not found" />
  const { operation, events, reload } = state.data
  if (isOrchestrationOperation(operation)) {
    return <DurableOperationDetail operation={operation} reload={reload} />
  }
  const terminal = isTerminalStatus(operation.execution.status)
  const details = (
    <section className="sw-section">
      <SectionHeader title="Execution details" description="Immutable intent and current run metadata." />
      <KeyValueGrid
        items={[
          { label: 'Operation ID', value: <span className="mono">{operation.id}</span> },
          { label: 'Run ID', value: <span className="mono">{operation.execution.runId || 'No data'}</span> },
          { label: 'Requested by', value: operation.requestedBy || 'system' },
          { label: 'Targets', value: `${operation.targetServerIds.length} Servers` },
          {
            label: 'Platform',
            value: operation.platformId ? (
              <Button variant="plain" size="sm" px="0" h="auto" colorPalette="brand" onClick={() => navigate(scopedHref(`/platforms/${operation.platformId}`))}>
                {operation.platformId}
              </Button>
            ) : (
              'No data'
            ),
          },
          {
            label: 'Retry of',
            value: operation.retryOfOperationId ? (
              <Button variant="plain" size="sm" px="0" h="auto" colorPalette="brand" onClick={() => navigate(scopedHref(`/workflows/${operation.retryOfOperationId}`))}>
                {operation.retryOfOperationId}
              </Button>
            ) : (
              'No data'
            ),
          },
          { label: 'Status reason', value: operation.execution.statusReason || 'No data' },
          { label: 'Updated', value: formatDateTime(operation.updatedAt) },
        ]}
      />
    </section>
  )

  return (
    <div className="operator-page">
      <PageHeader
        title={operation.intent || operation.execution.playbook}
        breadcrumbs={[{ label: 'Workflows', href: scopedHref('/workflows') }, { label: operation.id }]}
        subtitle={`${operation.kind} - ${operation.execution.playbook}`}
        metadata={<StatusBadge status={operation.execution.status} />}
        actions={
          <>
            <Button variant="outline" onClick={reload}>
              <RefreshCw size={16} />
              Refresh
            </Button>
            {terminal && <OperationRetryButton operation={operation} retrying={retrying} onRetry={() => void retry()} />}
          </>
        }
      />
      <ol className="sw-operation-timeline" aria-label="Operation status timeline">
        <TimelineStep label="Requested" value={formatDateTime(operation.requestedAt)} state="complete" />
        <TimelineStep label="Started" value={formatDateTime(operation.execution.startedAt ?? undefined)} state={operation.execution.startedAt ? 'complete' : 'current'} />
        <TimelineStep
          label={terminal ? operation.execution.status : 'Running'}
          value={terminal ? formatDateTime(operation.execution.finishedAt ?? undefined) : 'In progress'}
          state={operation.execution.status === 'failed' ? 'failed' : terminal ? 'complete' : 'current'}
        />
      </ol>
      <section className="sw-section sw-operation-debugger">
        <Tabs.Root value={tab} onValueChange={(details) => setTab(details.value)} aria-label="Operation workspace">
          <Tabs.List px="4">
            <Tabs.Trigger value="stdout">Stdout</Tabs.Trigger>
            <Tabs.Trigger value="events">Events</Tabs.Trigger>
            <Tabs.Trigger value="details">Details</Tabs.Trigger>
          </Tabs.List>
          <Tabs.Content value="stdout">
            <div className="sw-tab-content">
              <OperationLogWorkspace operationId={operation.id} />
            </div>
          </Tabs.Content>
          <Tabs.Content value="events">
            <div className="sw-tab-content">
              <OperationEventWorkspace events={events} running={!terminal} />
            </div>
          </Tabs.Content>
          <Tabs.Content value="details">
            <div className="sw-tab-content">{details}</div>
          </Tabs.Content>
        </Tabs.Root>
      </section>
    </div>
  )
}

function TimelineStep({
  label,
  value,
  state,
}: {
  label: string
  value: string
  state: 'complete' | 'current' | 'failed'
}) {
  return (
    <li data-state={state}>
      <span aria-hidden="true" />
      <div>
        <strong>{label}</strong>
        <small>{value}</small>
      </div>
      {state === 'current' && <StatusBadge status="running" label="Current" />}
    </li>
  )
}

function OperationRetryButton({
  operation,
  retrying,
  onRetry,
}: {
  operation: Operation
  retrying: boolean
  onRetry: () => void
}) {
  const protection = useTargetLockProtection(operation.targetServerIds)
  const disabledReason = protection.checking
    ? 'Checking target protection.'
    : protection.error
      ? protection.error
      : protection.lockedNames.length > 0
        ? `${protection.lockedNames.join(', ')} ${protection.lockedNames.length === 1 ? 'is' : 'are'} locked. Unlock ${protection.lockedNames.length === 1 ? 'it' : 'them'} before retrying this Operation.`
        : undefined

  return (
    <Tooltip content={disabledReason ?? 'Create a new Operation with the same intent and targets'}>
      <span>
        <Button
          colorPalette="brand"
          onClick={onRetry}
          loading={retrying}
          disabled={retrying || Boolean(disabledReason)}
          aria-label={disabledReason ? `Retry: ${disabledReason}` : 'Retry'}
        >
          <Redo size={16} />
          Retry
        </Button>
      </span>
    </Tooltip>
  )
}
