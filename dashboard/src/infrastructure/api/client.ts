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

/**
 * The resolved API origin, shared by fetch-based adapters and the SSE adapter. Empty means
 * same-origin (the dev proxy and production Nginx both serve /api on the dashboard origin).
 */
export const API_BASE_URL = resolveApiBaseUrl()

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

/** Bytes transferred so far for an in-flight upload; `total` is 0 when the browser cannot report it. */
export interface UploadProgress {
  loaded: number
  total: number
}

/**
 * Uploads a `multipart/form-data` body and reports progress.
 *
 * It uses `XMLHttpRequest` rather than `fetch` because only XHR exposes upload progress events,
 * which the OS image upload dialog needs for a multi-gigabyte artifact. The browser sets the
 * multipart `Content-Type` (with boundary) itself, so this deliberately does not set it; the
 * bearer token is attached like {@link apiRequest}. On failure it parses the same JSON error
 * envelope into an {@link ApiRequestError}; on success it returns the parsed JSON body, or
 * `undefined` for an empty (e.g. 204) response.
 */
export function apiUpload<T>(
  path: string,
  form: FormData,
  options: { method?: string; onProgress?: (progress: UploadProgress) => void } = {},
): Promise<T> {
  const token = tokenStore.get()

  return new Promise<T>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open(options.method ?? 'POST', `${API_BASE_URL}${path}`)
    if (token) {
      xhr.setRequestHeader('Authorization', `Bearer ${token}`)
    }
    // Report upload progress; total is 0 until the browser knows the full length.
    if (options.onProgress) {
      xhr.upload.onprogress = (event) => {
        options.onProgress?.({ loaded: event.loaded, total: event.lengthComputable ? event.total : 0 })
      }
    }
    xhr.onload = () => {
      const text = xhr.responseText
      if (xhr.status >= 200 && xhr.status < 300) {
        if (!text) {
          resolve(undefined as T)
          return
        }
        try {
          resolve(JSON.parse(text) as T)
        } catch {
          // A success with an unparseable body is treated as no content rather than an error.
          resolve(undefined as T)
        }
        return
      }
      // Failure: reuse the shared error envelope so callers branch on the same codes as apiRequest.
      let code = 'internal_error'
      let message = `Request failed with status ${xhr.status}`
      let requestId = xhr.getResponseHeader('X-Request-ID') ?? undefined
      try {
        const parsed = JSON.parse(text) as { error?: { code?: string; message?: string; requestId?: string } }
        code = parsed.error?.code ?? code
        message = parsed.error?.message ?? message
        requestId = parsed.error?.requestId ?? requestId
      } catch {
        // A non-JSON error body leaves the status-derived message in place.
      }
      reject(new ApiRequestError(code, message, xhr.status, requestId))
    }
    xhr.onerror = () => reject(new ApiRequestError('network_error', 'The upload could not reach the server.', 0))
    xhr.onabort = () => reject(new ApiRequestError('aborted', 'The upload was cancelled.', 0))
    xhr.send(form)
  })
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
