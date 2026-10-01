import type { SSHKey, SSHKeySyncState } from '@/domain/access/types'

/** Operator-facing label for a sync state; the domain value itself is what the API returns. */
export function syncStateLabel(state: SSHKeySyncState): string {
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

/** Chakra palette for a sync state badge; always paired with the text label, never colour alone. */
export function syncStatePalette(state: SSHKeySyncState): string {
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
 * The single most important sync state across a key's provisioners, for a compact table cell:
 * any failure first (it needs action), then pending, then unsupported, and synced only when every
 * capable provisioner holds the key. `undefined` means the installation has no provisioner yet.
 */
export function worstSyncState(key: SSHKey): SSHKeySyncState | undefined {
  const states = new Set(key.providerSync.map((entry) => entry.state))
  if (states.size === 0) return undefined
  for (const state of ['failed', 'pending', 'unsupported', 'synced'] as const) {
    if (states.has(state)) return state
  }
  return 'pending'
}

/**
 * Whether a key still waits for a provisioner sync. It is the signal for following that one key:
 * `pending` is the only state the backend's own sync pass advances without operator action, so the
 * key is re-read while it is present and no longer once every entry has settled.
 */
export function isSyncPending(key: SSHKey): boolean {
  return key.providerSync.some((entry) => entry.state === 'pending')
}

/** Base filename for a downloaded private key, derived from the key name and safe on every OS. */
export function privateKeyFileName(name: string): string {
  const safe = name.trim().toLowerCase().replace(/[^a-z0-9._-]+/g, '-').replace(/^-+|-+$/g, '')
  return `id_ed25519_${safe || 'swallow'}`
}
