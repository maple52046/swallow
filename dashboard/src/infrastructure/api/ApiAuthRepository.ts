import type { AuthRepository } from '@/application/ports/AuthRepository'
import type { User, UserRole } from '@/domain/user/types'
import * as authApi from './authApi'
import { ApiRequestError } from './client'
import { clearTokens, currentAccessToken, discardLegacyToken, onSessionEnded, refreshSession } from './session'
import type { CurrentUser } from './authApi'

const USER_ROLES: readonly string[] = ['admin', 'owner', 'user'] satisfies readonly UserRole[]

function isUserRole(role: string): role is UserRole {
  return USER_ROLES.includes(role)
}

/** The contract's role; one this dashboard does not know is least-privileged (auth-me.md), never failing open. */
function mapRole(role: CurrentUser['role']): UserRole {
  return isUserRole(role) ? role : 'user'
}

function toUser(response: CurrentUser): User {
  return { id: response.id, username: response.username, displayName: response.username, role: mapRole(response.role), status: 'active' }
}

/**
 * HTTP adapter for the Session lifecycle (decision 042): sign-in, resuming a Session after a page
 * load through the refresh cookie, logout, and the ended-Session signal. Tokens never reach the
 * presentation; it sees only domain Users.
 */
export class ApiAuthRepository implements AuthRepository {
  async restoreSession(): Promise<User | null> {
    discardLegacyToken()
    if (currentAccessToken() === null && (await refreshSession()) === 'ended') return null
    try {
      return toUser(await authApi.getMe())
    } catch (error) {
      // A valid Session whose user was deleted (404), or a token the server no longer accepts
      // (401), means signed out rather than a page error.
      if (error instanceof ApiRequestError && (error.status === 401 || error.status === 404)) {
        clearTokens()
        return null
      }
      throw error
    }
  }

  async login(username: string, password: string): Promise<User> {
    try {
      await authApi.login(username, password)
      return toUser(await authApi.getMe())
    } catch (error) {
      if (error instanceof ApiRequestError && error.status === 401) {
        throw new ApiRequestError(error.code, 'Invalid username or password', error.status, error.requestId)
      }
      throw error
    }
  }

  logout(): Promise<void> {
    return authApi.logout()
  }

  onSessionEnded(listener: () => void): () => void {
    return onSessionEnded(listener)
  }
}
