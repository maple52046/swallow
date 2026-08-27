import type { AuthRepository } from '@/application/ports/AuthRepository'
import type { User, UserRole } from '@/domain/user/types'
import * as authApi from './authApi'
import { ApiRequestError, tokenStore } from './client'
import type { MeResponse } from './types'

function mapRole(role: MeResponse['role']): UserRole {
  return role === 'user' ? 'member' : role
}

function toUser(response: MeResponse): User {
  return { id: response.id, username: response.username, displayName: response.username, role: mapRole(response.role), status: 'active' }
}

/** HTTP/token adapter for login, session verification, and logout. */
export class ApiAuthRepository implements AuthRepository {
  hasSession(): boolean { return tokenStore.get() !== null }
  async restoreSession(): Promise<User> { return toUser(await authApi.getMe()) }
  async login(username: string, password: string): Promise<User> {
    try {
      await authApi.login(username, password)
      return toUser(await authApi.getMe())
    } catch (error) {
      if (error instanceof ApiRequestError && error.status === 401) throw new Error('Invalid username or password')
      if (error instanceof Error) throw new Error(error.message)
      throw error
    }
  }
  logout(): void { authApi.logout() }
}
