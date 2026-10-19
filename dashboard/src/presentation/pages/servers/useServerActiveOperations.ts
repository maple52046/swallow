import { useCallback, useEffect, useRef, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import {
  isTerminalStatus,
  operationStatus,
  type Operation,
} from '@/domain/operation/types'

/** A running Operation blocking a Server release, reduced to what the prompt shows. */
export interface ActiveOperationSummary {
  id: string
  kind: string
  /** Human-readable operator intent, e.g. "Deploy production k0s platform". */
  intent: string
  status: string
}

/**
 * Detects and clears the durable Operations that would make a Server release fail.
 *
 * The backend rejects a release while any target Server still has unfinished durable
 * work, so this hook lets the Release dialog (and the provider-failure Recover action)
 * surface that work up front and, on request, cancel it first. Cancellation is usually
 * asynchronous — the Temporal workflow records it — so {@link cancelAll} issues the cancels
 * and then polls until no target reports active work. A cancel the backend refuses for an
 * Operation that is still active rejects at once with the backend's reason instead of
 * waiting out the poll, because waiting cannot clear it. A bounded wait keeps a slow
 * cancellation from hanging the dialog forever; the backend 409 on the release itself
 * remains the final backstop if a race still slips through.
 */
export interface UseServerActiveOperationsResult {
  /** True while the initial detection request is in flight. */
  loading: boolean
  /** The unique non-terminal Operations found across all target Servers. */
  activeOperations: ActiveOperationSummary[]
  /** Cancels every detected Operation and resolves once the targets are clear. */
  cancelAll: () => Promise<void>
  /** Re-reads active Operations for the current targets. */
  refresh: () => Promise<void>
}

const POLL_INTERVAL_MS = 1000
const MAX_POLL_ATTEMPTS = 30

function summarize(operation: Operation): ActiveOperationSummary {
  return {
    id: operation.id,
    kind: operation.kind,
    intent: operation.intent,
    status: operationStatus(operation),
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

  const cancelAll = useCallback(async () => {
    const current = await read()
    if (current.length === 0) return
    const results = await Promise.allSettled(current.map((operation) => operations.cancelOperation(operation.id)))
    const refused: Array<{ operation: ActiveOperationSummary; reason: unknown }> = current.flatMap((operation, index) => {
      const result = results[index]
      return result.status === 'rejected' ? [{ operation, reason: result.reason }] : []
    })
    if (refused.length > 0) {
      // A cancel that races a natural finish is refused too, and that is success for our
      // purpose: the work is gone. A refused Operation that is still active will not stop by
      // waiting, so report the backend's reason now rather than polling into a timeout that
      // blames slowness for a cancel that was never accepted.
      const stillActive = new Set((await read()).map((operation) => operation.id))
      const blocking = refused.find(({ operation }) => stillActive.has(operation.id))
      if (blocking) {
        const reason = blocking.reason instanceof Error ? blocking.reason.message : 'Unknown error'
        throw new Error(`Could not cancel ${blocking.operation.intent}: ${reason}`)
      }
    }

    for (let attempt = 0; attempt < MAX_POLL_ATTEMPTS; attempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS))
      const remaining = await read()
      if (mounted.current) setActiveOperations(remaining)
      if (remaining.length === 0) return
    }
    throw new Error(
      'The running operations did not stop in time. Please wait a moment and try again.',
    )
  }, [operations, read])

  return { loading, activeOperations, cancelAll, refresh }
}
