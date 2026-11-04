import type { HealthAxis, Server, ServerStatus } from '@/domain/server/types'

/**
 * A Server as a stream `upsert` carries it (servers-stream.md): the complete Server, except that
 * the stream never classifies health. Its `status.health` is `null`, which means "not resolved in
 * this event", never a health state, so a consumer must not show it or let it overwrite the health
 * it last read. Use `withKnownHealth` to complete it and `streamChangeMayMoveHealth` to decide
 * whether the Server should be re-read (`serverStreamPatch.ts`). A non-null value is accepted so a
 * future stream that does classify health is used as is.
 */
export interface StreamedServer extends Omit<Server, 'status'> {
  readonly status: Omit<ServerStatus, 'health'> & { readonly health: HealthAxis | null }
}

/**
 * One live change to a Server projection, delivered by {@link ServerEventStream}. `upsert`
 * carries the full projection (with unresolved health) so a list can replace or insert the row;
 * `removed` carries only the id so the list can drop it. The union is discriminated on `kind` so a
 * consumer handles each case exhaustively.
 */
export type ServerStreamEvent =
  | { readonly kind: 'upsert'; readonly server: StreamedServer }
  | { readonly kind: 'removed'; readonly id: string }

/** Scopes a subscription. `siteId` narrows the stream to one Site, matching the list scope. */
export interface ServerStreamQuery {
  readonly siteId?: string
}

/** Browser connection lifecycle exposed without leaking the concrete EventSource adapter. */
export type ServerStreamConnectionState = 'connecting' | 'connected' | 'reconnecting' | 'closed'

/** Callbacks a subscriber provides. */
export interface ServerStreamHandlers {
  /** Called once per change. */
  readonly onEvent: (event: ServerStreamEvent) => void
  /**
   * Called when the stream reconnects after a drop, signalling that events may have been
   * missed while disconnected and the consumer should reload once to resync. Not called on
   * the first successful connect, when the caller has already loaded a fresh snapshot.
   */
  readonly onReset?: () => void
  /** Reports whether live patches are connected, retrying, or no longer available. */
  readonly onConnectionChange?: (state: ServerStreamConnectionState) => void
}

/**
 * A live source of Server projection changes, so a screen keeps its list fresh by patching
 * rows instead of polling and re-reading the whole list.
 *
 * This is an application port: the presentation layer depends on it, and an infrastructure
 * adapter (SSE/EventSource) implements it. Delivery is best-effort — a dropped subscriber
 * reconnects and resyncs via onReset — so a consumer must treat the initial list load as the
 * source of truth and the stream as incremental updates on top of it.
 */
export interface ServerEventStream {
  /**
   * Start delivering changes for the given scope. Returns an unsubscribe function that stops
   * delivery and releases the underlying connection; it is safe to call more than once.
   */
  subscribe(query: ServerStreamQuery, handlers: ServerStreamHandlers): () => void
}
