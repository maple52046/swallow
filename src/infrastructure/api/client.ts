/** The api-server's default listen port. */
const DEFAULT_API_PORT = 30051

/**
 * Resolves the API address, preferring an explicit VITE_API_BASE_URL.
 *
 * Without one it is derived from whatever host the browser used to reach the dashboard.
 * That matters because the dashboard is served from a host that is usually not the
 * browser's: a hardcoded localhost works only when the two happen to be the same
 * machine, and everywhere else it fails by sending every request to the user's own
 * laptop — which looks like the backend being down rather than a misconfiguration.
 */
function resolveApiBaseUrl(): string {
  const configured = import.meta.env.VITE_API_BASE_URL as string | undefined
  if (configured) {
    return configured.replace(/\/+$/, '')
  }
  const { protocol, hostname } = window.location
  return `${protocol}//${hostname}:${DEFAULT_API_PORT}`
}

const API_BASE_URL = resolveApiBaseUrl()

const TOKEN_KEY = 'access_token'

export const tokenStore = {
  get(): string | null {
    return localStorage.getItem(TOKEN_KEY)
  },
  set(token: string): void {
    localStorage.setItem(TOKEN_KEY, token)
  },
  clear(): void {
    localStorage.removeItem(TOKEN_KEY)
  },
}

export class ApiRequestError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly status: number,
  ) {
    super(message)
    this.name = 'ApiRequestError'
  }
}

export async function apiRequest<T>(path: string, options: RequestInit = {}): Promise<T> {
  const token = tokenStore.get()

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string> | undefined),
  }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...options,
    headers,
  })

  if (response.status === 204 || response.headers.get('content-length') === '0') {
    return undefined as T
  }

  const body = await response.json().catch(() => null)

  if (!response.ok) {
    const code = (body as { error?: { code?: string; message?: string } } | null)?.error?.code ?? 'internal_error'
    const message = (body as { error?: { code?: string; message?: string } } | null)?.error?.message ?? `Request failed with status ${response.status}`
    throw new ApiRequestError(code, message, response.status)
  }

  return body as T
}
