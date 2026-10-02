import { useState } from 'react'
import { Button, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { ApiKey } from '@/domain/access/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'

interface DeleteApiKeyDialogProps {
  apiKey: ApiKey
  onClose: () => void
  /** Called after the key is deleted, with the toast title to show. */
  onDeleted: (title: string) => void
}

/**
 * Confirms deleting (revoking) one of the signed-in admin's API Keys. The consequence is stated
 * before the request: everything using the key stops working at once, and it cannot be restored.
 * A backend refusal keeps the dialog open with the reason.
 */
export function DeleteApiKeyDialog({ apiKey, onClose, onDeleted }: DeleteApiKeyDialogProps) {
  const { apiKeys } = useApp()
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (submitting) return
    setSubmitting(true)
    setError('')
    try {
      await apiKeys.deleteApiKey(apiKey.id)
      onDeleted('API key deleted')
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The API key could not be deleted.')
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      role="alertdialog"
      closeOnInteractOutside={!submitting}
      title={`Delete ${apiKey.name}`}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button colorPalette="red" onClick={() => void submit()} loading={submitting}>
            Delete key
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="API key could not be deleted">
            {error}
          </Alert>
        )}
        <Text>
          Scripts, CI jobs, and CLI profiles using <strong>{apiKey.name}</strong> ({apiKey.prefix}…) stop working immediately.
        </Text>
        <Alert status="warning" title="This cannot be undone">
          A deleted key cannot be restored. Create a new key and update its users if you still need access.
        </Alert>
      </Stack>
    </Modal>
  )
}
