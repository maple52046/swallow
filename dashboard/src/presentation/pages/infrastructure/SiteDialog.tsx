import { useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Form,
  FormGroup,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  TextArea,
  TextInput,
} from '@patternfly/react-core'
import { useApp } from '@/di/AppProvider'
import type { Site } from '@/domain/site/types'

interface SiteDialogProps {
  site?: Site
  onClose: () => void
  onSaved: (site: Site) => void
}

/**
 * Creates or edits the deliberately thin Site aggregate.
 * Identity remains backend-owned; only the operator label and optional description change.
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
    <Modal isOpen onClose={close} variant="small" aria-labelledby="site-editor-title">
      <ModalHeader
        title={editing ? 'Edit site' : 'Create site'}
        labelId="site-editor-title"
        description="A Site is a physical or logical infrastructure location such as a datacenter, cage, or lab."
      />
      <ModalBody>
        <Form className="sw-resource-form">
          {error && <Alert variant={AlertVariant.danger} title="Site could not be saved" isInline>{error}</Alert>}
          <FormGroup label="Name" isRequired fieldId="site-name">
            <TextInput id="site-name" value={name} onChange={(_event, value) => setName(value)} autoFocus />
          </FormGroup>
          <FormGroup label="Description" fieldId="site-description">
            <TextArea id="site-description" value={description} onChange={(_event, value) => setDescription(value)} resizeOrientation="vertical" />
          </FormGroup>
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button onClick={() => void submit()} isLoading={submitting} isDisabled={!name.trim() || submitting}>
          {editing ? 'Save changes' : 'Create site'}
        </Button>
        <Button variant="link" onClick={close} isDisabled={submitting}>Cancel</Button>
      </ModalFooter>
    </Modal>
  )
}
