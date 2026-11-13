import type { AuthRepository } from '@/application/ports/AuthRepository'
import type { PoolMembership, User } from '@/domain/user/types'
import * as authApi from './authApi'
import { ApiRequestError } from './client'
import { clearTokens, currentAccessToken, discardLegacyToken, onSessionEnded, refreshSession } from './session'
import type { CurrentUser } from './authApi'

/**
 * Maps `GET /api/v1/auth/me` onto the domain User. The roles are compared one by one rather than
 * passed through, so a role a newer API adds is read as the least privileged (auth-me.md): `user`
 * for the User Role and `member` for a Pool Role. The result only decides what the dashboard offers;
 * the server authorizes every request itself.
 */
function toUser(response: CurrentUser): User {
  const memberships: PoolMembership[] = response.memberships.map((membership) => ({
    poolId: membership.poolId,
    poolName: membership.poolName,
    siteId: membership.siteId,
    role: membership.role === 'owner' ? 'owner' : 'member',
  }))
  return {
    id: response.id,
    username: response.username,
    displayName: response.username,
    role: response.role === 'admin' ? 'admin' : 'user',
    status: 'active',
    memberships,
  }
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
