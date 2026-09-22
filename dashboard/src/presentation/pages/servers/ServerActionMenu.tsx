import { useState } from 'react'
import { Button, HStack, Menu, Portal } from '@chakra-ui/react'
import { ChevronDown, MapPinned, Pencil, Tags } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { ProvisionerCapabilities, ProvisioningActionResult, ReleaseServerInput, Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/list'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { ServerDeleteDialog } from './ServerDeleteDialog'
import { ServerPlacementDialog } from './ServerPlacementDialog'
import { ServerTagEditor } from './ServerTagEditor'
import { ServerReleaseDialog } from './ServerReleaseDialog'
import { actionLabel, isRamDeploy, serverActionAvailability, type ServerMenuAction } from './serverActions'
import { ServerLockDialog } from './ServerLockDialog'
import { ServerPowerOffWarningDialog } from './ServerPowerOffWarningDialog'
import { ServerActionResultDialog } from './ServerActionResultDialog'
import { ServerActionMenuRow, ServerTakeActionMenu } from './ServerTakeActionMenu'
import {
  rejectedServerActionOutcome,
  serverActionRunResult,
  persistServerActionResult,
  type ServerActionRunResult,
} from './serverActionResults'

/**
 * Capability-gated command surface for one Server on the detail page.
 *
 * Swallow-owned placement and tags live in a separate Edit menu. Provider operations reuse
 * the shared nested Take-action menu so list and detail never drift. Destructive lifecycle
 * commands keep their existing confirmation, diagnostics, and refresh contracts.
 */
export function ServerActionMenu({
  server,
  capabilities,
  deployDisabledReason,
  onActed,
  onPlacementChanged,
  onTagsChanged,
}: {
  server: Server
  capabilities: ProvisionerCapabilities | null
  deployDisabledReason?: string
  onActed: (
    action: ServerMenuAction,
    input: ReleaseServerInput | undefined,
    result?: ProvisioningActionResult,
  ) => void
  /** Called after a zone/pool placement change so the caller can reload the projection. */
  onPlacementChanged?: () => void
  /** Called after a tag edit so the caller can reload the projection (tags drive Server Type). */
  onTagsChanged?: () => void
}) {
  const serverId = server.id
  const serverName = serverDisplayName(server)
  const { servers, provisioning } = useApp()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const [busy, setBusy] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [releaseOpen, setReleaseOpen] = useState(false)
  const [lastActionResult, setLastActionResult] = useState<ServerActionRunResult | null>(null)
  const [resultDialogOpen, setResultDialogOpen] = useState(false)
  const [lockAction, setLockAction] = useState<'lock' | 'unlock' | null>(null)
  // Gates a power off on this Server behind a warning + acknowledgement when it is a RAM
  // (ephemeral) deployment, which has no persistent disk so anything written to it is lost.
  const [powerOffWarnOpen, setPowerOffWarnOpen] = useState(false)
  const [placementOpen, setPlacementOpen] = useState(false)
  const [tagEditorOpen, setTagEditorOpen] = useState(false)

  const run = async (action: ServerMenuAction, releaseInput?: ReleaseServerInput) => {
    if (action === 'delete') {
      setDeleteOpen(true)
      return
    }
    if (action === 'release' && !releaseInput) {
      setReleaseOpen(true)
      return
    }

    setBusy(true)
    try {
      if (action === 'release') {
        await provisioning.createReleaseOperation({ serverIds: [serverId], ...releaseInput })
        showToast({
          tone: 'success',
          title: 'Release Operation created',
          description: 'Swallow will observe the provider until the Server is ready.',
        })
        onActed(action, releaseInput)
        return
      }
      if (action === 'recover') {
        // Recover is a Swallow-owned durable Operation, not a single provider action: it
        // chooses the primitive by state (Mark fixed / exit rescue / Release) and converges to
        // Ready. It reuses the same accepted-and-followed contract as Release.
        await provisioning.createRecoverOperation({ serverIds: [serverId] })
        showToast({
          tone: 'success',
          title: 'Recover Operation created',
          description: 'Swallow will return the Server to the ready pool.',
        })
        onActed(action, releaseInput)
        return
      }
      const result = await servers.runServerAction(serverId, action)
      persistServerActionResult(
        serverActionRunResult(action, [
          {
            serverId,
            serverName,
            accepted: true,
            taskId: result.taskId,
            message: `Provisioner accepted the action and reported ${result.state}.`,
          },
        ]),
      )
      showToast({
        tone: 'success',
        title: `${actionLabel(action)} accepted`,
        description: `${serverName} reports "${result.state}"; reconciliation will follow it.`,
      })
      setLastActionResult(null)
      onActed(action, releaseInput, result)
    } catch (error) {
      const actionResult = serverActionRunResult(action, [rejectedServerActionOutcome({ serverId, serverName }, error)])
      setLastActionResult(actionResult)
      persistServerActionResult(actionResult)
      setResultDialogOpen(true)
      showToast({ tone: 'error', title: `${actionLabel(action)} failed`, description: actionResult.outcomes[0].message ?? 'Unknown error' })
    } finally {
      setBusy(false)
    }
  }

  const queryPower = async () => {
    try {
      const result = await servers.queryPowerState(serverId)
      showToast({ tone: 'info', title: `Live power state: ${result.powerState}` })
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not read power state', description: error instanceof Error ? error.message : 'Unknown error' })
    }
  }

  const deployHref = () => {
    const target = new URL(scopedHref('/provisioning/deploy'), window.location.origin)
    target.searchParams.append('serverId', serverId)
    navigate(`${target.pathname}${target.search}`)
  }

  const chooseAction = (action: ServerMenuAction) => {
    if (action === 'lock' || action === 'unlock') setLockAction(action)
    else if (action === 'power-off' && isRamDeploy(server)) setPowerOffWarnOpen(true)
    else void run(action)
  }

  return (
    <>
      <HStack gap="2" className="sw-server-header-actions">
        <Menu.Root positioning={{ placement: 'bottom-end' }}>
          <Menu.Trigger asChild>
            <Button variant="outline" disabled={busy}>
              <Pencil size={16} />
              Edit
              <ChevronDown size={16} />
            </Button>
          </Menu.Trigger>
          <Portal>
            <Menu.Positioner>
              <Menu.Content minW="13rem">
                <Menu.Item value="set-placement" onSelect={() => setPlacementOpen(true)}>
                  <ServerActionMenuRow icon={MapPinned} label="Set zone / pool" />
                </Menu.Item>
                <Menu.Item value="edit-tags" onSelect={() => setTagEditorOpen(true)}>
                  <ServerActionMenuRow icon={Tags} label="Edit tags" />
                </Menu.Item>
              </Menu.Content>
            </Menu.Positioner>
          </Portal>
        </Menu.Root>

        <ServerTakeActionMenu
          targets={[server]}
          capabilities={capabilities}
          filterByCapabilities
          includeDeploy
          includeQueryPower
          busy={busy}
          trigger="take-action"
          deployDisabledReason={deployDisabledReason}
          onAction={chooseAction}
          onDeploy={deployHref}
          onQueryPower={() => void queryPower()}
        />
      </HStack>

      {releaseOpen && (
        <ServerReleaseDialog
          targets={[{ serverId, serverName }]}
          supportsReleaseOptions={capabilities?.releaseOptions ?? true}
          supportsNetworkConfiguration={capabilities?.networkConfiguration ?? true}
          onClose={() => setReleaseOpen(false)}
          onRelease={(input) => run('release', input)}
        />
      )}
      {lockAction && (
        <ServerLockDialog
          action={lockAction}
          targets={serverActionAvailability(lockAction, [server]).eligible}
          skipped={serverActionAvailability(lockAction, [server]).skipped}
          busy={busy}
          onClose={() => setLockAction(null)}
          onConfirm={() => {
            const action = lockAction
            setLockAction(null)
            void run(action)
          }}
        />
      )}
      {powerOffWarnOpen && (
        <ServerPowerOffWarningDialog
          ramTargets={[server]}
          totalTargets={1}
          busy={busy}
          onClose={() => setPowerOffWarnOpen(false)}
          onConfirm={() => {
            setPowerOffWarnOpen(false)
            void run('power-off')
          }}
        />
      )}
      {lastActionResult && resultDialogOpen && (
        <ServerActionResultDialog result={lastActionResult} onClose={() => setResultDialogOpen(false)} />
      )}
      {deleteOpen && (
        <ServerDeleteDialog
          serverId={serverId}
          serverName={serverName}
          onClose={() => setDeleteOpen(false)}
          onDeleted={() => navigate(scopedHref('/servers'), { replace: true })}
        />
      )}
      {placementOpen && (
        <ServerPlacementDialog
          server={server}
          onClose={() => setPlacementOpen(false)}
          onChanged={() => {
            showToast({ tone: 'success', title: 'Placement updated', description: 'Reconciliation will confirm the new zone/pool.' })
            onPlacementChanged?.()
          }}
        />
      )}
      {tagEditorOpen && (
        <ServerTagEditor
          servers={[server]}
          onClose={() => setTagEditorOpen(false)}
          onSaved={() => onTagsChanged?.()}
        />
      )}
    </>
  )
}
