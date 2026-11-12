import { useCallback, useEffect, useRef, useState } from 'react'
import { Box, Button, HStack, Link as ChakraLink, Stack, Switch, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { CalendarPlus, RefreshCw } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { ValidationSchedule, ValidationSuite } from '@/domain/validation/types'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { InventorySurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
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

type OpenEditor = { key: number; schedule?: ValidationSchedule } | null

function scheduleState(schedule: ValidationSchedule): { status: string; label: string; detail?: string } {
  const realized = schedule.status.conditions.find((condition) => condition.type === 'realized')
  if (!realized || schedule.status.observedGeneration < schedule.generation) return { status: 'pending', label: 'Applying' }
  if (realized.status === 'true') return { status: 'active', label: 'Active' }
  if (realized.reason === 'disabled') return { status: 'canceled', label: 'Disabled' }
  const labels: Record<string, string> = { suite_missing: 'Suite retired', target_missing: 'Target gone' }
  return { status: 'warning', label: labels[realized.reason] ?? realized.reason, detail: realized.message }
}

/** Site-scoped Validation automation inventory with generation-safe inline enablement. */
export function ValidationSchedulesPage() {
  const { validation } = useApp()
  const { showToast } = useToast()
  const { siteId, sites, scopedHref } = useSiteScope()
  const [state, setState] = useState<PageState>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)
  const [editor, setEditor] = useState<OpenEditor>(null)
  const [deleting, setDeleting] = useState<ValidationSchedule | null>(null)
  const [busy, setBusy] = useState(false)
  const [toggling, setToggling] = useState<string | null>(null)
  const ticket = useRef(0)

  const load = useCallback(() => {
    const mine = ++ticket.current
    setState({ status: 'loading' })
    Promise.all([validation.listSchedules(siteId), validation.listSuites(true)]).then(
      ([schedules, suites]) => {
        if (mine === ticket.current) setState({ status: 'ready', schedules, suites })
      },
      (caught: unknown) => {
        if (mine === ticket.current) setState({ status: 'error', ...failureOf(caught, 'Could not load the Validation Schedules.') })
      },
    )
  }, [siteId, validation])

  useEffect(() => {
    load()
    return () => { ticket.current += 1 }
  }, [load, nonce])

  const schedules = state.status === 'ready' ? state.schedules : []
  const suites = state.status === 'ready' ? state.suites : []
  const siteName = new Map(sites.map((site) => [site.id, site.name]))
  const serverNames = useServerNames(schedules.flatMap((schedule) => schedule.spec.serverIds))
  const platformNames = usePlatformNamesById(schedules.flatMap((schedule) => schedule.spec.platformId ? [schedule.spec.platformId] : []))
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

  const setEnabled = async (schedule: ValidationSchedule, enabled: boolean) => {
    setToggling(schedule.id)
    try {
      await validation.replaceSchedule(schedule.id, {
        name: schedule.spec.name,
        suiteId: schedule.spec.suiteId,
        serverIds: schedule.spec.serverIds,
        platformId: schedule.spec.platformId ?? undefined,
        parameters: schedule.spec.parameters,
        trigger: schedule.spec.trigger.kind === 'cron'
          ? { kind: 'cron', cron: schedule.spec.trigger.cron ?? '', timezone: schedule.spec.trigger.timezone ?? 'UTC' }
          : { kind: 'after-deployment' },
        enabled,
        generation: schedule.generation,
      })
      setNonce((value) => value + 1)
    } catch (caught: unknown) {
      showToast({
        tone: 'error',
        title: 'Schedule state was not changed',
        description: `${failureText(failureOf(caught, 'The schedule may have changed.'))} Reloading the current generation.`,
      })
      setNonce((value) => value + 1)
    } finally {
      setToggling(null)
    }
  }

  const lastRun = (schedule: ValidationSchedule) => (
    <Stack gap="0.5">
      {schedule.status.lastRunId ? (
        <ChakraLink asChild color="brand.fg">
          <RouterLink to={scopedHref(`/validation/runs/${encodeURIComponent(schedule.status.lastRunId)}`, schedule.siteId)}>
            {formatDateTime(schedule.status.lastRunAt ?? undefined)}
          </RouterLink>
        </ChakraLink>
      ) : <Text>None yet</Text>}
      {schedule.status.lastSkipReason && <Text fontSize="xs" color="fg.muted">Latest occurrence skipped: {schedule.status.lastSkipReason}</Text>}
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
        <RouterLink to={scopedHref(`/validation/runs?scheduleId=${encodeURIComponent(schedule.id)}`, schedule.siteId)}>Runs</RouterLink>
      </Button>
      <Button size="xs" variant="outline" onClick={() => setEditor({ key: Date.now(), schedule })}>Edit</Button>
      <Button size="xs" variant="outline" colorPalette="red" onClick={() => setDeleting(schedule)}>Delete</Button>
    </HStack>
  )
  const enabledSwitch = (schedule: ValidationSchedule) => (
    <Switch.Root
      checked={schedule.spec.enabled}
      onCheckedChange={(details) => void setEnabled(schedule, details.checked === true)}
      disabled={toggling === schedule.id}
    >
      <Switch.HiddenInput />
      <Switch.Control><Switch.Thumb /></Switch.Control>
      <Switch.Label>{schedule.spec.enabled ? 'Enabled' : 'Disabled'}</Switch.Label>
    </Switch.Root>
  )

  return (
    <div className="console-page">
      <ValidationHeader
        subtitle="Automate assurance after deployments or on a recurring cadence without turning one skipped occurrence into a failed schedule."
        actions={
          <HStack gap="2">
            <Button variant="outline" onClick={() => setNonce((value) => value + 1)}><RefreshCw size={16} /> Refresh</Button>
            <Button colorPalette="brand" onClick={() => setEditor({ key: Date.now() })} disabled={state.status !== 'ready'}>
              <CalendarPlus size={16} /> Create schedule
            </Button>
          </HStack>
        }
      />
      <InventorySurface
        headingId="validation-schedules-title"
        eyebrow="Validation automation"
        title="Assurance schedules"
        summary={state.status === 'ready' ? `${schedules.length} schedules in the current Site scope` : 'Loading schedules'}
      >
        {state.status === 'loading' && <Box p="4"><LoadingState rows={4} /></Box>}
        {state.status === 'error' && <Box p="4"><ErrorState message={state.message} requestId={state.requestId} onRetry={() => setNonce((value) => value + 1)} /></Box>}
        {state.status === 'ready' && schedules.length === 0 && (
          <Box p="4"><EmptyState title="No schedules" message="Create a daily, weekly, custom cron, or after-deployment assurance schedule." /></Box>
        )}
        {state.status === 'ready' && schedules.length > 0 && (
          <ResponsiveDataView
            desktop={
              <StickyTableFrame>
                <Table.Root size="sm" aria-label="Validation Schedules">
                  <Table.Header><Table.Row>
                    <Table.ColumnHeader>Schedule</Table.ColumnHeader>
                    <Table.ColumnHeader>Target</Table.ColumnHeader>
                    <Table.ColumnHeader>Cadence</Table.ColumnHeader>
                    <Table.ColumnHeader>Enabled</Table.ColumnHeader>
                    <Table.ColumnHeader>Controller</Table.ColumnHeader>
                    <Table.ColumnHeader>Next run</Table.ColumnHeader>
                    <Table.ColumnHeader>Latest run</Table.ColumnHeader>
                    <Table.ColumnHeader><VisuallyHidden>Actions</VisuallyHidden></Table.ColumnHeader>
                  </Table.Row></Table.Header>
                  <Table.Body>{schedules.map((schedule) => (
                    <Table.Row key={schedule.id}>
                      <Table.Cell><Stack gap="0.5"><Text fontWeight="medium">{schedule.spec.name}</Text><Text fontSize="xs" color="fg.muted">{suiteName(schedule.spec.suiteId)}</Text>{!siteId && <Text fontSize="xs" color="fg.muted">Site: {siteName.get(schedule.siteId) ?? schedule.siteId}</Text>}</Stack></Table.Cell>
                      <Table.Cell>{targetLabel(schedule)}</Table.Cell>
                      <Table.Cell>{scheduleTriggerLabel(schedule.spec.trigger)}</Table.Cell>
                      <Table.Cell>{enabledSwitch(schedule)}</Table.Cell>
                      <Table.Cell>{badge(schedule)}</Table.Cell>
                      <Table.Cell>{formatDateTime(schedule.status.nextRunAt ?? undefined)}</Table.Cell>
                      <Table.Cell>{lastRun(schedule)}</Table.Cell>
                      <Table.Cell textAlign="end">{actions(schedule)}</Table.Cell>
                    </Table.Row>
                  ))}</Table.Body>
                </Table.Root>
              </StickyTableFrame>
            }
            mobile={
              <div className="sw-resource-card-list" aria-label="Validation Schedules">
                {schedules.map((schedule) => (
                  <ResourceCard key={schedule.id} title={schedule.spec.name} description={suiteName(schedule.spec.suiteId)} status={badge(schedule)} actions={actions(schedule)}>
                    {!siteId && <ResourceCardField label="Site">{siteName.get(schedule.siteId) ?? schedule.siteId}</ResourceCardField>}
                    <ResourceCardField label="Target">{targetLabel(schedule)}</ResourceCardField>
                    <ResourceCardField label="Cadence">{scheduleTriggerLabel(schedule.spec.trigger)}</ResourceCardField>
                    <ResourceCardField label="Enabled">{enabledSwitch(schedule)}</ResourceCardField>
                    <ResourceCardField label="Next run">{formatDateTime(schedule.status.nextRunAt ?? undefined)}</ResourceCardField>
                    <ResourceCardField label="Latest run">{lastRun(schedule)}</ResourceCardField>
                  </ResourceCard>
                ))}
              </div>
            }
          />
        )}
      </InventorySurface>
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
      <ConfirmDialog open={deleting !== null} title="Delete schedule" confirmLabel="Delete schedule" busy={busy} onConfirm={() => void remove()} onCancel={() => setDeleting(null)}>
        <Text>{deleting?.spec.name} stops starting runs. Runs it already started remain readable.</Text>
      </ConfirmDialog>
    </div>
  )
}
