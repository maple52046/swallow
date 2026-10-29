import { useCallback, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Platform, UninstallPlatformOptions } from '@/domain/platform/types'
import { useToast } from '@/presentation/components/toast/toastContext'
import { platformBulkDeleteToast, platformBulkUninstallToast } from './platformRemovalCopy'

/** One platform's outcome, kept per target so one failure never aborts the rest. */
export interface PlatformBulkOutcome {
  platformId: string
  platformName: string
  accepted: boolean
  message?: string
}

/**
 * Runs a bulk Uninstall or a bulk record-only Delete by fanning out over the per-platform API.
 *
 * Swallow exposes no bulk platform endpoint, so each call is independent: failures are collected
 * per target instead of aborting the remaining platforms. `runUninstall` sends the same request
 * body to every target and creates one durable Operation per platform (with `deleteRecord`, each
 * record is deleted only after its own uninstall succeeds); `runDelete` sends Delete and takes no
 * uninstall options, so a record-only Delete can never carry them. When at least one request was
 * accepted a single toast summarises the result; when every request failed no toast is shown,
 * because the dialog stays open and lists each failure. The caller reloads the list rather than
 * following any single Operation.
 */
export function usePlatformBulkActions() {
  const { platforms } = useApp()
  const { showToast } = useToast()
  const [running, setRunning] = useState(false)

  const fanOut = useCallback(
    async (
      targets: readonly Platform[],
      request: (platform: Platform) => Promise<unknown>,
      copy: (accepted: number) => { title: string; partialTitle: string; description: string },
      partialNote = '',
    ): Promise<PlatformBulkOutcome[]> => {
      setRunning(true)
      try {
        const settled = await Promise.allSettled(targets.map(request))
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
        const accepted = outcomes.length - failures.length
        const text = copy(accepted)
        if (failures.length === 0) {
          showToast({ tone: 'success', title: text.title, description: text.description })
        } else if (accepted > 0) {
          showToast({
            tone: 'warning',
            title: text.partialTitle,
            description: `${accepted} of ${outcomes.length} requests were accepted; ${failures.length} failed. Failed: ${failures.map((failure) => failure.platformName).join(', ')}.${partialNote}`,
          })
        }
        return outcomes
      } finally {
        setRunning(false)
      }
    },
    [showToast],
  )

  const runUninstall = useCallback(
    (targets: readonly Platform[], options: UninstallPlatformOptions) => fanOut(
      targets,
      (platform) => platforms.uninstallPlatform(platform.id, options),
      (accepted) => platformBulkUninstallToast({ releaseServers: options.releaseServers, deleteRecord: Boolean(options.deleteRecord) }, accepted),
      options.deleteRecord ? ' A record is deleted only after its own uninstall succeeds.' : '',
    ),
    [fanOut, platforms],
  )

  const runDelete = useCallback(
    (targets: readonly Platform[]) => fanOut(targets, (platform) => platforms.deletePlatform(platform.id), platformBulkDeleteToast),
    [fanOut, platforms],
  )

  return { runUninstall, runDelete, running }
}
