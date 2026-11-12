import { useState } from 'react'
import { Button, Field, Input, Stack, Textarea } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { ValidationImpact, ValidationRunAs, ValidationSuite } from '@/domain/validation/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { IMPACT_COPY } from '@/presentation/components/validation/validationPresentation'

/** The API's bounds on a script's timeout (validation.md, Suites). */
const MAX_TIMEOUT_SECONDS = 3600
const DEFAULT_TIMEOUT_SECONDS = 600
/** The API's bound on a script's size: 64 KiB. */
const MAX_SCRIPT_BYTES = 64 * 1024

interface UserSuiteDialogProps {
  open: boolean
  onClose: () => void
  /** The user suite to revise; absent to create one. */
  suite?: ValidationSuite
  /** Called with the suite the API stored, after the dialog closes. */
  onSaved: (suite: ValidationSuite) => void
}

/**
 * Creates or revises a user-defined Validation Suite (`POST`/`PUT /api/v1/validation/suites`): a
 * script the run executes on each target Server, whose exit code is its one check's verdict — 0
 * passes, any other code fails, and running past the timeout is an error. The admin sets the
 * account it runs as, its timeout, and its impact; changing any of them, or the script, makes a
 * new revision, and runs already accepted keep theirs.
 *
 * The script is stored and returned as written and runs with the chosen account's privileges on
 * every target, so the dialog warns against secrets in it; only an admin reaches this dialog, and
 * the API enforces that again. Client checks (a name, a non-empty script within 64 KiB, a timeout
 * in range) only spare a round trip: the API's refusal is shown in the dialog with its request id.
 * The caller remounts the dialog (a new `key`) for each opening.
 */
export function UserSuiteDialog({ open, onClose, suite, onSaved }: UserSuiteDialogProps) {
  const { validation } = useApp()
  const [name, setName] = useState(suite?.name ?? '')
  const [description, setDescription] = useState(suite?.description ?? '')
  const [script, setScript] = useState(suite?.script ?? '#!/bin/sh\n')
  const [runAs, setRunAs] = useState<ValidationRunAs>(suite?.runAs ?? 'automation-user')
  const [timeoutText, setTimeoutText] = useState(String(suite?.timeoutSeconds ?? DEFAULT_TIMEOUT_SECONDS))
  const [impact, setImpact] = useState<ValidationImpact>(suite?.impact ?? 'workload')
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<DisplayedFailure | null>(null)

  const timeoutSeconds = Number(timeoutText)
  const timeoutValid = Number.isInteger(timeoutSeconds) && timeoutSeconds >= 1 && timeoutSeconds <= MAX_TIMEOUT_SECONDS
  const scriptBytes = new TextEncoder().encode(script).length
  const scriptValid = script.trim() !== '' && scriptBytes <= MAX_SCRIPT_BYTES
  const ready = name.trim() !== '' && scriptValid && timeoutValid

  const submit = async () => {
    if (!ready) return
    setBusy(true)
    setFailure(null)
    const input = { name: name.trim(), description: description.trim(), script, runAs, timeoutSeconds, impact }
    try {
      const saved = suite ? await validation.replaceSuite(suite.id, input) : await validation.createSuite(input)
      onClose()
      onSaved(saved)
    } catch (caught: unknown) {
      setFailure(failureOf(caught, 'The suite was not saved.'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={suite ? `Edit ${suite.name}` : 'New script suite'}
      description="The script runs on each target Server; exit code 0 passes, any other code fails."
      size="xl"
      dismissDisabled={busy}
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>Cancel</Button>
          <Button type="submit" colorPalette="brand" loading={busy} disabled={!ready}>{suite ? 'Save revision' : 'Create suite'}</Button>
        </>
      }
    >
      <Stack gap="4">
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
        <Alert status="warning" title="Do not put secrets in the script">
          The script is stored and shown as written, and runs with the chosen account’s privileges on every target.
        </Alert>
        <Field.Root>
          <Field.Label htmlFor="user-suite-run-as">Runs as</Field.Label>
          <Select
            id="user-suite-run-as"
            aria-label="Runs as"
            value={runAs}
            onChange={(value) => setRunAs(value === 'root' ? 'root' : 'automation-user')}
            options={[
              { value: 'automation-user', label: 'The automation user' },
              { value: 'root', label: 'root' },
            ]}
            disabled={busy}
          />
        </Field.Root>
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
