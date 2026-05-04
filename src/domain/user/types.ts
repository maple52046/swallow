export type UserRole = 'admin' | 'owner' | 'member'
export type UserStatus = 'active' | 'disabled'

export interface User {
  id: string
  username: string
  displayName: string
  role: UserRole
  status: UserStatus
}
