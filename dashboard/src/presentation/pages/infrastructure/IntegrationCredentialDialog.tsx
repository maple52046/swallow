import { useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  TextInput,
} from '@patternfly/react-core'
import { useApp } from '@/di/AppProvider'
import type { Integration } from '@/domain/site/types'

interface IntegrationCredentialDialogProps {
  integration: Integration
  onClose: () => void
  onReplaced: () => void
}

/**
 * Replaces an Integration secret through the write-only endpoint.
 * The value lives only in transient component state and is cleared before dismissal.
 */
export function IntegrationCredentialDialog({ integration, onClose, onReplaced }: IntegrationCredentialDialogProps) {
  const { sites } = useApp()
  const [credential, setCredential] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const close = () => {
    if (submitting) return
    setCredential('')
    onClose()
  }

  const submit = async () => {
    if (!credential || submitting) return
    setSubmitting(true)
    setError('')
    try {
      await sites.replaceIntegrationCredential(integration.id, credential)
      setCredential('')
      onReplaced()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Credential could not be replaced.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal isOpen onClose={close} variant="small" aria-labelledby="integration-credential-title">
      <ModalHeader
        title="Replace credential"
        labelId="integration-credential-title"
        description={`Replace the stored credential for ${integration.name}.`}
      />
      <ModalBody className="sw-resource-form">
        {error && <Alert variant={AlertVariant.danger} title="Credential could not be replaced" isInline>{error}</Alert>}
        <FormGroup label="New credential" isRequired fieldId="replacement-credential">
          <TextInput
            id="replacement-credential"
            type="password"
            value={credential}
            onChange={(_event, value) => setCredential(value)}
            autoComplete="new-password"
            autoFocus
          />
          <FormHelperText>
            <HelperText><HelperTextItem>This value cannot be viewed again after saving.</HelperTextItem></HelperText>
          </FormHelperText>
        </FormGroup>
      </ModalBody>
      <ModalFooter>
        <Button onClick={() => void submit()} isLoading={submitting} isDisabled={!credential || submitting}>Replace credential</Button>
        <Button variant="link" onClick={close} isDisabled={submitting}>Cancel</Button>
      </ModalFooter>
    </Modal>
  )
}
