import { useState } from 'react'
import { Dropdown, DropdownItem, DropdownList, MenuToggle } from '@patternfly/react-core'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import type { ProvisionerCapabilities, ReleaseServerInput } from '@/domain/server/types'
import { ServerDeleteDialog } from './ServerDeleteDialog'
import { ServerReleaseDialog } from './ServerReleaseDialog'
import { SERVER_ACTION_GROUPS, actionLabel, type ServerMenuAction } from './serverActions'
import { ServerActionResultDialog } from './ServerActionResultDialog'
import {
  rejectedServerActionOutcome,
  serverActionRunResult,
  persistServerActionResult,
  type ServerActionRunResult,
} from './serverActionResults'

/**
 * Capability-gated machine action menu for one Server.
 *
 * Accepted asynchronous actions refresh the current projection. Permanent deletion opens
 * the shared typed-confirmation dialog and navigates back to the Server list only after the
 * backend has removed both the provider Machine and the Swallow projection.
 */
export function ServerActionMenu({ serverId, serverName, capabilities, deployDisabledReason, onActed }: { serverId: string; serverName: string; capabilities: ProvisionerCapabilities | null; deployDisabledReason?: string; onActed: (action: ServerMenuAction) => void }) {
  const { servers } = useApp()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const [busy, setBusy] = useState(false)
  const [open, setOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [releaseOpen, setReleaseOpen] = useState(false)
  const [lastActionResult, setLastActionResult] = useState<ServerActionRunResult | null>(null)
  const [resultDialogOpen, setResultDialogOpen] = useState(false)

  const run = async (action: ServerMenuAction, releaseInput?: ReleaseServerInput) => {
    setOpen(false)
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
      const result = action === 'release'
        ? await servers.releaseServer(serverId, releaseInput)
        : await servers.runServerAction(serverId, action)
      persistServerActionResult(serverActionRunResult(action, [{
        serverId,
        serverName,
        accepted: true,
        message: `Provisioner accepted the action and reported ${result.state}.`,
      }]))
      showToast({
        tone: 'success',
        title: `${actionLabel(action)} accepted`,
        description: `${serverName} reports "${result.state}"; reconciliation will follow it.`,
      })
      setLastActionResult(null)
      onActed(action)
    } catch (error) {
      const actionResult = serverActionRunResult(action, [
        rejectedServerActionOutcome({ serverId, serverName }, error),
      ])
      setLastActionResult(actionResult)
      persistServerActionResult(actionResult)
      setResultDialogOpen(true)
      showToast({
        tone: 'error',
        title: `${actionLabel(action)} failed`,
        description: actionResult.outcomes[0].message ?? 'Unknown error',
      })
    } finally {
      setBusy(false)
    }
  }

  const queryPower = async () => {
    setOpen(false)
    try {
      const result = await servers.queryPowerState(serverId)
      showToast({ tone: 'info', title: `Live power state: ${result.powerState}` })
    } catch (error) {
      showToast({
        tone: 'error',
        title: 'Could not read power state',
        description: error instanceof Error ? error.message : 'Unknown error',
      })
    }
  }

  const groups = SERVER_ACTION_GROUPS.filter(
    (group) => group.capability === null || capabilities?.[group.capability],
  )
  const deployHref = () => {
    const target = new URL(scopedHref('/provisioning/deploy'), window.location.origin)
    target.searchParams.append('serverId', serverId)
    navigate(`${target.pathname}${target.search}`)
  }

  return (
    <>
      <Dropdown
        isOpen={open}
        onOpenChange={setOpen}
        toggle={(ref) => (
          <MenuToggle
            ref={ref}
            variant="primary"
            isExpanded={open}
            isDisabled={busy}
            onClick={() => setOpen((value) => !value)}
          >
            {busy ? 'Working...' : 'Take action'}
          </MenuToggle>
        )}
      >
        <DropdownList>
          <DropdownItem isDisabled={Boolean(deployDisabledReason)} onClick={deployHref}>
            {deployDisabledReason ? `Deploy OS - ${deployDisabledReason}` : 'Deploy OS'}
          </DropdownItem>
          {groups.flatMap((group) => [
            <DropdownItem key={`${group.label}-label`} isDisabled>{group.label}</DropdownItem>,
            ...group.actions.map((entry) => (
              <DropdownItem
                key={entry.action}
                isDanger={entry.destructive}
                onClick={() => void run(entry.action)}
              >
                {entry.label}
              </DropdownItem>
            )),
          ])}
          {capabilities?.power && (
            <DropdownItem onClick={() => void queryPower()}>Query power state</DropdownItem>
          )}
        </DropdownList>
      </Dropdown>
      {releaseOpen && (
        <ServerReleaseDialog
          targets={[{ serverId, serverName }]}
          supportsReleaseOptions={capabilities?.releaseOptions ?? true}
          onClose={() => setReleaseOpen(false)}
          onRelease={(input) => run('release', input)}
        />
      )}
      {lastActionResult && resultDialogOpen && (
        <ServerActionResultDialog
          result={lastActionResult}
          onClose={() => setResultDialogOpen(false)}
        />
      )}
      {deleteOpen && (
        <ServerDeleteDialog
          serverId={serverId}
          serverName={serverName}
          onClose={() => setDeleteOpen(false)}
          onDeleted={() => navigate(scopedHref('/servers'), { replace: true })}
        />
      )}
    </>
  )
}
