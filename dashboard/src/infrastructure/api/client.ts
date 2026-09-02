/**
 * Resolves the API address, preferring an explicit VITE_API_BASE_URL.
 *
 * The default is deliberately empty: both Vite in development and Nginx in production
 * proxy /api on the dashboard origin.
 */
function resolveApiBaseUrl(): string {
  const configured = import.meta.env.VITE_API_BASE_URL as string | undefined
  if (configured) {
    return configured.replace(/\/+$/, '')
  }
  return ''
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

/** A client-safe API failure with an opaque server-log correlation ID. */
export class ApiRequestError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly status: number,
    public readonly requestId?: string,
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
    const requestId = (body as { error?: { requestId?: string } } | null)?.error?.requestId
      ?? response.headers.get('X-Request-ID') ?? undefined
    const code = (body as { error?: { code?: string; message?: string; requestId?: string } } | null)?.error?.code ?? 'internal_error'
    const message = (body as { error?: { code?: string; message?: string; requestId?: string } } | null)?.error?.message ?? `Request failed with status ${response.status}`
    throw new ApiRequestError(code, message, response.status, requestId)
  }

  return body as T
}

/**
 * Fetches an endpoint whose success body is plain text rather than JSON, such as operation
 * logs. Errors still use the JSON error envelope, so this parses that shape only on failure
 * and returns the raw text on success. An empty body yields an empty string.
 */
export async function apiRequestText(path: string, options: RequestInit = {}): Promise<string> {
  const token = tokenStore.get()

  const headers: Record<string, string> = {
    ...(options.headers as Record<string, string> | undefined),
  }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const response = await fetch(`${API_BASE_URL}${path}`, { ...options, headers })
  const text = await response.text()

  if (!response.ok) {
    let code = 'internal_error'
    let message = `Request failed with status ${response.status}`
    let requestId = response.headers.get('X-Request-ID') ?? undefined
    try {
      const parsed = JSON.parse(text) as { error?: { code?: string; message?: string; requestId?: string } }
      code = parsed.error?.code ?? code
      message = parsed.error?.message ?? message
      requestId = parsed.error?.requestId ?? requestId
    } catch {
      // A non-JSON error body leaves the status-derived message in place.
    }
    throw new ApiRequestError(code, message, response.status, requestId)
  }

  return text
}
