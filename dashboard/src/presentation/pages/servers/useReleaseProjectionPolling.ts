import { useCallback, useEffect, useRef, useState } from 'react'
import type { ProvisioningTask } from '@/domain/provisioning/types'
import type { ProvisioningState } from '@/domain/server/types'

const POLL_INTERVAL_MS = 2_000
const MAX_POLL_ATTEMPTS = 60
const SETTLED_RELEASE_STATES = new Set<ProvisioningState>([
  'ready',
  'failed',
  'broken',
  'retired',
])
const TERMINAL_TASK_STATES = new Set<ProvisioningTask['status']>(['succeeded', 'failed'])

export interface ReleaseProjectionTarget {
  serverId: string
  taskId?: string
}

/**
 * Refreshes Server projections after an asynchronous Release until every target settles.
 * The provider owns the lifecycle transition and Swallow advances each target through
 * a live targeted refresh. When Release created static-IP cleanup work, polling follows
 * the durable task to a terminal state before its final projection refresh. Polling is
 * bounded, never survives unmount, and treats a target absent from the current working
 * set as settled because a filter may legitimately exclude it after its state changes.
 */
export function useReleaseProjectionPolling(
  states: ReadonlyMap<string, ProvisioningState | null>,
  refresh: (serverIds: readonly string[]) => Promise<void>,
  getTask: (taskId: string) => Promise<ProvisioningTask>,
): {
  isPolling: boolean
  start: (targets: readonly ReleaseProjectionTarget[]) => void
} {
  const [targets, setTargets] = useState<readonly ReleaseProjectionTarget[]>([])
  const attempts = useRef(0)
  const statesRef = useRef(states)

  useEffect(() => {
    statesRef.current = states
  }, [states])

  const start = useCallback((nextTargets: readonly ReleaseProjectionTarget[]) => {
    const unique = [...new Map(nextTargets.map((target) => [target.serverId, target])).values()]
    attempts.current = 0
    setTargets(unique)
  }, [])

  useEffect(() => {
    if (targets.length === 0) return

    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const targetIds = targets.map((target) => target.serverId)
    const tick = async () => {
      if (cancelled) return
      await refresh(targetIds)
      if (cancelled) return
      attempts.current += 1

      const taskStates = await Promise.all(targets.map(async (target) => {
        if (!target.taskId) return true
        try {
          const task = await getTask(target.taskId)
          return TERMINAL_TASK_STATES.has(task.status)
        } catch {
          return false
        }
      }))
      if (cancelled) return
      const projectionsSettled = targetIds.every((id) => {
        const state = statesRef.current.get(id)
        return state === undefined || (state !== null && SETTLED_RELEASE_STATES.has(state))
      })
      if (projectionsSettled && taskStates.every(Boolean)) {
        await refresh(targetIds)
        if (cancelled) return
        setTargets([])
        return
      }
      if (attempts.current >= MAX_POLL_ATTEMPTS) {
        setTargets([])
        return
      }
      timer = setTimeout(() => void tick(), POLL_INTERVAL_MS)
    }

    void tick()
    return () => {
      cancelled = true
      if (timer !== undefined) clearTimeout(timer)
    }
  }, [getTask, refresh, targets])

  return { isPolling: targets.length > 0, start }
}
