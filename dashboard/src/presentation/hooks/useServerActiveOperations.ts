import { useCallback, useEffect, useRef, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import {
  isTerminalStatus,
  operationStatus,
  type Operation,
} from '@/domain/operation/types'

/** An unfinished Operation holding a Server, reduced to what a confirmation prompt shows. */
export interface ActiveOperationSummary {
  id: string
  kind: string
  /** Human-readable operator intent, e.g. "Deploy production k0s platform". */
  intent: string
  status: string
  /** Servers the Operation targets, so a multi-Server prompt can say which ones are held. */
  serverIds: readonly string[]
}

/**
 * Detects the durable Operations that would make a Release, Recover, Delete, or Platform
 * uninstall of these Servers fail.
 *
 * The backend rejects those actions while any target Server still has unfinished durable work,
 * so this hook lets their dialogs surface that work up front. Canceling it is the backend's job
 * (`cancelActiveWorkflows`, decision 061): the browser never waits for a cancellation, which can
 * take minutes once a canceled deployment returns its Server to Ready.
 */
export interface UseServerActiveOperationsResult {
  /** True while the initial detection request is in flight. */
  loading: boolean
  /** The unique non-terminal Operations found across all target Servers. */
  activeOperations: ActiveOperationSummary[]
  /** Re-reads active Operations for the current targets. */
  refresh: () => Promise<void>
}

function summarize(operation: Operation): ActiveOperationSummary {
  return {
    id: operation.id,
    kind: operation.kind,
    intent: operation.intent,
    status: operationStatus(operation),
    serverIds: operation.targetServerIds,
  }
}

/**
 * Reads the active (non-terminal) Operations for the given Servers.
 *
 * Server IDs are joined into a stable dependency key so passing a fresh array each render
 * does not re-trigger detection. A failed read fails open (treated as "no known active
 * work") rather than blocking a release the operator asked for.
 */
export function useServerActiveOperations(
  serverIds: readonly string[],
): UseServerActiveOperationsResult {
  const { operations } = useApp()
  const [loading, setLoading] = useState(serverIds.length > 0)
  const [activeOperations, setActiveOperations] = useState<ActiveOperationSummary[]>([])
  // Detection may resolve after the dialog closes; a ref guards against setting state on an
  // unmounted dialog without cancelling the in-flight promises.
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const idsKey = [...serverIds].sort().join(',')

  const read = useCallback(async (): Promise<ActiveOperationSummary[]> => {
    if (serverIds.length === 0) return []
    const pages = await Promise.all(
      serverIds.map((serverId) =>
        operations.listOperations({ serverId, active: true, pageSize: 100 }),
      ),
    )
    const byId = new Map<string, ActiveOperationSummary>()
    for (const page of pages) {
      for (const operation of page.items) {
        // The list endpoint's active filter is authoritative on the server, but re-check
        // client-side so a compatibility payload without the filter cannot show a finished
        // Operation as blocking work.
        if (isTerminalStatus(operationStatus(operation))) continue
        byId.set(operation.id, summarize(operation))
      }
    }
    return [...byId.values()]
    // idsKey stands in for serverIds so a new array identity does not churn the callback.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [operations, idsKey])

  const refresh = useCallback(async () => {
    try {
      const found = await read()
      if (mounted.current) setActiveOperations(found)
    } catch {
      if (mounted.current) setActiveOperations([])
    } finally {
      if (mounted.current) setLoading(false)
    }
  }, [read])

  useEffect(() => {
    setLoading(serverIds.length > 0)
    void refresh()
    // idsKey captures the meaningful change in serverIds.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [idsKey, refresh])

  return { loading, activeOperations, refresh }
}
