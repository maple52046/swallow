import type { AccessAuditEntry } from '@/domain/access/accounts'
import type { SelectOption } from '@/presentation/components/ui/select'
import type { UserRole } from '@/domain/user/types'

/** The User Roles an admin may give, with what each one means in words. */
export const USER_ROLE_OPTIONS: readonly SelectOption[] = [
  { value: 'user', label: 'User — access through Pool Memberships' },
  { value: 'admin', label: 'Admin — administers Swallow, sudo on every Server' },
]

/** The short label of a User Role for tables and badges. */
export function userRoleLabel(role: UserRole): string {
  return role === 'admin' ? 'Admin' : 'User'
}

/**
 * The client-side form of the users.md username rule: a Linux login name of 1 to 32 characters,
 * starting with a lowercase letter or underscore, then lowercase letters, digits, `_`, or `-`.
 * Reserved names (system and service accounts) are only known to the API, which refuses them with
 * its own message.
 */
export const USERNAME_PATTERN = /^[a-z_][a-z0-9_-]{0,31}$/

/** What an audit action did, in words, for the access audit list. */
export function auditActionLabel(action: AccessAuditEntry['action']): string {
  switch (action) {
    case 'user.created': return 'User created'
    case 'user.updated': return 'User changed'
    case 'user.deleted': return 'User deleted'
    case 'member.added': return 'Member added'
    case 'member.updated': return 'Membership changed'
    case 'member.removed': return 'Member removed'
    case 'pool_policy.updated': return 'Pool sudo defaults changed'
    case 'settings.updated': return 'Access settings changed'
    default: return action
  }
}
