import { useCallback, useEffect, useRef, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import {
  loadServerWorkingSet,
  type ServerWorkingSet,
  type WorkingSetQuery,
} from '@/application/usecases/servers/loadServerWorkingSet'
import type {
  ServerStreamConnectionState,
  ServerStreamEvent,
  StreamedServer,
} from '@/application/ports/ServerEventStream'
import { streamChangeMayMoveHealth, withKnownHealth } from '@/application/usecases/servers/serverStreamPatch'
import type { Server } from '@/domain/server/types'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'
import { useServerReread } from './useServerReread'

/**
 * Discriminated state for the Servers working set.
 *
 * `loading` is only a load with no rows for the current query signature, and `error` is only a
 * current-signature load that failed. Once rows exist, background refresh failures keep the
 * last-good data and surface through `refreshError` (one line that includes the request ID, if
 * any); `refreshedAt` remains the last successful complete read rather than advancing for
 * individual SSE patches. `error` carries the failed read's message and request ID.
 */
export type WorkingSetState =
  | { status: 'loading' }
  | { status: 'ready'; data: ServerWorkingSet; refreshedAt: string; refreshError?: string }
  | ({ status: 'error' } & DisplayedFailure)

interface StoredWorkingSetState {
  queryKey: string
  value: WorkingSetState
}

/**
 * Loads a complete Server working set and layers best-effort SSE patches over the snapshot.
 *
 * Query changes expose `loading` immediately so a previous Site cannot remain visible. Stale
 * requests are discarded, reconnects request one complete resync, and an SSE removal reloads
 * an include-absent working set because the frame cannot distinguish deletion from absence.
 * The query has no `provisioningState`: a stream frame cannot tell whether a patched Server
 * still belongs to such a set, so a caller that needs it loads the set without the stream.
 *
 * Health: a stream upsert carries no health, so a patched row keeps the health last read for it.
 * When the upsert changes the row's provisioning state, lock, or `absent` flag, or names a Server
 * the set does not hold yet, that one Server is re-read (`useServerReread`) and the answer replaces
 * the row, so for example a released Server turns N/A without a full reload. A new Server joins the
 * set only with that answer, because the dashboard has no health to show for it before. A failed
 * re-read keeps the patched row; for a Server the set does not hold, it falls back to one complete
 * reload so the row is not silently missing. Re-reads in flight are dropped when the query changes.
 */
export function useServerWorkingSet(query: Pick<WorkingSetQuery, 'siteId' | 'keyword' | 'includeAbsent'>): {
  state: WorkingSetState
  reload: () => void
  /** True while a manual reload is in flight, for a refresh control's busy state. */
  isRefreshing: boolean
  /** Best-effort SSE connectivity; list reads remain usable in every state. */
  streamStatus: ServerStreamConnectionState
} {
  const { servers, serverEvents } = useApp()
  const { siteId, keyword, includeAbsent } = query
  const queryKey = [siteId ?? '', keyword ?? '', includeAbsent ? 'absent' : 'current'].join('|')
  const [storedState, setStoredState] = useState<StoredWorkingSetState>({
    queryKey,
    value: { status: 'loading' },
  })
  const [nonce, setNonce] = useState(0)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [streamStatus, setStreamStatus] = useState<ServerStreamConnectionState>('connecting')
  const reloadInFlight = useRef(false)

  const reload = useCallback(() => {
    if (reloadInFlight.current) return
    reloadInFlight.current = true
    setIsRefreshing(true)
    setNonce((value) => value + 1)
  }, [])

  // The stream is scoped by Site server-side; the remaining coarse filters are applied to a
  // patch through this ref so changing them never tears down and rebuilds the connection.
  const filterRef = useRef<WorkingSetQuery>(query)
  useEffect(() => {
    filterRef.current = { siteId, keyword, includeAbsent }
  }, [siteId, keyword, includeAbsent])

  useEffect(() => {
    let cancelled = false
    loadServerWorkingSet(servers, { siteId, keyword, includeAbsent })
      .then((data) => {
        if (!cancelled) {
          setStoredState({
            queryKey,
            value: { status: 'ready', data, refreshedAt: new Date().toISOString() },
          })
        }
      })
      .catch((err: Error) => {
        if (cancelled) return
        const failure = failureOf(err, 'Servers could not be loaded.')
        setStoredState((current) => {
          if (current.queryKey === queryKey && current.value.status === 'ready') {
            return {
              queryKey,
              value: { ...current.value, refreshError: failureText(failure) },
            }
          }
          return { queryKey, value: { status: 'error', ...failure } }
        })
      })
      .finally(() => {
        reloadInFlight.current = false
        if (!cancelled) setIsRefreshing(false)
      })

    return () => {
      cancelled = true
    }
  }, [servers, siteId, keyword, includeAbsent, nonce, queryKey])

  // The latest stored state, read by the stream callbacks (never during render) to decide whether
  // an upsert needs a re-read before its state update is applied.
  const storedRef = useRef(storedState)
  useEffect(() => {
    storedRef.current = storedState
  })

  const { reread, isRereading } = useServerReread(queryKey, {
    onRead: (id, server) => {
      setStoredState((current) => updateReadyServers(current, queryKey, (held) => applyServerRead(held, id, server, filterRef.current)))
    },
    onFailure: (id) => {
      const snapshot = storedRef.current
      const held = snapshot.queryKey === queryKey && snapshot.value.status === 'ready' ? snapshot.value.data.servers : null
      if (held && !held.some((server) => server.id === id)) reload()
    },
  })

  useEffect(() => {
    const unsubscribe = serverEvents.subscribe(
      { siteId },
      {
        onEvent: (event) => {
          // `removed` means either deletion or transition to absent. When absent projections
          // belong to the snapshot, only a complete read can tell which result is correct.
          if (event.kind === 'removed' && includeAbsent) {
            reload()
            return
          }
          const snapshot = storedRef.current
          if (snapshot.queryKey !== queryKey || snapshot.value.status !== 'ready') return
          setStoredState((current) => updateReadyServers(current, queryKey, (held) => applyServerEvent(held, event, filterRef.current)))
          // An unfinished re-read is restarted too, so the answer that lands postdates this event.
          if (event.kind === 'upsert') {
            const { server } = event
            if (needsReread(snapshot.value.data.servers, server, filterRef.current) || isRereading(server.id)) reread(server.id)
          }
        },
        onReset: reload,
        onConnectionChange: setStreamStatus,
      },
    )
    return unsubscribe
  }, [includeAbsent, isRereading, queryKey, reload, reread, serverEvents, siteId])

  const state = storedState.queryKey === queryKey ? storedState.value : { status: 'loading' as const }
  return { state, reload, isRefreshing, streamStatus }
}

/**
 * Replaces the ready working set's rows through `update`, keeping the same state object (no
 * re-render) when the state belongs to another query, is not ready, or `update` changed nothing.
 */
function updateReadyServers(
  current: StoredWorkingSetState,
  queryKey: string,
  update: (servers: Server[]) => Server[],
): StoredWorkingSetState {
  if (current.queryKey !== queryKey || current.value.status !== 'ready') return current
  const next = update(current.value.data.servers)
  if (next === current.value.data.servers) return current
  return {
    queryKey,
    value: { ...current.value, data: { ...current.value.data, servers: next, total: next.length } },
  }
}

/**
 * Applies one stream event to the working set, returning the same array reference when nothing
 * changed. Upserts that leave the coarse scope remove their former row; matching upserts replace a
 * held row with the new projection and the row's known health. An upsert for a Server the set does
 * not hold changes nothing here: it joins through its re-read, which carries its health.
 */
function applyServerEvent(
  current: Server[],
  event: ServerStreamEvent,
  filter: WorkingSetQuery,
): Server[] {
  if (event.kind === 'removed') {
    return current.some((server) => server.id === event.id)
      ? current.filter((server) => server.id !== event.id)
      : current
  }

  const incoming = event.server
  const existingIndex = current.findIndex((server) => server.id === incoming.id)
  if (!matchesWorkingSetScope(incoming, filter)) {
    return existingIndex === -1
      ? current
      : current.filter((server) => server.id !== incoming.id)
  }
  if (existingIndex === -1) return current
  const next = current.slice()
  next[existingIndex] = withKnownHealth(incoming, current[existingIndex].status.health)
  return next
}

/**
 * Whether an in-scope upsert needs its Server re-read: the set does not hold it yet (no health to
 * keep), or it changed a fact that can move its health (`streamChangeMayMoveHealth`).
 */
function needsReread(held: readonly Server[], incoming: StreamedServer, filter: WorkingSetQuery): boolean {
  if (!matchesWorkingSetScope(incoming, filter)) return false
  const known = held.find((server) => server.id === incoming.id)
  return !known || streamChangeMayMoveHealth(known, incoming)
}

/**
 * Applies a re-read Server: replaces or appends it while it matches the coarse scope, and drops its
 * row when it left the scope or no longer exists (`null`). Same array reference when nothing changed.
 */
function applyServerRead(
  current: Server[],
  id: string,
  server: Server | null,
  filter: WorkingSetQuery,
): Server[] {
  const index = current.findIndex((item) => item.id === id)
  if (server === null || !matchesWorkingSetScope(server, filter)) {
    return index === -1 ? current : current.filter((item) => item.id !== id)
  }
  if (index === -1) return [...current, server]
  const next = current.slice()
  next[index] = server
  return next
}

/** Mirrors the coarse Server API filters for a listed or streamed projection; health is not read. */
function matchesWorkingSetScope(server: { readonly status: StreamedServer['status'] }, filter: WorkingSetQuery): boolean {
  if (filter.siteId && server.status.source.siteId !== filter.siteId) return false
  if (!filter.includeAbsent && server.status.absent) return false
  const needle = filter.keyword?.trim().toLowerCase()
  if (needle) {
    const searchable = [
      server.status.hostname ?? '',
      server.status.fqdn ?? '',
      ...server.status.addresses,
      server.status.hardware.serialNumber ?? '',
      server.status.hardware.systemUuid ?? '',
    ]
    if (!searchable.some((value) => value.toLowerCase().includes(needle))) return false
  }
  return true
}
