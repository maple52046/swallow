import { useCallback, useEffect, useRef, useState } from 'react'
import { Badge, Box, Button, Code, Heading, HStack, Link as ChakraLink, Stack, Table, Text } from '@chakra-ui/react'
import { CalendarPlus, Play, ScrollText } from 'lucide-react'
import { Link as RouterLink, useParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Paginated } from '@/application/ports/ServerRepository'
import type { ValidationRunSummary, ValidationSchedule, ValidationSuite } from '@/domain/validation/types'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { KeyValueGrid, SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { useToast } from '@/presentation/components/toast/toastContext'
import { StartValidationRunDialog } from '@/presentation/components/validation/StartValidationRunDialog'
import { ValidationRunList } from '@/presentation/components/validation/ValidationRunList'
import { IMPACT_COPY, scheduleTriggerLabel, suiteSourceLabel } from '@/presentation/components/validation/validationPresentation'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime } from '@/shared/utils/time'
import { UserSuiteDialog } from './UserSuiteDialog'
import { ValidationScheduleDialog } from './ValidationScheduleDialog'

type SuiteState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'missing' }
  | { status: 'ready'; suite: ValidationSuite }

type SecondaryState =
  | { status: 'loading' }
  | {
      status: 'ready'
      runs?: Paginated<ValidationRunSummary>
      schedules?: ValidationSchedule[]
      runsFailure?: DisplayedFailure
      schedulesFailure?: DisplayedFailure
    }

/** Suite identity, checks, parameters, script metadata, recent Runs, and referencing Schedules. */
export function ValidationSuiteDetailPage() {
  const { id = '' } = useParams()
  const { validation } = useApp()
  const { siteId, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [suiteState, setSuiteState] = useState<SuiteState>({ status: 'loading' })
  const [secondary, setSecondary] = useState<SecondaryState>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)
  const [runDialog, setRunDialog] = useState(false)
  const [scheduleDialog, setScheduleDialog] = useState(false)
  const [editDialog, setEditDialog] = useState(false)
  const [retiring, setRetiring] = useState(false)
  const [retireBusy, setRetireBusy] = useState(false)
  const ticket = useRef(0)

  const loadSuite = useCallback(() => {
    const mine = ++ticket.current
    setSuiteState({ status: 'loading' })
    validation.getSuite(id).then(
      (suite) => {
        if (mine === ticket.current) setSuiteState(suite ? { status: 'ready', suite } : { status: 'missing' })
      },
      (caught: unknown) => {
        if (mine === ticket.current) setSuiteState({ status: 'error', ...failureOf(caught, 'Could not load the Validation Suite.') })
      },
    )
  }, [id, validation])

  useEffect(() => {
    loadSuite()
    return () => { ticket.current += 1 }
  }, [loadSuite, nonce])

  const suite = suiteState.status === 'ready' ? suiteState.suite : null
  useEffect(() => {
    if (!suite) return
    let cancelled = false
    setSecondary({ status: 'loading' })
    Promise.allSettled([
      validation.listRuns({ siteId, suiteId: suite.id, page: 1, pageSize: 5 }),
      validation.listSchedules(siteId),
    ]).then(([runs, schedules]) => {
      if (cancelled) return
      setSecondary({
        status: 'ready',
        runs: runs.status === 'fulfilled' ? runs.value : undefined,
        schedules: schedules.status === 'fulfilled' ? schedules.value.filter((item) => item.spec.suiteId === suite.id) : undefined,
        runsFailure: runs.status === 'rejected' ? failureOf(runs.reason, 'Recent runs could not be loaded.') : undefined,
        schedulesFailure: schedules.status === 'rejected' ? failureOf(schedules.reason, 'Schedules could not be loaded.') : undefined,
      })
    })
    return () => { cancelled = true }
  }, [siteId, suite, validation, nonce])

  const retire = async () => {
    if (!suite) return
    setRetireBusy(true)
    try {
      await validation.retireSuite(suite.id)
      showToast({ tone: 'success', title: 'Suite retired', description: suite.name })
      setRetiring(false)
      setNonce((value) => value + 1)
    } catch (caught: unknown) {
      showToast({ tone: 'error', title: 'The suite was not retired', description: failureText(failureOf(caught, '')) })
    } finally {
      setRetireBusy(false)
    }
  }

  const breadcrumbs = [
    { label: 'Validation', href: scopedHref('/validation/suites') },
    { label: 'Suites', href: scopedHref('/validation/suites') },
    { label: suite?.name ?? 'Suite' },
  ]
  if (!suite) {
    return (
      <div className="console-page">
        <PageHeader title="Validation Suite" breadcrumbs={breadcrumbs} />
        {suiteState.status === 'loading' && <LoadingState rows={6} />}
        {suiteState.status === 'error' && <ErrorState message={suiteState.message} requestId={suiteState.requestId} onRetry={() => setNonce((value) => value + 1)} />}
        {suiteState.status === 'missing' && <EmptyState title="Suite not found" message="No Validation Suite has this id, or it is no longer available in this release." />}
      </div>
    )
  }

  const parameters = suite.parameters ? Object.entries(suite.parameters.properties) : []
  const schedules = secondary.status === 'ready' ? secondary.schedules ?? [] : []
  return (
    <div className="console-page">
      <PageHeader
        title={suite.name}
        subtitle={suite.description || 'Validation assurance capability'}
        breadcrumbs={breadcrumbs}
        metadata={
          <HStack gap="2" wrap="wrap">
            {suite.experimental && <Badge colorPalette="orange" variant="subtle">Experimental</Badge>}
            {suite.retired && <Badge colorPalette="gray">Retired</Badge>}
            <Badge variant="outline">{IMPACT_COPY[suite.impact].label}</Badge>
          </HStack>
        }
        stackActionsOnMobile
        actions={!suite.retired ? (
          <>
            <Button colorPalette="brand" onClick={() => setRunDialog(true)}><Play size={16} /> Run suite</Button>
            {suite.impact !== 'disruptive' && (
              <Button variant="outline" onClick={() => setScheduleDialog(true)}><CalendarPlus size={16} /> Create schedule</Button>
            )}
            {suite.source === 'user' && (
              <>
                <Button variant="outline" onClick={() => setEditDialog(true)}>Edit</Button>
                <Button variant="outline" colorPalette="red" onClick={() => setRetiring(true)}>Retire</Button>
              </>
            )}
          </>
        ) : undefined}
      />

      <SectionSurface title="Capability" description="The target, impact, and maximum report-run budget.">
        <KeyValueGrid items={[
          { label: 'Target', value: suite.target === 'platform' ? (suite.platformType ? `${suite.platformType} Platform` : 'Platform') : 'Deployed Servers' },
          { label: 'Impact', value: `${IMPACT_COPY[suite.impact].label} — ${IMPACT_COPY[suite.impact].description}` },
          { label: 'Budget', value: `${suite.budgetSeconds} seconds` },
          { label: 'Source', value: suiteSourceLabel(suite) },
        ]} />
      </SectionSurface>

      <SectionSurface title="Checks" description="Gating checks may stop a deployment when this suite is composed into its Workflow.">
        <Stack gap="3">
          {suite.checks.map((check) => (
            <Box key={check.id} borderBottomWidth="1px" borderColor="border" pb="3" _last={{ borderBottomWidth: 0, pb: 0 }}>
              <HStack gap="2" wrap="wrap">
                <Heading as="h3" size="sm">{check.title}</Heading>
                {check.gating && <Badge variant="outline">Gating</Badge>}
              </HStack>
              <Text color="fg.muted" mt="1">{check.description || 'No additional description.'}</Text>
              <Code mt="2" fontSize="xs">{check.id}</Code>
            </Box>
          ))}
        </Stack>
      </SectionSurface>

      <SectionSurface title="Parameters">
        {parameters.length === 0 ? <Text color="fg.muted">This suite does not require configuration parameters.</Text> : (
          <Stack gap="3">
            {parameters.map(([name, parameter]) => (
              <Box key={name}>
                <HStack gap="2" wrap="wrap">
                  <Text fontWeight="semibold">{parameter.title || name}</Text>
                  <Code fontSize="xs">{name}</Code>
                  {suite.parameters?.required?.includes(name) && <Badge variant="outline">Required</Badge>}
                </HStack>
                <Text color="fg.muted" fontSize="sm">{parameter.description || `Value type: ${parameter.type}`}</Text>
              </Box>
            ))}
          </Stack>
        )}
      </SectionSurface>

      {suite.source === 'user' && (
        <SectionSurface title="Script" description="Scripts are stored as written. Do not put credentials or other secrets in this content.">
          <KeyValueGrid items={[
            { label: 'Revision', value: suite.revision ?? '—' },
            { label: 'Run as', value: suite.runAs ?? '—' },
            { label: 'Timeout', value: suite.timeoutSeconds ? `${suite.timeoutSeconds} seconds` : '—' },
            { label: 'SHA-256', value: <Code fontSize="xs" wordBreak="break-all">{suite.scriptSha256 ?? '—'}</Code> },
          ]} />
          <Box as="details" mt="4">
            <Box as="summary" cursor="pointer" fontWeight="semibold"><HStack as="span" gap="2"><ScrollText size={16} /> View script</HStack></Box>
            <Box as="pre" className="sw-mono" mt="3" p="4" bg="bg.muted" rounded="md" overflowX="auto" whiteSpace="pre-wrap" wordBreak="break-word">{suite.script}</Box>
          </Box>
        </SectionSurface>
      )}

      <SectionSurface
        title="Recent runs"
        actions={<ChakraLink asChild color="brand.fg"><RouterLink to={scopedHref(`/validation/runs?suiteId=${encodeURIComponent(suite.id)}`)}>View all runs</RouterLink></ChakraLink>}
      >
        {secondary.status === 'loading' && <LoadingState rows={3} />}
        {secondary.status === 'ready' && secondary.runsFailure && <FailureMessage failure={secondary.runsFailure} />}
        {secondary.status === 'ready' && secondary.runs?.items.length === 0 && <EmptyState title="No runs yet" message="Run this suite to record its first point-in-time result." />}
        {secondary.status === 'ready' && secondary.runs && secondary.runs.items.length > 0 && (
          <ValidationRunList runs={secondary.runs.items} label="Recent runs of this suite" showSite={!siteId} />
        )}
      </SectionSurface>

      <SectionSurface title="Schedules">
        {secondary.status === 'loading' && <LoadingState rows={2} />}
        {secondary.status === 'ready' && secondary.schedulesFailure && <FailureMessage failure={secondary.schedulesFailure} />}
        {secondary.status === 'ready' && secondary.schedules && schedules.length === 0 && <Text color="fg.muted">No schedule in the current Site scope references this suite.</Text>}
        {secondary.status === 'ready' && schedules.length > 0 && (
          <StickyTableFrame>
            <Table.Root size="sm" aria-label="Schedules using this suite">
              <Table.Header><Table.Row><Table.ColumnHeader>Name</Table.ColumnHeader><Table.ColumnHeader>Cadence</Table.ColumnHeader><Table.ColumnHeader>Updated</Table.ColumnHeader></Table.Row></Table.Header>
              <Table.Body>{schedules.map((schedule) => (
                <Table.Row key={schedule.id}>
                  <Table.Cell><ChakraLink asChild color="brand.fg"><RouterLink to={scopedHref('/validation/schedules', schedule.siteId)}>{schedule.spec.name}</RouterLink></ChakraLink></Table.Cell>
                  <Table.Cell>{scheduleTriggerLabel(schedule.spec.trigger)}</Table.Cell>
                  <Table.Cell>{formatDateTime(schedule.updatedAt)}</Table.Cell>
                </Table.Row>
              ))}</Table.Body>
            </Table.Root>
          </StickyTableFrame>
        )}
      </SectionSurface>

      <StartValidationRunDialog key={runDialog ? 'open' : 'closed'} open={runDialog} onClose={() => setRunDialog(false)} suites={[suite]} preset={{ suiteId: suite.id }} />
      <ValidationScheduleDialog
        key={scheduleDialog ? 'open' : 'closed'}
        open={scheduleDialog}
        onClose={() => setScheduleDialog(false)}
        suites={[suite]}
        presetSuiteId={suite.id}
        onSaved={(saved) => {
          showToast({ tone: 'success', title: 'Schedule created', description: saved.spec.name })
          setNonce((value) => value + 1)
        }}
      />
      <UserSuiteDialog
        key={editDialog ? 'open' : 'closed'}
        open={editDialog}
        onClose={() => setEditDialog(false)}
        suite={suite}
        onSaved={(saved) => {
          showToast({ tone: 'success', title: `Revision ${saved.revision ?? ''} saved`, description: saved.name })
          setNonce((value) => value + 1)
        }}
      />
      <ConfirmDialog open={retiring} title="Retire suite" confirmLabel="Retire suite" busy={retireBusy} onConfirm={() => void retire()} onCancel={() => setRetiring(false)}>
        <Text>{suite.name} leaves the catalog and cannot start new runs. Past runs remain readable.</Text>
      </ConfirmDialog>
    </div>
  )
}
