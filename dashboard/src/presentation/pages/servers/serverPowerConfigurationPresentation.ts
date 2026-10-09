import type { PowerDriverOption, PowerFamily, SetServerPowerConfigurationInput } from '@/domain/server/types'

/**
 * The libvirt URI shape the API accepts for a virsh driver (contract server-detail-actions.md
 * "Power Configuration"): `qemu+ssh://[user@]host[:port]/system`, with no password, query, or
 * fragment. It is only early feedback; the API validates again and the provisioner after it.
 */
export const VIRSH_ADDRESS_PATTERN = /^qemu\+ssh:\/\/(?:[^:@/?#\s]+@)?[^@/?#\s]+\/system$/

/** The edit form's fields, as typed; `driver` is '' until the operator chooses one. */
export interface PowerConfigurationDraft {
  driver: string
  address: string
  powerId: string
  username: string
  password: string
  /** Remove the stored password; only offered when one is set and the driver is unchanged. */
  clearPassword: boolean
}

/** The family of the chosen driver among the writable options, or null before a choice. */
export function draftFamily(draft: PowerConfigurationDraft, options: readonly PowerDriverOption[]): PowerFamily | null {
  return options.find((option) => option.driver === draft.driver)?.family ?? null
}

/** Per-field problems of a draft, so each field can show its own message; empty when it can be sent. */
export function powerConfigurationDraftErrors(
  draft: PowerConfigurationDraft,
  options: readonly PowerDriverOption[],
): Partial<Record<'driver' | 'address' | 'powerId', string>> {
  const family = draftFamily(draft, options)
  if (!family) return { driver: 'Choose a driver.' }
  const errors: Partial<Record<'address' | 'powerId', string>> = {}
  const address = draft.address.trim()
  if (!address) {
    errors.address = family === 'virsh' ? 'Enter the hypervisor URI.' : 'Enter the BMC address.'
  } else if (family === 'virsh' && !VIRSH_ADDRESS_PATTERN.test(address)) {
    errors.address = 'Use qemu+ssh://[user@]host[:port]/system, without a password, query, or fragment.'
  }
  if (family === 'virsh' && !draft.powerId.trim()) errors.powerId = 'Enter the libvirt domain name or UUID.'
  if (family === 'virsh' && /\s/.test(draft.powerId.trim())) errors.powerId = 'The domain name must not contain spaces.'
  return errors
}

/**
 * The request for a valid draft. Only the chosen family's parameters are sent, because the API
 * refuses parameters that do not apply to the driver. The password follows the contract's write-only
 * semantics: a typed password replaces it, "Remove" clears it (''), and otherwise it is omitted — the
 * provisioner keeps it for an unchanged driver and clears it when the driver changes.
 */
export function powerConfigurationInput(
  draft: PowerConfigurationDraft,
  options: readonly PowerDriverOption[],
): SetServerPowerConfigurationInput {
  const family = draftFamily(draft, options)
  const input: SetServerPowerConfigurationInput = { driver: draft.driver, address: draft.address.trim() }
  if (family === 'virsh') input.powerId = draft.powerId.trim()
  if (family === 'bmc') input.username = draft.username.trim()
  if (draft.password) input.password = draft.password
  else if (draft.clearPassword) input.password = ''
  return input
}
