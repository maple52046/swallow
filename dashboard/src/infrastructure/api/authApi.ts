import { apiRequest } from './client'
import type { JsonRequestBody, JsonResponse } from './contractTypes'
import type { operations } from './generated/openapi'
import { clearTokens, storeTokens } from './session'

/**
 * The signed-in user `GET /api/v1/auth/me` returns, as the contract sends it; ApiAuthRepository
 * maps it to the domain User.
 */
export type CurrentUser = JsonResponse<operations['getCurrentUser'], 200>

/**
 * Signs in with cookie delivery (the contract default): the response body carries only the access
 * token, which goes to memory, and the browser stores the HttpOnly refresh cookie. `credentials:
 * 'include'` lets that cookie be set and sent when the API is on another development origin.
 */
export async function login(username: string, password: string): Promise<void> {
  const body: JsonRequestBody<operations['login']> = { username, password, refreshTokenDelivery: 'cookie' }
  const response = await apiRequest<JsonResponse<operations['login'], 200>>('/api/v1/auth/login', {
    method: 'POST',
    credentials: 'include',
    body: JSON.stringify(body),
  })
  storeTokens(response)
}

/** Reads the user the current Session belongs to. */
export async function getMe(): Promise<CurrentUser> {
  return apiRequest<CurrentUser>('/api/v1/auth/me')
}

/**
 * Ends the Session on the server (the cookie names it; the access token, when present, too) and
 * always forgets the local token, even if the request fails, so signing out never leaves a usable
 * credential in this tab.
 */
export async function logout(): Promise<void> {
  try {
    await apiRequest<void>('/api/v1/auth/logout', { method: 'POST', credentials: 'include' })
  } finally {
    clearTokens()
  }
}
