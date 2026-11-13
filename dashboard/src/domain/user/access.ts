import type { PoolRole, User } from './types'

/**
 * Pool-scoped visibility rules the dashboard uses to decide which surfaces and actions to offer
 * (identity-and-access.md §4, decision 082). They mirror the server's authorization only to avoid
 * offering what would be refused: the server judges every request on its own, so these helpers must
 * never be the only guard, and they err towards hiding (an unknown role is the least privileged).
 */

/** Whether the user administers swallow as a whole. */
export function isAdmin(user: User | null): boolean {
  return user?.role === 'admin'
}

/** The user's Pool Role in `poolId`, or `null` when it is not a member (or the Pool is none). */
export function poolRoleOf(user: User | null, poolId: string | null | undefined): PoolRole | null {
  if (!user || !poolId) return null
  return user.memberships.find((membership) => membership.poolId === poolId)?.role ?? null
}

/**
 * Whether the user may operate the resources of `poolId`: every admin, and an owner of that Pool. A
 * resource in no Pool (`null`) belongs to admins only.
 */
export function canOperatePool(user: User | null, poolId: string | null | undefined): boolean {
  return isAdmin(user) || poolRoleOf(user, poolId) === 'owner'
}

/** Whether the user owns at least one Pool, which decides whether deploy and install entry points are offered. */
export function ownsAnyPool(user: User | null): boolean {
  return isAdmin(user) || (user?.memberships.some((membership) => membership.role === 'owner') ?? false)
}
