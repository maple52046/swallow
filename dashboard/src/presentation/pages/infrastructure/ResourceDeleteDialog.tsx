import { useState } from 'react'
import { Button, Field, Input, Stack } from '@chakra-ui/react'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'

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
 *
 * The destructive action stays disabled until the operator retypes the exact
 * resource name, and provider dependency conflicts surface inline so the resource
 * context is never lost. Dismissal is blocked while the delete is in flight.
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
    <Modal
      open
      onClose={close}
      role="alertdialog"
      closeOnInteractOutside={!submitting}
      title={`Delete ${noun}`}
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="red" loading={submitting} disabled={confirmation !== name || submitting}>
            Delete {noun}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title={`${resourceLabel} could not be deleted`}>
            {error}
          </Alert>
        )}
        <Alert status="warning" title="This cannot be undone">
          Only the {resourceLabel} registration is removed from Swallow. {warning}
        </Alert>
        <Field.Root required>
          <Field.Label>
            Type "{name}" to confirm <Field.RequiredIndicator />
          </Field.Label>
          <Input
            aria-label={`${resourceLabel} name confirmation`}
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
            autoFocus
          />
        </Field.Root>
      </Stack>
    </Modal>
  )
}
