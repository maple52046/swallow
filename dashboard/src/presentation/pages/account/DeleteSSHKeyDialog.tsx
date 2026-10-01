import { useState } from 'react'
import { Button, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { SSHKey } from '@/domain/access/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'

interface DeleteSSHKeyDialogProps {
  /** The Access Key being deleted; the Deployment Key is never offered here. */
  sshKey: SSHKey
  onClose: () => void
  /** Called after the key is deleted, with the toast title to show. */
  onDeleted: (title: string) => void
}

/**
 * Confirms deleting one of the signed-in admin's Access Keys.
 *
 * The consequence is stated before the request: swallow removes the key from provisioners where it
 * registered it, so Servers deployed afterwards stop accepting it, but Servers already deployed keep
 * it in their authorized_keys. A backend refusal keeps the dialog open with the reason.
 */
export function DeleteSSHKeyDialog({ sshKey, onClose, onDeleted }: DeleteSSHKeyDialogProps) {
  const { sshKeys } = useApp()
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (submitting) return
    setSubmitting(true)
    setError('')
    try {
      await sshKeys.deleteAccessKey(sshKey.id)
      onDeleted('SSH key deleted')
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The key could not be deleted.')
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      role="alertdialog"
      closeOnInteractOutside={!submitting}
      title={`Delete ${sshKey.name}`}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button colorPalette="red" onClick={() => void submit()} loading={submitting}>
            Delete key
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Key could not be deleted">
            {error}
          </Alert>
        )}
        <Text>
          Swallow removes <strong>{sshKey.name}</strong> ({sshKey.fingerprint}) from every provisioner where it registered
          it, so Servers deployed from now on will not accept it.
        </Text>
        <Alert status="warning" title="Deployed Servers keep the key">
          Servers that were already deployed still have this key in their authorized_keys. Remove it on those hosts, or redeploy them, if it must stop working there.
        </Alert>
      </Stack>
    </Modal>
  )
}
