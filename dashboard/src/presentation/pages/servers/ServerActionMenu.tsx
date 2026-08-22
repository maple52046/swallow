import { useState } from 'react'
import { Button, DropdownMenu } from '@radix-ui/themes'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/radix/toast/toastContext'
import type { ProvisionerCapabilities } from '@/domain/server/types'
import { SERVER_ACTION_GROUPS, actionLabel, type BulkAction } from './serverActions'

interface ServerActionMenuProps {
  serverId: string
  serverName: string
  /** Live capabilities; when null (detail unavailable) only capability-free actions show. */
  capabilities: ProvisionerCapabilities | null
  /** Called after an action is accepted, to refetch the detail. */
  onActed: () => void
}

/**
 * The capability-gated action menu in the detail header.
 *
 * Shows only the action groups the provisioner supports (Lifecycle is always available;
 * Power/validation/operator groups appear per `capabilities`), matching MAAS's header
 * action menu. Deploy is intentionally absent here because it needs inputs — it lives as
 * a form on the Summary tab. Each action fans through the per-server API and reports the
 * outcome as a toast, then triggers a refetch. Power state can also be queried read-only.
 */
export function ServerActionMenu({ serverId, serverName, capabilities, onActed }: ServerActionMenuProps) {
  const { servers } = useApp()
  const { showToast } = useToast()
  const [busy, setBusy] = useState(false)

  const runAction = async (action: BulkAction) => {
    setBusy(true)
    try {
      const result =
        action === 'release'
          ? await servers.releaseServer(serverId)
          : await servers.runServerAction(serverId, action)
      showToast({
        tone: 'success',
        title: `${actionLabel(action)} accepted`,
        description: `${serverName} reports "${result.state}"; the reconciler will follow it.`,
      })
      onActed()
    } catch (err) {
      showToast({
        tone: 'error',
        title: `${actionLabel(action)} failed`,
        description: err instanceof Error ? err.message : 'Unknown error',
      })
    } finally {
      setBusy(false)
    }
  }

  const queryPower = async () => {
    try {
      const result = await servers.queryPowerState(serverId)
      showToast({ tone: 'info', title: `Live power state: ${result.powerState}` })
    } catch (err) {
      showToast({
        tone: 'error',
        title: 'Could not read power state',
        description: err instanceof Error ? err.message : 'Unknown error',
      })
    }
  }

  const visibleGroups = SERVER_ACTION_GROUPS.filter(
    (group) => group.capability === null || capabilities?.[group.capability],
  )

  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        <Button loading={busy}>
          Actions
          <DropdownMenu.TriggerIcon />
        </Button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end">
        {visibleGroups.map((group, groupIndex) => (
          <DropdownMenu.Group key={group.label}>
            {groupIndex > 0 && <DropdownMenu.Separator />}
            <DropdownMenu.Label>{group.label}</DropdownMenu.Label>
            {group.actions.map((entry) => (
              <DropdownMenu.Item
                key={entry.action}
                color={entry.destructive ? 'red' : undefined}
                onSelect={() => void runAction(entry.action)}
              >
                {entry.label}
              </DropdownMenu.Item>
            ))}
          </DropdownMenu.Group>
        ))}
        {capabilities?.power && (
          <>
            <DropdownMenu.Separator />
            <DropdownMenu.Item onSelect={() => void queryPower()}>Query power state</DropdownMenu.Item>
          </>
        )}
      </DropdownMenu.Content>
    </DropdownMenu.Root>
  )
}
