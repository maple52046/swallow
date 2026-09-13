import { useState } from 'react'
import { Button, Field, Input, Stack } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { Integration } from '@/domain/site/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'

interface IntegrationCredentialDialogProps {
  integration: Integration
  onClose: () => void
  onReplaced: () => void
}

/**
 * Replaces an Integration secret through the write-only endpoint.
 *
 * The value lives only in transient component state and is cleared before
 * dismissal, so a replaced credential is never retained in the client. The helper
 * text warns that the value cannot be viewed again after saving.
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
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Replace credential"
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!credential || submitting}>
            Replace credential
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Credential could not be replaced">
            {error}
          </Alert>
        )}
        <Field.Root required>
          <Field.Label>
            New credential <Field.RequiredIndicator />
          </Field.Label>
          <Input
            type="password"
            value={credential}
            onChange={(event) => setCredential(event.target.value)}
            autoComplete="new-password"
            autoFocus
          />
          <Field.HelperText>This value cannot be viewed again after saving.</Field.HelperText>
        </Field.Root>
      </Stack>
    </Modal>
  )
}
