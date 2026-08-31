import { useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  FormGroup,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  TextInput,
} from '@patternfly/react-core'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'

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
 * Success means the backend removed both the provisioner's Machine and the Swallow
 * projection; provider safeguards can refuse the request and are shown inline without
 * closing the dialog. The parent owns navigation or list refresh after `onDeleted`.
 */
export function ServerDeleteDialog({
  serverId,
  serverName,
  onClose,
  onDeleted,
}: ServerDeleteDialogProps) {
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
      showToast({
        tone: 'success',
        title: 'Server deleted',
        description: `${serverName} was removed from the provisioner and Swallow.`,
      })
      onDeleted()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Could not delete the Server.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      isOpen
      onClose={close}
      variant="small"
      aria-labelledby="server-delete-title"
    >
      <ModalHeader
        title="Delete server"
        labelId="server-delete-title"
        description="This permanently removes the provisioner's Machine and its Swallow Server record."
      />
      <ModalBody>
        {error && (
          <Alert variant={AlertVariant.danger} title="Server could not be deleted" isInline>
            {error}
          </Alert>
        )}
        <Alert variant={AlertVariant.warning} title="This cannot be undone" isInline>
          Swallow asks the provisioner to delete the Machine first. Provider safeguards are
          not force-overridden; if deletion is refused, neither record is removed.
        </Alert>
        <FormGroup
          label={`Type "${serverName}" to confirm`}
          isRequired
          fieldId="server-delete-confirmation"
        >
          <TextInput
            id="server-delete-confirmation"
            value={confirmation}
            onChange={(_event, value) => setConfirmation(value)}
            autoFocus
            aria-label="Server name confirmation"
            onKeyDown={(event) => {
              if (event.key === 'Enter') void submit()
            }}
          />
        </FormGroup>
      </ModalBody>
      <ModalFooter>
        <Button
          variant="danger"
          onClick={() => void submit()}
          isLoading={submitting}
          isDisabled={confirmation !== serverName || submitting}
        >
          Delete server
        </Button>
        <Button variant="link" onClick={close} isDisabled={submitting}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  )
}
