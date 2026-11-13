import type { EffectiveSudoValue, SudoSetting, SudoSource } from '@/domain/access/accounts'
import type { PoolRole } from '@/domain/user/types'
import type { SelectOption } from '@/presentation/components/ui/select'

/** The Pool Roles an owner may give, with what each one means in words, for the add dialog. */
export const POOL_ROLE_CHOICES: readonly SelectOption[] = [
  { value: 'member', label: 'Member — reads the Pool, logs in to its Servers' },
  { value: 'owner', label: 'Owner — operates the Pool and manages its members' },
]

/** The same roles with short labels, for the selector in each member row where space is narrow. */
export const POOL_ROLE_OPTIONS: readonly SelectOption[] = [
  { value: 'member', label: 'Member' },
  { value: 'owner', label: 'Owner' },
]

/** The short label of a Pool Role for tables, badges, and the role selector. */
export function poolRoleLabel(role: PoolRole): string {
  return role === 'owner' ? 'Owner' : 'Member'
}

/** Reads a selector value back as a Pool Role; anything else is the least privileged, `member`. */
export function toPoolRole(value: string): PoolRole {
  return value === 'owner' ? 'owner' : 'member'
}

/** The choices of one membership override; `inherit` names the Pool default it follows. */
export function sudoSettingOptions(poolDefault: boolean): readonly SelectOption[] {
  return [
    { value: 'inherit', label: `Pool default (${poolDefault ? 'yes' : 'no'})` },
    { value: 'on', label: 'Yes' },
    { value: 'off', label: 'No' },
  ]
}

/** Reads a selector value back as a Sudo Setting; anything unexpected is `inherit`. */
export function toSudoSetting(value: string): SudoSetting {
  return value === 'on' || value === 'off' ? value : 'inherit'
}

/** Which layer decided an effective value, in words (pool-access.md `source`). */
function sourceLabel(source: SudoSource): string {
  switch (source) {
    case 'role': return 'owner role'
    case 'settings': return 'installation setting'
    case 'pool': return 'Pool default'
    case 'member': return 'member override'
    default: return source
  }
}

/** An effective Sudo Policy value and why, as one phrase ("Yes, Pool default"). */
export function effectiveLabel(value: EffectiveSudoValue): string {
  return `${value.effective ? 'Yes' : 'No'}, ${sourceLabel(value.source)}`
}
