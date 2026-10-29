import { useState } from 'react'
import { Button, Field, Input, Stack } from '@chakra-ui/react'
import type { Platform } from '@/domain/platform/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { PlatformBulkFailures, PlatformNameList } from './PlatformBulkLists'
import { PLATFORM_BULK_RECORD_DELETE_CONSEQUENCE, plural } from './platformRemovalCopy'
import { usePlatformBulkActions, type PlatformBulkOutcome } from './usePlatformBulkActions'

interface PlatformBulkDeleteDialogProps {
  /** Every selected Platform; record-only Delete has no eligibility gate. */
  platforms: readonly Platform[]
  onClose: () => void
  /** Called with the ids whose record was deleted once at least one was; failures stay selected. */
  onDone: (acceptedIds: readonly string[]) => void
}

/**
 * Confirms the bulk record-only Delete, reached from "Delete only…" or from "Delete
 * selected" when no selected Platform leads with Uninstall. It removes each Swallow record and its
 * owned projections after the backend cancels that Platform's in-flight workflows, and never
 * changes hosts, so it applies to every selected Platform, including locked or uninstalling ones.
 * The gate is typing `delete`. It stays open with each failure listed when every request failed.
 */
export function PlatformBulkDeleteDialog({ platforms, onClose, onDone }: PlatformBulkDeleteDialogProps) {
  const { runDelete, running } = usePlatformBulkActions()
  const [confirmation, setConfirmation] = useState('')
  const [failures, setFailures] = useState<PlatformBulkOutcome[]>([])
  const count = platforms.length
  const canSubmit = confirmation === 'delete' && !running && count > 0
  const consequence = PLATFORM_BULK_RECORD_DELETE_CONSEQUENCE

  const close = () => {
    if (!running) onClose()
  }

  const submit = async () => {
    if (!canSubmit) return
    setFailures([])
    const outcomes = await runDelete(platforms)
    const accepted = outcomes.filter((outcome) => outcome.accepted)
    if (accepted.length === 0) {
      setFailures(outcomes)
      return
    }
    onDone(accepted.map((outcome) => outcome.platformId))
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!running}
      dismissDisabled={running}
      title="Delete selected platform records"
      description="Permanently delete every selected Swallow record. Hosts will not be changed."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={running}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="red" loading={running} disabled={!canSubmit}>
            {`Delete ${count} Swallow ${plural(count, 'record')}`}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        <PlatformBulkFailures failures={failures} />
        <PlatformNameList label="Platform records to delete" platforms={platforms} />
        <Alert status={consequence.status} title={consequence.title}>
          {consequence.body}
        </Alert>
        <Field.Root required>
          <Field.Label>
            Type "delete" to confirm <Field.RequiredIndicator />
          </Field.Label>
          <Input value={confirmation} disabled={running} onChange={(event) => setConfirmation(event.target.value)} autoFocus aria-label="Type delete to confirm" />
        </Field.Root>
      </Stack>
    </Modal>
  )
}
