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

export interface LoginResponse {
  accessToken: string
}

export interface MeResponse {
  id: string
  username: string
  role: 'admin' | 'owner' | 'user'
}
