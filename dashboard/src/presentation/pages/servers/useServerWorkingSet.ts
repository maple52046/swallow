import { useCallback, useEffect, useRef, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import {
  loadServerWorkingSet,
  type ServerWorkingSet,
  type WorkingSetQuery,
} from '@/application/usecases/servers/loadServerWorkingSet'
import type { ServerStreamEvent } from '@/application/ports/ServerEventStream'
import type { Server } from '@/domain/server/types'

/** Discriminated state so loading/ready/error cannot contradict each other. */
export type WorkingSetState =
  | { status: 'loading' }
  | { status: 'ready'; data: ServerWorkingSet }
  | { status: 'error'; message: string }

/**
 * Loads the servers working set for the list and re-runs when the coarse query changes.
 *
 * The coarse query (`keyword`, `includeAbsent`) is what the API filters server-side; the
 * page applies grouping and the multi-dimension filter to the returned set. A stale-guard
 * ignores out-of-order responses so fast typing cannot let an earlier result overwrite a
 * later one, and `reload` refetches after an action changes the fleet.
 */
export function useServerWorkingSet(query: WorkingSetQuery): {
  state: WorkingSetState
  reload: () => void
} {
  const { servers, serverEvents } = useApp()
  const [state, setState] = useState<WorkingSetState>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)
  const reloadInFlight = useRef(false)

  const reload = useCallback(() => {
    if (reloadInFlight.current) return
    reloadInFlight.current = true
    setNonce((value) => value + 1)
  }, [])

  // Destructured so the effect depends on the primitive query fields, not a fresh object
  // identity each render.
  const { siteId, keyword, includeAbsent } = query

  // The stream is scoped by Site server-side; the remaining coarse filters are applied to a
  // patch through this ref so changing them never tears down and rebuilds the connection. The
  // ref is updated in an effect (not during render) and one commit behind is harmless because
  // events are applied asynchronously.
  const filterRef = useRef<WorkingSetQuery>(query)
  useEffect(() => {
    filterRef.current = { siteId, keyword, includeAbsent }
  }, [siteId, keyword, includeAbsent])

  useEffect(() => {
    let cancelled = false

    loadServerWorkingSet(servers, { siteId, keyword, includeAbsent })
      .then((data) => {
        if (!cancelled) setState({ status: 'ready', data })
      })
      .catch((err: Error) => {
        if (!cancelled) setState({ status: 'error', message: err.message })
      })
      .finally(() => {
        reloadInFlight.current = false
      })

    return () => {
      cancelled = true
    }
  }, [servers, siteId, keyword, includeAbsent, nonce])

  useEffect(() => {
    const unsubscribe = serverEvents.subscribe(
      { siteId },
      {
        onEvent: (event) => {
          setState((current) => {
            if (current.status !== 'ready') return current
            const next = applyServerEvent(current.data.servers, event, filterRef.current)
            if (next === current.data.servers) return current
            return { status: 'ready', data: { ...current.data, servers: next } }
          })
        },
        onReset: reload,
      },
    )
    return unsubscribe
  }, [serverEvents, siteId, reload])

  return { state, reload }
}

/**
 * Applies one stream event to the working set, returning the same array reference when
 * nothing changed so React can skip the re-render. A removal drops the row; an upsert
 * replaces or inserts the row when it still matches the coarse scope, or drops it when a
 * change moved it out of scope (for example it became absent while absent rows are hidden).
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
  if (existingIndex === -1) {
    return [...current, merged]
  }
  const next = current.slice()
  next[existingIndex] = merged
  return next
}

/**
 * Mirrors the coarse, server-side working-set filters so a patched row stays consistent with
 * what the initial load would have returned. `siteId` is already enforced by the stream
 * scope; `keyword` matches the same fields as the list endpoint; absent rows are excluded
 * unless requested.
 */
function matchesWorkingSetScope(server: Server, filter: WorkingSetQuery): boolean {
  if (filter.siteId && server.source.siteId !== filter.siteId) return false
  if (!filter.includeAbsent && server.absent) return false
  const needle = filter.keyword?.trim().toLowerCase()
  if (needle) {
    const haystacks = [
      server.hostname ?? '',
      server.fqdn ?? '',
      ...server.addresses,
      server.hardware.serialNumber ?? '',
      server.hardware.systemUuid ?? '',
    ]
    if (!haystacks.some((value) => value.toLowerCase().includes(needle))) return false
  }
  return true
}

/**
 * Keeps the last-known health axis when the stream omits it. Health is resolved from the
 * metrics backend at list-query time and is not carried on the change stream, so a naive
 * replace would blank a row's health until the next full reload.
 */
function preserveHealth(incoming: Server, existing: Server | undefined): Server {
  if (!incoming.health && existing?.health) {
    return { ...incoming, health: existing.health }
  }
  return incoming
}
