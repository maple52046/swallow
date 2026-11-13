import { useState } from 'react'
import { Button, Field, Input, Stack, Textarea } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { Pool } from '@/domain/infrastructure/types'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'

interface PoolDialogProps {
  /** The Pool being edited, or undefined when creating. */
  pool?: Pool
  onClose: () => void
  onSaved: (pool: Pool) => void
}

/**
 * Creates or renames one Pool (admin only, from the Pools page; contract infrastructure.md).
 *
 * A Pool belongs to the installation, so there is no Site to choose, and it exists only in swallow:
 * the save takes effect when it is answered, and nothing is waited for in the provisioner. A
 * duplicate name comes back from the API as a conflict and is shown inline. Submit stays disabled
 * until a name is present, and dismissal is blocked while the save is in flight.
 */
export function PoolDialog({ pool, onClose, onSaved }: PoolDialogProps) {
  const { infrastructure } = useApp()
  const [name, setName] = useState(pool?.name ?? '')
  const [description, setDescription] = useState(pool?.description ?? '')
  const [error, setError] = useState<DisplayedFailure | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const editing = Boolean(pool)
  const valid = Boolean(name.trim())

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!valid || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      const input = { name: name.trim(), description: description.trim() }
      const saved = pool ? await infrastructure.updatePool(pool.id, input) : await infrastructure.createPool(input)
      onSaved(saved)
    } catch (caught) {
      setError(failureOf(caught, 'Pool could not be saved.'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title={editing ? 'Edit pool' : 'Create pool'}
      description="A Pool groups servers from any site for the people who use them. Its owners operate its servers, and its members log in to them."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!valid || submitting}>
            {editing ? 'Save changes' : 'Create pool'}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Pool could not be saved">
            <FailureMessage failure={error} />
          </Alert>
        )}
        <Field.Root required>
          <Field.Label>
            Name <Field.RequiredIndicator />
          </Field.Label>
          <Input value={name} onChange={(event) => setName(event.target.value)} autoFocus />
          {editing && <Field.HelperText>Renaming keeps the Pool's members, sudo defaults, and Linux group names.</Field.HelperText>}
        </Field.Root>
        <Field.Root>
          <Field.Label>Description</Field.Label>
          <Textarea value={description} onChange={(event) => setDescription(event.target.value)} rows={3} />
        </Field.Root>
      </Stack>
    </Modal>
  )
}
