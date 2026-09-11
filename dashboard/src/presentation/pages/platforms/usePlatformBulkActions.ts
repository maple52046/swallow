import { useCallback, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Platform, UninstallPlatformOptions } from '@/domain/platform/types'
import { useToast } from '@/presentation/components/toast/toastContext'

/** The kind of destructive lifecycle action fanned out across many platforms. */
export type PlatformBulkAction = 'uninstall' | 'delete'

/** One platform's outcome, kept per target so one failure never aborts the rest. */
export interface PlatformBulkOutcome {
  platformId: string
  platformName: string
  accepted: boolean
  message?: string
}

/**
 * Runs uninstall or delete across many platforms by fanning out over the per-platform API.
 *
 * Swallow exposes no bulk platform endpoint, so each call is independent: failures are
 * collected per target instead of aborting the remaining platforms, and a single toast
 * summarises the result. Uninstall creates one durable Operation per platform; the caller
 * reloads the list rather than following any single Operation.
 */
export function usePlatformBulkActions() {
  const { platforms } = useApp()
  const { showToast } = useToast()
  const [running, setRunning] = useState(false)

  const run = useCallback(
    async (
      action: PlatformBulkAction,
      targets: readonly Platform[],
      options?: UninstallPlatformOptions,
    ): Promise<PlatformBulkOutcome[]> => {
      setRunning(true)
      try {
        const settled = await Promise.allSettled(
          targets.map((platform) =>
            action === 'uninstall'
              ? platforms.uninstallPlatform(platform.id, options)
              : platforms.deletePlatform(platform.id),
          ),
        )

        const outcomes: PlatformBulkOutcome[] = settled.map((result, index) => {
          const target = targets[index]
          return result.status === 'fulfilled'
            ? { platformId: target.id, platformName: target.name, accepted: true }
            : {
                platformId: target.id,
                platformName: target.name,
                accepted: false,
                message: result.reason instanceof Error ? result.reason.message : 'The action failed.',
              }
        })

        const failures = outcomes.filter((outcome) => !outcome.accepted)
        const verb = action === 'uninstall' ? 'Uninstall' : 'Delete'
        if (failures.length === 0) {
          showToast({
            tone: 'success',
            title: `${verb}: ${outcomes.length} accepted`,
            description: action === 'uninstall'
              ? 'Swallow is uninstalling the selected platforms; the list will converge.'
              : 'The selected platform records were removed. Hosts were not changed.',
          })
        } else {
          const accepted = outcomes.length - failures.length
          showToast({
            tone: accepted > 0 ? 'warning' : 'error',
            title: accepted > 0 ? `${verb} partially accepted` : `${verb} failed`,
            description: failures.length === 1
              ? `${failures[0].platformName}: ${failures[0].message}`
              : `${accepted} accepted; ${failures.length} failed.`,
          })
        }

        return outcomes
      } finally {
        setRunning(false)
      }
    },
    [platforms, showToast],
  )

  return { run, running }
}
