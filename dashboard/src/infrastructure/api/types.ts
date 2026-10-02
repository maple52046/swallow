/**
 * Wire types for the auth endpoints.
 *
 * Every other resource's response maps directly onto its domain type, so those have no
 * separate wire type: the contract was designed so a client does not reshape anything,
 * and a translation layer would only be somewhere for the two to drift apart.
 */

export interface LoginRequest {
  username: string
  password: string
}

/**
 * Login and refresh response (auth-login / auth-refresh contracts). The browser uses cookie
 * delivery, so `refreshToken` never appears here; the refresh token stays in an HttpOnly cookie.
 */
export interface LoginResponse {
  accessToken: string
  accessTokenExpiresAt: string
}

export interface MeResponse {
  id: string
  username: string
  role: 'admin' | 'owner' | 'user'
  /** How this request authenticated; the dashboard always uses a Session. */
  authMethod?: 'session' | 'api_key'
}
