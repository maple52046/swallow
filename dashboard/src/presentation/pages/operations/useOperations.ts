import { useCallback, useEffect, useRef, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { ListOperationsFilters, Operation } from '@/domain/operation/types'
import { operationStatus } from '@/domain/operation/types'
import { isWorkflowChangingStatus } from './workflowListPresentation'

const POLL_INTERVAL_MS = 5000

/** Query-keyed state prevents a new URL filter from temporarily showing the previous result. */
export type OperationsState =
  | { status: 'loading'; filterKey: string }
  | { status: 'error'; filterKey: string; message: string }
  | {
      status: 'ready'
      filterKey: string
      operations: Operation[]
      total: number
      refreshedAt: string
      /** A background failure leaves the last-good page visible and retrying. */
      refreshError?: string
    }

/** Stable identity for one server-paginated Workflow working set. */
function operationsFilterKey(filters: ListOperationsFilters): string {
  return [
    filters.siteId ?? '',
    filters.platformId ?? '',
    filters.serverId ?? '',
    filters.kind ?? '',
    filters.status ?? '',
    filters.active ? 'active' : '',
    filters.page ?? '',
    filters.pageSize ?? '',
  ].join('|')
}

/**
 * Loads one page of Workflows and keeps changing executions current.
 *
 * Server-side filters and ordering remain authoritative. Polling runs only
 * while the current page contains a status that can still advance, stops after
 * terminal convergence, and preserves the last-good page through transient
 * refresh failures. Changing the filter signature returns loading immediately
 * so rows from a previous URL-owned view never appear under the new controls.
 */
export function useOperations(filters: ListOperationsFilters): {
  state: OperationsState
  /** Forces an immediate read without changing URL-owned filters. */
  reload: () => void
} {
  const { operations } = useApp()
  const filterKey = operationsFilterKey(filters)
  const [snapshot, setSnapshot] = useState<OperationsState>({ status: 'loading', filterKey })
  const [nonce, setNonce] = useState(0)
  const loadedFilterRef = useRef<string | undefined>(undefined)
  const pollingFilterRef = useRef<string | undefined>(undefined)
  const pollingEnabledRef = useRef(false)
  const reload = useCallback(() => setNonce((value) => value + 1), [])

  const { siteId, platformId, serverId, kind, status, active, page, pageSize } = filters

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let shouldPoll = pollingFilterRef.current === filterKey && pollingEnabledRef.current

    if (loadedFilterRef.current !== filterKey) {
      shouldPoll = false
    }

    const scheduleNext = () => {
      if (!cancelled && shouldPoll) timer = setTimeout(load, POLL_INTERVAL_MS)
    }

    const load = () => {
      operations
        .listOperations({ siteId, platformId, serverId, kind, status, active, page, pageSize })
        .then((result) => {
          if (cancelled) return
          shouldPoll = result.items.some((operation) => isWorkflowChangingStatus(operationStatus(operation)))
          pollingFilterRef.current = filterKey
          pollingEnabledRef.current = shouldPoll
          loadedFilterRef.current = filterKey
          setSnapshot({
            status: 'ready',
            filterKey,
            operations: result.items,
            total: result.total,
            refreshedAt: new Date().toISOString(),
          })
          scheduleNext()
        })
        .catch((error: Error) => {
          if (cancelled) return
          if (loadedFilterRef.current === filterKey) {
            setSnapshot((current) => current.status === 'ready' && current.filterKey === filterKey
              ? { ...current, refreshError: error.message }
              : current)
            scheduleNext()
            return
          }
          setSnapshot({ status: 'error', filterKey, message: error.message })
        })
    }

    load()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [operations, siteId, platformId, serverId, kind, status, active, page, pageSize, filterKey, nonce])

  const state = snapshot.filterKey === filterKey
    ? snapshot
    : { status: 'loading' as const, filterKey }
  return { state, reload }
}
