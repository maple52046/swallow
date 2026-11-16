import { canOperatePool, isAdmin, poolRoleOf } from '@/domain/user/access'
import type { User } from '@/domain/user/types'
import type { ValidationRunSummary, ValidationStarterLevel, ValidationSuite } from './types'

/**
 * Who may write, start, and cancel what in the Validation area (validation contract,
 * Authentication and Authorization; identity-and-access §4.2). Like `domain/user/access`, these
 * rules only decide which actions the dashboard offers and why it withholds one: the API judges
 * every request on its own, so they are never the only guard, and they err towards withholding (an
 * unknown starter level reads as `owner`).
 */

/**
 * The least Pool Role a User needs on every target to start `suite`. A module suite, and a user
 * suite whose level the dashboard does not recognize, need `owner`.
 */
export function suiteStarterLevel(suite: Pick<ValidationSuite, 'source' | 'starterLevel'>): ValidationStarterLevel {
  return suite.source === 'user' && suite.starterLevel === 'member' ? 'member' : 'owner'
}

/**
 * Whether `user` wrote `suite`, compared by user id: the username in `createdBy` is display only and
 * may name a deleted User whose name was reused. A suite without `createdById` has no author.
 */
export function isSuiteAuthor(user: User | null, suite: Pick<ValidationSuite, 'source' | 'createdById'>): boolean {
  return Boolean(user) && suite.source === 'user' && suite.createdById !== null && suite.createdById === user?.id
}

/** Whether `user` may replace `suite`: its author only, never an admin who is not the author. */
export function canEditSuite(user: User | null, suite: ValidationSuite): boolean {
  return !suite.retired && isSuiteAuthor(user, suite)
}

/** Whether `user` may retire `suite`: its author or an admin. Module suites are never retired. */
export function canRetireSuite(user: User | null, suite: ValidationSuite): boolean {
  return !suite.retired && suite.source === 'user' && (isAdmin(user) || isSuiteAuthor(user, suite))
}

/**
 * Whether `user` may start `suite` on a target in `poolId`, as far as Pool Roles decide it: an
 * admin always; an owner of the Pool for any suite; a member of it for a `member` suite. A target
 * in no Pool is the admins'. Host Access and `sudo` on the target are checked only by the API.
 */
export function canStartOnPool(user: User | null, suite: Pick<ValidationSuite, 'source' | 'starterLevel'>, poolId: string | null | undefined): boolean {
  if (suiteStarterLevel(suite) === 'member') return isAdmin(user) || poolRoleOf(user, poolId) !== null
  return canOperatePool(user, poolId)
}

/**
 * Why `user` cannot start `suite` anywhere, or null when some target could allow it:
 *
 * - `retired`: a retired suite runs nowhere;
 * - `no-pool`: the user is in no Pool, so no target can be started on;
 * - `owners-only`: the suite needs a Pool owner and the user owns no Pool.
 */
export type SuiteStartBlocker = 'retired' | 'no-pool' | 'owners-only'

/** See {@link SuiteStartBlocker}; admins are never blocked by Pool Roles. */
export function suiteStartBlocker(user: User | null, suite: ValidationSuite): SuiteStartBlocker | null {
  if (suite.retired) return 'retired'
  if (isAdmin(user)) return null
  const memberships = user?.memberships ?? []
  if (memberships.length === 0) return 'no-pool'
  if (suiteStarterLevel(suite) === 'owner' && !memberships.some((membership) => membership.role === 'owner')) return 'owners-only'
  return null
}

/**
 * Whether `user` may cancel `run`: its starter (`requestedById`), an owner of every target's Pool
 * (`ownsTargets`, which the caller resolves from the targets), or an admin. Whether the run can
 * still be canceled at all (an unfinished report run) is the caller's to check.
 */
export function canCancelRun(user: User | null, run: Pick<ValidationRunSummary, 'requestedById'>, ownsTargets: boolean): boolean {
  if (!user) return false
  return isAdmin(user) || ownsTargets || (run.requestedById !== null && run.requestedById === user.id)
}
