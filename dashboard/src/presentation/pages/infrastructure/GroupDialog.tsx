import { useState } from 'react'
import { Button, Field, Input, Stack, Textarea } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { type GroupKind, type GroupingResource, groupKindLabel } from '@/domain/infrastructure/types'
import type { Site } from '@/domain/site/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'

interface GroupDialogProps {
  /** Whether this dialog manages a Zone or a Pool; drives labels and the target endpoint. */
  kind: GroupKind
  /** The group being edited, or undefined when creating. */
  group?: GroupingResource
  /** Sites the operator may attach a new group to; used only in the create flow. */
  sites: Site[]
  /** Preselected Site for the create flow, typically the active global Site scope. */
  defaultSiteId?: string
  onClose: () => void
  onSaved: (group: GroupingResource) => void
}

/**
 * Creates or edits one swallow-owned Zone or Pool.
 *
 * One dialog serves both resources because they are structurally identical; the `kind` selects
 * the wording and the endpoint. A group is Site-scoped, so create requires a Site (defaulted to
 * the active scope); editing keeps the Site fixed because a group cannot move between Sites.
 * Submit stays disabled until a Site and name are present, and dismissal is blocked while the
 * save is in flight so a double submit cannot occur.
 */
export function GroupDialog({ kind, group, sites, defaultSiteId, onClose, onSaved }: GroupDialogProps) {
  const { infrastructure } = useApp()
  const [siteId, setSiteId] = useState(group?.siteId ?? defaultSiteId ?? '')
  const [name, setName] = useState(group?.name ?? '')
  const [description, setDescription] = useState(group?.description ?? '')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const editing = Boolean(group)
  const label = groupKindLabel(kind)
  const noun = label.toLowerCase()
  const valid = Boolean(siteId && name.trim())

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!valid || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const saved = group
        ? await infrastructure.updateGroup(kind, group.id, { name: name.trim(), description: description.trim() })
        : await infrastructure.createGroup(kind, { siteId, name: name.trim(), description: description.trim() })
      onSaved(saved)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : `${label} could not be saved.`)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title={editing ? `Edit ${noun}` : `Create ${noun}`}
      description={
        kind === 'zone'
          ? 'A Zone groups servers for availability or organization, realized in the provisioner when it supports zones.'
          : 'A Pool partitions servers for allocation, realized in the provisioner when it supports resource pools.'
      }
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
            {editing ? 'Save changes' : `Create ${noun}`}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title={`${label} could not be saved`}>
            {error}
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
          {editing && <Field.HelperText>A {noun} cannot move between Sites.</Field.HelperText>}
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
