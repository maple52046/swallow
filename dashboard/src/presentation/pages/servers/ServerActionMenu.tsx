import { useState } from 'react'
import { Box, Button, HStack, Menu, Portal, Text } from '@chakra-ui/react'
import {
  BadgeCheck,
  ChevronDown,
  ChevronRight,
  CircleStop,
  ClipboardCheck,
  FlaskConical,
  Gauge,
  LifeBuoy,
  ListChecks,
  Lock,
  LockOpen,
  LogOut,
  MapPinned,
  Pencil,
  Power,
  PowerOff,
  RefreshCw,
  Rocket,
  Tags,
  Trash2,
  TriangleAlert,
  Wrench,
  type LucideIcon,
} from 'lucide-react'
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
import { SERVER_ACTION_GROUPS, actionLabel, serverActionAvailability, type ServerActionDef, type ServerMenuAction } from './serverActions'
import { ServerLockDialog } from './ServerLockDialog'
import { ServerActionResultDialog } from './ServerActionResultDialog'
import {
  rejectedServerActionOutcome,
  serverActionRunResult,
  persistServerActionResult,
  type ServerActionRunResult,
} from './serverActionResults'

const ACTION_ICONS: Record<ServerMenuAction, LucideIcon> = {
  release: RefreshCw,
  delete: Trash2,
  'power-on': Power,
  'power-off': PowerOff,
  commission: ClipboardCheck,
  test: FlaskConical,
  abort: CircleStop,
  'override-failed-testing': BadgeCheck,
  lock: Lock,
  unlock: LockOpen,
  'mark-broken': TriangleAlert,
  'mark-fixed': Wrench,
  'rescue-mode': LifeBuoy,
  'exit-rescue-mode': LogOut,
}

// The shared action catalogue keeps domain grouping; this map changes only detail-page labels and glyphs.
const GROUP_PRESENTATION: Partial<Record<string, { label: string; icon: LucideIcon }>> = {
  Power: { label: 'Power', icon: Power },
  'Hardware validation': { label: 'Hardware checks', icon: ClipboardCheck },
  'Operator state': { label: 'State & recovery', icon: Wrench },
}

/**
 * One compact menu row. Disabled reasons remain visible under the command and every icon is
 * decorative, leaving the text as the stable accessible name used by keyboard and test flows.
 */
function ActionMenuRow({ icon: Icon, label, reason, nested = false }: { icon: LucideIcon; label: string; reason?: string; nested?: boolean }) {
  return (
    <HStack width="full" align="flex-start" gap="3">
      <Box color="fg.muted" mt="0.5" aria-hidden><Icon size={16} /></Box>
      <Box flex="1" minW="0">
        <Text>{label}</Text>
        {reason && <Text fontSize="xs" color="fg.muted">{reason}</Text>}
      </Box>
      {nested && <ChevronRight size={16} aria-hidden />}
    </HStack>
  )
}

/**
 * Capability-gated command surface for one Server.
 *
 * Swallow-owned placement and tags live in a separate Edit menu. Provider operations are
 * grouped into nested Power, Hardware checks, and State & recovery menus so the first level
 * stays short without hiding disabled reasons. Destructive lifecycle commands remain visible
 * at the bottom and keep their existing confirmation, diagnostics, and refresh contracts.
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
  const operationGroups = groups.filter((group) => group.label !== 'Lifecycle' && group.label !== 'Removal')
  const lifecycleActions = groups.filter((group) => group.label === 'Lifecycle' || group.label === 'Removal').flatMap((group) => group.actions)
  const deployHref = () => {
    const target = new URL(scopedHref('/provisioning/deploy'), window.location.origin)
    target.searchParams.append('serverId', serverId)
    navigate(`${target.pathname}${target.search}`)
  }
  const chooseAction = (entry: ServerActionDef) => {
    if (entry.action === 'lock' || entry.action === 'unlock') setLockAction(entry.action)
    else void run(entry.action)
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
                  <ActionMenuRow icon={MapPinned} label="Set zone / pool" />
                </Menu.Item>
                <Menu.Item value="edit-tags" onSelect={() => setTagEditorOpen(true)}>
                  <ActionMenuRow icon={Tags} label="Edit tags" />
                </Menu.Item>
              </Menu.Content>
            </Menu.Positioner>
          </Portal>
        </Menu.Root>

        <Menu.Root positioning={{ placement: 'bottom-end' }}>
          <Menu.Trigger asChild>
            <Button colorPalette="brand" disabled={busy}>
              <ListChecks size={16} />
              {busy ? 'Working...' : 'Take action'}
              <ChevronDown size={16} />
            </Button>
          </Menu.Trigger>
          <Portal>
            <Menu.Positioner>
              <Menu.Content minW="17rem">
                <Menu.Item value="deploy" disabled={Boolean(deployDisabledReason)} onSelect={deployHref}>
                  <ActionMenuRow icon={Rocket} label="Deploy OS" reason={deployDisabledReason} />
                </Menu.Item>
                <Menu.Separator />
                {operationGroups.map((group) => {
                  const presentation = GROUP_PRESENTATION[group.label] ?? { label: group.label, icon: ListChecks }
                  return (
                    <Menu.Root key={group.label} positioning={{ placement: 'right-start', gutter: 4 }}>
                      <Menu.TriggerItem>
                        <ActionMenuRow icon={presentation.icon} label={presentation.label} nested />
                      </Menu.TriggerItem>
                      <Portal>
                        <Menu.Positioner>
                          <Menu.Content minW="18rem">
                            {group.actions.map((entry) => {
                              const availability = serverActionAvailability(entry.action, [server])
                              const Icon = ACTION_ICONS[entry.action]
                              return (
                                <Menu.Item
                                  key={entry.action}
                                  value={entry.action}
                                  disabled={Boolean(availability.disabledReason)}
                                  color={entry.destructive ? 'red.fg' : undefined}
                                  onSelect={() => chooseAction(entry)}
                                >
                                  <ActionMenuRow icon={Icon} label={entry.label} reason={availability.disabledReason} />
                                </Menu.Item>
                              )
                            })}
                            {group.label === 'Power' && capabilities?.power && (
                              <>
                                <Menu.Separator />
                                <Menu.Item value="query-power" onSelect={() => void queryPower()}>
                                  <ActionMenuRow icon={Gauge} label="Query power state" />
                                </Menu.Item>
                              </>
                            )}
                          </Menu.Content>
                        </Menu.Positioner>
                      </Portal>
                    </Menu.Root>
                  )
                })}
                {lifecycleActions.length > 0 && <Menu.Separator />}
                {lifecycleActions.map((entry) => {
                  const availability = serverActionAvailability(entry.action, [server])
                  const Icon = ACTION_ICONS[entry.action]
                  return (
                    <Menu.Item
                      key={entry.action}
                      value={entry.action}
                      disabled={Boolean(availability.disabledReason)}
                      color={entry.destructive ? 'red.fg' : undefined}
                      onSelect={() => chooseAction(entry)}
                    >
                      <ActionMenuRow icon={Icon} label={entry.label} reason={availability.disabledReason} />
                    </Menu.Item>
                  )
                })}
              </Menu.Content>
            </Menu.Positioner>
          </Portal>
        </Menu.Root>
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
