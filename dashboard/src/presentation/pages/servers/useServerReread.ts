import { useCallback, useEffect, useMemo, useRef } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Server } from '@/domain/server/types'

/** What the caller does with a finished re-read. */
export interface ServerRereadHandlers {
  /** The Server as the API reads it now, health classified; `null` when it no longer exists. */
  readonly onRead: (id: string, server: Server | null) => void
  /** The read failed; the caller keeps what it shows (the stream and the next full read recover). */
  readonly onFailure?: (id: string) => void
}

/** The re-reads started for one `scopeKey`; `live` turns false when that scope ends. */
interface RereadScope {
  live: boolean
  /** The newest unfinished read per Server id; an answer whose token is not here is stale. */
  readonly latest: Map<string, symbol>
}

/** The re-read controls a stream-patching hook uses. */
export interface ServerReread {
  /** Starts a read of one Server, superseding an unfinished read of the same id. */
  readonly reread: (id: string) => void
  /** True while a read of `id` started in the current scope has not answered yet. */
  readonly isRereading: (id: string) => boolean
}

/**
 * Re-reads single Servers on demand through `ServerRepository.getServer` (GET /servers/{id}), for
 * hooks that patch held Servers from the live stream. A stream upsert never classifies health
 * (servers-stream.md), so when it changes a fact that can move health (see
 * `streamChangeMayMoveHealth`), or brings a Server the caller does not hold yet, the caller re-reads
 * that one Server instead of reloading everything.
 *
 * Lifecycle and safety:
 * - Each `reread(id)` supersedes an unfinished read of the same id; the superseded answer is
 *   dropped, so an older read can never overwrite a newer one.
 * - Every unfinished read is dropped when `scopeKey` changes (a new Site or query) or the component
 *   unmounts. The repository offers no abort, so the request still completes, but nothing it returns
 *   reaches the caller.
 * - The handlers are read through a ref at answer time, so callers need not memoize them and always
 *   see their latest state setters and scope.
 */
export function useServerReread(scopeKey: string, handlers: ServerRereadHandlers): ServerReread {
  const { servers } = useApp()
  const handlersRef = useRef(handlers)
  useEffect(() => {
    handlersRef.current = handlers
  })
  const scopeRef = useRef<RereadScope | null>(null)

  // One scope per key and repository; its cleanup is what drops the answers of the scope it ends.
  useEffect(() => {
    const scope: RereadScope = { live: true, latest: new Map() }
    scopeRef.current = scope
    return () => {
      scope.live = false
    }
  }, [scopeKey, servers])

  const reread = useCallback((id: string) => {
    const scope = scopeRef.current
    if (!scope?.live) return
    const token = Symbol(id)
    scope.latest.set(id, token)
    const settle = (deliver: () => void) => {
      if (!scope.live || scope.latest.get(id) !== token) return
      scope.latest.delete(id)
      deliver()
    }
    servers.getServer(id).then(
      (server) => settle(() => handlersRef.current.onRead(id, server)),
      () => settle(() => handlersRef.current.onFailure?.(id)),
    )
  }, [servers])

  const isRereading = useCallback((id: string) => scopeRef.current?.latest.has(id) ?? false, [])

  // Stable identity, so a stream subscription that depends on it is not torn down every render.
  return useMemo(() => ({ reread, isRereading }), [reread, isRereading])
}
