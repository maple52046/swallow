import { useState } from 'react'
import { Button, Field, Input, Stack, Text, Textarea } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { SSHKey } from '@/domain/access/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'

interface DeploymentKeyDialogProps {
  /** `regenerate` creates a new ed25519 pair; `replace` uploads an existing private key. */
  mode: 'regenerate' | 'replace'
  /** The current Deployment Key, named in the confirmation. */
  current: SSHKey
  onClose: () => void
  /** Called after the key changed, with the toast title to show. */
  onChanged: (title: string) => void
}

/**
 * Replaces the system-owned Deployment Key, either by generating a new pair or by uploading an
 * existing unencrypted private key. One component serves both because they share the same
 * consequence and confirmation; only the input differs.
 *
 * Consequence stated up front: Servers deployed before the change authorize only the previous
 * public key, so swallow can no longer log in to them with the new key unless a Site overrides the
 * key or the Server is redeployed. The uploaded private key lives only in this dialog's state until
 * it is sent; it is never stored in the browser and never returned by the API.
 */
export function DeploymentKeyDialog({ mode, current, onClose, onChanged }: DeploymentKeyDialogProps) {
  const { sshKeys } = useApp()
  const [privateKey, setPrivateKey] = useState('')
  const [name, setName] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const isReplace = mode === 'replace'
  const canSubmit = !submitting && (!isReplace || privateKey.trim() !== '')

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!canSubmit) return
    setSubmitting(true)
    setError('')
    try {
      if (isReplace) {
        await sshKeys.replaceDeploymentKey({ privateKey, name: name.trim() || undefined })
        onChanged('Deployment key replaced')
      } else {
        await sshKeys.regenerateDeploymentKey()
        onChanged('Deployment key regenerated')
      }
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The deployment key could not be changed.')
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      role="alertdialog"
      closeOnInteractOutside={!submitting}
      title={isReplace ? 'Replace deployment key' : 'Regenerate deployment key'}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button colorPalette="red" onClick={() => void submit()} loading={submitting} disabled={!canSubmit}>
            {isReplace ? 'Replace key' : 'Regenerate key'}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Deployment key could not be changed">
            {error}
          </Alert>
        )}
        <Alert status="warning" title="Servers deployed earlier keep the old key">
          Servers already deployed authorize only the current key ({current.fingerprint}). After this change swallow cannot log in to them with the new key
          unless their Site overrides the key or they are redeployed.
        </Alert>
        {isReplace ? (
          <>
            <Field.Root required>
              <Field.Label htmlFor="deployment-key-private">
                Private key <Field.RequiredIndicator />
              </Field.Label>
              <Textarea
                id="deployment-key-private"
                value={privateKey}
                onChange={(event) => setPrivateKey(event.target.value)}
                rows={8}
                className="sw-mono"
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                spellCheck={false}
                autoComplete="off"
                disabled={submitting}
                autoFocus
              />
              <Field.HelperText>An unencrypted OpenSSH, PKCS#1, PKCS#8, or SEC1 key. It is stored encrypted and never shown again.</Field.HelperText>
            </Field.Root>
            <Field.Root>
              <Field.Label htmlFor="deployment-key-name">Name</Field.Label>
              <Input
                id="deployment-key-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                maxLength={100}
                placeholder={current.name}
                disabled={submitting}
              />
            </Field.Root>
          </>
        ) : (
          <Text>
            Swallow generates a new ed25519 key pair for <strong>{current.name}</strong>, registers its public key in each key-capable provisioner,
            and removes the previous one. The private key is never shown.
          </Text>
        )}
      </Stack>
    </Modal>
  )
}
