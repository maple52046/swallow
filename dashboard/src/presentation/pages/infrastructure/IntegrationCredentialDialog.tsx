import { useState } from 'react'
import { Button, Field, Input, Stack } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { Integration } from '@/domain/site/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { credentialGuidance } from './credentialGuidance'
import { CredentialGuidanceNote } from './CredentialGuidanceNote'

interface IntegrationCredentialDialogProps {
  integration: Integration
  onClose: () => void
  onReplaced: () => void
}

/**
 * Sets or replaces an Integration secret through the write-only endpoint.
 *
 * The title, helper text, and CTA adapt to whether a credential already exists ("Set" vs
 * "Replace"), because the row action looks the same in both cases and an operator needs to know
 * which they are doing. A provider-specific guidance note explains what the credential is, what it
 * maps to, and what it affects. The value lives only in transient component state and is cleared
 * before dismissal, so a credential is never retained in the client, and the copy warns that it
 * cannot be viewed again after saving.
 */
export function IntegrationCredentialDialog({ integration, onClose, onReplaced }: IntegrationCredentialDialogProps) {
  const { sites } = useApp()
  const [credential, setCredential] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  // "Set" when none exists yet, "Replace" when overwriting one, so the operator is never unsure
  // which action the shared key-icon triggered.
  const replacing = integration.hasCredential
  const term = credentialGuidance(integration.providerKind).term

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
      title={`${replacing ? 'Replace' : 'Set'} credential — ${integration.name}`}
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
            {replacing ? 'Replace' : 'Set'} credential
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title={`Credential could not be ${replacing ? 'replaced' : 'set'}`}>
            {error}
          </Alert>
        )}
        <CredentialGuidanceNote providerKind={integration.providerKind} />
        <Field.Root required>
          <Field.Label>
            {replacing ? `New ${term}` : term} <Field.RequiredIndicator />
          </Field.Label>
          <Input
            type="password"
            value={credential}
            onChange={(event) => setCredential(event.target.value)}
            autoComplete="new-password"
            autoFocus
          />
          <Field.HelperText>
            {replacing
              ? 'Replaces the stored value. It cannot be viewed again after saving.'
              : 'Stored encrypted. It cannot be viewed again after saving.'}
          </Field.HelperText>
        </Field.Root>
      </Stack>
    </Modal>
  )
}
