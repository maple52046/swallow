import { useState } from 'react'
import { Button, Field, HStack, Input, Stack, Text, Textarea } from '@chakra-ui/react'
import { Download } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import { CopyButton } from '@/presentation/components/CopyButton'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { downloadTextFile } from '@/presentation/utils/download'
import { privateKeyFileName } from './sshKeyPresentation'

interface GenerateSSHKeyDialogProps {
  onClose: () => void
  /** Called once the operator has confirmed saving the private key, with the toast title to show. */
  onGenerated: (title: string) => void
}

/** The private key held only in this dialog's memory between generation and confirmation. */
interface GeneratedSecret {
  name: string
  privateKey: string
}

/**
 * Generates an ed25519 Access Key pair for the signed-in admin and shows its private key once.
 *
 * swallow keeps only the public key, so this dialog is the only place the private key ever exists
 * outside the operator's machine. Security and state contract:
 * - The private key lives only in component state; it is never written to storage or logged, and
 *   it is dropped when the dialog unmounts.
 * - After generation the dialog cannot be dismissed (Escape, backdrop, close button) until the
 *   operator confirms they saved the key, because a dismissal would lose it irrecoverably.
 * - Download creates a local file from memory; nothing is sent anywhere.
 */
export function GenerateSSHKeyDialog({ onClose, onGenerated }: GenerateSSHKeyDialogProps) {
  const { sshKeys } = useApp()
  const [name, setName] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [secret, setSecret] = useState<GeneratedSecret | null>(null)
  const [saved, setSaved] = useState(false)

  const close = () => {
    if (submitting) return
    // Once a key exists, only the explicit "Done" after confirmation may close the dialog.
    if (secret && !saved) return
    if (secret) {
      onGenerated('SSH key pair generated')
      return
    }
    onClose()
  }

  const generate = async () => {
    if (submitting || !name.trim()) return
    setSubmitting(true)
    setError('')
    try {
      const result = await sshKeys.generateAccessKey(name.trim())
      setSecret({ name: result.key.name, privateKey: result.privateKey })
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The key pair could not be generated.')
    } finally {
      setSubmitting(false)
    }
  }

  const download = () => {
    if (!secret) return
    downloadTextFile(privateKeyFileName(secret.name), secret.privateKey, 'application/x-pem-file')
  }

  if (secret) {
    return (
      <Modal
        open
        onClose={close}
        closeOnInteractOutside={false}
        title="Save your private key"
        description="This is the only time the private key is shown. Swallow keeps only the public key."
        footer={
          <Button colorPalette="brand" onClick={close} disabled={!saved}>
            Done
          </Button>
        }
      >
        <Stack gap="4">
          <Alert status="warning" title="It cannot be shown again">
            Download or copy the private key now and store it somewhere safe. If you lose it, delete this key and generate a new one.
          </Alert>
          <Field.Root>
            <HStack justify="space-between" width="full">
              <Field.Label htmlFor="ssh-key-generated-private">Private key for {secret.name}</Field.Label>
              <HStack gap="1">
                <CopyButton value={secret.privateKey} label="Copy private key" />
                <Button size="xs" variant="outline" onClick={download}>
                  <Download size={14} />
                  Download
                </Button>
              </HStack>
            </HStack>
            <Textarea
              id="ssh-key-generated-private"
              value={secret.privateKey}
              readOnly
              rows={8}
              className="sw-mono"
              spellCheck={false}
            />
            <Field.HelperText>
              Save it with permissions 0600, for example as ~/.ssh/{privateKeyFileName(secret.name)}.
            </Field.HelperText>
          </Field.Root>
          <Checkbox checked={saved} onCheckedChange={setSaved}>
            I have saved the private key
          </Checkbox>
        </Stack>
      </Modal>
    )
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Generate SSH key pair"
      description="Swallow generates an ed25519 key pair, keeps the public key as one of your Access Keys, and shows the private key once."
      onSubmit={(event) => {
        event.preventDefault()
        void generate()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={submitting || !name.trim()}>
            Generate
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Key pair could not be generated">
            {error}
          </Alert>
        )}
        <Field.Root required>
          <Field.Label htmlFor="ssh-key-generate-name">
            Name <Field.RequiredIndicator />
          </Field.Label>
          <Input
            id="ssh-key-generate-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            maxLength={100}
            placeholder="e.g. ops-jumpbox"
            disabled={submitting}
            autoFocus
          />
        </Field.Root>
        <Text color="fg.muted" fontSize="sm">
          The public key is registered in each key-capable provisioner, so Servers deployed afterwards accept it.
        </Text>
      </Stack>
    </Modal>
  )
}
