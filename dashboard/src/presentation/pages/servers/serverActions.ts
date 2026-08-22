/**
 * The catalogue of provisioner actions the UI offers, grouped as MAAS groups them.
 *
 * Single source of truth reused by the list row menu, the bulk-action bar, and the detail
 * page, so the three never drift. Each group maps to a provisioner capability flag; the
 * detail page (which has the live capability set) hides unsupported groups, while the list
 * shows them all and lets the backend refuse per server — matching MAAS, where bulk menus
 * always appear and the server validates.
 */
import type { ProvisionerCapabilities, ServerAction } from '@/domain/server/types'

/** A bulk/row action. `release` is included alongside the `ServerAction` union because it
 * is a lifecycle action offered in bulk but reached through a different repository method. */
export type BulkAction = ServerAction | 'release'

export interface ServerActionDef {
  action: BulkAction
  label: string
  /** Marks a destructive action for red styling and (optionally) confirmation. */
  destructive?: boolean
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
export function actionLabel(action: BulkAction): string {
  for (const group of SERVER_ACTION_GROUPS) {
    const found = group.actions.find((entry) => entry.action === action)
    if (found) return found.label
  }
  return action
}
