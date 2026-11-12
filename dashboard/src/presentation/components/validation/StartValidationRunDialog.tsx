import { useState } from 'react'
import { Button, Field, Stack, Text } from '@chakra-ui/react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { moduleSettingsSpec, moduleSettingsValid } from '@/domain/module/settings'
import type { ValidationSuite } from '@/domain/validation/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { ModuleSettingsFields } from '@/presentation/components/ModuleSettings'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useShowExperimentalSuites } from '@/presentation/hooks/useShowExperimentalSuites'
import { HiddenExperimentalSuites } from './HiddenExperimentalSuites'
import { IMPACT_COPY, offeredSuites } from './validationPresentation'
import { ValidationTargetFields } from './ValidationTargetFields'

/** What the dialog opens with: a suite chosen in the catalog, or a Server's or Platform's own page. */
export interface StartRunPreset {
  suiteId?: string
  serverIds?: string[]
  platformId?: string
}

interface StartValidationRunDialogProps {
  open: boolean
  onClose: () => void
  /**
   * The catalog to choose from; retired suites are left out, and experimental ones unless the admin
   * shows them (the preset suite is always offered).
   */
  suites: readonly ValidationSuite[]
  preset?: StartRunPreset
}

/**
 * Starts one report run of a Validation Suite (`POST /api/v1/validation/runs`): the admin chooses
 * the suite, its targets (`ValidationTargetFields`), and its parameters (the suite's spec schema,
 * through the generic `ModuleSettingsFields`), and confirms a `disruptive` suite's impact, which
 * the API refuses without. On acceptance the run's page opens, where it is followed to its verdict.
 *
 * The dialog keeps its own form state for one opening; the caller remounts it (a new `key`) to
 * start over with another preset. A refusal (a busy, locked, or undeployed target; automation that
 * cannot run) is shown in the dialog with its request id, and the form stays as it was.
 */
export function StartValidationRunDialog({ open, onClose, suites, preset }: StartValidationRunDialogProps) {
  const { validation } = useApp()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [showExperimental] = useShowExperimentalSuites()
  const { offered: choices, hiddenExperimental } = offeredSuites(suites, showExperimental, preset?.suiteId)
  const [suiteId, setSuiteId] = useState(preset?.suiteId ?? '')
  const [serverIds, setServerIds] = useState<string[]>(preset?.serverIds ?? [])
  const [platformId, setPlatformId] = useState(preset?.platformId ?? '')
  const [parameters, setParameters] = useState<Record<string, string>>({})
  const [confirmed, setConfirmed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<DisplayedFailure | null>(null)
  const suite = choices.find((item) => item.id === suiteId) ?? null

  const chooseSuite = (id: string) => {
    setSuiteId(id)
    setParameters({})
    setConfirmed(false)
    setFailure(null)
  }
  const targetsChosen = suite?.target === 'platform' ? platformId !== '' : serverIds.length > 0
  const parametersValid = !suite?.parameters || moduleSettingsValid(suite.parameters, parameters)
  const ready = suite !== null && targetsChosen && parametersValid && (suite.impact !== 'disruptive' || confirmed)

  const submit = async () => {
    if (!suite || !ready) return
    setBusy(true)
    setFailure(null)
    try {
      const run = await validation.startRun({
        suiteId: suite.id,
        serverIds: suite.target === 'servers' ? serverIds : undefined,
        platformId: suite.target === 'platform' ? platformId : undefined,
        parameters: suite.parameters ? moduleSettingsSpec(suite.parameters, parameters) : undefined,
        confirm: suite.impact === 'disruptive' ? confirmed : undefined,
      })
      showToast({ tone: 'success', title: 'Validation Run started', description: suite.name })
      onClose()
      navigate(scopedHref(`/validation/runs/${encodeURIComponent(run.id)}`))
    } catch (caught: unknown) {
      setFailure(failureOf(caught, 'The run was not started.'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Run a Validation Suite"
      size="lg"
      dismissDisabled={busy}
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>Cancel</Button>
          <Button type="submit" colorPalette="brand" loading={busy} disabled={!ready}>Start run</Button>
        </>
      }
    >
      <Stack gap="5">
        <Field.Root required>
          <Field.Label htmlFor="validation-run-suite">Suite <Field.RequiredIndicator /></Field.Label>
          <Select
            id="validation-run-suite"
            aria-label="Suite"
            value={suiteId}
            onChange={chooseSuite}
            placeholder="Choose a suite"
            options={choices.map((item) => ({ value: item.id, label: item.name }))}
            disabled={busy}
          />
          {suite && (
            <Field.HelperText>
              {suite.experimental && 'Experimental: its checks may change. '}
              {suite.description || `${suite.checks.length} checks`} Impact: {IMPACT_COPY[suite.impact].label} — {IMPACT_COPY[suite.impact].description}
            </Field.HelperText>
          )}
          {hiddenExperimental > 0 && <HiddenExperimentalSuites count={hiddenExperimental} />}
        </Field.Root>
        {suite && (
          <ValidationTargetFields
            key={suite.id}
            suite={suite}
            serverIds={serverIds}
            onServerIdsChange={setServerIds}
            platformId={platformId}
            onPlatformIdChange={setPlatformId}
            idPrefix="validation-run"
            disabled={busy}
          />
        )}
        {suite?.parameters && Object.keys(suite.parameters.properties).length > 0 && (
          <Stack gap="3">
            <Text fontWeight="medium" fontSize="sm">Parameters</Text>
            <ModuleSettingsFields
              schema={suite.parameters}
              value={(name) => parameters[name] ?? ''}
              onChange={(name, text) => setParameters((current) => ({ ...current, [name]: text }))}
              idPrefix={`validation-run-${suite.id}`}
              disabled={busy}
            />
          </Stack>
        )}
        {suite?.impact === 'disruptive' && (
          <Checkbox checked={confirmed} onCheckedChange={setConfirmed} disabled={busy}>
            I understand this suite may interrupt service on its targets.
          </Checkbox>
        )}
        {failure && <FailureMessage failure={failure} />}
      </Stack>
    </Modal>
  )
}
