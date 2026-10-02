import { API_BASE_URL } from './baseUrl'
import {
  accessTokenExpiresSoon,
  currentAccessToken,
  notifySessionEnded,
  refreshSession,
} from './session'

export { API_BASE_URL }

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

/** Auth endpoints answer 401 for their own reasons (wrong password, ended Session); renewing and
 * retrying them would loop or mask the real error. */
const AUTH_PATH = '/api/v1/auth/'

/**
 * Renews the access token before a request when it expires within the leeway. A refresh that
 * ends the Session is reported to the presentation; the request then goes out without a token
 * and fails with the server's 401.
 */
async function ensureFreshToken(path: string): Promise<void> {
  if (path.startsWith(AUTH_PATH) || !accessTokenExpiresSoon()) return
  if ((await refreshSession()) === 'ended') notifySessionEnded()
}

/**
 * Sends one authenticated request and, when the server answers 401 to a request that carried an
 * access token, refreshes the Session once and sends it again. `send` must be safe to call twice
 * (JSON and text bodies are strings; uploads resend their FormData). A refresh that ends the
 * Session notifies the presentation and returns the original 401 response.
 */
async function withSession<R extends { status: number }>(path: string, send: (token: string | null) => Promise<R>): Promise<R> {
  await ensureFreshToken(path)
  const token = currentAccessToken()
  const first = await send(token)
  if (first.status !== 401 || token === null || path.startsWith(AUTH_PATH)) return first
  if ((await refreshSession()) === 'ended') {
    notifySessionEnded()
    return first
  }
  return send(currentAccessToken())
}

function authHeaders(token: string | null, base?: HeadersInit): Record<string, string> {
  const headers: Record<string, string> = { ...(base as Record<string, string> | undefined) }
  if (token) headers['Authorization'] = `Bearer ${token}`
  return headers
}

/**
 * Calls a JSON endpoint with the Session's access token, renewing it before expiry and once after
 * a 401. Errors use the shared envelope and are thrown as {@link ApiRequestError}.
 */
export async function apiRequest<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await withSession(path, (token) => fetch(`${API_BASE_URL}${path}`, {
    ...options,
    headers: authHeaders(token, { 'Content-Type': 'application/json', ...(options.headers as Record<string, string> | undefined) }),
  }))

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
 * multipart `Content-Type` (with boundary) itself, so this deliberately does not set it. The
 * access token is renewed before the upload starts when it is about to expire, so a long upload
 * rarely meets a 401; if it does, the upload restarts once with the renewed token (progress
 * starts over). On failure it parses the same JSON error
 * envelope into an {@link ApiRequestError}; on success it returns the parsed JSON body, or
 * `undefined` for an empty (e.g. 204) response.
 */
export function apiUpload<T>(
  path: string,
  form: FormData,
  options: { method?: string; onProgress?: (progress: UploadProgress) => void } = {},
): Promise<T> {
  return withSession(path, (token) => sendUpload(path, form, token, options)).then((outcome) => {
    if (outcome.error) throw outcome.error
    return outcome.value as T
  })
}

interface UploadOutcome {
  status: number
  value?: unknown
  error?: ApiRequestError
}

/** One XHR upload attempt; it resolves (never rejects) so {@link withSession} can see the status. */
function sendUpload(
  path: string,
  form: FormData,
  token: string | null,
  options: { method?: string; onProgress?: (progress: UploadProgress) => void },
): Promise<UploadOutcome> {
  return new Promise<UploadOutcome>((resolve) => {
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
          resolve({ status: xhr.status })
          return
        }
        try {
          resolve({ status: xhr.status, value: JSON.parse(text) })
        } catch {
          // A success with an unparseable body is treated as no content rather than an error.
          resolve({ status: xhr.status })
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
      resolve({ status: xhr.status, error: new ApiRequestError(code, message, xhr.status, requestId) })
    }
    xhr.onerror = () => resolve({ status: 0, error: new ApiRequestError('network_error', 'The upload could not reach the server.', 0) })
    xhr.onabort = () => resolve({ status: 0, error: new ApiRequestError('aborted', 'The upload was cancelled.', 0) })
    xhr.send(form)
  })
}

/**
 * Fetches an endpoint whose success body is plain text rather than JSON, such as operation
 * logs. Errors still use the JSON error envelope, so this parses that shape only on failure
 * and returns the raw text on success. An empty body yields an empty string.
 */
export async function apiRequestText(path: string, options: RequestInit = {}): Promise<string> {
  const response = await withSession(path, (token) => fetch(`${API_BASE_URL}${path}`, {
    ...options,
    headers: authHeaders(token, options.headers),
  }))
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
