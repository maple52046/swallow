import { useCallback, useEffect, useRef, useState } from 'react'
import { Button, Text, VisuallyHidden } from '@chakra-ui/react'
import { Link as RouterLink, useParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { isValidationRunActive, type ValidationResultTarget, type ValidationRun } from '@/domain/validation/types'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { KeyValueGrid, SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useToast } from '@/presentation/components/toast/toastContext'
import {
  countsLabel,
  RUN_STATUS_LABEL,
  runOutcome,
  runSuiteLabel,
  runTriggerLabel,
} from '@/presentation/components/validation/validationPresentation'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { usePlatformNamesById } from '@/presentation/hooks/usePlatformNames'
import { useServerNames } from '@/presentation/hooks/useServerNames'
import { useSettlingRefresh } from '@/presentation/hooks/useSettlingRefresh'
import { formatDateTime } from '@/shared/utils/time'
import { ValidationResultsMatrix } from './ValidationResultsMatrix'

type PageState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'missing' }
  | { status: 'ready'; run: ValidationRun }

/**
 * One Validation Run (route `/validation/runs/:id`, decision 081): its suite and targets, what
 * started it, its status and verdict, and every check's verdict on every target
 * (`ValidationResultsMatrix`). The run's Workflow page holds the Task logs and is linked.
 *
 * - The run is read on each visit and re-read every few seconds in place while it has not ended
 *   (`useSettlingRefresh`), so results fill in and the verdict appears without a reload; a failed
 *   re-read keeps what is shown. A run that does not exist (or is past its retention) is a missing
 *   state, not an error.
 * - An unfinished report run can be canceled after a confirmation; the API cancels its Workflow and
 *   the run ends `canceled` with the results recorded so far. A gate run is canceled with its
 *   deployment, so it offers no Cancel.
 * - Targets are shown by name; an id without a name is shown as the id.
 */
export function ValidationRunDetailPage() {
  const { id = '' } = useParams()
  const { validation } = useApp()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [nonce, setNonce] = useState(0)
  const requestKey = `${id}#${nonce}`
  // The answer is kept with the request it answers (the run, or a retry), so another run or a retry
  // reads as loading until it lands.
  const [stored, setStored] = useState<{ key: string; state: PageState }>({ key: '', state: { status: 'loading' } })
  const state: PageState = stored.key === requestKey ? stored.state : { status: 'loading' }
  const [confirming, setConfirming] = useState(false)
  const [canceling, setCanceling] = useState(false)
  const ticket = useRef(0)

  /** Re-reads the run in place; rejects with the read's failure for the caller to handle. */
  const refresh = useCallback(async () => {
    const mine = ++ticket.current
    const run = await validation.getRun(id)
    if (mine === ticket.current) setStored({ key: requestKey, state: run ? { status: 'ready', run } : { status: 'missing' } })
  }, [validation, id, requestKey])

  // The visit's first read and the read after a retry; cleanup drops an answer for a run the
  // user left.
  useEffect(() => {
    const mine = ticket.current + 1
    refresh().catch((caught: unknown) => {
      if (mine === ticket.current) setStored({ key: requestKey, state: { status: 'error', ...failureOf(caught, 'Could not load the Validation Run.') } })
    })
    return () => {
      ticket.current += 1
    }
  }, [refresh, requestKey])

  const run = state.status === 'ready' ? state.run : null
  useSettlingRefresh(run !== null && isValidationRunActive(run.status), refresh)
  const serverNames = useServerNames(run ? [...run.target.serverIds, ...run.results.filter((r) => r.target.kind === 'server').map((r) => r.target.id)] : [])
  const platformNames = usePlatformNamesById(run?.target.platformId ? [run.target.platformId] : [])
  const targetName = (target: ValidationResultTarget) =>
    target.kind === 'platform' ? `Platform ${platformNames.get(target.id) ?? target.id}` : serverNames.get(target.id) ?? target.id

  const cancel = async () => {
    setCanceling(true)
    try {
      await validation.cancelRun(id)
      showToast({ tone: 'success', title: 'Cancel requested', description: 'The run ends canceled once its Workflow stops.' })
      await refresh().catch(() => undefined)
    } catch (caught: unknown) {
      showToast({ tone: 'error', title: 'The run was not canceled', description: failureText(failureOf(caught, '')) })
    } finally {
      setCanceling(false)
      setConfirming(false)
    }
  }

  const breadcrumbs = [{ label: 'Validation', href: scopedHref('/validation/runs') }, { label: 'Run' }]
  if (state.status !== 'ready' || !run) {
    return (
      <div className="console-page">
        <PageHeader title="Validation Run" breadcrumbs={breadcrumbs} />
        {state.status === 'loading' && <LoadingState rows={6} />}
        {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={() => setNonce((value) => value + 1)} />}
        {state.status === 'missing' && (
          <EmptyState title="Run not found" message="No Validation Run has this id. Runs are kept for 90 days by default." />
        )}
      </div>
    )
  }

  const outcome = runOutcome(run)
  const cancellable = run.mode === 'report' && isValidationRunActive(run.status)
  const target = run.target.platformId
    ? targetName({ kind: 'platform', id: run.target.platformId })
    : run.target.serverIds.map((serverId) => targetName({ kind: 'server', id: serverId })).join(', ')
  return (
    <div className="console-page">
      <PageHeader
        title={run.suite.name}
        subtitle={`Validation Run of ${target}`}
        breadcrumbs={breadcrumbs}
        metadata={<StatusBadge status={outcome.status} label={outcome.label} />}
        stackActionsOnMobile
        actions={
          <>
            <Button asChild variant="outline">
              <RouterLink to={scopedHref(`/workflows/${encodeURIComponent(run.workflowId)}`)}>View Workflow</RouterLink>
            </Button>
            {cancellable && (
              <Button variant="outline" colorPalette="red" onClick={() => setConfirming(true)}>Cancel run</Button>
            )}
          </>
        }
      />
      <SectionSurface title="Summary">
        <KeyValueGrid items={[
          { label: 'Status', value: RUN_STATUS_LABEL[run.status] },
          { label: 'Results', value: countsLabel(run.counts) },
          ...(run.statusReason ? [{ label: 'Reason', value: run.statusReason }] : []),
          { label: run.target.platformId ? 'Platform' : 'Servers', value: target },
          ...(run.target.platformId ? [{ label: 'Entered through', value: run.target.serverIds.map((serverId) => targetName({ kind: 'server', id: serverId })).join(', ') || '—' }] : []),
          { label: 'Started by', value: runTriggerLabel(run) },
          { label: 'Mode', value: run.mode === 'gate' ? 'Gate of a deployment' : 'Report' },
          { label: 'Suite', value: runSuiteLabel(run.suite) },
          { label: 'Requested', value: formatDateTime(run.requestedAt) },
          { label: 'Started', value: formatDateTime(run.startedAt ?? undefined) },
          { label: 'Finished', value: formatDateTime(run.finishedAt ?? undefined) },
          ...(Object.keys(run.parameters).length > 0 ? [{
            label: 'Parameters',
            value: <Text as="span" className="sw-mono" fontSize="xs">{JSON.stringify(run.parameters)}</Text>,
          }] : []),
        ]} />
      </SectionSurface>
      <SectionSurface
        title="Results"
        description="Each check’s verdict on each target. Choose a verdict for what the check found; the Workflow has the Task logs."
      >
        {run.results.length === 0 && !isValidationRunActive(run.status) ? (
          <EmptyState title="No results" message="The run ended before any check recorded a result." />
        ) : (
          <ValidationResultsMatrix run={run} targetName={targetName} />
        )}
      </SectionSurface>
      <ConfirmDialog
        open={confirming}
        title="Cancel Validation Run"
        confirmLabel="Cancel run"
        busy={canceling}
        onConfirm={() => void cancel()}
        onCancel={() => setConfirming(false)}
      >
        <Text>
          The run of {run.suite.name} on {target} stops. The results recorded so far are kept, and the run ends canceled without a
          verdict. Start it again to validate again.
        </Text>
      </ConfirmDialog>
      {/* Announces the run's outcome as polling moves it, which the badge alone would not. */}
      <VisuallyHidden aria-live="polite">{`Run ${outcome.label}`}</VisuallyHidden>
    </div>
  )
}
