/**
 * The signed-in user's role, in the API's vocabulary (auth-me.md): `admin`, `owner`, or `user`.
 * Every management area's API requires `admin` today.
 */
export type UserRole = 'admin' | 'owner' | 'user'
export type UserStatus = 'active' | 'disabled'

export interface User {
  id: string
  username: string
  displayName: string
  role: UserRole
  status: UserStatus
}
