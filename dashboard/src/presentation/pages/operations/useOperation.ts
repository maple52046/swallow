import { useCallback, useEffect, useRef, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import {
  isTerminalStatus,
  type Operation,
  type OperationEvents,
} from '@/domain/operation/types'

/**
 * How often a running operation is re-read. An HA platform deployment runs for minutes
 * across several phases, so a few seconds between reads keeps progress live without
 * hammering the API; polling stops as soon as the run reaches a terminal state.
 */
const POLL_INTERVAL_MS = 5000

export interface OperationDetailData {
  operation: Operation
  /** Task-level progress; null when the events read failed but the operation loaded. */
  events: OperationEvents | null
  /** Manual refetch, e.g. after a retry navigates back to this view. */
  reload: () => void
}

export type OperationDetailState =
  | { status: 'loading' }
  | { status: 'not-found' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: OperationDetailData }

/**
 * Loads one operation and its task events, polling while the run is active.
 *
 * The operation projection is authoritative for status; the events read is a live view of
 * the runner's progress and may fail independently, so an events failure degrades to null
 * rather than failing the page. Polling is the project's first, and is bounded: it runs
 * only while the status is non-terminal and is always cleared on unmount or completion, so
 * it cannot outlive the screen. `reload` forces an immediate refetch.
 */
export function useOperation(id: string | undefined): OperationDetailState {
  const { operations } = useApp()
  const [state, setState] = useState<OperationDetailState>(
    id ? { status: 'loading' } : { status: 'not-found' },
  )
  const [nonce, setNonce] = useState(0)
  const reload = useCallback(() => setNonce((value) => value + 1), [])

  // Kept in a ref so the polling effect can read the latest status without re-subscribing.
  const statusRef = useRef<string>('pending')

  useEffect(() => {
    if (!id) return

    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined

    const load = () => {
      Promise.all([
        operations.getOperation(id),
        operations.getEvents(id).then(
          (events) => events,
          () => null,
        ),
      ])
        .then(([operation, events]) => {
          if (cancelled) return
          if (operation === null) {
            setState({ status: 'not-found' })
            return
          }
          statusRef.current = operation.execution.status
          setState({ status: 'ready', data: { operation, events, reload } })
          // Reschedule only while the run can still change, so a finished operation stops
          // polling on its own.
          if (!isTerminalStatus(operation.execution.status)) {
            timer = setTimeout(load, POLL_INTERVAL_MS)
          }
        })
        .catch((err: Error) => {
          if (cancelled) return
          setState({ status: 'error', message: err.message })
        })
    }

    load()

    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [operations, id, nonce, reload])

  return state
}
