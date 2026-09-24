import { useState } from 'react'
import { Button, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { SoftwareAssignment } from '@/domain/software/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { softwareKindLabel } from './softwarePresentation'

interface UninstallSoftwareDialogProps {
  /** The assignment being removed; identifies the (server, kind) pair to uninstall. */
  assignment: SoftwareAssignment
  /** Human label for the target Server, resolved by the page from its server map. */
  serverLabel: string
  onClose: () => void
  /** Called with the accepted Workflow id so the page can navigate to its progress view. */
  onLaunched: (operationId: string) => void
}

/**
 * Confirms uninstalling one software kind from one Server.
 *
 * Uninstall runs the software's uninstall playbook and marks the Software Assignment absent; it
 * leaves the host operating system intact. Dismissal is blocked while the request is in flight so a
 * double submit cannot occur, and a backend rejection (for example a busy or non-deployed target)
 * is surfaced inline rather than closing the dialog.
 */
export function UninstallSoftwareDialog({ assignment, serverLabel, onClose, onLaunched }: UninstallSoftwareDialogProps) {
  const { software } = useApp()
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const label = softwareKindLabel(assignment.kind)

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (submitting) return
    setSubmitting(true)
    setError('')
    try {
      const result = await software.uninstallSoftware({ kind: assignment.kind, serverIds: [assignment.serverId] })
      onLaunched(result.operationId)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The software could not be uninstalled.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title={`Uninstall ${label}`}
      description={`Remove ${label} from ${serverLabel}. The host operating system is left intact.`}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button colorPalette="red" loading={submitting} onClick={() => void submit()}>
            Uninstall
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="The software could not be uninstalled">
            {error}
          </Alert>
        )}
        <Text>
          This starts a Workflow that removes {label} from {serverLabel} and marks its Software
          Assignment absent.
        </Text>
      </Stack>
    </Modal>
  )
}
