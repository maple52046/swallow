import type {
  ServerEventStream,
  ServerStreamEvent,
  ServerStreamHandlers,
  ServerStreamQuery,
} from '@/application/ports/ServerEventStream'
import type { Server } from '@/domain/server/types'
import { API_BASE_URL } from './baseUrl'
import { accessTokenExpiresSoon, currentAccessToken, notifySessionEnded, refreshSession } from './session'

/**
 * The stream frame shape, matching the servers-stream contract. `server` is the same object
 * shape as a servers-list item, which maps 1:1 onto the domain {@link Server}, so no reshape
 * is needed — mirroring {@link ApiServerRepository}.
 */
interface ServerStreamMessageDTO {
  type: string
  id: string
  server?: Server
}

/** Reconnect backoff after the browser gives up on the stream: 1s, 2s, 4s, … capped at 30s. */
const RECONNECT_BASE_MS = 1_000
const RECONNECT_MAX_MS = 30_000

/**
 * EventSource-backed {@link ServerEventStream}. It authenticates with the Session access token in
 * a query parameter because a browser EventSource cannot set an Authorization header (see the
 * servers-stream contract; API keys are never used here).
 *
 * Lifecycle: transient network drops are retried by EventSource itself (`reconnecting`), and
 * onReset fires on each reopen so the consumer resyncs. When the browser gives up (`CLOSED`, which
 * is what a `401` for an expired access token causes) the state is `closed` — live updates really
 * are unavailable — and this adapter reconnects itself with backoff, renewing the access token first
 * when it is about to expire, because the original URL carries the old token; a successful reopen
 * reports `connected` again. A refresh that ends the Session reports it and stops reconnecting.
 * Unsubscribing stops the pending reconnect timer and closes the connection.
 */
export class ApiServerEventStream implements ServerEventStream {
  subscribe(query: ServerStreamQuery, handlers: ServerStreamHandlers): () => void {
    let source: EventSource | null = null
    let stopped = false
    let openedBefore = false
    let attempts = 0
    let retryTimer: ReturnType<typeof setTimeout> | undefined

    const open = () => {
      const params = new URLSearchParams()
      if (query.siteId) params.set('siteId', query.siteId)
      const token = currentAccessToken()
      if (token) params.set('access_token', token)
      const suffix = params.toString() ? `?${params.toString()}` : ''

      handlers.onConnectionChange?.(openedBefore ? 'reconnecting' : 'connecting')
      const current = new EventSource(`${API_BASE_URL}/api/v1/servers/stream${suffix}`)
      source = current

      current.onopen = () => {
        attempts = 0
        handlers.onConnectionChange?.('connected')
        // The first open follows a fresh list load, so there is nothing to resync; a later
        // open is a reconnect after a gap, so ask the consumer to reload once.
        if (openedBefore) {
          handlers.onReset?.()
        }
        openedBefore = true
      }

      current.onmessage = (event: MessageEvent) => {
        const parsed = parseServerStreamEvent(event.data)
        if (parsed) {
          handlers.onEvent(parsed)
        }
      }

      current.onerror = () => {
        if (current.readyState !== EventSource.CLOSED) {
          // EventSource is retrying a transient drop on its own; onReset fires on the next onopen.
          handlers.onConnectionChange?.('reconnecting')
          return
        }
        current.close()
        scheduleReconnect()
      }
    }

    const scheduleReconnect = () => {
      if (stopped) return
      handlers.onConnectionChange?.('closed')
      const delay = Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * 2 ** attempts)
      attempts += 1
      // Treat a reconnect after the browser gave up like a reopen, so the consumer resyncs.
      openedBefore = true
      retryTimer = setTimeout(() => {
        void reconnect()
      }, delay)
    }

    const reconnect = async () => {
      if (stopped) return
      if (accessTokenExpiresSoon() || currentAccessToken() === null) {
        try {
          if ((await refreshSession()) === 'ended') {
            notifySessionEnded()
            handlers.onConnectionChange?.('closed')
            return
          }
        } catch {
          // The server is unreachable; keep backing off rather than ending the Session.
          scheduleReconnect()
          return
        }
      }
      if (!stopped) open()
    }

    open()
    return () => {
      stopped = true
      clearTimeout(retryTimer)
      source?.close()
    }
  }
}

/** Parses one SSE frame into a domain event, ignoring heartbeats and unknown frame types. */
function parseServerStreamEvent(data: unknown): ServerStreamEvent | null {
  if (typeof data !== 'string' || data.length === 0) {
    return null
  }
  let message: ServerStreamMessageDTO
  try {
    message = JSON.parse(data) as ServerStreamMessageDTO
  } catch {
    return null
  }
  if (message.type === 'upsert' && message.server) {
    return { kind: 'upsert', server: message.server }
  }
  if (message.type === 'removed' && message.id) {
    return { kind: 'removed', id: message.id }
  }
  return null
}
