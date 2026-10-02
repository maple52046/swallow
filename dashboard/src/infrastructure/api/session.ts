import { API_BASE_URL } from './baseUrl'

/**
 * Browser half of a Session (decision 042, contracts auth-login / auth-refresh / auth-logout).
 *
 * Security model: the access token lives only in this module's memory — never in
 * `localStorage` — so a page reload or a new tab starts without one and obtains it from the
 * refresh endpoint. The refresh token is an HttpOnly `swallow_refresh` cookie that page scripts
 * cannot read; this module only asks the browser to send it (`credentials: 'include'`) to
 * `/api/v1/auth/*`. Nothing here logs or persists a token.
 *
 * State is module-level by design: one browser tab has one Session, shared by every adapter
 * (fetch, XHR upload, EventSource). Refreshes are single-flight within the tab and serialized
 * across tabs with the Web Locks API, so tabs do not rotate the shared cookie concurrently; the
 * server's 30-second rotation grace covers browsers without Web Locks.
 */

/** Renew this long before `accessTokenExpiresAt`, so requests do not race the expiry. */
const REFRESH_LEEWAY_MS = 60_000
const REFRESH_LOCK = 'swallow-auth-refresh'
const LEGACY_TOKEN_KEY = 'access_token'

/** Outcome of a refresh: the access token was renewed, or the Session is over. */
export type RefreshOutcome = 'renewed' | 'ended'

interface TokenResponse {
  accessToken: string
  accessTokenExpiresAt?: string
}

let accessToken: string | null = null
let expiresAtMs: number | null = null
let inflight: Promise<RefreshOutcome> | null = null
const endedListeners = new Set<() => void>()

/** The current access token, or null before sign-in, after logout, or after a reload. */
export function currentAccessToken(): string | null {
  return accessToken
}

/** Stores a token response from login or refresh. */
export function storeTokens(response: TokenResponse): void {
  accessToken = response.accessToken
  const parsed = response.accessTokenExpiresAt ? Date.parse(response.accessTokenExpiresAt) : Number.NaN
  expiresAtMs = Number.isNaN(parsed) ? null : parsed
}

/** Forgets the access token (logout, or a Session the server ended). */
export function clearTokens(): void {
  accessToken = null
  expiresAtMs = null
}

/**
 * Removes the access token older releases kept in `localStorage`. It is never read: a token there
 * is readable by any injected script, which is what Sessions exist to prevent.
 */
export function discardLegacyToken(): void {
  try {
    localStorage.removeItem(LEGACY_TOKEN_KEY)
  } catch {
    // Storage may be unavailable (privacy mode); there is then nothing to discard.
  }
}

/** Whether the token exists and expires within the refresh leeway. */
export function accessTokenExpiresSoon(): boolean {
  return accessToken !== null && expiresAtMs !== null && expiresAtMs - Date.now() < REFRESH_LEEWAY_MS
}

/**
 * Exchanges the refresh cookie for a new access token. Concurrent callers in this tab share one
 * request; other tabs wait on the Web Lock. Resolves `'ended'` on `401` (expired, logged out, or
 * revoked) after clearing the token, and rejects on network failure — a dropped connection is not
 * a reason to sign the operator out.
 */
export function refreshSession(): Promise<RefreshOutcome> {
  if (inflight) return inflight
  const run = () => requestRefresh()
  const locks = typeof navigator !== 'undefined' && 'locks' in navigator ? navigator.locks : null
  // The DOM typing makes request() return Promise<callback result>, which for an async callback
  // reads as a nested promise; at runtime the lock promise resolves with the callback's value.
  const pending = (locks ? (locks.request(REFRESH_LOCK, run) as unknown as Promise<RefreshOutcome>) : run()).finally(() => {
    inflight = null
  })
  inflight = pending
  return pending
}

async function requestRefresh(): Promise<RefreshOutcome> {
  const response = await fetch(`${API_BASE_URL}/api/v1/auth/refresh`, {
    method: 'POST',
    credentials: 'include',
  })
  if (response.status === 401) {
    clearTokens()
    return 'ended'
  }
  if (!response.ok) {
    throw new Error(`Session refresh failed with status ${response.status}`)
  }
  storeTokens((await response.json()) as TokenResponse)
  return 'renewed'
}

/**
 * Reports that the Session ended while the operator was working, so the presentation can return
 * to sign-in. Called by adapters when a refresh after a `401` ends the Session.
 */
export function notifySessionEnded(): void {
  clearTokens()
  endedListeners.forEach((listener) => listener())
}

/** Subscribes to {@link notifySessionEnded}; returns the unsubscribe function. */
export function onSessionEnded(listener: () => void): () => void {
  endedListeners.add(listener)
  return () => {
    endedListeners.delete(listener)
  }
}
