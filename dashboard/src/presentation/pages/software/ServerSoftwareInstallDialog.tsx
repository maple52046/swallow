import { Button } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { serverDisplayName, type Server } from '@/domain/server/types'
import { ErrorState } from '@/presentation/components/ErrorState'
import { Modal } from '@/presentation/components/ui/modal'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { InstallSoftwareDialog } from './InstallSoftwareDialog'

interface ServerSoftwareInstallDialogProps {
  /** The Server to install on; the caller has checked it against the install-target rule. */
  server: Server
  onClose: () => void
  /** Called with the accepted Workflow id, as from the Software page. */
  onLaunched: (operationId: string) => void
}

/**
 * Install software from a Server's detail page: the shared install dialog with this Server as its
 * fixed target. The software catalog is read when it opens, because the detail page does not load
 * it otherwise.
 *
 * Nothing is rendered while that short read runs. A placeholder dialog would have to be swapped for
 * the install dialog once the catalog arrives, and unmounting one dialog returns focus outside the
 * next, which then dismisses itself. A failed read shows the reason in a dialog with a retry.
 */
export function ServerSoftwareInstallDialog({ server, onClose, onLaunched }: ServerSoftwareInstallDialogProps) {
  const { software } = useApp()
  const catalog = useAsyncData(() => software.listCatalog(), [software])

  if (catalog.status === 'loading') return null
  if (catalog.status === 'ready') {
    return <InstallSoftwareDialog catalog={catalog.data} servers={[server]} fixedTargets onClose={onClose} onLaunched={onLaunched} />
  }
  return (
    <Modal
      open
      onClose={onClose}
      title="Install software"
      description={`The software catalog could not be read, so nothing can be installed on ${serverDisplayName(server)} yet.`}
      footer={
        <Button variant="ghost" onClick={onClose}>
          Close
        </Button>
      }
    >
      <ErrorState message={catalog.message} onRetry={catalog.reload} />
    </Modal>
  )
}
