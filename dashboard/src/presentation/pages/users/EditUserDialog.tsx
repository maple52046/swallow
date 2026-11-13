import { useState } from 'react'
import { Button, Field, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { Account } from '@/domain/access/accounts'
import type { UserRole } from '@/domain/user/types'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { passwordProblem } from '@/presentation/components/passwordRules'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { PasswordInput } from '@/presentation/components/ui/password-input'
import { Select } from '@/presentation/components/ui/select'
import { USER_ROLE_OPTIONS } from './accountPresentation'

interface EditUserDialogProps {
  account: Account
  /** Whether the account is the signed-in admin's own, which the role change warns about. */
  isSelf: boolean
  onClose: () => void
  /** Called after the change is saved, with the toast title to show. */
  onSaved: (title: string) => void
}

/**
 * Changes a User's role, resets its password, or both (users.md `PATCH`). Only what changed is
 * sent: an untouched role is omitted, and an empty password field means "keep the password". The
 * API refuses demoting the last admin with `conflict`, which keeps the dialog open with the reason.
 * A reset password does not sign the person out elsewhere (a recorded decision), which the dialog
 * says. The new password lives only in this dialog's state.
 */
export function EditUserDialog({ account, isSelf, onClose, onSaved }: EditUserDialogProps) {
  const { accounts } = useApp()
  const [role, setRole] = useState<UserRole>(account.role)
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)

  const problem = password ? passwordProblem(password) : null
  const roleChanged = role !== account.role
  const ready = (roleChanged || password !== '') && problem === null

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!ready || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      await accounts.updateAccount(account.id, {
        ...(roleChanged ? { role } : {}),
        ...(password ? { password } : {}),
      })
      onSaved(`User ${account.username} updated`)
    } catch (caught) {
      setError(failureOf(caught, 'The user could not be updated.'))
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title={`Edit ${account.username}`}
      description="The username cannot change: it is the person's Linux login and owns their files on the Servers."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!ready}>
            Save
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="User not updated">
            <FailureMessage failure={error} />
          </Alert>
        )}
        <Field.Root>
          <Field.Label htmlFor="user-edit-role">Role</Field.Label>
          <Select
            id="user-edit-role"
            aria-label="Role"
            value={role}
            onChange={(value) => setRole(value === 'admin' ? 'admin' : 'user')}
            options={USER_ROLE_OPTIONS}
            disabled={submitting}
          />
          {isSelf && roleChanged && (
            <Field.HelperText>You will lose admin access as soon as you save.</Field.HelperText>
          )}
        </Field.Root>
        <Field.Root invalid={problem !== null}>
          <Field.Label htmlFor="user-edit-password">Reset password</Field.Label>
          <PasswordInput
            id="user-edit-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            autoComplete="new-password"
            placeholder="Leave empty to keep the password"
            disabled={submitting}
          />
          {problem ? <Field.ErrorText>{problem}</Field.ErrorText> : <Field.HelperText>At least 8 characters. Give it to the person privately.</Field.HelperText>}
        </Field.Root>
        <Text color="fg.muted" fontSize="sm">
          Changes reach the Servers within a minute. Resetting the password does not sign the person out of browsers where they are signed in.
        </Text>
      </Stack>
    </Modal>
  )
}
