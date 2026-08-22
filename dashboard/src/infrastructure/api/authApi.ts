import { apiRequest, tokenStore } from './client'
import type { LoginRequest, LoginResponse, MeResponse } from './types'

export async function login(username: string, password: string): Promise<LoginResponse> {
  const body: LoginRequest = { username, password }
  const response = await apiRequest<LoginResponse>('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  tokenStore.set(response.accessToken)
  return response
}

export async function getMe(): Promise<MeResponse> {
  return apiRequest<MeResponse>('/api/v1/auth/me')
}

export function logout(): void {
  tokenStore.clear()
}
