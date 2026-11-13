import { useState } from 'react'
import { Button, Field, Input, Stack, Textarea } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { Zone } from '@/domain/infrastructure/types'
import type { Site } from '@/domain/site/types'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'

interface ZoneDialogProps {
  /** The Zone being edited, or undefined when creating. */
  zone?: Zone
  /** Sites the user may attach a new Zone to; used only in the create flow. */
  sites: Site[]
  /** Preselected Site for the create flow, typically the active global Site scope. */
  defaultSiteId?: string
  onClose: () => void
  onSaved: (zone: Zone) => void
}

/**
 * Creates or edits one swallow-owned Zone.
 *
 * A Zone is Site-scoped, so create requires a Site (defaulted to the active scope); editing keeps
 * the Site fixed because a Zone cannot move between Sites. Submit stays disabled until a Site and
 * name are present, and dismissal is blocked while the save is in flight so a double submit cannot
 * occur.
 */
export function ZoneDialog({ zone, sites, defaultSiteId, onClose, onSaved }: ZoneDialogProps) {
  const { infrastructure } = useApp()
  const [siteId, setSiteId] = useState(zone?.siteId ?? defaultSiteId ?? '')
  const [name, setName] = useState(zone?.spec.name ?? '')
  const [description, setDescription] = useState(zone?.spec.description ?? '')
  const [error, setError] = useState<DisplayedFailure | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const editing = Boolean(zone)
  const valid = Boolean(siteId && name.trim())

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!valid || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      const saved = zone
        ? await infrastructure.updateZone(zone.id, { name: name.trim(), description: description.trim() })
        : await infrastructure.createZone({ siteId, name: name.trim(), description: description.trim() })
      onSaved(saved)
    } catch (caught) {
      setError(failureOf(caught, 'Zone could not be saved.'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title={editing ? 'Edit zone' : 'Create zone'}
      description="A Zone groups servers of one site for availability or organization, realized in the provisioner when it supports zones."
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
            {editing ? 'Save changes' : 'Create zone'}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Zone could not be saved">
            <FailureMessage failure={error} />
          </Alert>
        )}
        <Field.Root required>
          <Field.Label>
            Site <Field.RequiredIndicator />
          </Field.Label>
          <Select
            id="group-site"
            value={siteId}
            disabled={editing}
            aria-label="Site"
            placeholder="Select a Site"
            onChange={setSiteId}
            options={sites.map((site) => ({ value: site.id, label: site.name }))}
          />
          {editing && <Field.HelperText>A zone cannot move between Sites.</Field.HelperText>}
        </Field.Root>
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
