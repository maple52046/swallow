import { useState } from 'react'
import { Button, Field, Input, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { moduleSettingsFormValues, moduleSettingsSpec, moduleSettingsValid } from '@/domain/module/settings'
import type { ValidationSchedule, ValidationScheduleTriggerKind, ValidationSuite } from '@/domain/validation/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { ModuleSettingsFields } from '@/presentation/components/ModuleSettings'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { HiddenExperimentalSuites } from '@/presentation/components/validation/HiddenExperimentalSuites'
import { offeredSuites } from '@/presentation/components/validation/validationPresentation'
import { ValidationTargetFields } from '@/presentation/components/validation/ValidationTargetFields'
import { useShowExperimentalSuites } from '@/presentation/hooks/useShowExperimentalSuites'

interface ValidationScheduleDialogProps {
  open: boolean
  onClose: () => void
  suites: readonly ValidationSuite[]
  /** The schedule to change; absent to create one. */
  schedule?: ValidationSchedule
  /** Called with the schedule the API stored, after the dialog closes. */
  onSaved: (schedule: ValidationSchedule) => void
}

/**
 * Creates or changes a Validation Schedule (`POST`/`PUT /api/v1/validation/schedules`): which suite
 * runs on which targets with which parameters, and when — on a five-field cron expression read in
 * an IANA time zone, or after each deployment of the target. A disabled schedule starts nothing.
 *
 * Only suites a schedule may start are offered: not retired and not `disruptive` (the API refuses
 * those, since each of their runs must be confirmed by a person), and experimental ones only when the
 * admin shows them, except the suite of the schedule being edited. A change sends the generation
 * the page read, so a schedule someone else changed meanwhile is refused (`409`) rather than
 * overwritten; the refusal is shown with its request id and the admin reloads the list. The cron
 * expression and time zone are checked by the API, whose message names the problem. The caller
 * remounts the dialog (a new `key`) for each opening.
 */
export function ValidationScheduleDialog({ open, onClose, suites, schedule, onSaved }: ValidationScheduleDialogProps) {
  const { validation } = useApp()
  const [showExperimental] = useShowExperimentalSuites()
  const spec = schedule?.spec
  const { offered: choices, hiddenExperimental } = offeredSuites(
    suites, showExperimental, spec?.suiteId, (suite) => suite.impact !== 'disruptive',
  )
  const [name, setName] = useState(spec?.name ?? '')
  const [suiteId, setSuiteId] = useState(spec?.suiteId ?? '')
  const [serverIds, setServerIds] = useState<string[]>(spec?.serverIds ?? [])
  const [platformId, setPlatformId] = useState(spec?.platformId ?? '')
  const suite = choices.find((item) => item.id === suiteId) ?? null
  const [parameters, setParameters] = useState<Record<string, string>>(
    suite?.parameters ? moduleSettingsFormValues(suite.parameters, spec?.parameters) : {},
  )
  const [triggerKind, setTriggerKind] = useState<ValidationScheduleTriggerKind>(spec?.trigger.kind ?? 'cron')
  const [cron, setCron] = useState(spec?.trigger.cron ?? '0 2 * * *')
  const [timezone, setTimezone] = useState(spec?.trigger.timezone ?? 'UTC')
  const [enabled, setEnabled] = useState(spec?.enabled ?? true)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<DisplayedFailure | null>(null)

  const chooseSuite = (id: string) => {
    setSuiteId(id)
    setParameters({})
    setFailure(null)
  }
  const targetsChosen = suite?.target === 'platform' ? platformId !== '' : serverIds.length > 0
  const parametersValid = !suite?.parameters || moduleSettingsValid(suite.parameters, parameters)
  const ready = name.trim() !== '' && suite !== null && targetsChosen && parametersValid && (triggerKind !== 'cron' || cron.trim() !== '')

  const submit = async () => {
    if (!suite || !ready) return
    setBusy(true)
    setFailure(null)
    const input = {
      name: name.trim(), suiteId: suite.id,
      serverIds: suite.target === 'servers' ? serverIds : undefined,
      platformId: suite.target === 'platform' ? platformId : undefined,
      parameters: suite.parameters ? moduleSettingsSpec(suite.parameters, parameters) : undefined,
      trigger: triggerKind === 'cron' ? { kind: triggerKind, cron: cron.trim(), timezone: timezone.trim() } : { kind: triggerKind },
      enabled, generation: schedule?.generation,
    }
    try {
      const saved = schedule ? await validation.replaceSchedule(schedule.id, input) : await validation.createSchedule(input)
      onClose()
      onSaved(saved)
    } catch (caught: unknown) {
      setFailure(failureOf(caught, 'The schedule was not saved.'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={schedule ? `Edit ${schedule.spec.name}` : 'New schedule'}
      size="lg"
      dismissDisabled={busy}
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>Cancel</Button>
          <Button type="submit" colorPalette="brand" loading={busy} disabled={!ready}>{schedule ? 'Save schedule' : 'Create schedule'}</Button>
        </>
      }
    >
      <Stack gap="5">
        <Field.Root required>
          <Field.Label htmlFor="validation-schedule-name">Name <Field.RequiredIndicator /></Field.Label>
          <Input id="validation-schedule-name" value={name} onChange={(event) => setName(event.target.value)} disabled={busy} />
        </Field.Root>
        <Field.Root required>
          <Field.Label htmlFor="validation-schedule-suite">Suite <Field.RequiredIndicator /></Field.Label>
          <Select
            id="validation-schedule-suite"
            aria-label="Suite"
            value={suiteId}
            onChange={chooseSuite}
            placeholder="Choose a suite"
            options={choices.map((item) => ({ value: item.id, label: item.name }))}
            disabled={busy}
          />
          <Field.HelperText>A disruptive suite is not offered: each of its runs must be confirmed by a person.</Field.HelperText>
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
            idPrefix="validation-schedule"
            disabled={busy}
          />
        )}
        {suite?.parameters && Object.keys(suite.parameters.properties).length > 0 && (
          <Stack gap="3">
            <Text fontWeight="medium" fontSize="sm">Parameters</Text>
            <ModuleSettingsFields
              schema={suite.parameters}
              value={(setting) => parameters[setting] ?? ''}
              onChange={(setting, text) => setParameters((current) => ({ ...current, [setting]: text }))}
              idPrefix={`validation-schedule-${suite.id}`}
              disabled={busy}
            />
          </Stack>
        )}
        <Field.Root>
          <Field.Label htmlFor="validation-schedule-trigger">When</Field.Label>
          <Select
            id="validation-schedule-trigger"
            aria-label="When"
            value={triggerKind}
            onChange={(value) => setTriggerKind(value === 'after-deployment' ? 'after-deployment' : 'cron')}
            options={[
              { value: 'cron', label: 'On a cron schedule' },
              { value: 'after-deployment', label: 'After each deployment of the target' },
            ]}
            disabled={busy}
          />
          {triggerKind === 'after-deployment' && (
            <Field.HelperText>The deployment that exists when the schedule is saved does not start a run; each later one does.</Field.HelperText>
          )}
        </Field.Root>
        {triggerKind === 'cron' && (
          <Stack gap="4" direction={{ base: 'column', md: 'row' }}>
            <Field.Root required>
              <Field.Label htmlFor="validation-schedule-cron">Cron expression <Field.RequiredIndicator /></Field.Label>
              <Input id="validation-schedule-cron" className="sw-mono" value={cron} onChange={(event) => setCron(event.target.value)} disabled={busy} />
              <Field.HelperText>Minute, hour, day of month, month, day of week. A time missed while swallow was down runs once.</Field.HelperText>
            </Field.Root>
            <Field.Root>
              <Field.Label htmlFor="validation-schedule-timezone">Time zone</Field.Label>
              <Input id="validation-schedule-timezone" value={timezone} onChange={(event) => setTimezone(event.target.value)} disabled={busy} />
              <Field.HelperText>An IANA name, such as Asia/Taipei; UTC when empty.</Field.HelperText>
            </Field.Root>
          </Stack>
        )}
        <Checkbox checked={enabled} onCheckedChange={setEnabled} disabled={busy}>Enabled</Checkbox>
        {failure && <FailureMessage failure={failure} />}
      </Stack>
    </Modal>
  )
}
