import type {
  ServerEventStream,
  ServerStreamEvent,
  ServerStreamHandlers,
  ServerStreamQuery,
} from '@/application/ports/ServerEventStream'
import type { Server } from '@/domain/server/types'
import { API_BASE_URL, tokenStore } from './client'

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

/**
 * EventSource-backed {@link ServerEventStream}. It authenticates with the access token in a
 * query parameter because a browser EventSource cannot set an Authorization header (see the
 * servers-stream contract), and relies on the browser's native EventSource reconnect for
 * transient drops — firing onReset on each reconnect so the consumer resyncs. A terminal
 * error (e.g. an expired token) leaves the stream closed; the screen still works via manual
 * reload or navigation, which re-subscribes with a fresh token.
 */
export class ApiServerEventStream implements ServerEventStream {
  subscribe(query: ServerStreamQuery, handlers: ServerStreamHandlers): () => void {
    const params = new URLSearchParams()
    if (query.siteId) params.set('siteId', query.siteId)
    const token = tokenStore.get()
    if (token) params.set('access_token', token)
    const suffix = params.toString() ? `?${params.toString()}` : ''

    handlers.onConnectionChange?.('connecting')
    const source = new EventSource(`${API_BASE_URL}/api/v1/servers/stream${suffix}`)
    let openedBefore = false

    source.onopen = () => {
      handlers.onConnectionChange?.('connected')
      // The first open follows a fresh list load, so there is nothing to resync; a later
      // open is a reconnect after a gap, so ask the consumer to reload once.
      if (openedBefore) {
        handlers.onReset?.()
      }
      openedBefore = true
    }

    source.onmessage = (event: MessageEvent) => {
      const parsed = parseServerStreamEvent(event.data)
      if (parsed) {
        handlers.onEvent(parsed)
      }
    }

    // Transient drops are retried by EventSource itself; onReset fires on the next onopen.
    source.onerror = () => {
      handlers.onConnectionChange?.(
        source.readyState === EventSource.CLOSED ? 'closed' : 'reconnecting',
      )
    }

    return () => source.close()
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
