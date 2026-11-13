import { useState } from 'react'
import { Button, Text } from '@chakra-ui/react'
import { Power, PowerOff, type LucideIcon } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import type { Server, ServerPowerConfiguration } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/types'
import { isAdmin } from '@/domain/user/access'
import { powerStateLabel } from '@/presentation/components/axisBadgeUtils'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { powerDriverLabel } from '@/presentation/components/serverSummary/powerConfigurationLabels'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { PowerConfigurationFields } from './PowerConfigurationFields'
import { serverActionAvailability } from './serverActions'
import { projectedPowerControl } from './serverListPresentation'
import { usePowerConfigurationForm } from './usePowerConfigurationForm'

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

/**
 * What the dialog shows: `reading` until the Power Configuration is read, `actions` for the power
 * tiles, `configure` for the Power Configuration form.
 */
type PowerDialogView = 'reading' | 'actions' | 'configure'

interface ServerPowerDialogProps {
  /** The Server whose power the user is acting on; drives the current-state hint and gating. */
  server: Server
  /**
   * Runs the chosen power action. The dialog does not execute it directly: it hands the action
   * back to the list, which routes it through the shared action runner (toast + list reload).
   */
  onSelect: (action: PowerAction) => void
  /** Dismisses the dialog without acting; also called once a Power Configuration is saved. */
  onClose: () => void
}

/**
 * Which view the power control leads to. A Server without a driver the provisioner can use
 * (`none`, or `manual`, where a person switches it) goes straight to the form: the power actions
 * could not work, and a user who clicks an unknown power state needs the configuration first. A
 * user's explicit choice wins, but a Server powered through a provisioner VM host (not editable) or
 * whose configuration could not be read only ever shows the actions. Only an unknown control waits
 * for the read (`reading`).
 */
function powerDialogView(
  control: 'none' | 'manual' | 'automatic' | null,
  editable: boolean,
  state: { reading: boolean; readFailed: boolean },
  choice: Exclude<PowerDialogView, 'reading'> | null,
): PowerDialogView {
  if (state.readFailed || !editable) return 'actions'
  if (!control) return state.reading ? 'reading' : 'actions'
  if (choice) return choice
  return control === 'none' || control === 'manual' ? 'configure' : 'actions'
}

/** Narrows the live configuration's control to the values the dialog branches on. */
function knownControl(configuration: ServerPowerConfiguration | null): 'none' | 'manual' | 'automatic' | null {
  const control = configuration?.control
  return control === 'none' || control === 'manual' || control === 'automatic' ? control : null
}

/**
 * The power dialog of the Server list, opened by the icon-and-text power button before tags in the
 * Server field. The power state alone cannot say why it is unknown, so the dialog branches on the
 * power control the Server's projection mirrors (`provisioning.powerControl`) and opens the right
 * view at once; the Server's Power Configuration is still read live (decision 054) for the form's
 * drivers and values, and a projection that has not observed the control yet waits for that read:
 *
 * - With a driver the provisioner switches and reads power through (`automatic`), it shows the
 *   Power on / Power off tiles. When the power state is unknown or in error, a warning above them
 *   says the provisioner cannot read the power and offers to edit the configuration.
 * - Without one (`none`, or `manual`), it shows the Power Configuration form at once — the shared
 *   `usePowerConfigurationForm` and `PowerConfigurationFields` of the Server Summary, whose driver
 *   choice waits for the live read. Saving is one step: the API keeps the configuration only when
 *   the provisioner reads the power through it, so Save shows "Checking…" until it answers; a
 *   refusal stays in the form with the reason and what was typed, and a success closes the dialog
 *   with a toast of the power read (the form also refreshes the Server's projection, so the list's
 *   button follows).
 * - When the configuration cannot be read (a provisioner without the capability, or one that is
 *   unreachable), the reason is shown above the tiles, which stay as before.
 * - Power Configuration is admin-only, so a Pool owner's dialog never reads or edits it: it shows
 *   the tiles, and for a Server without a usable driver, a note that an admin sets one up.
 *
 * The list has no live `ProvisionerCapabilities`, so — like the row's action menu — both tiles are
 * offered and the backend refuses unsupported ones. `serverActionAvailability` still gates locked
 * Servers: a gated tile is `aria-disabled` with the reason in a Tooltip and its click suppressed, so
 * the reason stays discoverable without breaking the square grid. Selecting an action closes the
 * dialog and delegates execution to the caller. Every view is one Modal, so switching views never
 * replays its open animation; saving blocks dismissal until the provisioner answers.
 */
export function ServerPowerDialog({ server, onSelect, onClose }: ServerPowerDialogProps) {
  const { servers } = useApp()
  const { currentUser } = useAuth()
  // Power Configuration is an admin route; a Pool owner powers the Server with the actions alone.
  const admin = isAdmin(currentUser)
  const name = serverDisplayName(server)
  const loaded = useAsyncData(
    async () => (admin ? servers.getPowerConfiguration(server.id) : null),
    [admin, servers, server.id],
    { unavailableStatuses: [] },
  )
  const [choice, setChoice] = useState<Exclude<PowerDialogView, 'reading'> | null>(null)
  const configuration = loaded.status === 'ready' ? loaded.data : null
  const readFailed = loaded.status === 'error' || loaded.status === 'unavailable'
  const form = usePowerConfigurationForm(server, configuration, onClose)
  // Until the live read answers, the projection decides: its power control, and VM-host membership
  // for editability (the API marks exactly those Servers read-only).
  const control = knownControl(configuration) ?? projectedPowerControl(server)
  const editable = admin && (configuration ? configuration.editable : !server.status.providerPod)
  const view = powerDialogView(control, editable, { reading: loaded.status === 'loading', readFailed }, choice)

  if (view === 'configure') {
    const editingDriver = choice === 'configure'
    return (
      <Modal
        open
        onClose={() => !form.busy && onClose()}
        closeOnInteractOutside={!form.busy}
        title="Power configuration"
        description={editingDriver
          ? `How the provisioner switches and reads the power of ${name}. Saved to the provisioner.`
          : `${name} has no power driver the provisioner can use, so swallow cannot switch or read its power. Set how the provisioner reaches it.`}
        onSubmit={(event) => {
          event.preventDefault()
          void form.save()
        }}
        footer={
          <>
            {editingDriver && (
              <Button variant="ghost" onClick={() => setChoice('actions')} disabled={form.busy}>
                Back
              </Button>
            )}
            <Button variant="ghost" onClick={onClose} disabled={form.busy}>
              Cancel
            </Button>
            <Button type="submit" colorPalette="brand" loading={form.busy} loadingText="Checking…" disabled={!form.canSubmit}>
              Save
            </Button>
          </>
        }
      >
        <PowerConfigurationFields server={server} form={form} />
      </Modal>
    )
  }

  const shownState = server.status.provisioning?.powerState ?? null
  const unreadable = view === 'actions' && control === 'automatic' && (shownState === 'unknown' || shownState === 'error')
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
        {view === 'reading' ? 'Reading its power configuration…' : `Current power state: ${powerStateLabel(shownState)}.`}
      </Text>
      {readFailed && (
        <Alert status="warning" title="Power configuration unavailable" mb="3">
          {loaded.status === 'error' ? <FailureMessage failure={loaded} /> : loaded.status === 'unavailable' && loaded.message}
        </Alert>
      )}
      {unreadable && (
        <Alert status="warning" title="The provisioner cannot read its power" mb="3">
          <Text>
            {configuration
              ? `It powers ${name} through ${powerDriverLabel(configuration.driver)}; check that driver's address and account.`
              : admin
                ? `It has a power driver for ${name}; check that driver's address and account.`
                : `It has a power driver for ${name}; an admin can check that driver's address and account.`}
          </Text>
          {editable && (
            <Button size="xs" variant="outline" colorPalette="brand" mt="2" onClick={() => setChoice('configure')}>
              Edit power configuration
            </Button>
          )}
        </Alert>
      )}
      {!admin && (control === 'none' || control === 'manual') && (
        <Alert status="info" title="No power driver the provisioner can use" mb="3">
          swallow cannot switch or read the power of {name} until an admin sets its Power Configuration.
        </Alert>
      )}
      {view === 'actions' && (
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
      )}
    </Modal>
  )
}
