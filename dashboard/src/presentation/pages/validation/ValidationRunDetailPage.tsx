import { useCallback, useEffect, useRef, useState } from 'react'
import { Alert, Box, Button, Heading, HStack, Stack, Text, VisuallyHidden } from '@chakra-ui/react'
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
  orderedIssues,
  RUN_STATUS_LABEL,
  runDuration,
  runOutcome,
  runSuiteLabel,
  runTriggerLabel,
  VERDICT_BADGE,
  VERDICT_LABEL,
} from '@/presentation/components/validation/validationPresentation'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { usePlatformNamesById } from '@/presentation/hooks/usePlatformNames'
import { useServerNames } from '@/presentation/hooks/useServerNames'
import { useSettlingRefresh } from '@/presentation/hooks/useSettlingRefresh'
import { formatDateTime } from '@/shared/utils/time'
import { ValidationResultInspector, ValidationResultsMatrix } from './ValidationResultsMatrix'

type PageState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'missing' }
  | { status: 'ready'; run: ValidationRun; refreshError?: string }

/** Issue-first Validation Run workspace with the complete matrix retained for audit. */
export function ValidationRunDetailPage() {
  const { id = '' } = useParams()
  const { validation } = useApp()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [nonce, setNonce] = useState(0)
  const requestKey = `${id}#${nonce}`
  const [stored, setStored] = useState<{ key: string; state: PageState }>({ key: '', state: { status: 'loading' } })
  const state: PageState = stored.key === requestKey ? stored.state : { status: 'loading' }
  const [confirming, setConfirming] = useState(false)
  const [canceling, setCanceling] = useState(false)
  const ticket = useRef(0)

  const refresh = useCallback(async () => {
    const mine = ++ticket.current
    try {
      const run = await validation.getRun(id)
      if (mine === ticket.current) setStored({ key: requestKey, state: run ? { status: 'ready', run } : { status: 'missing' } })
    } catch (caught: unknown) {
      if (mine !== ticket.current) return
      const failure = failureOf(caught, 'Could not refresh the Validation Run.')
      setStored((current) => current.key === requestKey && current.state.status === 'ready'
        ? { key: requestKey, state: { ...current.state, refreshError: failureText(failure) } }
        : { key: requestKey, state: { status: 'error', ...failure } })
      throw caught
    }
  }, [validation, id, requestKey])

  useEffect(() => {
    void refresh().catch(() => undefined)
    return () => { ticket.current += 1 }
  }, [refresh, requestKey])

  const run = state.status === 'ready' ? state.run : null
  useSettlingRefresh(run !== null && isValidationRunActive(run.status), refresh)
  const serverNames = useServerNames(run ? [...run.target.serverIds, ...run.results.filter((result) => result.target.kind === 'server').map((result) => result.target.id)] : [])
  const platformNames = usePlatformNamesById(run?.target.platformId ? [run.target.platformId] : [])
  const targetName = (target: ValidationResultTarget) =>
    target.kind === 'platform' ? `Platform ${platformNames.get(target.id) ?? target.id}` : serverNames.get(target.id) ?? target.id

  const cancel = async () => {
    setCanceling(true)
    try {
      await validation.cancelRun(id)
      showToast({ tone: 'success', title: 'Cancel requested', description: 'The run ends canceled once its workflow stops.' })
      await refresh().catch(() => undefined)
    } catch (caught: unknown) {
      showToast({ tone: 'error', title: 'The run was not canceled', description: failureText(failureOf(caught, '')) })
    } finally {
      setCanceling(false)
      setConfirming(false)
    }
  }

  const breadcrumbs = [{ label: 'Validation', href: scopedHref('/validation/runs') }, { label: 'Run' }]
  if (!run) {
    return (
      <div className="console-page">
        <PageHeader title="Validation Run" breadcrumbs={breadcrumbs} />
        {state.status === 'loading' && <LoadingState rows={6} />}
        {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={() => setNonce((value) => value + 1)} />}
        {state.status === 'missing' && <EmptyState title="Run not found" message="No Validation Run has this id. Runs are kept for 90 days by default." />}
      </div>
    )
  }

  const outcome = runOutcome(run)
  const cancellable = run.mode === 'report' && isValidationRunActive(run.status)
  const target = run.target.platformId
    ? targetName({ kind: 'platform', id: run.target.platformId })
    : run.target.serverIds.map((serverId) => targetName({ kind: 'server', id: serverId })).join(', ')
  const issues = orderedIssues(run.results)
  const checkTitle = (checkId: string) => run.checks.find((check) => check.id === checkId)?.title ?? checkId

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
            <Button asChild variant="outline"><RouterLink to={scopedHref(`/workflows/${encodeURIComponent(run.workflowId)}`, run.siteId)}>View workflow</RouterLink></Button>
            {cancellable && <Button variant="outline" colorPalette="red" onClick={() => setConfirming(true)}>Cancel run</Button>}
          </>
        }
      />
      {state.status === 'ready' && state.refreshError && (
        <Alert.Root status="warning">
          <Alert.Indicator />
          <Alert.Content><Alert.Title>Live update failed</Alert.Title><Alert.Description>{state.refreshError} Showing the last successful read and retrying.</Alert.Description></Alert.Content>
        </Alert.Root>
      )}
      <SectionSurface title="Run summary">
        <KeyValueGrid items={[
          { label: 'Outcome', value: RUN_STATUS_LABEL[run.status] },
          { label: 'Findings', value: countsLabel(run.counts) },
          ...(run.statusReason ? [{ label: 'Status reason', value: run.statusReason }] : []),
          { label: run.target.platformId ? 'Platform' : 'Servers', value: target },
          { label: 'Trigger', value: runTriggerLabel(run) },
          { label: 'Mode', value: run.mode === 'gate' ? 'Deployment gate' : 'Report' },
          { label: 'Suite revision', value: runSuiteLabel(run.suite) },
          { label: 'Requested', value: formatDateTime(run.requestedAt) },
          { label: 'Duration', value: runDuration(run.startedAt, run.finishedAt) },
        ]} />
        {run.mode === 'gate' && isValidationRunActive(run.status) && (
          <Text mt="4" color="fg.muted">This run is part of a deployment gate. Cancel or retry it from the deployment workflow.</Text>
        )}
      </SectionSurface>

      <SectionSurface
        title="Issues found"
        description="Failed means a check completed and found the target outside its expected condition. Error means the check could not complete a judgment."
      >
        {issues.length > 0 ? (
          <Stack gap="4">
            {issues.map((result) => (
              <Box key={`${result.check}:${result.target.kind}:${result.target.id}`} borderLeftWidth="3px" borderColor={result.verdict === 'failed' ? 'red.solid' : 'orange.solid'} ps="4">
                <HStack gap="3" mb="2" wrap="wrap">
                  <Heading as="h3" size="sm">{checkTitle(result.check)}</Heading>
                  <StatusBadge status={VERDICT_BADGE[result.verdict]} label={VERDICT_LABEL[result.verdict]} />
                </HStack>
                <ValidationResultInspector result={result} checkTitle={checkTitle(result.check)} targetName={targetName(result.target)} />
              </Box>
            ))}
          </Stack>
        ) : (
          <Text color="fg.muted">
            {isValidationRunActive(run.status)
              ? 'The run is still executing. Failed checks and execution errors will appear here as they are recorded.'
              : run.verdict === 'passed' ? 'No failed checks or execution errors were recorded.' : 'The run ended without a failed or error result.'}
          </Text>
        )}
      </SectionSurface>

      <SectionSurface
        title="Complete results"
        description="Every check by target, including Passed and Skipped results. Choose a verdict to inspect its measurements and evidence."
      >
        {run.results.length === 0 && !isValidationRunActive(run.status)
          ? <EmptyState title="No results" message="The run ended before any check recorded a result." />
          : <ValidationResultsMatrix run={run} targetName={targetName} />}
      </SectionSurface>

      <ConfirmDialog open={confirming} title="Cancel Validation Run" confirmLabel="Cancel run" busy={canceling} onConfirm={() => void cancel()} onCancel={() => setConfirming(false)}>
        <Text>The run of {run.suite.name} on {target} stops. Results recorded so far remain available, and the run ends canceled without a verdict.</Text>
      </ConfirmDialog>
      <VisuallyHidden aria-live="polite">{`Run ${outcome.label}`}</VisuallyHidden>
    </div>
  )
}
