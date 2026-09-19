/**
 * The catalogue of provisioner actions the UI offers, grouped as MAAS groups them.
 *
 * Single source of truth reused by the shared `ServerTakeActionMenu` on the list row menu,
 * the bulk-action bar, and the detail page, so the three never drift. Each group maps to a
 * provisioner capability flag; detail hides unsupported groups via live capabilities, while
 * the list shows them all and lets the backend refuse per server — matching MAAS, where bulk
 * menus always appear and the server validates.
 */
import type { ProvisionerCapabilities, Server, ServerAction } from '@/domain/server/types'

/** A bulk/row action. `release` is included alongside the `ServerAction` union because it
 * is a lifecycle action offered in bulk but reached through a different repository method. */
export type BulkAction = ServerAction | 'release'

/** Every single-Server menu action; permanent deletion is intentionally excluded from bulk. */
export type ServerMenuAction = BulkAction | 'delete'

export interface ServerActionDef {
  action: ServerMenuAction
  label: string
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
    actions: [{ action: 'release', label: 'Release', destructive: true }],
  },
  {
    label: 'Removal',
    capability: 'machineRemoval',
    actions: [{ action: 'delete', label: 'Delete server', destructive: true, bulk: false }],
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
      { action: 'commission', label: 'Commission' },
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
    if (found) return found.label
  }

  return action
}

const ACTIVE_PROVIDER_STATES = new Set(['commissioning', 'deploying', 'releasing', 'testing'])

export interface ServerActionAvailability {
  eligible: readonly Server[]
  skipped: readonly Server[]
  disabledReason?: string
}

/**
 * One protection policy for list rows, bulk actions, and Server Detail.
 *
 * Lock and Unlock converge mixed selections by acting only on Servers that need the
 * requested state. Every other action is atomic from the UI's perspective and remains
 * disabled when any selected Server is protected.
 */
export function serverActionAvailability(
  action: ServerMenuAction,
  targets: readonly Server[],
): ServerActionAvailability {
  if (targets.length === 0) {
    return { eligible: [], skipped: [], disabledReason: 'Select at least one Server.' }
  }
  if (action === 'lock') {
    const unlocked = targets.filter((server) => !server.provisioning?.locked)
    const active = unlocked.filter((server) => ACTIVE_PROVIDER_STATES.has(server.provisioning?.state ?? ''))
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
