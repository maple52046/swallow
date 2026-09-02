import { useCallback, useEffect, useRef, useState } from 'react'
import type { ProvisioningState } from '@/domain/server/types'

const POLL_INTERVAL_MS = 2_000
const MAX_POLL_ATTEMPTS = 60
const SETTLED_RELEASE_STATES = new Set<ProvisioningState>([
  'ready',
  'failed',
  'broken',
  'retired',
])

/**
 * Refreshes Server projections after an asynchronous Release until every target settles.
 *
 * The provider owns the lifecycle transition and Swallow advances each target through
 * a live targeted refresh. Polling is bounded, never survives unmount, and treats a target that
 * disappears from the current working set as settled because a filter may legitimately
 * exclude it after its state changes.
 */
export function useReleaseProjectionPolling(
  states: ReadonlyMap<string, ProvisioningState | null>,
  refresh: (serverIds: readonly string[]) => Promise<void>,
): {
  isPolling: boolean
  start: (serverIds: readonly string[]) => void
} {
  const [targetIds, setTargetIds] = useState<readonly string[]>([])
  const attempts = useRef(0)
  const statesRef = useRef(states)

  useEffect(() => {
    statesRef.current = states
  }, [states])

  const start = useCallback((serverIds: readonly string[]) => {
    const unique = [...new Set(serverIds)]
    attempts.current = 0
    setTargetIds(unique)
  }, [])

  useEffect(() => {
    if (targetIds.length === 0) return

    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const tick = async () => {
      if (cancelled) return
      const settled = attempts.current > 0 && targetIds.every((id) => {
        const state = statesRef.current.get(id)
        return state === undefined || (state !== null && SETTLED_RELEASE_STATES.has(state))
      })
      if (settled) {
        setTargetIds([])
        return
      }
      await refresh(targetIds)
      if (cancelled) return
      attempts.current += 1
      if (attempts.current >= MAX_POLL_ATTEMPTS) {
        setTargetIds([])
        return
      }
      timer = setTimeout(() => void tick(), POLL_INTERVAL_MS)
    }

    void tick()
    return () => {
      cancelled = true
      if (timer !== undefined) clearTimeout(timer)
    }
  }, [refresh, targetIds])

  return { isPolling: targetIds.length > 0, start }
}
