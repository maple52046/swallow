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
} from '@/application/ports/ServerEventStream'
import type { Server } from '@/domain/server/types'

/**
 * Discriminated state for the Servers working set.
 *
 * `loading` is only a load with no rows for the current query signature, and `error` is only a
 * current-signature load that failed. Once rows exist, background refresh failures keep the
 * last-good data and surface through `refreshError`; `refreshedAt` remains the last successful
 * complete read rather than advancing for individual SSE patches.
 */
export type WorkingSetState =
  | { status: 'loading' }
  | { status: 'ready'; data: ServerWorkingSet; refreshedAt: string; refreshError?: string }
  | { status: 'error'; message: string }

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
 */
export function useServerWorkingSet(query: WorkingSetQuery): {
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
        setStoredState((current) => {
          if (current.queryKey === queryKey && current.value.status === 'ready') {
            return {
              queryKey,
              value: { ...current.value, refreshError: err.message },
            }
          }
          return { queryKey, value: { status: 'error', message: err.message } }
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
          setStoredState((current) => {
            if (current.queryKey !== queryKey || current.value.status !== 'ready') return current
            const next = applyServerEvent(current.value.data.servers, event, filterRef.current)
            if (next === current.value.data.servers) return current
            return {
              queryKey,
              value: {
                ...current.value,
                data: { ...current.value.data, servers: next, total: next.length },
              },
            }
          })
        },
        onReset: reload,
        onConnectionChange: setStreamStatus,
      },
    )
    return unsubscribe
  }, [includeAbsent, queryKey, reload, serverEvents, siteId])

  const state = storedState.queryKey === queryKey ? storedState.value : { status: 'loading' as const }
  return { state, reload, isRefreshing, streamStatus }
}

/**
 * Applies one stream event to the working set, returning the same array reference when nothing
 * changed. Upserts that leave the coarse scope remove their former row; matching upserts replace
 * or append the complete projection.
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

  const merged = preserveHealth(incoming, existingIndex === -1 ? undefined : current[existingIndex])
  if (existingIndex === -1) return [...current, merged]
  const next = current.slice()
  next[existingIndex] = merged
  return next
}

/** Mirrors the coarse Server API filters for a streamed projection. */
function matchesWorkingSetScope(server: Server, filter: WorkingSetQuery): boolean {
  if (filter.siteId && server.source.siteId !== filter.siteId) return false
  if (!filter.includeAbsent && server.absent) return false
  const needle = filter.keyword?.trim().toLowerCase()
  if (needle) {
    const searchable = [
      server.hostname ?? '',
      server.fqdn ?? '',
      ...server.addresses,
      server.hardware.serialNumber ?? '',
      server.hardware.systemUuid ?? '',
    ]
    if (!searchable.some((value) => value.toLowerCase().includes(needle))) return false
  }
  return true
}

/**
 * Preserves the last-known Health axis when the stream omits it. Health is resolved from the
 * metrics backend at list-query time and is not carried on the Server event stream.
 */
function preserveHealth(incoming: Server, existing: Server | undefined): Server {
  return !incoming.health && existing?.health
    ? { ...incoming, health: existing.health }
    : incoming
}
