import { useState } from 'react'
import { Button, Field, Input, List, Stack } from '@chakra-ui/react'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import {
  useOSImageBulkActions,
  type OSImageBulkAction,
  type OSImageBulkTarget,
} from './useOSImageBulkActions'

interface BulkImageActionDialogProps {
  action: OSImageBulkAction
  /** Images the action will run against (already filtered to the eligible ones). */
  targets: OSImageBulkTarget[]
  /** Selected images excluded because they are ineligible for this action. */
  skipped: OSImageBulkTarget[]
  onClose: () => void
  /** Called after at least one target succeeded, so the page reloads and clears selection. */
  onDone: () => void
}

/**
 * Confirms and fans out a delete or name-reset across the selected OS images.
 *
 * Delete is irreversible on the provider, so it is gated by typing the verb, matching the
 * platform bulk dialog; resetting a swallow name overlay is reversible by renaming again, so it
 * confirms with a single click. The dialog stays open with an inline error only when every
 * target failed; a partial success hands control back to the page, where the per-image toast
 * summary explains what was skipped or refused.
 */
export function BulkImageActionDialog({ action, targets, skipped, onClose, onDone }: BulkImageActionDialogProps) {
  const { run, running } = useOSImageBulkActions()
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState('')

  const isDelete = action === 'delete'
  const count = targets.length
  const noun = `image${count === 1 ? '' : 's'}`
  const title = isDelete ? `Delete ${count} ${noun}` : `Reset ${count} ${noun}`

  const close = () => {
    if (!running) onClose()
  }

  const submit = async () => {
    if (running || count === 0) return
    if (isDelete && confirmation !== 'delete') return
    setError('')
    const outcomes = await run(action, targets)
    if (outcomes.every((outcome) => !outcome.accepted)) {
      setError(
        isDelete
          ? 'Every selected image failed to delete. Review the error and retry.'
          : 'Every selected name failed to reset. Review the error and retry.',
      )
      return
    }
    onDone()
  }

  return (
    <Modal
      open
      onClose={close}
      role={isDelete ? 'alertdialog' : undefined}
      closeOnInteractOutside={!running}
      title={title}
      description={isDelete ? undefined : 'Clears Swallow label overrides and restores provider values without changing the provider images.'}
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={running}>
            Cancel
          </Button>
          <Button
            type="submit"
            colorPalette={isDelete ? 'red' : 'brand'}
            loading={running}
            disabled={running || count === 0 || (isDelete && confirmation !== 'delete')}
          >
            {title}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Action failed">
            {error}
          </Alert>
        )}
        {isDelete && (
          <Alert status="warning" title="This cannot be undone">
            Each selected image is permanently removed from its provider. Deployment templates and in-flight
            deployments that reference it may fail until the image is restored or those templates are updated.
          </Alert>
        )}
        <Field.Root>
          <Field.Label>{`${count} ${noun} affected`}</Field.Label>
          <List.Root id="bulk-image-targets" listStyle="none" ps="0">
            {targets.map((target) => (
              <List.Item key={`${target.integrationId}:${target.imageId}:${target.architecture}`}>
                {target.name} ({target.architecture})
              </List.Item>
            ))}
          </List.Root>
        </Field.Root>
        {skipped.length > 0 && (
          <Alert
            status="info"
            title={`${skipped.length} selected ${skipped.length === 1 ? 'image' : 'images'} skipped`}
          >
            {isDelete
              ? 'Images that cannot be deleted from their provider are left unchanged.'
              : 'Only images with a swallow override can be reset; the rest already show their provider values.'}
          </Alert>
        )}
        {isDelete && (
          <Field.Root required>
            <Field.Label>
              Type "delete" to confirm <Field.RequiredIndicator />
            </Field.Label>
            <Input
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              autoFocus
              aria-label="Type delete to confirm"
            />
          </Field.Root>
        )}
      </Stack>
    </Modal>
  )
}
