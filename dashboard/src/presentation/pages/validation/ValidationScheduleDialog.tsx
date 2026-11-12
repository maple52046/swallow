import { useState } from 'react'
import { Button, Field, Input, Stack, Switch, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { moduleSettingsFormValues, moduleSettingsSpec, moduleSettingsValid } from '@/domain/module/settings'
import type { ValidationSchedule, ValidationSuite } from '@/domain/validation/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { ModuleSettingsFields } from '@/presentation/components/ModuleSettings'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { HiddenExperimentalSuites } from '@/presentation/components/validation/HiddenExperimentalSuites'
import {
  cronForSchedulePreset,
  offeredSuites,
  schedulePreset,
  type SchedulePresetKind,
} from '@/presentation/components/validation/validationPresentation'
import { ValidationTargetFields } from '@/presentation/components/validation/ValidationTargetFields'
import { useShowExperimentalSuites } from '@/presentation/hooks/useShowExperimentalSuites'

interface ValidationScheduleDialogProps {
  open: boolean
  onClose: () => void
  suites: readonly ValidationSuite[]
  schedule?: ValidationSchedule
  /** Preselects a suite when creation starts from Suite detail. */
  presetSuiteId?: string
  onSaved: (schedule: ValidationSchedule) => void
}

const WEEKDAYS = [
  { value: '1', label: 'Monday' },
  { value: '2', label: 'Tuesday' },
  { value: '3', label: 'Wednesday' },
  { value: '4', label: 'Thursday' },
  { value: '5', label: 'Friday' },
  { value: '6', label: 'Saturday' },
  { value: '0', label: 'Sunday' },
]

/**
 * Creates or replaces a Validation Schedule with friendly deployment, daily, and weekly presets.
 * An existing cron expression that does not match a preset stays byte-for-byte in Custom until the
 * admin edits it. Replacements carry the generation the dialog read.
 */
export function ValidationScheduleDialog({
  open,
  onClose,
  suites,
  schedule,
  presetSuiteId,
  onSaved,
}: ValidationScheduleDialogProps) {
  const { validation } = useApp()
  const [showExperimental] = useShowExperimentalSuites()
  const spec = schedule?.spec
  const initialPreset = schedulePreset(spec?.trigger ?? { kind: 'cron', cron: '0 2 * * *', timezone: 'UTC' })
  const { offered: choices, hiddenExperimental } = offeredSuites(
    suites, showExperimental, spec?.suiteId ?? presetSuiteId, (suite) => suite.impact !== 'disruptive',
  )
  const [name, setName] = useState(spec?.name ?? '')
  const [suiteId, setSuiteId] = useState(spec?.suiteId ?? presetSuiteId ?? '')
  const [serverIds, setServerIds] = useState<string[]>(spec?.serverIds ?? [])
  const [platformId, setPlatformId] = useState(spec?.platformId ?? '')
  const suite = choices.find((item) => item.id === suiteId) ?? null
  const [parameters, setParameters] = useState<Record<string, string>>(
    suite?.parameters ? moduleSettingsFormValues(suite.parameters, spec?.parameters) : {},
  )
  const [presetKind, setPresetKind] = useState<SchedulePresetKind>(initialPreset.kind)
  const [time, setTime] = useState(`${initialPreset.hour.padStart(2, '0')}:${initialPreset.minute.padStart(2, '0')}`)
  const [weekday, setWeekday] = useState(initialPreset.weekday)
  const [customCron, setCustomCron] = useState(initialPreset.cron)
  const [timezone, setTimezone] = useState(spec?.trigger.timezone ?? 'UTC')
  const [enabled, setEnabled] = useState(spec?.enabled ?? true)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<DisplayedFailure | null>(null)

  const chooseSuite = (id: string) => {
    setSuiteId(id)
    setParameters({})
    setFailure(null)
  }
  const [hour, minute] = time.split(':')
  const cron = cronForSchedulePreset({
    kind: presetKind,
    hour: String(Number(hour || 0)),
    minute: String(Number(minute || 0)),
    weekday,
    cron: customCron,
  })
  const targetsChosen = suite?.target === 'platform' ? platformId !== '' : serverIds.length > 0
  const parametersValid = !suite?.parameters || moduleSettingsValid(suite.parameters, parameters)
  const scheduleValid = presetKind === 'after-deployment' || (presetKind === 'custom' ? customCron.trim() !== '' : /^\d\d:\d\d$/.test(time))
  const ready = name.trim() !== '' && suite !== null && targetsChosen && parametersValid && scheduleValid

  const submit = async () => {
    if (!suite || !ready) return
    setBusy(true)
    setFailure(null)
    const input = {
      name: name.trim(),
      suiteId: suite.id,
      serverIds: suite.target === 'servers' ? serverIds : undefined,
      platformId: suite.target === 'platform' ? platformId : undefined,
      parameters: suite.parameters ? moduleSettingsSpec(suite.parameters, parameters) : undefined,
      trigger: presetKind === 'after-deployment'
        ? { kind: 'after-deployment' as const }
        : { kind: 'cron' as const, cron: cron.trim(), timezone: timezone.trim() || 'UTC' },
      enabled,
      generation: schedule?.generation,
    }
    try {
      const saved = schedule ? await validation.replaceSchedule(schedule.id, input) : await validation.createSchedule(input)
      onClose()
      onSaved(saved)
    } catch (caught: unknown) {
      setFailure(failureOf(caught, schedule
        ? 'The schedule was not saved. It may have changed; reload before trying again.'
        : 'The schedule was not created.'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={schedule ? `Edit ${schedule.spec.name}` : 'Create schedule'}
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
            disabled={busy || Boolean(presetSuiteId)}
          />
          <Field.HelperText>Disruptive suites are not schedulable because each run requires confirmation.</Field.HelperText>
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
            value={presetKind}
            onChange={(value) => setPresetKind(value as SchedulePresetKind)}
            options={[
              { value: 'after-deployment', label: 'After each deployment' },
              { value: 'daily', label: 'Daily' },
              { value: 'weekly', label: 'Weekly' },
              { value: 'custom', label: 'Custom cron' },
            ]}
            disabled={busy}
          />
          {presetKind === 'after-deployment' && <Field.HelperText>Starts after each future deployment of the target.</Field.HelperText>}
        </Field.Root>
        {(presetKind === 'daily' || presetKind === 'weekly') && (
          <Stack direction={{ base: 'column', md: 'row' }} gap="4">
            {presetKind === 'weekly' && (
              <Field.Root>
                <Field.Label htmlFor="validation-schedule-weekday">Day</Field.Label>
                <Select id="validation-schedule-weekday" aria-label="Day" value={weekday} onChange={setWeekday} options={WEEKDAYS} disabled={busy} />
              </Field.Root>
            )}
            <Field.Root>
              <Field.Label htmlFor="validation-schedule-time">Time</Field.Label>
              <Input id="validation-schedule-time" type="time" value={time} onChange={(event) => setTime(event.target.value)} disabled={busy} />
            </Field.Root>
          </Stack>
        )}
        {presetKind === 'custom' && (
          <Field.Root required>
            <Field.Label htmlFor="validation-schedule-cron">Cron expression <Field.RequiredIndicator /></Field.Label>
            <Input id="validation-schedule-cron" className="sw-mono" value={customCron} onChange={(event) => setCustomCron(event.target.value)} disabled={busy} />
            <Field.HelperText>Five fields: minute, hour, day of month, month, day of week.</Field.HelperText>
          </Field.Root>
        )}
        {presetKind !== 'after-deployment' && (
          <Field.Root>
            <Field.Label htmlFor="validation-schedule-timezone">Time zone</Field.Label>
            <Input id="validation-schedule-timezone" value={timezone} onChange={(event) => setTimezone(event.target.value)} disabled={busy} />
            <Field.HelperText>An IANA time zone, such as Asia/Taipei. Defaults to UTC.</Field.HelperText>
          </Field.Root>
        )}
        <Switch.Root checked={enabled} onCheckedChange={(details) => setEnabled(details.checked === true)} disabled={busy}>
          <Switch.HiddenInput />
          <Switch.Control><Switch.Thumb /></Switch.Control>
          <Switch.Label>Enabled</Switch.Label>
        </Switch.Root>
        {failure && <FailureMessage failure={failure} />}
      </Stack>
    </Modal>
  )
}
