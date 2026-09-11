import { useState } from 'react'
import { Button, Field, Input, Stack } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'

interface ServerDeleteDialogProps {
  serverId: string
  serverName: string
  onClose: () => void
  onDeleted: () => void
}

/**
 * Confirms the provider-backed deletion shared by Server list rows and the detail page.
 *
 * The operator must type the displayed Server name before the destructive call is enabled.
 * Success means the backend removed both the provisioner's Machine and the Swallow projection;
 * provider safeguards can refuse the request and are shown inline without closing the dialog.
 * The parent owns navigation or list refresh after `onDeleted`.
 */
export function ServerDeleteDialog({ serverId, serverName, onClose, onDeleted }: ServerDeleteDialogProps) {
  const { servers } = useApp()
  const { showToast } = useToast()
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (confirmation !== serverName || submitting) return
    setSubmitting(true)
    setError('')
    try {
      await servers.deleteServer(serverId)
      showToast({ tone: 'success', title: 'Server deleted', description: `${serverName} was removed from the provisioner and Swallow.` })
      onDeleted()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Could not delete the Server.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      role="alertdialog"
      closeOnInteractOutside={!submitting}
      title="Delete server"
      description="This permanently removes the provisioner's Machine and its Swallow Server record."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="red" loading={submitting} disabled={confirmation !== serverName || submitting}>
            Delete server
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Server could not be deleted">
            {error}
          </Alert>
        )}
        <Alert status="warning" title="This cannot be undone">
          Swallow asks the provisioner to delete the Machine first. Provider safeguards are not force-overridden; if
          deletion is refused, neither record is removed.
        </Alert>
        <Field.Root required>
          <Field.Label>
            Type "{serverName}" to confirm <Field.RequiredIndicator />
          </Field.Label>
          <Input
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
            autoFocus
            aria-label="Server name confirmation"
          />
        </Field.Root>
      </Stack>
    </Modal>
  )
}
