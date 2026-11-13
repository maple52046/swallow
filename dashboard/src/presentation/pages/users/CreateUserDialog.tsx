import { useState } from 'react'
import { Button, Field, Input, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { UserRole } from '@/domain/user/types'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { passwordProblem } from '@/presentation/components/passwordRules'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { PasswordInput } from '@/presentation/components/ui/password-input'
import { Select } from '@/presentation/components/ui/select'
import { USER_ROLE_OPTIONS, USERNAME_PATTERN } from './accountPresentation'

interface CreateUserDialogProps {
  onClose: () => void
  /** Called after the User is created, with the toast title to show. */
  onCreated: (title: string) => void
}

/**
 * Creates a User (users.md): a username that is also its Linux login, an initial password the
 * admin hands over out of band, and a User Role. The username rule and the password rule are
 * checked while typing; reserved names and taken usernames come back from the API and keep the
 * dialog open with the reason. The password lives only in this dialog's state and is dropped when it
 * closes; the field is marked as a new password so the browser does not offer the admin's own.
 */
export function CreateUserDialog({ onClose, onCreated }: CreateUserDialogProps) {
  const { accounts } = useApp()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<UserRole>('user')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)

  const usernameInvalid = username !== '' && !USERNAME_PATTERN.test(username)
  const problem = password ? passwordProblem(password) : null
  const ready = username !== '' && !usernameInvalid && password !== '' && problem === null

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!ready || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      const created = await accounts.createAccount({ username, password, role })
      onCreated(`User ${created.username} created`)
    } catch (caught) {
      setError(failureOf(caught, 'The user could not be created.'))
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Create user"
      description="The person signs in to Swallow with this username and password, and logs in to the Servers of their Pools with their SSH keys."
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
            Create user
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="User not created">
            <FailureMessage failure={error} />
          </Alert>
        )}
        <Field.Root required invalid={usernameInvalid}>
          <Field.Label htmlFor="user-create-username">Username <Field.RequiredIndicator /></Field.Label>
          <Input
            id="user-create-username"
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            maxLength={32}
            autoComplete="off"
            spellCheck={false}
            className="sw-mono"
            placeholder="e.g. alice"
            disabled={submitting}
            autoFocus
          />
          {usernameInvalid ? (
            <Field.ErrorText>Start with a lowercase letter or _, then use lowercase letters, digits, _ or -.</Field.ErrorText>
          ) : (
            <Field.HelperText>Also the Linux login. It cannot be changed later.</Field.HelperText>
          )}
        </Field.Root>
        <Field.Root required invalid={problem !== null}>
          <Field.Label htmlFor="user-create-password">Initial password <Field.RequiredIndicator /></Field.Label>
          <PasswordInput
            id="user-create-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            autoComplete="new-password"
            disabled={submitting}
          />
          {problem ? <Field.ErrorText>{problem}</Field.ErrorText> : <Field.HelperText>At least 8 characters. Give it to the person privately; they can change it.</Field.HelperText>}
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="user-create-role">Role</Field.Label>
          <Select
            id="user-create-role"
            aria-label="Role"
            value={role}
            onChange={(value) => setRole(value === 'admin' ? 'admin' : 'user')}
            options={USER_ROLE_OPTIONS}
            disabled={submitting}
          />
        </Field.Root>
        <Text color="fg.muted" fontSize="sm">
          A new user can sign in at once. An owner adds them to a Pool before they can reach any Server.
        </Text>
      </Stack>
    </Modal>
  )
}
