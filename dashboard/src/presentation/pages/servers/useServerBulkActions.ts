import { useCallback, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/radix/toast/toastContext'
import { actionLabel, type BulkAction } from './serverActions'

/** One server's failure within a bulk run. */
export interface BulkFailure {
  serverId: string
  message: string
}

/** The outcome of a bulk run, for callers that want to react beyond the toast. */
export interface BulkActionResult {
  total: number
  succeeded: number
  failures: BulkFailure[]
}

/**
 * Runs a provisioner action across many servers by fanning out over the per-server API.
 *
 * swallow exposes no bulk endpoint, so "bulk" is N independent calls. They run concurrently
 * and failures are collected rather than aborting the batch — one server that cannot do
 * the action (the backend refuses unsupported ones per server) must not stop the rest.
 * The result is summarised in a toast (success count and, when any failed, a partial
 * summary) and also returned so the caller can refresh or clear selection.
 *
 * `release` is handled through the repository's release method; every other action goes
 * through `runServerAction`.
 */
export function useServerBulkActions() {
  const { servers } = useApp()
  const { showToast } = useToast()
  const [running, setRunning] = useState(false)

  const run = useCallback(
    async (action: BulkAction, serverIds: readonly string[]): Promise<BulkActionResult> => {
      setRunning(true)
      try {
        const outcomes = await Promise.allSettled(
          serverIds.map((id) =>
            action === 'release' ? servers.releaseServer(id) : servers.runServerAction(id, action),
          ),
        )

        const failures: BulkFailure[] = []
        outcomes.forEach((outcome, index) => {
          if (outcome.status === 'rejected') {
            const reason = outcome.reason
            failures.push({
              serverId: serverIds[index],
              message: reason instanceof Error ? reason.message : 'Unknown error',
            })
          }
        })

        const total = serverIds.length
        const succeeded = total - failures.length
        const label = actionLabel(action)

        if (failures.length === 0) {
          showToast({
            tone: 'success',
            title: `${label}: ${succeeded} accepted`,
            description: 'The provisioner is carrying them out; the list will converge.',
          })
        } else {
          showToast({
            tone: succeeded > 0 ? 'warning' : 'error',
            title: `${label}: ${succeeded} of ${total} accepted`,
            description: `${failures.length} failed, e.g. ${failures[0].message}`,
          })
        }

        return { total, succeeded, failures }
      } finally {
        setRunning(false)
      }
    },
    [servers, showToast],
  )

  return { run, running }
}
