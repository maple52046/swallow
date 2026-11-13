import { useState } from 'react'
import { Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { Account } from '@/domain/access/accounts'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { Alert } from '@/presentation/components/ui/alert'

interface DeleteUserDialogProps {
  account: Account
  onClose: () => void
  /** Called after the User is deleted, with the toast title to show. */
  onDeleted: (title: string) => void
}

/**
 * Confirms deleting a User (users.md `DELETE`). The consequences are stated before the request: the
 * person is signed out everywhere, loses every Pool Membership, Access Key and API Key, and can no
 * longer log in to any Server; the files they own on Servers stay, and their uid is never reused.
 * The API refuses the last admin and the caller's own account with `conflict`, which keeps the
 * dialog open with the reason.
 */
export function DeleteUserDialog({ account, onClose, onDeleted }: DeleteUserDialogProps) {
  const { accounts } = useApp()
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)

  const confirm = async () => {
    if (submitting) return
    setSubmitting(true)
    setError(null)
    try {
      await accounts.deleteAccount(account.id)
      onDeleted(`User ${account.username} deleted`)
    } catch (caught) {
      setError(failureOf(caught, 'The user could not be deleted.'))
      setSubmitting(false)
    }
  }

  return (
    <ConfirmDialog open title={`Delete ${account.username}`} confirmLabel="Delete user" onConfirm={() => void confirm()} onCancel={onClose} busy={submitting}>
      <Stack gap="4">
        {error && (
          <Alert status="error" title="User not deleted">
            <FailureMessage failure={error} />
          </Alert>
        )}
        <Text>
          <strong>{account.username}</strong> is signed out everywhere, leaves every Pool, and loses their SSH and API keys. Within a minute they can no longer log in to any Server.
        </Text>
        <Alert status="warning" title="This cannot be undone">
          Files they own on Servers stay. Their uid is never given to anyone else, so a new user with the same name does not inherit those files.
        </Alert>
      </Stack>
    </ConfirmDialog>
  )
}
