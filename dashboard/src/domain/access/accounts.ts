import type { Condition } from '@/domain/convergence/types'
import type { PoolRole, UserRole } from '@/domain/user/types'

/**
 * A User as an admin manages it (contract users.md, glossary User). The username is also the
 * person's Linux login on the Servers the Directory reaches. `uidNumber` is the POSIX uid swallow
 * allocated once; it is `null` only for a User created before POSIX identities, until the API
 * allocates one.
 */
export interface Account {
  id: string
  username: string
  role: UserRole
  uidNumber: number | null
  createdAt: string
  updatedAt: string
}

/**
 * A member's override of one Pool default (glossary Sudo Policy): `inherit` takes the Pool's value.
 */
export type SudoSetting = 'inherit' | 'on' | 'off'

/** Which layer of the Sudo Policy decided an effective value (pool-access.md). */
export type SudoSource = 'role' | 'settings' | 'pool' | 'member'

/** One overridable Sudo Policy value of a membership: the stored setting and what it resolves to. */
export interface EffectiveSudoValue {
  setting: SudoSetting
  effective: boolean
  source: SudoSource
}

/**
 * One Pool Membership with its resolved Sudo Policy (contract pool-access.md). An owner always has
 * sudo (source `role`), whatever its stored `sudo` setting; a Platform may still deny a member sudo
 * on its nodes, which is not part of these values.
 */
export interface PoolMember {
  userId: string
  username: string
  poolId: string
  role: PoolRole
  sudo: EffectiveSudoValue
  sudoPasswordless: EffectiveSudoValue
  createdAt: string
  updatedAt: string
}

/**
 * A User an owner may add to a Pool (contract pool-access.md `member-candidates`): not a member yet,
 * and shown by id and username only, because an owner may not list Users.
 */
export interface PoolMemberCandidate {
  userId: string
  username: string
}

/**
 * A Pool's Sudo Policy defaults: whether members have sudo, and whether sudo asks for a password,
 * unless a membership overrides them. `updatedAt` is `null` while the Pool has its initial defaults.
 */
export interface PoolAccessPolicy {
  poolId: string
  memberSudo: boolean
  sudoPasswordless: boolean
  updatedAt: string | null
  updatedBy: string
}

/** The installation's access settings (contract access.md). `posixIdStart` is read-only. */
export interface AccessSettings {
  adminSudoPasswordless: boolean
  posixIdStart: number
  updatedAt: string | null
  updatedBy: string
}

/** One recorded access change; it never carries a password. */
export interface AccessAuditEntry {
  id: string
  at: string
  actor: string
  action:
    | 'user.created'
    | 'user.updated'
    | 'user.deleted'
    | 'member.added'
    | 'member.updated'
    | 'member.removed'
    | 'pool_policy.updated'
    | 'settings.updated'
  target: string
  poolId: string
  detail: string
}

/**
 * The installation's Directory and the Directory controller's last report (contract access.md).
 * Without a Directory (`configured` false) nothing else is meaningful: the counts are 0 and the
 * conditions empty.
 */
export interface DirectoryStatus {
  configured: boolean
  hostUrl: string
  baseDn: string
  conditions: Condition[]
  lastAppliedAt: string | null
  observedAt: string | null
  users: number
  groups: number
  sudoRules: number
  hosts: number
}
