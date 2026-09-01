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

interface ResourceDeleteDialogProps {
  resourceLabel: 'Site' | 'Integration'
  name: string
  warning: string
  onClose: () => void
  onDelete: () => Promise<void>
  onDeleted: () => void
}

/**
 * Typed confirmation shared by Site and Integration deletion.
 * Provider dependency conflicts remain inline so operators keep the exact resource context.
 */
export function ResourceDeleteDialog({
  resourceLabel,
  name,
  warning,
  onClose,
  onDelete,
  onDeleted,
}: ResourceDeleteDialogProps) {
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (confirmation !== name || submitting) return
    setSubmitting(true)
    setError('')
    try {
      await onDelete()
      onDeleted()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : `${resourceLabel} could not be deleted.`)
    } finally {
      setSubmitting(false)
    }
  }

  const noun = resourceLabel.toLowerCase()
  return (
    <Modal isOpen onClose={close} variant="small" aria-labelledby="resource-delete-title">
      <ModalHeader
        title={`Delete ${noun}`}
        labelId="resource-delete-title"
        description={`This removes only the ${resourceLabel} registration from Swallow.`}
      />
      <ModalBody className="sw-resource-form">
        {error && <Alert variant={AlertVariant.danger} title={`${resourceLabel} could not be deleted`} isInline>{error}</Alert>}
        <Alert variant={AlertVariant.warning} title="This cannot be undone" isInline>{warning}</Alert>
        <FormGroup label={`Type "${name}" to confirm`} isRequired fieldId="resource-delete-confirmation">
          <TextInput
            id="resource-delete-confirmation"
            aria-label={`${resourceLabel} name confirmation`}
            value={confirmation}
            onChange={(_event, value) => setConfirmation(value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') void submit()
            }}
            autoFocus
          />
        </FormGroup>
      </ModalBody>
      <ModalFooter>
        <Button variant="danger" onClick={() => void submit()} isLoading={submitting} isDisabled={confirmation !== name || submitting}>
          Delete {noun}
        </Button>
        <Button variant="link" onClick={close} isDisabled={submitting}>Cancel</Button>
      </ModalFooter>
    </Modal>
  )
}
