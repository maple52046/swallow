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
 * Opens the shared Software-first installation flow with one fixed Server.
 *
 * Catalog and existing assignments are read together so each choice can distinguish a new install,
 * retry, reconfigure, mutual exclusion, or in-progress Workflow. Nothing is rendered during that
 * short read: swapping a placeholder dialog would move focus outside the newly mounted dialog and
 * dismiss it. A failed read remains a real dialog with a retry action.
 */
export function ServerSoftwareInstallDialog({ server, onClose, onLaunched }: ServerSoftwareInstallDialogProps) {
  const { software } = useApp()
  const workspace = useAsyncData(async () => {
    const [catalog, assignments] = await Promise.all([
      software.listCatalog(),
      software.listAssignments({ serverId: server.id }),
    ])
    return { catalog, assignments }
  }, [software, server.id])

  if (workspace.status === 'loading') return null
  if (workspace.status === 'ready') {
    return (
      <InstallSoftwareDialog
        catalog={workspace.data.catalog}
        assignments={workspace.data.assignments}
        servers={[server]}
        fixedServer={server}
        onClose={onClose}
        onLaunched={onLaunched}
      />
    )
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
      <ErrorState message={workspace.message} onRetry={workspace.reload} />
    </Modal>
  )
}
