import { useState } from 'react'
import { Box, Button, Menu, Portal, Text } from '@chakra-ui/react'
import { ChevronDown } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { serverDisplayName } from '@/domain/server/list'
import type { ProvisionerCapabilities, ProvisioningActionResult, ReleaseServerInput, Server } from '@/domain/server/types'
import { ServerDeleteDialog } from './ServerDeleteDialog'
import { ServerReleaseDialog } from './ServerReleaseDialog'
import { SERVER_ACTION_GROUPS, actionLabel, serverActionAvailability, type ServerMenuAction } from './serverActions'
import { ServerLockDialog } from './ServerLockDialog'
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
 * Accepted asynchronous actions refresh the current projection. Permanent deletion opens the
 * shared typed-confirmation dialog and navigates back to the Server list only after the backend
 * has removed both the provider Machine and the Swallow projection. Gated actions (locked
 * Server, missing capability) stay visible but disabled, with the reason shown beneath the item.
 */
export function ServerActionMenu({
  server,
  capabilities,
  deployDisabledReason,
  onActed,
}: {
  server: Server
  capabilities: ProvisionerCapabilities | null
  deployDisabledReason?: string
  onActed: (
    action: ServerMenuAction,
    input: ReleaseServerInput | undefined,
    // Optional: an accepted release creates a durable Operation rather than returning a
    // synchronous provisioning result, so it reloads without one.
    result?: ProvisioningActionResult,
  ) => void
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
        // Stay on the Server detail page and reload so its projection converges in place.
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

  const groups = SERVER_ACTION_GROUPS.filter((group) => group.capability === null || capabilities?.[group.capability])
  const deployHref = () => {
    const target = new URL(scopedHref('/provisioning/deploy'), window.location.origin)
    target.searchParams.append('serverId', serverId)
    navigate(`${target.pathname}${target.search}`)
  }

  return (
    <>
      <Menu.Root>
        <Menu.Trigger asChild>
          <Button colorPalette="brand" disabled={busy}>
            {busy ? 'Working...' : 'Take action'}
            <ChevronDown size={16} />
          </Button>
        </Menu.Trigger>
        <Portal>
          <Menu.Positioner>
            <Menu.Content minW="14rem">
              <Menu.Item value="deploy" disabled={Boolean(deployDisabledReason)} onClick={deployHref}>
                {deployDisabledReason ? `Deploy OS - ${deployDisabledReason}` : 'Deploy OS'}
              </Menu.Item>
              {groups.map((group) => (
                <Menu.ItemGroup key={group.label}>
                  <Menu.ItemGroupLabel>{group.label}</Menu.ItemGroupLabel>
                  {group.actions.map((entry) => {
                    const availability = serverActionAvailability(entry.action, [server])
                    return (
                      <Menu.Item
                        key={entry.action}
                        value={entry.action}
                        disabled={Boolean(availability.disabledReason)}
                        color={entry.destructive ? 'red.fg' : undefined}
                        onClick={() => {
                          if (entry.action === 'lock' || entry.action === 'unlock') setLockAction(entry.action)
                          else void run(entry.action)
                        }}
                      >
                        <Box>
                          <Text>{entry.label}</Text>
                          {availability.disabledReason && (
                            <Text fontSize="xs" color="fg.muted">
                              {availability.disabledReason}
                            </Text>
                          )}
                        </Box>
                      </Menu.Item>
                    )
                  })}
                </Menu.ItemGroup>
              ))}
              {capabilities?.power && (
                <Menu.Item value="query-power" onClick={() => void queryPower()}>
                  Query power state
                </Menu.Item>
              )}
            </Menu.Content>
          </Menu.Positioner>
        </Portal>
      </Menu.Root>
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
    </>
  )
}
