import type { SSHKey, SSHKeyRealization } from '@/domain/access/types'
import { ConditionTypes, findCondition } from '@/domain/convergence/types'

/**
 * How an SSH Key stands on one provisioner, reduced from that provisioner's conditions for this
 * page's wording (glossary SSH Key):
 *
 * - `pending`: not judged yet for the key's current generation (new key, replaced Deployment Key
 *   material, or a provisioner the controller has not reached).
 * - `synced`: the provisioner holds the key's current material.
 * - `failed`: the provisioner refused the key or could not be reached; the message says why.
 * - `unsupported`: the provisioner cannot hold SSH keys at all.
 */
export type KeyRealizationState = 'pending' | 'synced' | 'failed' | 'unsupported'

/**
 * The state of `key` on one provisioner. A judgement made for an older generation describes
 * replaced material, so it reads as pending; an unknown future reason reads as failed only when
 * the backend says the key is not realized, never as success.
 */
export function realizationState(key: SSHKey, entry: SSHKeyRealization): KeyRealizationState {
  const realized = findCondition(entry.conditions, ConditionTypes.realized)
  if (!realized || realized.observedGeneration < key.generation) return 'pending'
  if (realized.status === 'true') return entry.observedGeneration >= key.generation ? 'synced' : 'pending'
  if (realized.reason === 'capability_missing') return 'unsupported'
  if (realized.reason === 'converging') return 'pending'
  return 'failed'
}

/** The provisioner's reason for a failed realization, as display text; undefined when it has none. */
export function realizationMessage(entry: SSHKeyRealization): string | undefined {
  return findCondition(entry.conditions, ConditionTypes.realized)?.message || undefined
}

/** Operator-facing label for a realization state. */
export function syncStateLabel(state: KeyRealizationState): string {
  switch (state) {
    case 'synced':
      return 'Synced'
    case 'failed':
      return 'Failed'
    case 'unsupported':
      return 'Not supported'
    case 'pending':
      return 'Pending'
    default:
      // An unknown future value is shown neutrally rather than as success.
      return 'Unknown'
  }
}

/** Chakra palette for a state badge; always paired with the text label, never colour alone. */
export function syncStatePalette(state: KeyRealizationState): string {
  switch (state) {
    case 'synced':
      return 'green'
    case 'failed':
      return 'red'
    case 'pending':
      return 'blue'
    default:
      return 'gray'
  }
}

/**
 * The single most important state across a key's provisioners, for a compact table cell: any
 * failure first (it needs action), then pending, then unsupported, and synced only when every
 * capable provisioner holds the key. `undefined` means the installation has no provisioner yet.
 */
export function worstSyncState(key: SSHKey): KeyRealizationState | undefined {
  const states = new Set(key.status.provisioners.map((entry) => realizationState(key, entry)))
  if (states.size === 0) return undefined
  for (const state of ['failed', 'pending', 'unsupported', 'synced'] as const) {
    if (states.has(state)) return state
  }
  return 'pending'
}

/**
 * Whether a key still waits for a provisioner. It is the signal for following that one key:
 * `pending` is the only state the backend's SSH Key controller advances without operator action,
 * so the key is re-read while it is present and no longer once every entry has settled.
 */
export function isSyncPending(key: SSHKey): boolean {
  return key.status.provisioners.some((entry) => realizationState(key, entry) === 'pending')
}

/** Base filename for a downloaded private key, derived from the key name and safe on every OS. */
export function privateKeyFileName(name: string): string {
  const safe = name.trim().toLowerCase().replace(/[^a-z0-9._-]+/g, '-').replace(/^-+|-+$/g, '')
  return `id_ed25519_${safe || 'swallow'}`
}
