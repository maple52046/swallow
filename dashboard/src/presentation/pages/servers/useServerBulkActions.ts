import { useCallback, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'
import type { ReleaseServerInput } from '@/domain/server/types'
import { actionLabel, type BulkAction } from './serverActions'
import {
  failedServerActionOutcomes,
  rejectedServerActionOutcome,
  serverActionRunResult,
  persistServerActionResult,
  type ServerActionRunResult,
  type ServerActionTarget,
} from './serverActionResults'

/**
 * Runs a provisioner action across many Servers by fanning out over the per-Server API.
 *
 * Swallow exposes no bulk action endpoint, so failures are collected per target instead
 * of aborting the remaining independent calls. The complete result is returned for the
 * persistent details UI; the toast is only a concise immediate summary.
 */
export function useServerBulkActions() {
  const { servers } = useApp()
  const { showToast } = useToast()
  const [running, setRunning] = useState(false)

  const run = useCallback(
    async (
      action: BulkAction,
      targets: readonly ServerActionTarget[],
      releaseInput?: ReleaseServerInput,
    ): Promise<ServerActionRunResult> => {
      setRunning(true)
      try {
        const settled = await Promise.allSettled(
          targets.map((target) =>
            action === 'release'
              ? servers.releaseServer(target.serverId, releaseInput)
              : servers.runServerAction(target.serverId, action),
          ),
        )

        const outcomes = settled.map((outcome, index) => {
          const target = targets[index]
          return outcome.status === 'fulfilled'
            ? { ...target, accepted: true, taskId: outcome.value.taskId }
            : rejectedServerActionOutcome(target, outcome.reason)
        })
        const result = serverActionRunResult(action, outcomes)
        persistServerActionResult(result)
        const failures = failedServerActionOutcomes(result)
        const label = actionLabel(action)

        if (failures.length === 0) {
          showToast({
            tone: 'success',
            title: `${label}: ${result.succeeded} accepted`,
            description: 'The provisioner is carrying them out; the list will converge.',
          })
        } else {
          showToast({
            tone: result.succeeded > 0 ? 'warning' : 'error',
            title: result.succeeded > 0 ? `${label} partially accepted` : `${label} failed`,
            description: failures.length === 1
              ? `${failures[0].serverName}: ${failures[0].message}`
              : `${result.succeeded} accepted; ${failures.length} failed. Review the result details.`,
          })
        }

        return result
      } finally {
        setRunning(false)
      }
    },
    [servers, showToast],
  )

  return { run, running }
}
