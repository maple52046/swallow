import { useState, type FormEvent } from 'react'
import { Button, Field, HStack, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { PageHeader } from '@/presentation/components/PageHeader'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { PasswordInput } from '@/presentation/components/ui/password-input'
import { passwordProblem } from '@/presentation/components/passwordRules'

/**
 * The signed-in user's own password (route `/account/password`, auth-me.md, decision 082).
 *
 * Any User may change its own password with the current one; the request needs a password Session,
 * which is the only way the dashboard signs in. The password is the swallow sign-in only: Servers
 * accept SSH keys, never a password. The fields are cleared after a successful change, the values
 * live only in this form's state, and the browser is told which field is the new password so a
 * password manager can update its entry. Other Sessions stay signed in (a recorded decision).
 */
export function ChangePasswordPage() {
  const { accounts } = useApp()
  const { showToast } = useToast()
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)

  const problem = newPassword ? passwordProblem(newPassword) : null
  const mismatch = confirmation !== '' && confirmation !== newPassword
  const ready = currentPassword !== '' && newPassword !== '' && problem === null && confirmation === newPassword

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!ready || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      await accounts.changeOwnPassword({ currentPassword, newPassword })
      setCurrentPassword('')
      setNewPassword('')
      setConfirmation('')
      showToast({ tone: 'success', title: 'Password changed' })
    } catch (caught) {
      setError(failureOf(caught, 'The password could not be changed.'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="console-page">
      <PageHeader title="Change password" subtitle="The password you sign in to Swallow with. Servers accept your SSH keys, not this password." />
      <SectionSurface title="Your password" description="Enter your current password, then the new one twice.">
        <form onSubmit={(event) => void submit(event)} aria-label="Change password">
          <Stack gap="4" maxW="md">
            {error && (
              <Alert status="error" title="Password not changed">
                <FailureMessage failure={error} />
              </Alert>
            )}
            <Field.Root required>
              <Field.Label htmlFor="password-current">Current password <Field.RequiredIndicator /></Field.Label>
              <PasswordInput
                id="password-current"
                value={currentPassword}
                onChange={(event) => setCurrentPassword(event.target.value)}
                autoComplete="current-password"
                disabled={submitting}
              />
            </Field.Root>
            <Field.Root required invalid={problem !== null}>
              <Field.Label htmlFor="password-new">New password <Field.RequiredIndicator /></Field.Label>
              <PasswordInput
                id="password-new"
                value={newPassword}
                onChange={(event) => setNewPassword(event.target.value)}
                autoComplete="new-password"
                disabled={submitting}
              />
              {problem ? <Field.ErrorText>{problem}</Field.ErrorText> : <Field.HelperText>At least 8 characters.</Field.HelperText>}
            </Field.Root>
            <Field.Root required invalid={mismatch}>
              <Field.Label htmlFor="password-confirm">Confirm new password <Field.RequiredIndicator /></Field.Label>
              <PasswordInput
                id="password-confirm"
                value={confirmation}
                onChange={(event) => setConfirmation(event.target.value)}
                autoComplete="new-password"
                disabled={submitting}
              />
              {mismatch && <Field.ErrorText>The passwords do not match.</Field.ErrorText>}
            </Field.Root>
            <HStack>
              <Button type="submit" colorPalette="brand" loading={submitting} disabled={!ready}>
                Change password
              </Button>
            </HStack>
            <Text color="fg.muted" fontSize="sm">
              Other browsers where you are signed in stay signed in. Sign out there if you changed the password because it leaked.
            </Text>
          </Stack>
        </form>
      </SectionSurface>
    </div>
  )
}
