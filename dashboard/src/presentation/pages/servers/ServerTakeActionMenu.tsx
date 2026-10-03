/**
 * Shared Take-action menu for Server list and Server detail.
 *
 * One nested presentation (icons, remapped group labels, disabled reasons) so list row
 * kebab / bulk Take action and detail Take action never drift. Callers own execution,
 * confirmation dialogs, and whether Deploy OS / Query power appear.
 */
import { Box, Button, HStack, IconButton, Menu, Portal, Text } from '@chakra-ui/react'
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
  PackagePlus,
  LockOpen,
  LogOut,
  MoreVertical,
  Power,
  PowerOff,
  RefreshCw,
  Rocket,
  RotateCcw,
  Trash2,
  TriangleAlert,
  Wrench,
  type LucideIcon,
} from 'lucide-react'
import type { ProvisionerCapabilities, Server } from '@/domain/server/types'
import {
  SERVER_ACTION_GROUPS,
  serverActionAvailability,
  type ServerActionDef,
  type ServerMenuAction,
} from './serverActions'

const ACTION_ICONS: Record<ServerMenuAction, LucideIcon> = {
  recover: RotateCcw,
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

// Always-on operator guidance shown under an action even when it is enabled. Rescue is a
// diagnostic environment, not a recovery path, so the menu states that up front rather than
// letting an operator learn it by exiting rescue back into Failed. See the rescue-mode glossary.
const ACTION_HINTS: Partial<Record<ServerMenuAction, string>> = {
  recover: 'Returns the Server to Ready (Mark fixed, exit rescue, or Release by state).',
  'rescue-mode': 'Ephemeral diagnostic boot. Exit returns to the previous state; use Recover or Release to reach Ready.',
  'exit-rescue-mode': 'Restores the pre-rescue state (often still Failed or Broken); use Recover to reach Ready.',
}

/** Detail-oriented labels/glyphs for catalogue groups; list reuses the same map. */
const GROUP_PRESENTATION: Partial<Record<string, { label: string; icon: LucideIcon }>> = {
  Power: { label: 'Power', icon: Power },
  'Hardware validation': { label: 'Hardware checks', icon: ClipboardCheck },
  'Operator state': { label: 'State & recovery', icon: Wrench },
}

/**
 * One compact menu row. Disabled reasons stay under the command; icons are decorative so
 * the text remains the accessible name for keyboard and e2e selectors.
 */
export function ServerActionMenuRow({
  icon: Icon,
  label,
  reason,
  nested = false,
}: {
  icon: LucideIcon
  label: string
  reason?: string
  nested?: boolean
}) {
  return (
    <HStack width="full" align="flex-start" gap="3">
      <Box color="fg.muted" mt="0.5" aria-hidden>
        <Icon size={16} />
      </Box>
      <Box flex="1" minW="0">
        <Text>{label}</Text>
        {reason && (
          <Text fontSize="xs" color="fg.muted">
            {reason}
          </Text>
        )}
      </Box>
      {nested && <ChevronRight size={16} aria-hidden />}
    </HStack>
  )
}

export type ServerTakeActionTrigger = 'take-action' | 'kebab' | 'actions'

/**
 * Nested Take-action surface shared by list and detail.
 *
 * - Detail passes `capabilities` so unsupported groups hide; list omits it and shows every
 *   catalogue group (backend refuses per Server).
 * - `includeSingleOnly` keeps Delete on single-Server menus and out of bulk.
 * - Deploy OS / Install software / Query power are optional slots owned by detail; list keeps
 *   those elsewhere. A disabled slot shows its reason under the command.
 */
export function ServerTakeActionMenu({
  targets,
  capabilities = null,
  filterByCapabilities = false,
  includeSingleOnly = true,
  includeDeploy = false,
  includeQueryPower = false,
  busy = false,
  trigger = 'take-action',
  /** List toolbars pass `sm`; omit on detail so the header control matches Edit. */
  size,
  deployDisabledReason,
  includeInstallSoftware = false,
  installSoftwareDisabledReason,
  onAction,
  onDeploy,
  onInstallSoftware,
  onQueryPower,
}: {
  targets: readonly Server[]
  /** Live provisioner capabilities; only used when `filterByCapabilities` is true. */
  capabilities?: ProvisionerCapabilities | null
  /** Detail hides unsupported groups; list leaves every group visible. */
  filterByCapabilities?: boolean
  /** When false, actions marked `bulk: false` (Delete) are omitted. */
  includeSingleOnly?: boolean
  includeDeploy?: boolean
  includeQueryPower?: boolean
  busy?: boolean
  trigger?: ServerTakeActionTrigger
  size?: 'sm' | 'md'
  deployDisabledReason?: string
  /** Detail only: offer Install software for the one target (Managed Software, decision 038). */
  includeInstallSoftware?: boolean
  installSoftwareDisabledReason?: string
  onAction: (action: ServerMenuAction) => void
  onDeploy?: () => void
  onInstallSoftware?: () => void
  onQueryPower?: () => void
}) {
  const groups = SERVER_ACTION_GROUPS.filter((group) => {
    if (filterByCapabilities && group.capability !== null && !capabilities?.[group.capability]) {
      return false
    }
    const actions = group.actions.filter((entry) => includeSingleOnly || entry.bulk !== false)
    return actions.length > 0
  }).map((group) => ({
    ...group,
    actions: group.actions.filter((entry) => includeSingleOnly || entry.bulk !== false),
  }))
  const operationGroups = groups.filter((group) => group.label !== 'Lifecycle' && group.label !== 'Removal')
  const lifecycleActions = groups
    .filter((group) => group.label === 'Lifecycle' || group.label === 'Removal')
    .flatMap((group) => group.actions)

  const chooseAction = (entry: ServerActionDef) => {
    onAction(entry.action)
  }

  return (
    <Menu.Root positioning={{ placement: 'bottom-end' }}>
      <Menu.Trigger asChild>
        {trigger === 'kebab' ? (
          <IconButton variant="ghost" size={size ?? 'sm'} aria-label="Actions" disabled={busy}>
            <MoreVertical size={16} />
          </IconButton>
        ) : trigger === 'actions' ? (
          <Button variant="outline" size={size ?? 'sm'} disabled={busy}>
            Actions
            <ChevronDown size={16} />
          </Button>
        ) : (
          <Button colorPalette="brand" size={size} disabled={busy}>
            <ListChecks size={16} />
            {busy ? 'Working...' : 'Take action'}
            <ChevronDown size={16} />
          </Button>
        )}
      </Menu.Trigger>
      <Portal>
        <Menu.Positioner>
          <Menu.Content minW="17rem">
            {includeDeploy && (
              <Menu.Item value="deploy" disabled={Boolean(deployDisabledReason)} onSelect={() => onDeploy?.()}>
                <ServerActionMenuRow icon={Rocket} label="Deploy OS" reason={deployDisabledReason} />
              </Menu.Item>
            )}
            {includeInstallSoftware && (
              <Menu.Item value="install-software" disabled={Boolean(installSoftwareDisabledReason)} onSelect={() => onInstallSoftware?.()}>
                <ServerActionMenuRow icon={PackagePlus} label="Install software" reason={installSoftwareDisabledReason} />
              </Menu.Item>
            )}
            {(includeDeploy || includeInstallSoftware) && <Menu.Separator />}
            {operationGroups.map((group) => {
              const presentation = GROUP_PRESENTATION[group.label] ?? { label: group.label, icon: ListChecks }
              return (
                <Menu.Root key={group.label} positioning={{ placement: 'right-start', gutter: 4 }}>
                  <Menu.TriggerItem>
                    <ServerActionMenuRow icon={presentation.icon} label={presentation.label} nested />
                  </Menu.TriggerItem>
                  <Portal>
                    <Menu.Positioner>
                      <Menu.Content minW="18rem">
                        {group.actions.map((entry) => {
                          const availability = serverActionAvailability(entry.action, targets)
                          const Icon = ACTION_ICONS[entry.action]
                          return (
                            <Menu.Item
                              key={entry.action}
                              value={entry.action}
                              disabled={Boolean(availability.disabledReason)}
                              color={entry.destructive ? 'red.fg' : undefined}
                              onSelect={() => chooseAction(entry)}
                            >
                              <ServerActionMenuRow icon={Icon} label={entry.label} reason={availability.disabledReason ?? ACTION_HINTS[entry.action]} />
                            </Menu.Item>
                          )
                        })}
                        {group.label === 'Power' && includeQueryPower && capabilities?.power && (
                          <>
                            <Menu.Separator />
                            <Menu.Item value="query-power" onSelect={() => onQueryPower?.()}>
                              <ServerActionMenuRow icon={Gauge} label="Query power state" />
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
              const availability = serverActionAvailability(entry.action, targets)
              const Icon = ACTION_ICONS[entry.action]
              return (
                <Menu.Item
                  key={entry.action}
                  value={entry.action}
                  disabled={Boolean(availability.disabledReason)}
                  color={entry.destructive ? 'red.fg' : undefined}
                  onSelect={() => chooseAction(entry)}
                >
                  <ServerActionMenuRow icon={Icon} label={entry.label} reason={availability.disabledReason ?? ACTION_HINTS[entry.action]} />
                </Menu.Item>
              )
            })}
          </Menu.Content>
        </Menu.Positioner>
      </Portal>
    </Menu.Root>
  )
}
