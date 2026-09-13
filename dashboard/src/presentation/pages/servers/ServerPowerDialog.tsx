import { Button, Text } from '@chakra-ui/react'
import { Power, PowerOff, type LucideIcon } from 'lucide-react'
import type { Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/list'
import { powerStateLabel } from '@/presentation/components/axisBadgeUtils'
import { Modal } from '@/presentation/components/ui/modal'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { serverActionAvailability } from './serverActions'

/** The provisioner power actions this dialog can trigger. A subset of the Power action group. */
type PowerAction = 'power-on' | 'power-off'

/** One square tile in the action grid. `reason` present means the action is gated (e.g. locked). */
interface PowerTile {
  action: PowerAction
  label: string
  Icon: LucideIcon
  /** Icon tint conveying the action's meaning: powering on is success-green, off is danger-red. */
  tone: 'success' | 'danger'
  reason?: string
}

interface ServerPowerDialogProps {
  /** The Server whose power the operator is acting on; drives the current-state hint and gating. */
  server: Server
  /**
   * Runs the chosen power action. The dialog does not execute it directly: it hands the action
   * back to the list, which routes it through the shared action runner (toast + list reload).
   */
  onSelect: (action: PowerAction) => void
  /** Dismisses the dialog without acting. */
  onClose: () => void
}

/**
 * Lets an operator pick a power action (on/off) for one Server from the server list, opened by
 * the power-state icon button in the Power column.
 *
 * The two actions are presented as a two-column grid of square tiles. The list has no live
 * `ProvisionerCapabilities`, so — like the row's action menu — both actions are offered and the
 * backend refuses unsupported ones. `serverActionAvailability` still gates locked Servers here:
 * a gated tile is `aria-disabled` with the reason in a Tooltip and its click suppressed, so the
 * reason stays discoverable without breaking the square grid. Selecting an action closes the
 * dialog and delegates execution to the caller.
 */
export function ServerPowerDialog({ server, onSelect, onClose }: ServerPowerDialogProps) {
  const name = serverDisplayName(server)
  const currentState = server.provisioning?.powerState ?? null
  // Both actions are always offered (a provider can report `unknown`/`error`, and forcing the
  // opposite action must stay possible); the current state is shown as a hint above the grid.
  const tiles: readonly PowerTile[] = [
    { action: 'power-on', label: 'Power on', Icon: Power, tone: 'success', reason: serverActionAvailability('power-on', [server]).disabledReason },
    { action: 'power-off', label: 'Power off', Icon: PowerOff, tone: 'danger', reason: serverActionAvailability('power-off', [server]).disabledReason },
  ]

  return (
    <Modal
      open
      onClose={onClose}
      size="xs"
      title="Power actions"
      footer={
        <Button variant="ghost" onClick={onClose}>
          Cancel
        </Button>
      }
    >
      <Text mb="3">
        <strong>{name}</strong>
        <br />
        Current power state: {powerStateLabel(currentState)}.
      </Text>
      <div className="sw-power-action-grid">
        {tiles.map(({ action, label, Icon, tone, reason }) => {
          const button = (
            <Button
              variant="plain"
              className="sw-power-tile"
              aria-disabled={Boolean(reason)}
              // aria-disabled keeps the button focusable/hoverable so the Tooltip reason is
              // reachable; guard the click so a gated action cannot still fire.
              onClick={() => {
                if (!reason) onSelect(action)
              }}
            >
              <span className={`sw-power-tile__icon sw-power-tile__icon--${tone}`} aria-hidden>
                <Icon size={28} />
              </span>
              <span className="sw-power-tile__label">{label}</span>
            </Button>
          )
          return <div key={action}>{reason ? <Tooltip content={reason}>{button}</Tooltip> : button}</div>
        })}
      </div>
    </Modal>
  )
}
