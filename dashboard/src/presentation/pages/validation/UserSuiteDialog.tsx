import { useState } from 'react'
import { Button, Field, Input, Stack, Switch, Text, Textarea } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { suiteStarterLevel } from '@/domain/validation/access'
import {
  ValidationConflictError,
  type ValidationImpact,
  type ValidationStarterLevel,
  type ValidationSuite,
} from '@/domain/validation/types'
import { ChoiceCardGroup } from '@/presentation/components/ChoiceCardGroup'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { IMPACT_COPY, STARTER_LEVEL_COPY, SUDO_COPY } from '@/presentation/components/validation/validationPresentation'

/** The API's bounds on a script's timeout (validation.md, Suites). */
const MAX_TIMEOUT_SECONDS = 3600
const DEFAULT_TIMEOUT_SECONDS = 600
/** The API's bound on a script's size: 64 KiB. */
const MAX_SCRIPT_BYTES = 64 * 1024

interface UserSuiteDialogProps {
  open: boolean
  onClose: () => void
  /** The user suite to revise, which the signed-in User wrote; absent to create one. */
  suite?: ValidationSuite
  /** Called with the suite the API stored, after the dialog closes. */
  onSaved: (suite: ValidationSuite) => void
  /**
   * Called when a save was refused because the suite changed since the dialog opened, after the
   * dialog closes: the caller reads the suite again, so its author edits the new revision.
   */
  onReload?: () => void
}

/**
 * Creates or revises a user-defined Validation Suite (`POST`/`PUT /api/v1/validation/suites`): a
 * script the run executes on each target Server as the account of the User who starts it, whose
 * exit code is its one check's verdict — 0 passes, any other code fails, and running past the
 * timeout is an error. Any User may create one and becomes its author; only the author revises it
 * (the caller offers Edit only to them, and the API enforces it again). The author chooses whether it
 * runs through the starter's sudo, who may start it, its timeout, and its impact; changing any of
 * them, or the script, makes a new revision, and runs already accepted keep theirs.
 *
 * A revision is saved only at the revision the dialog opened (`revision` in the body). When the API
 * refuses it (`409`), the dialog says the suite changed and offers to reload it, never to overwrite.
 * The script is stored and shown to every User as written, so the dialog warns against secrets, and
 * it says that whoever reads a run reads its output. Client checks (a name, a non-empty script
 * within 64 KiB, a timeout in range) only spare a round trip: other refusals are shown with their
 * request id. The caller remounts the dialog (a new `key`) for each opening.
 */
export function UserSuiteDialog({ open, onClose, suite, onSaved, onReload }: UserSuiteDialogProps) {
  const { validation } = useApp()
  const [name, setName] = useState(suite?.name ?? '')
  const [description, setDescription] = useState(suite?.description ?? '')
  const [script, setScript] = useState(suite?.script ?? '#!/bin/sh\n')
  const [sudo, setSudo] = useState(suite?.sudo === true)
  const [starterLevel, setStarterLevel] = useState<ValidationStarterLevel>(suite ? suiteStarterLevel(suite) : 'owner')
  const [timeoutText, setTimeoutText] = useState(String(suite?.timeoutSeconds ?? DEFAULT_TIMEOUT_SECONDS))
  const [impact, setImpact] = useState<ValidationImpact>(suite?.impact ?? 'workload')
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<DisplayedFailure | null>(null)
  const [stale, setStale] = useState<DisplayedFailure | null>(null)

  const timeoutSeconds = Number(timeoutText)
  const timeoutValid = Number.isInteger(timeoutSeconds) && timeoutSeconds >= 1 && timeoutSeconds <= MAX_TIMEOUT_SECONDS
  const scriptBytes = new TextEncoder().encode(script).length
  const scriptValid = script.trim() !== '' && scriptBytes <= MAX_SCRIPT_BYTES
  const ready = name.trim() !== '' && scriptValid && timeoutValid && stale === null

  const submit = async () => {
    if (!ready) return
    setBusy(true)
    setFailure(null)
    const input = {
      name: name.trim(), description: description.trim(), script, sudo, starterLevel, timeoutSeconds, impact,
      revision: suite?.revision ?? undefined,
    }
    try {
      const saved = suite ? await validation.replaceSuite(suite.id, input) : await validation.createSuite(input)
      onClose()
      onSaved(saved)
    } catch (caught: unknown) {
      if (suite && caught instanceof ValidationConflictError) setStale(failureOf(caught, ''))
      else setFailure(failureOf(caught, 'The suite was not saved.'))
    } finally {
      setBusy(false)
    }
  }

  const reload = () => {
    onClose()
    onReload?.()
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={suite ? `Edit ${suite.name}` : 'New script suite'}
      description="The script runs on each target Server as the account of the User who starts the run; exit code 0 passes, any other code fails."
      size="xl"
      dismissDisabled={busy}
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>Cancel</Button>
          {stale
            ? <Button colorPalette="brand" onClick={reload}>Reload the suite</Button>
            : <Button type="submit" colorPalette="brand" loading={busy} disabled={!ready}>{suite ? 'Save revision' : 'Create suite'}</Button>}
        </>
      }
    >
      <Stack gap="4">
        {stale && (
          <Alert status="warning" title="The suite changed since you opened it">
            A newer revision than revision {suite?.revision} was saved, or the suite was retired, while this editor was open. Your
            change was not saved and nothing was overwritten. Reload the suite to see where it is now, then edit that.
            {stale.requestId ? ` Request ID: ${stale.requestId}` : ''}
          </Alert>
        )}
        <Field.Root required>
          <Field.Label htmlFor="user-suite-name">Name <Field.RequiredIndicator /></Field.Label>
          <Input id="user-suite-name" value={name} onChange={(event) => setName(event.target.value)} disabled={busy} />
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="user-suite-description">Description</Field.Label>
          <Input id="user-suite-description" value={description} onChange={(event) => setDescription(event.target.value)} disabled={busy} />
        </Field.Root>
        <Field.Root required invalid={!scriptValid}>
          <Field.Label htmlFor="user-suite-script">Script <Field.RequiredIndicator /></Field.Label>
          <Textarea
            id="user-suite-script"
            className="sw-mono"
            fontSize="sm"
            rows={10}
            spellCheck={false}
            value={script}
            onChange={(event) => setScript(event.target.value)}
            disabled={busy}
          />
          {scriptValid ? (
            <Field.HelperText>Starts with an interpreter line such as #!/bin/bash; without one it runs under /bin/sh. The last 8 KiB of its output is kept as evidence.</Field.HelperText>
          ) : (
            <Field.ErrorText>{script.trim() === '' ? 'Enter the script to run.' : 'The script is larger than 64 KiB.'}</Field.ErrorText>
          )}
        </Field.Root>
        <Alert status="warning" title="Do not put secrets in the script, and expect its output to be read">
          Every User reads the script as written. Its output becomes the run’s evidence, which everyone who can read the run
          reads.
        </Alert>
        <Field.Root>
          <Switch.Root checked={sudo} onCheckedChange={(details) => setSudo(details.checked === true)} disabled={busy}>
            <Switch.HiddenInput />
            <Switch.Control><Switch.Thumb /></Switch.Control>
            <Switch.Label>Run with sudo</Switch.Label>
          </Switch.Root>
          <Field.HelperText>{SUDO_COPY[sudo ? 'on' : 'off']}</Field.HelperText>
        </Field.Root>
        <Stack gap="2">
          <Text fontWeight="medium" fontSize="sm" aria-hidden>Who can start</Text>
          <ChoiceCardGroup
            label="Who can start"
            value={starterLevel}
            onChange={setStarterLevel}
            disabled={busy}
            options={(['owner', 'member'] as const).map((level) => ({
              value: level, label: STARTER_LEVEL_COPY[level].label, description: STARTER_LEVEL_COPY[level].description,
            }))}
          />
          <Text fontSize="sm" color="fg.muted">The script runs on each target as the account of whoever starts it, with that User’s rights there.</Text>
        </Stack>
        <Field.Root invalid={!timeoutValid}>
          <Field.Label htmlFor="user-suite-timeout">Timeout (seconds)</Field.Label>
          <Input id="user-suite-timeout" type="number" min={1} max={MAX_TIMEOUT_SECONDS} value={timeoutText} onChange={(event) => setTimeoutText(event.target.value)} disabled={busy} />
          {timeoutValid
            ? <Field.HelperText>A run past it is stopped and its verdict is an error.</Field.HelperText>
            : <Field.ErrorText>Enter whole seconds from 1 to {MAX_TIMEOUT_SECONDS}.</Field.ErrorText>}
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="user-suite-impact">Impact</Field.Label>
          <Select
            id="user-suite-impact"
            aria-label="Impact"
            value={impact}
            onChange={(value) => setImpact(value === 'observe' || value === 'disruptive' ? value : 'workload')}
            options={(['observe', 'workload', 'disruptive'] as const).map((value) => ({ value, label: IMPACT_COPY[value].label }))}
            disabled={busy}
          />
          <Field.HelperText>{IMPACT_COPY[impact].description}</Field.HelperText>
        </Field.Root>
        {failure && <FailureMessage failure={failure} />}
      </Stack>
    </Modal>
  )
}
