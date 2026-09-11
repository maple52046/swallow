import { useState } from 'react'
import { Button, Field, Input, Stack, Textarea } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { Site } from '@/domain/site/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'

interface SiteDialogProps {
  site?: Site
  onClose: () => void
  onSaved: (site: Site) => void
}

/**
 * Creates or edits the deliberately thin Site aggregate.
 *
 * Identity remains backend-owned; only the operator label and optional description
 * change. Submit stays disabled until a name is present, and dismissal is blocked
 * while the save is in flight.
 */
export function SiteDialog({ site, onClose, onSaved }: SiteDialogProps) {
  const { sites } = useApp()
  const [name, setName] = useState(site?.name ?? '')
  const [description, setDescription] = useState(site?.description ?? '')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const editing = Boolean(site)

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!name.trim() || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const saved = site
        ? await sites.updateSite(site.id, { name: name.trim(), description: description.trim() })
        : await sites.createSite({ name: name.trim(), description: description.trim() })
      onSaved(saved)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Site could not be saved.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title={editing ? 'Edit site' : 'Create site'}
      description="A Site is a physical or logical infrastructure location such as a datacenter, cage, or lab."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!name.trim() || submitting}>
            {editing ? 'Save changes' : 'Create site'}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Site could not be saved">
            {error}
          </Alert>
        )}
        <Field.Root required>
          <Field.Label>
            Name <Field.RequiredIndicator />
          </Field.Label>
          <Input value={name} onChange={(event) => setName(event.target.value)} autoFocus />
        </Field.Root>
        <Field.Root>
          <Field.Label>Description</Field.Label>
          <Textarea value={description} onChange={(event) => setDescription(event.target.value)} rows={3} />
        </Field.Root>
      </Stack>
    </Modal>
  )
}
