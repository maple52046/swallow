/**
 * The signed-in user's User Role, in the API's vocabulary (auth-me.md, glossary User Role): `admin`
 * administers swallow as a whole; `user` may manage only its own account, and everything else comes
 * from its Pool Memberships. An unknown value is read as `user`, the least privileged.
 */
export type UserRole = 'admin' | 'user'
export type UserStatus = 'active' | 'disabled'

/**
 * What a Pool Membership lets its User do in one Pool (glossary Pool Role): an `owner` operates the
 * Pool's Servers and Platforms and manages its members; a `member` reads them and logs in where
 * Host Access allows. An unknown value is read as `member`.
 */
export type PoolRole = 'owner' | 'member'

/**
 * One of the signed-in user's Pool Memberships as auth-me reports it, with the Pool's name for
 * display. A Pool belongs to the installation, not to a Site, so a membership applies to its Servers
 * in every Site. The dashboard uses it only to decide which actions to offer; the server judges
 * every request itself.
 */
export interface PoolMembership {
  poolId: string
  poolName: string
  role: PoolRole
}

/**
 * The signed-in user as the dashboard knows it from auth-me: identity, User Role, and Pool
 * Memberships. It decides which areas and actions the console offers; it is read again after the
 * user changes their own membership, and the server authorizes every request regardless.
 */
export interface User {
  id: string
  username: string
  displayName: string
  role: UserRole
  status: UserStatus
  /** The user's Pool Memberships; an admin may act in every Pool whether or not it is listed. */
  memberships: readonly PoolMembership[]
}
