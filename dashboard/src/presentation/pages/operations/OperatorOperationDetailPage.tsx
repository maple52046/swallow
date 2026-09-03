import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Tab, Tabs, TabTitleText, Tooltip } from '@patternfly/react-core'
import { RedoIcon, SyncAltIcon } from '@patternfly/react-icons'
import { useNavigate, useParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { isTerminalStatus, type Operation } from '@/domain/operation/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { KeyValueGrid, SectionHeader } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime } from '@/shared/utils/time'
import { OperationEventWorkspace } from './OperationEventWorkspace'
import { OperationLogWorkspace } from './OperationLogWorkspace'
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
  const [tab, setTab] = useState<string | number>('stdout')

  const retry = useCallback(async () => {
    if (!id) return
    setRetrying(true)
    try {
      const created = await operations.retryOperation(id)
      showToast({ title: 'Retry started', description: 'A new Operation was created with the same intent and targets.', tone: 'success' })
      navigate(scopedHref(`/operations/${created.id}`))
    } catch (error) {
      showToast({ title: 'Retry failed', description: error instanceof Error ? error.message : 'Could not create the retry.', tone: 'error' })
    } finally { setRetrying(false) }
  }, [id, navigate, operations, scopedHref, showToast])

  if (state.status === 'loading') return <LoadingState rows={7} />
  if (state.status === 'error') return <ErrorState message={state.message} />
  if (state.status === 'not-found') return <EmptyState title="Operation not found" />
  const { operation, events, reload } = state.data
  const terminal = isTerminalStatus(operation.execution.status)
  const details = <section className="sw-section"><SectionHeader title="Execution details" description="Immutable intent and current run metadata." /><KeyValueGrid items={[{ label: 'Operation ID', value: <span className="mono">{operation.id}</span> }, { label: 'Run ID', value: <span className="mono">{operation.execution.runId || 'No data'}</span> }, { label: 'Requested by', value: operation.requestedBy || 'system' }, { label: 'Targets', value: `${operation.targetServerIds.length} Servers` }, { label: 'Cluster', value: operation.clusterId ? <Button variant="link" isInline onClick={() => navigate(scopedHref(`/clusters/${operation.clusterId}`))}>{operation.clusterId}</Button> : 'No data' }, { label: 'Retry of', value: operation.retryOfOperationId ? <Button variant="link" isInline onClick={() => navigate(scopedHref(`/operations/${operation.retryOfOperationId}`))}>{operation.retryOfOperationId}</Button> : 'No data' }, { label: 'Status reason', value: operation.execution.statusReason || 'No data' }, { label: 'Updated', value: formatDateTime(operation.updatedAt) }]} /></section>

  return <div className="operator-page">
    <PageHeader title={operation.intent || operation.execution.playbook} breadcrumbs={[{ label: 'Operations', href: scopedHref('/operations') }, { label: operation.id }]} subtitle={`${operation.kind} - ${operation.execution.playbook}`} metadata={<StatusBadge status={operation.execution.status} />} actions={<><Button variant="secondary" icon={<SyncAltIcon />} onClick={reload}>Refresh</Button>{terminal && <OperationRetryButton operation={operation} retrying={retrying} onRetry={() => void retry()} />}</>} />
    <ol className="sw-operation-timeline" aria-label="Operation status timeline"><TimelineStep label="Requested" value={formatDateTime(operation.requestedAt)} state="complete" /><TimelineStep label="Started" value={formatDateTime(operation.execution.startedAt ?? undefined)} state={operation.execution.startedAt ? 'complete' : 'current'} /><TimelineStep label={terminal ? operation.execution.status : 'Running'} value={terminal ? formatDateTime(operation.execution.finishedAt ?? undefined) : 'In progress'} state={operation.execution.status === 'failed' ? 'failed' : terminal ? 'complete' : 'current'} /></ol>
    <section className="sw-section sw-operation-debugger"><Tabs activeKey={tab} onSelect={(_event, key) => setTab(key)} aria-label="Operation workspace"><Tab eventKey="stdout" title={<TabTitleText>Stdout</TabTitleText>}><div className="sw-tab-content"><OperationLogWorkspace operationId={operation.id} /></div></Tab><Tab eventKey="events" title={<TabTitleText>Events</TabTitleText>}><div className="sw-tab-content"><OperationEventWorkspace events={events} running={!terminal} /></div></Tab><Tab eventKey="details" title={<TabTitleText>Details</TabTitleText>}><div className="sw-tab-content">{details}</div></Tab></Tabs></section>
  </div>
}

function TimelineStep({ label, value, state }: { label: string; value: string; state: 'complete' | 'current' | 'failed' }) {
  return <li data-state={state}><span aria-hidden="true" /><div><strong>{label}</strong><small>{value}</small></div>{state === 'current' && <StatusBadge status="running" label="Current" />}</li>
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
  const { servers } = useApp()
  const targetKey = useMemo(
    () => [...operation.targetServerIds].sort().join(','),
    [operation.targetServerIds],
  )
  const [protection, setProtection] = useState<{
    targetKey: string
    lockedNames: string[]
    error?: string
  }>({ targetKey: '', lockedNames: [] })

  useEffect(() => {
    const ids = targetKey ? targetKey.split(',') : []
    let cancelled = false
    Promise.all(ids.map((id) => servers.getServer(id)))
      .then((targets) => {
        if (cancelled) return
        setProtection({
          targetKey,
          lockedNames: targets.flatMap((server) => (
            server?.provisioning?.locked ? [server.hostname || server.id] : []
          )),
        })
      })
      .catch(() => {
        if (!cancelled) {
          setProtection({
            targetKey,
            lockedNames: [],
            error: 'Target protection could not be checked. Refresh and try again.',
          })
        }
      })
    return () => {
      cancelled = true
    }
  }, [operation, servers, targetKey])

  const disabledReason = protection.targetKey !== targetKey
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
          icon={<RedoIcon />}
          onClick={onRetry}
          isLoading={retrying}
          isDisabled={retrying || Boolean(disabledReason)}
          aria-label={disabledReason ? `Retry: ${disabledReason}` : 'Retry'}
        >
          Retry
        </Button>
      </span>
    </Tooltip>
  )
}
