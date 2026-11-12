import { useCallback, useEffect, useRef, useState } from 'react'
import { Badge, Button, HStack, Stack, Switch, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { FilePlus2 } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import type { ValidationSuite } from '@/domain/validation/types'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { useToast } from '@/presentation/components/toast/toastContext'
import { IMPACT_COPY, offeredSuites, suiteSourceLabel } from '@/presentation/components/validation/validationPresentation'
import { StartValidationRunDialog } from '@/presentation/components/validation/StartValidationRunDialog'
import { useShowExperimentalSuites } from '@/presentation/hooks/useShowExperimentalSuites'
import { UserSuiteDialog } from './UserSuiteDialog'
import { ValidationHeader } from './ValidationHeader'

type PageState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; suites: ValidationSuite[] }

/** The dialog open on the page, keyed so each opening starts with a fresh form. */
type OpenDialog =
  | { kind: 'run'; key: number; suiteId: string }
  | { kind: 'edit'; key: number; suite?: ValidationSuite }
  | null

/** What a suite validates, in one phrase. */
function targetLabel(suite: ValidationSuite): string {
  if (suite.target !== 'platform') return 'Deployed Servers'
  return suite.platformType ? `A ${suite.platformType} Platform` : 'A Platform'
}

/** A suite's name, marked in words when it is experimental so the mark never depends on colour. */
function SuiteName({ suite }: { suite: ValidationSuite }) {
  return (
    <HStack gap="2" wrap="wrap">
      <Text fontWeight="medium">{suite.name}</Text>
      {suite.experimental && <Badge colorPalette="orange" variant="subtle">Experimental</Badge>}
    </HStack>
  )
}

/**
 * The Suites tab of Validation (route `/validation/suites`, decision 081): the catalog of
 * Validation Suites — those the build's modules declare (read-only, with the module's version) and
 * the script suites admins wrote. Each suite says what it validates, its impact, and its checks;
 * Run opens the run dialog on it. A script suite can be edited, which makes a new revision, or
 * retired: it leaves the catalog while its past runs stay readable, and the API refuses to retire
 * one a schedule still uses. Every route requires `admin`; a non-admin sees the API's error.
 *
 * Experimental suites (previews whose checks may change) are listed only while the section's
 * "Show experimental suites" switch is on; the switch is the admin's browser preference, read by
 * the run and schedule dialogs too, and its label counts the suites it hides.
 */
export function ValidationSuitesPage() {
  const { validation } = useApp()
  const { showToast } = useToast()
  const [showExperimental, setShowExperimental] = useShowExperimentalSuites()
  const [state, setState] = useState<PageState>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)
  const [dialog, setDialog] = useState<OpenDialog>(null)
  const [retiring, setRetiring] = useState<ValidationSuite | null>(null)
  const [busy, setBusy] = useState(false)
  const ticket = useRef(0)

  // Each read takes a ticket and only the newest publishes; cleanup drops a read the page left.
  const load = useCallback(() => {
    const mine = ++ticket.current
    validation.listSuites()
      .then((suites) => {
        if (mine === ticket.current) setState({ status: 'ready', suites })
      })
      .catch((caught: unknown) => {
        if (mine === ticket.current) setState({ status: 'error', ...failureOf(caught, 'Could not load the Validation Suites.') })
      })
  }, [validation])
  useEffect(() => {
    load()
    return () => {
      ticket.current += 1
    }
  }, [load, nonce])

  const retire = async () => {
    if (!retiring) return
    setBusy(true)
    try {
      await validation.retireSuite(retiring.id)
      showToast({ tone: 'success', title: 'Suite retired', description: retiring.name })
    } catch (caught: unknown) {
      showToast({ tone: 'error', title: 'The suite was not retired', description: failureText(failureOf(caught, '')) })
    } finally {
      setBusy(false)
      setRetiring(null)
      setNonce((value) => value + 1)
    }
  }

  const catalog = state.status === 'ready' ? state.suites : []
  const { offered: suites } = offeredSuites(catalog, showExperimental)
  const experimentalCount = catalog.filter((suite) => suite.experimental && !suite.retired).length
  const actions = (suite: ValidationSuite) => (
    <HStack gap="2" justify="flex-end">
      <Button size="xs" variant="outline" onClick={() => setDialog({ kind: 'run', key: Date.now(), suiteId: suite.id })} aria-label={`Run ${suite.name}`}>
        Run
      </Button>
      {suite.source === 'user' && (
        <>
          <Button size="xs" variant="outline" onClick={() => setDialog({ kind: 'edit', key: Date.now(), suite })} aria-label={`Edit ${suite.name}`}>
            Edit
          </Button>
          <Button size="xs" variant="outline" colorPalette="red" onClick={() => setRetiring(suite)} aria-label={`Retire ${suite.name}`}>
            Retire
          </Button>
        </>
      )}
    </HStack>
  )
  const checks = (suite: ValidationSuite) => suite.checks.map((check) => check.title).join(', ')

  return (
    <div className="console-page">
      <ValidationHeader
        subtitle="What swallow can validate: the suites its modules declare and the scripts you write."
        actions={
          <Button colorPalette="brand" onClick={() => setDialog({ kind: 'edit', key: Date.now() })}>
            <FilePlus2 size={16} aria-hidden /> New script suite
          </Button>
        }
      />
      <SectionSurface
        title="Suites"
        actions={
          <Switch.Root checked={showExperimental} onCheckedChange={(details) => setShowExperimental(details.checked === true)}>
            <Switch.HiddenInput />
            <Switch.Control><Switch.Thumb /></Switch.Control>
            <Switch.Label>
              Show experimental suites{state.status === 'ready' && experimentalCount > 0 ? ` (${experimentalCount})` : ''}
            </Switch.Label>
          </Switch.Root>
        }
      >
        {state.status === 'loading' && <LoadingState rows={5} />}
        {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={() => setNonce((value) => value + 1)} />}
        {state.status === 'ready' && suites.length === 0 && (
          <EmptyState
            title="No Validation Suites"
            message={experimentalCount > 0 && !showExperimental
              ? 'Only experimental suites are available. Turn on Show experimental suites to list them, or write a script suite.'
              : 'This build declares no suite. Write a script suite to validate Servers with your own checks.'}
          />
        )}
        {state.status === 'ready' && suites.length > 0 && (
          <ResponsiveDataView
            desktop={
              <StickyTableFrame>
                <Table.Root size="sm" aria-label="Validation Suites">
                  <Table.Header>
                    <Table.Row>
                      <Table.ColumnHeader>Suite</Table.ColumnHeader>
                      <Table.ColumnHeader>Validates</Table.ColumnHeader>
                      <Table.ColumnHeader>Impact</Table.ColumnHeader>
                      <Table.ColumnHeader>Source</Table.ColumnHeader>
                      <Table.ColumnHeader><VisuallyHidden>Actions</VisuallyHidden></Table.ColumnHeader>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {suites.map((suite) => (
                      <Table.Row key={suite.id}>
                        <Table.Cell maxW="xl">
                          <Stack gap="0.5">
                            <SuiteName suite={suite} />
                            {suite.description && <Text fontSize="xs" color="fg.muted">{suite.description}</Text>}
                            <Text fontSize="xs" color="fg.muted">Checks: {checks(suite)}</Text>
                          </Stack>
                        </Table.Cell>
                        <Table.Cell>{targetLabel(suite)}</Table.Cell>
                        <Table.Cell>{IMPACT_COPY[suite.impact].label}</Table.Cell>
                        <Table.Cell>{suiteSourceLabel(suite)}</Table.Cell>
                        <Table.Cell textAlign="end">{actions(suite)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table.Root>
              </StickyTableFrame>
            }
            mobile={
              <div className="sw-resource-card-list" aria-label="Validation Suites">
                {suites.map((suite) => (
                  <ResourceCard key={suite.id} title={<SuiteName suite={suite} />} description={suite.description} actions={actions(suite)}>
                    <ResourceCardField label="Validates">{targetLabel(suite)}</ResourceCardField>
                    <ResourceCardField label="Impact">{IMPACT_COPY[suite.impact].label}</ResourceCardField>
                    <ResourceCardField label="Source">{suiteSourceLabel(suite)}</ResourceCardField>
                    <ResourceCardField label="Checks">{checks(suite)}</ResourceCardField>
                  </ResourceCard>
                ))}
              </div>
            }
          />
        )}
      </SectionSurface>
      <StartValidationRunDialog
        key={dialog?.kind === 'run' ? dialog.key : 'run'}
        open={dialog?.kind === 'run'}
        onClose={() => setDialog(null)}
        suites={catalog}
        preset={{ suiteId: dialog?.kind === 'run' ? dialog.suiteId : undefined }}
      />
      <UserSuiteDialog
        key={dialog?.kind === 'edit' ? dialog.key : 'edit'}
        open={dialog?.kind === 'edit'}
        onClose={() => setDialog(null)}
        suite={dialog?.kind === 'edit' ? dialog.suite : undefined}
        onSaved={(saved) => {
          showToast({ tone: 'success', title: saved.revision && saved.revision > 1 ? `Revision ${saved.revision} saved` : 'Suite created', description: saved.name })
          setNonce((value) => value + 1)
        }}
      />
      <ConfirmDialog
        open={retiring !== null}
        title="Retire suite"
        confirmLabel="Retire suite"
        busy={busy}
        onConfirm={() => void retire()}
        onCancel={() => setRetiring(null)}
      >
        <Text>
          {retiring?.name} leaves the catalog and can no longer be run. Its past runs stay readable. A suite a schedule still uses cannot be
          retired: delete or change the schedule first.
        </Text>
      </ConfirmDialog>
    </div>
  )
}
