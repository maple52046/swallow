/**
 * The catalogue of provisioner actions the UI offers, grouped as MAAS groups them.
 *
 * Single source of truth reused by the shared `ServerTakeActionMenu` on the list row menu,
 * the bulk-action bar, and the detail page, so the three never drift. Each group maps to a
 * provisioner capability flag; detail hides unsupported groups via live capabilities, while
 * the list shows them all and lets the backend refuse per server — matching MAAS, where bulk
 * menus always appear and the server validates.
 */
import { isProvisioningInProgress, type ProvisionerCapabilities, type Server, type ServerAction } from '@/domain/server/types'

/** A bulk/row action. `release` is included alongside the `ServerAction` union because it
 * is a lifecycle action offered in bulk but reached through a different repository method. */
export type BulkAction = ServerAction | 'release'

/** Every single-Server menu action; permanent deletion is intentionally excluded from bulk. */
export type ServerMenuAction = BulkAction | 'delete'

export interface ServerActionDef {
  action: ServerMenuAction
  label: string
  /**
   * The action's name in result summaries and Activity, when the menu label only reads right
   * beside its neighbours (Delete only, next to Release).
   */
  resultLabel?: string
  /** Marks a destructive action for red styling; Release and Delete own their required confirmation flows. */
  destructive?: boolean
  /** False for actions whose blast radius must remain one explicitly named Server. */
  bulk?: boolean
}

export interface ServerActionGroupDef {
  label: string
  /** The capability that must be present for the detail page to show this group. */
  capability: keyof ProvisionerCapabilities | null
  actions: ServerActionDef[]
}

export const SERVER_ACTION_GROUPS: ServerActionGroupDef[] = [
  {
    label: 'Lifecycle',
    capability: null,
    // Recover ("Return to Ready") and Release are the two primary provider-recovery verbs
    // for a Server whose provisioning axis is not usable; both converge the Server to `ready`.
    // See docs/decisions/033 and the provider-recovery glossary term.
    actions: [
      { action: 'recover', label: 'Recover' },
      { action: 'release', label: 'Release', destructive: true },
    ],
  },
  {
    label: 'Removal',
    capability: 'machineRemoval',
    // Bulk delete is confirmed by typing the Server count (ServerDeleteDialog).
    actions: [{ action: 'delete', label: 'Delete only', resultLabel: 'Delete server', destructive: true }],
  },
  {
    label: 'Power',
    capability: 'power',
    actions: [
      { action: 'power-on', label: 'Power on' },
      { action: 'power-off', label: 'Power off', destructive: true },
    ],
  },
  {
    label: 'Hardware validation',
    capability: 'hardwareValidation',
    actions: [
      { action: 'inspect', label: 'Inspect hardware' },
      { action: 'test', label: 'Test' },
      { action: 'abort', label: 'Abort' },
      { action: 'override-failed-testing', label: 'Override failed testing' },
    ],
  },
  {
    label: 'Operator state',
    capability: 'operatorState',
    actions: [
      { action: 'lock', label: 'Lock' },
      { action: 'unlock', label: 'Unlock' },
      { action: 'mark-broken', label: 'Mark broken', destructive: true },
      { action: 'mark-fixed', label: 'Mark fixed' },
      { action: 'rescue-mode', label: 'Rescue mode' },
      { action: 'exit-rescue-mode', label: 'Exit rescue' },
    ],
  },
]

/** Human-readable label for an action value, for toast summaries. */
export function actionLabel(action: ServerMenuAction): string {
  for (const group of SERVER_ACTION_GROUPS) {
    const found = group.actions.find((entry) => entry.action === action)
    if (found) return found.resultLabel ?? found.label
  }

  return action
}

/**
 * Reports whether a Server is currently running a RAM (ephemeral) deployment: the OS runs from
 * memory and the disks are left untouched, so anything written to the Server is lost on power off
 * or reboot (provisioning re-provides the same OS, so only the data is lost). "RAM deploy" is the
 * operator term for the `ephemeral` provisioning fact (see the Deploy Target glossary). The list,
 * detail, and power dialogs use this to gate a destructive-power-off warning + confirmation, since
 * a disk deployment keeps its data across a power cycle but a RAM one does not.
 */
export function isRamDeploy(server: Server): boolean {
  return server.provisioning?.ephemeral === true
}

// State gates mirroring the Swallow-owned recovery policy (docs/decisions/033). The dashboard
// gates on the same normalized `provisioning.state` the backend does, so an operator sees a
// disabled control with a Swallow reason instead of a provider rejection after the fact.
// `allocated` is included because MAAS parks a machine there when a deployment was reserved
// but never finished (a failed/canceled deploy, or a verification borrow that ended early);
// the provider allows Release from it, so it is a real "return it to the pool" source, not a
// dead end (decision 033/036).
const RELEASE_SOURCE_STATES = new Set(['deployed', 'allocated', 'failed', 'broken', 'rescue'])
// Recover is offered only for the not-usable states it is meant to fix. The backend also
// tolerates `deployed`/`ready` (for internal and uninstall reuse), but offering Recover on a
// healthy deployed Server would release it — a footgun — so the operator menu excludes those.
// `allocated` is offered: it means "stuck reserved", and Recover returns it to Ready.
const RECOVER_SOURCE_STATES = new Set(['allocated', 'failed', 'broken', 'rescue'])
const RESCUE_ENTER_STATES = new Set(['deployed', 'broken', 'failed'])

export interface ServerActionAvailability {
  eligible: readonly Server[]
  skipped: readonly Server[]
  disabledReason?: string
  /**
   * Set when the reason is a Platform Claim. The menu states it once, as `platformClaimNote`
   * above Release and Delete, instead of under each.
   */
  platformClaimed?: boolean
}

/** The normalized provisioning state to branch on, or '' when the axis is unobserved. */
function provisioningState(server: Server): string {
  return server.provisioning?.state ?? ''
}

/**
 * All-or-nothing state gate shared by the recovery-related actions. A locked target blocks
 * the action (recovery mutations require an unlocked Server); otherwise the action is offered
 * only when every target is in an allowed state, matching the backend's batch rejection so the
 * UI never offers a control the Operation would refuse.
 *
 * No allowed state is an in-progress one, so a target with running provider work (Releasing,
 * Inspecting, Testing, Deploying) is reported as such instead of with the action's source
 * states, which would read as if the Server were simply in the wrong state.
 */
function stateGatedAvailability(
  targets: readonly Server[],
  allows: (server: Server) => boolean,
  reason: string,
): ServerActionAvailability {
  const locked = targets.filter((server) => server.provisioning?.locked)
  if (locked.length > 0) {
    const name = locked[0].hostname ?? locked[0].id
    return { eligible: [], skipped: locked, disabledReason: `${name} is locked. Unlock it before starting this action.` }
  }
  const busy = targets.filter((server) => isProvisioningInProgress(server.provisioning?.state))
  if (busy.length > 0) {
    const name = busy[0].hostname ?? busy[0].id
    return {
      eligible: [],
      skipped: busy,
      disabledReason: `${name} is ${provisioningState(busy[0])}. Wait for the provider to finish before starting this action.`,
    }
  }
  const ineligible = targets.filter((server) => !allows(server))
  if (ineligible.length > 0) {
    return { eligible: [], skipped: ineligible, disabledReason: reason }
  }
  return { eligible: [...targets], skipped: [] }
}

/** Optional context for `serverActionAvailability`. */
export interface ServerActionContext {
  /**
   * Platform names by id, so the Platform Claim note names the Platform. Without it (or for an
   * unknown id) the note names the Platform by id.
   */
  platformNames?: ReadonlyMap<string, string>
}

/**
 * The one sentence saying which Platform claims the selected Servers (decision 067), or
 * undefined when none is claimed. A claimed Server can be neither released nor deleted, and the
 * API refuses both with 409, so the menu shows this once above those two actions.
 */
export function platformClaimNote(targets: readonly Server[], platformNames?: ReadonlyMap<string, string>): string | undefined {
  const claimed = targets.filter((server) => server.platformClaim)
  if (claimed.length === 0) return undefined
  const names = [...new Set(claimed.map((server) => {
    const platformId = server.platformClaim?.platformId ?? ''
    return platformNames?.get(platformId) ?? platformId
  }))]
  const platform = `${names.length === 1 ? 'Platform' : 'Platforms'} ${names.join(', ')}`
  if (targets.length === 1) return `This server belongs to ${platform} and cannot be released or deleted.`
  if (claimed.length === 1) {
    return `${claimed[0].hostname ?? claimed[0].id} belongs to ${platform} and cannot be released or deleted.`
  }
  return `${claimed.length} selected servers belong to ${platform} and cannot be released or deleted.`
}

/** Release or Delete availability when a selected Server carries a Platform Claim, or undefined. */
function platformClaimAvailability(targets: readonly Server[], platformNames?: ReadonlyMap<string, string>): ServerActionAvailability | undefined {
  const note = platformClaimNote(targets, platformNames)
  if (!note) return undefined
  return { eligible: [], skipped: targets.filter((server) => server.platformClaim), disabledReason: note, platformClaimed: true }
}

/**
 * One protection policy for list rows, bulk actions, and Server Detail.
 *
 * Lock and Unlock converge mixed selections by acting only on Servers that need the
 * requested state. Every other action is atomic from the UI's perspective and remains
 * disabled when any selected Server is protected. Release and Delete are also disabled while any
 * selected Server carries a Platform Claim, mirroring the API's refusal.
 */
export function serverActionAvailability(
  action: ServerMenuAction,
  targets: readonly Server[],
  context: ServerActionContext = {},
): ServerActionAvailability {
  if (targets.length === 0) {
    return { eligible: [], skipped: [], disabledReason: 'Select at least one Server.' }
  }
  if (action === 'lock') {
    const unlocked = targets.filter((server) => !server.provisioning?.locked)
    const active = unlocked.filter((server) => isProvisioningInProgress(server.provisioning?.state))
    const eligible = unlocked.filter((server) => server.provisioning?.state === 'deployed')
    if (active.length > 0) {
      return {
        eligible: [],
        skipped: targets,
        disabledReason: `${active[0].hostname ?? active[0].id} has active provider work. Wait for it to finish.`,
      }
    }
    const unavailable = unlocked.filter((server) => server.provisioning?.state !== 'deployed')
    if (unavailable.length > 0) {
      const server = unavailable[0]
      const name = server.hostname ?? server.id
      const state = server.provisioning?.state ?? 'unknown'
      return {
        eligible: [],
        skipped: targets,
        disabledReason: `${name} is ${state}. Lock is available only when the Server is Deployed.`,
      }
    }
    return {
      eligible,
      skipped: targets.filter((server) => server.provisioning?.locked),
      disabledReason: eligible.length === 0 ? 'Every selected Server is already locked.' : undefined,
    }
  }
  if (action === 'unlock') {
    const eligible = targets.filter((server) => server.provisioning?.locked)
    return {
      eligible,
      skipped: targets.filter((server) => !server.provisioning?.locked),
      disabledReason: eligible.length === 0 ? 'Every selected Server is already unlocked.' : undefined,
    }
  }
  // Provider-recovery gating (decision 033). Recover and Release are the primary "make it
  // usable again" verbs; the raw operator-state primitives are gated to their real source
  // states so the menu explains why, rather than forwarding a later provider rejection.
  if (action === 'recover') {
    return stateGatedAvailability(targets, (server) => RECOVER_SOURCE_STATES.has(provisioningState(server)),
      'Recover returns an allocated, failed, broken, or rescue Server to Ready.')
  }
  if (action === 'release') {
    // Active provider work is the reason to wait (a claimed Server may already be releasing through
    // its Platform's uninstall). Otherwise the claim wins over lock and state reasons, because
    // clearing those would still leave Release refused.
    const busy = targets.some((server) => isProvisioningInProgress(server.provisioning?.state))
    return (busy ? undefined : platformClaimAvailability(targets, context.platformNames)) ??
      stateGatedAvailability(targets, (server) => RELEASE_SOURCE_STATES.has(provisioningState(server)),
        'Release is available from deployed, allocated, failed, broken, or rescue.')
  }
  if (action === 'mark-fixed') {
    return stateGatedAvailability(targets, (server) => provisioningState(server) === 'broken',
      'Mark fixed only clears a Broken Server. Use Recover or Release to return a failed Server to Ready.')
  }
  if (action === 'mark-broken') {
    return stateGatedAvailability(targets,
      (server) => provisioningState(server) !== 'broken' && !isProvisioningInProgress(server.provisioning?.state),
      'Mark broken is unavailable while the Server is already broken or has active provider work.')
  }
  if (action === 'rescue-mode') {
    return stateGatedAvailability(targets, (server) => RESCUE_ENTER_STATES.has(provisioningState(server)),
      'Rescue mode is a diagnostic environment, available from deployed, broken, or failed.')
  }
  if (action === 'exit-rescue-mode') {
    return stateGatedAvailability(targets, (server) => provisioningState(server) === 'rescue',
      'Exit rescue is available only while the Server is in rescue mode.')
  }
  // Delete stays available during provider work (its dialog warns about it), so only the claim
  // precedes the lock check.
  const claimed = action === 'delete' ? platformClaimAvailability(targets, context.platformNames) : undefined
  if (claimed) return claimed
  const locked = targets.filter((server) => server.provisioning?.locked)
  if (locked.length > 0) {
    const name = locked[0].hostname ?? locked[0].id
    return {
      eligible: [],
      skipped: locked,
      disabledReason: `${name} is locked. Unlock it before starting this action.`,
    }
  }
  return { eligible: [...targets], skipped: [] }
}
