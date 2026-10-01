import { useState } from 'react'
import { Button, Field, Input, Stack, Textarea } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'

interface ImportSSHKeyDialogProps {
  onClose: () => void
  /** Called after the key is stored, with the toast title to show. */
  onImported: (title: string) => void
}

/**
 * Imports an existing public key as one of the signed-in admin's Access Keys.
 *
 * Only a public key is accepted — swallow never stores an Access Key's private half — and the
 * backend is the validator of record for the key format, strength, and duplicates; its message is
 * shown inline so the operator can correct and retry. The dialog cannot be dismissed while the
 * request is in flight, so a double submit cannot store two records.
 */
export function ImportSSHKeyDialog({ onClose, onImported }: ImportSSHKeyDialogProps) {
  const { sshKeys } = useApp()
  const [name, setName] = useState('')
  const [publicKey, setPublicKey] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const canSubmit = Boolean(name.trim() && publicKey.trim()) && !submitting

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!canSubmit) return
    setSubmitting(true)
    setError('')
    try {
      await sshKeys.importAccessKey({ name: name.trim(), publicKey: publicKey.trim() })
      onImported('SSH key imported')
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The key could not be imported.')
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Import SSH key"
      description="Add a public key you already have. Swallow registers it in each key-capable provisioner so Servers deployed afterwards accept it."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!canSubmit}>
            Import
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Key could not be imported">
            {error}
          </Alert>
        )}
        <Field.Root required>
          <Field.Label htmlFor="ssh-key-import-name">
            Name <Field.RequiredIndicator />
          </Field.Label>
          <Input
            id="ssh-key-import-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            maxLength={100}
            placeholder="e.g. work-laptop"
            disabled={submitting}
            autoFocus
          />
        </Field.Root>
        <Field.Root required>
          <Field.Label htmlFor="ssh-key-import-public-key">
            Public key <Field.RequiredIndicator />
          </Field.Label>
          <Textarea
            id="ssh-key-import-public-key"
            value={publicKey}
            onChange={(event) => setPublicKey(event.target.value)}
            rows={4}
            className="sw-mono"
            placeholder="ssh-ed25519 AAAA… you@host"
            spellCheck={false}
            disabled={submitting}
          />
          <Field.HelperText>One line from a .pub file. ed25519, ECDSA, and RSA (2048 bits or more) keys are accepted.</Field.HelperText>
        </Field.Root>
      </Stack>
    </Modal>
  )
}
