import { apiRequest } from './client'
import { clearTokens, storeTokens } from './session'
import type { LoginRequest, LoginResponse, MeResponse } from './types'

/**
 * Signs in with cookie delivery (the contract default): the response body carries only the access
 * token, which goes to memory, and the browser stores the HttpOnly refresh cookie. `credentials:
 * 'include'` lets that cookie be set and sent when the API is on another development origin.
 */
export async function login(username: string, password: string): Promise<LoginResponse> {
  const body: LoginRequest = { username, password }
  const response = await apiRequest<LoginResponse>('/api/v1/auth/login', {
    method: 'POST',
    credentials: 'include',
    body: JSON.stringify(body),
  })
  storeTokens(response)
  return response
}

export async function getMe(): Promise<MeResponse> {
  return apiRequest<MeResponse>('/api/v1/auth/me')
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
