import { useCallback, useEffect, useRef, useState } from 'react'
import { Button, HStack, Link as ChakraLink, Stack, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { CalendarPlus } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { ValidationSchedule, ValidationSuite } from '@/domain/validation/types'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useToast } from '@/presentation/components/toast/toastContext'
import { scheduleTriggerLabel } from '@/presentation/components/validation/validationPresentation'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { usePlatformNamesById } from '@/presentation/hooks/usePlatformNames'
import { useServerNames } from '@/presentation/hooks/useServerNames'
import { formatDateTime } from '@/shared/utils/time'
import { ValidationHeader } from './ValidationHeader'
import { ValidationScheduleDialog } from './ValidationScheduleDialog'

type PageState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; schedules: ValidationSchedule[]; suites: ValidationSuite[] }

/** The editor open on the page, keyed so each opening starts with a fresh form. */
type OpenEditor = { key: number; schedule?: ValidationSchedule } | null

/**
 * Whether a schedule can start runs, from its `realized` Condition as the worker's
 * `validation-schedule` controller wrote it (contract `validation.md`, Schedules): the reason of a
 * schedule that cannot (disabled, its suite retired, its target gone), or "Not observed yet" before
 * the controller saw its spec. The dashboard derives nothing beyond naming the reason.
 */
function scheduleState(schedule: ValidationSchedule): { status: string; label: string; detail?: string } {
  const realized = schedule.status.conditions.find((condition) => condition.type === 'realized')
  if (!realized || schedule.status.observedGeneration < schedule.generation) return { status: 'pending', label: 'Not observed yet' }
  if (realized.status === 'true') return { status: 'active', label: 'Active' }
  if (realized.reason === 'disabled') return { status: 'canceled', label: 'Disabled' }
  const labels: Record<string, string> = { suite_missing: 'Suite retired', target_missing: 'Target gone' }
  return { status: 'warning', label: labels[realized.reason] ?? realized.reason, detail: realized.message }
}

/**
 * The Schedules tab of Validation (route `/validation/schedules`, decision 081): the Validation
 * Schedules that start runs on a cron expression or after each deployment of their target. Each
 * shows its suite and target, when it runs, whether it can (its `realized` Condition), its next and
 * latest run, and why the latest occurrence was skipped (a busy, locked, or undeployed target).
 * Schedules are read with the suite catalog (to name their suites and to offer suites in the
 * editor) on each visit and after each change. A schedule can be edited or deleted; deleting it
 * keeps its past runs. Every route requires `admin`; a non-admin sees the API's error.
 */
export function ValidationSchedulesPage() {
  const { validation } = useApp()
  const { showToast } = useToast()
  const { scopedHref } = useSiteScope()
  const [state, setState] = useState<PageState>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)
  const [editor, setEditor] = useState<OpenEditor>(null)
  const [deleting, setDeleting] = useState<ValidationSchedule | null>(null)
  const [busy, setBusy] = useState(false)
  const ticket = useRef(0)

  // The schedules and the catalog are read together; only the newest read may publish.
  const load = useCallback(() => {
    const mine = ++ticket.current
    Promise.all([validation.listSchedules(), validation.listSuites(true)])
      .then(([schedules, suites]) => {
        if (mine === ticket.current) setState({ status: 'ready', schedules, suites })
      })
      .catch((caught: unknown) => {
        if (mine === ticket.current) setState({ status: 'error', ...failureOf(caught, 'Could not load the Validation Schedules.') })
      })
  }, [validation])
  useEffect(() => {
    load()
    return () => {
      ticket.current += 1
    }
  }, [load, nonce])

  const schedules = state.status === 'ready' ? state.schedules : []
  const suites = state.status === 'ready' ? state.suites : []
  const serverNames = useServerNames(schedules.flatMap((schedule) => schedule.spec.serverIds))
  const platformNames = usePlatformNamesById(schedules.flatMap((schedule) => (schedule.spec.platformId ? [schedule.spec.platformId] : [])))
  const suiteName = (id: string) => suites.find((suite) => suite.id === id)?.name ?? id
  const targetLabel = (schedule: ValidationSchedule) => schedule.spec.platformId
    ? `Platform ${platformNames.get(schedule.spec.platformId) ?? schedule.spec.platformId}`
    : schedule.spec.serverIds.map((id) => serverNames.get(id) ?? id).join(', ')

  const remove = async () => {
    if (!deleting) return
    setBusy(true)
    try {
      await validation.deleteSchedule(deleting.id)
      showToast({ tone: 'success', title: 'Schedule deleted', description: deleting.spec.name })
    } catch (caught: unknown) {
      showToast({ tone: 'error', title: 'The schedule was not deleted', description: failureText(failureOf(caught, '')) })
    } finally {
      setBusy(false)
      setDeleting(null)
      setNonce((value) => value + 1)
    }
  }

  const lastRun = (schedule: ValidationSchedule) => (
    <Stack gap="0.5">
      {schedule.status.lastRunId ? (
        <ChakraLink asChild color="brand.fg">
          <RouterLink to={scopedHref(`/validation/runs/${encodeURIComponent(schedule.status.lastRunId)}`)}>
            {formatDateTime(schedule.status.lastRunAt ?? undefined)}
          </RouterLink>
        </ChakraLink>
      ) : (
        <Text>None yet</Text>
      )}
      {schedule.status.lastSkipReason && <Text fontSize="xs" color="fg.muted">Skipped: {schedule.status.lastSkipReason}</Text>}
    </Stack>
  )
  const badge = (schedule: ValidationSchedule) => {
    const current = scheduleState(schedule)
    return (
      <Stack gap="0.5" align="flex-start">
        <StatusBadge status={current.status} label={current.label} />
        {current.detail && <Text fontSize="xs" color="fg.muted">{current.detail}</Text>}
      </Stack>
    )
  }
  const actions = (schedule: ValidationSchedule) => (
    <HStack gap="2" justify="flex-end">
      <Button asChild size="xs" variant="ghost">
        <RouterLink to={scopedHref(`/validation/runs?scheduleId=${encodeURIComponent(schedule.id)}`)} aria-label={`Runs of ${schedule.spec.name}`}>Runs</RouterLink>
      </Button>
      <Button size="xs" variant="outline" onClick={() => setEditor({ key: Date.now(), schedule })} aria-label={`Edit ${schedule.spec.name}`}>Edit</Button>
      <Button size="xs" variant="outline" colorPalette="red" onClick={() => setDeleting(schedule)} aria-label={`Delete ${schedule.spec.name}`}>Delete</Button>
    </HStack>
  )

  return (
    <div className="console-page">
      <ValidationHeader
        subtitle="Runs swallow starts on its own: on a schedule, or after each deployment of a target."
        actions={
          <Button colorPalette="brand" onClick={() => setEditor({ key: Date.now() })} disabled={state.status !== 'ready'}>
            <CalendarPlus size={16} aria-hidden /> New schedule
          </Button>
        }
      />
      <SectionSurface title="Schedules">
        {state.status === 'loading' && <LoadingState rows={4} />}
        {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={() => setNonce((value) => value + 1)} />}
        {state.status === 'ready' && schedules.length === 0 && (
          <EmptyState title="No schedules" message="A schedule runs a suite on its own: nightly, weekly, or after each deployment of its target." />
        )}
        {state.status === 'ready' && schedules.length > 0 && (
          <ResponsiveDataView
            desktop={
              <StickyTableFrame>
                <Table.Root size="sm" aria-label="Validation Schedules">
                  <Table.Header>
                    <Table.Row>
                      <Table.ColumnHeader>Schedule</Table.ColumnHeader>
                      <Table.ColumnHeader>Target</Table.ColumnHeader>
                      <Table.ColumnHeader>When</Table.ColumnHeader>
                      <Table.ColumnHeader>State</Table.ColumnHeader>
                      <Table.ColumnHeader>Next run</Table.ColumnHeader>
                      <Table.ColumnHeader>Latest run</Table.ColumnHeader>
                      <Table.ColumnHeader><VisuallyHidden>Actions</VisuallyHidden></Table.ColumnHeader>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {schedules.map((schedule) => (
                      <Table.Row key={schedule.id}>
                        <Table.Cell>
                          <Stack gap="0.5">
                            <Text fontWeight="medium">{schedule.spec.name}</Text>
                            <Text fontSize="xs" color="fg.muted">{suiteName(schedule.spec.suiteId)}</Text>
                          </Stack>
                        </Table.Cell>
                        <Table.Cell>{targetLabel(schedule)}</Table.Cell>
                        <Table.Cell>{scheduleTriggerLabel(schedule.spec.trigger)}</Table.Cell>
                        <Table.Cell>{badge(schedule)}</Table.Cell>
                        <Table.Cell>{formatDateTime(schedule.status.nextRunAt ?? undefined)}</Table.Cell>
                        <Table.Cell>{lastRun(schedule)}</Table.Cell>
                        <Table.Cell textAlign="end">{actions(schedule)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table.Root>
              </StickyTableFrame>
            }
            mobile={
              <div className="sw-resource-card-list" aria-label="Validation Schedules">
                {schedules.map((schedule) => (
                  <ResourceCard
                    key={schedule.id}
                    title={schedule.spec.name}
                    description={suiteName(schedule.spec.suiteId)}
                    status={badge(schedule)}
                    actions={actions(schedule)}
                  >
                    <ResourceCardField label="Target">{targetLabel(schedule)}</ResourceCardField>
                    <ResourceCardField label="When">{scheduleTriggerLabel(schedule.spec.trigger)}</ResourceCardField>
                    <ResourceCardField label="Next run">{formatDateTime(schedule.status.nextRunAt ?? undefined)}</ResourceCardField>
                    <ResourceCardField label="Latest run">{lastRun(schedule)}</ResourceCardField>
                  </ResourceCard>
                ))}
              </div>
            }
          />
        )}
      </SectionSurface>
      <ValidationScheduleDialog
        key={editor?.key ?? 0}
        open={editor !== null}
        onClose={() => setEditor(null)}
        suites={suites}
        schedule={editor?.schedule}
        onSaved={(saved) => {
          showToast({ tone: 'success', title: editor?.schedule ? 'Schedule saved' : 'Schedule created', description: saved.spec.name })
          setNonce((value) => value + 1)
        }}
      />
      <ConfirmDialog
        open={deleting !== null}
        title="Delete schedule"
        confirmLabel="Delete schedule"
        busy={busy}
        onConfirm={() => void remove()}
        onCancel={() => setDeleting(null)}
      >
        <Text>
          {deleting?.spec.name} stops starting runs. The runs it already started stay readable. This cannot be undone; create the schedule
          again to resume.
        </Text>
      </ConfirmDialog>
    </div>
  )
}
