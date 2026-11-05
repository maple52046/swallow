import { useCallback, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Platform, PlatformChangeResult, UninstallPlatformOptions } from '@/domain/platform/types'
import { failureOf, failureText } from '@/presentation/components/failure'
import { useToast } from '@/presentation/components/toast/toastContext'
import { platformBulkDeleteToast, platformBulkUninstallToast } from './platformRemovalCopy'

/** One platform's outcome, kept per target so one failure never aborts the rest. */
export interface PlatformBulkOutcome {
  platformId: string
  platformName: string
  accepted: boolean
  /**
   * The Change Plan an accepted Uninstall proposed. Absent when the Platform was deleted at once
   * because its install was never approved (platforms.md "Delete A Platform"), and for a
   * record-only Delete, which proposes nothing.
   */
  planId?: string
  /**
   * The proposed plan waits for a separate approval in Change Plans (the Change Plan policy or a
   * blocker); false when the confirmed dialog approved it and the uninstall started.
   */
  awaitingApproval?: boolean
  /** Why a rejected request failed, as one line that includes the request ID when there is one. */
  message?: string
}

/** Toast copy for a bulk run, worked out from the accepted outcomes. */
type BulkToastCopy = (accepted: readonly PlatformBulkOutcome[]) => { title: string; partialTitle: string; description: string }

/**
 * Runs a bulk Uninstall or a bulk record-only Delete by fanning out over the per-platform API.
 *
 * swallow exposes no bulk platform endpoint, so each call is independent: failures are collected
 * per target instead of aborting the remaining platforms. `runUninstall` sends the same Delete body
 * to every target, which proposes one Change Plan per platform, approved by the confirmed dialog
 * unless the Change Plan policy keeps it waiting (each record is deleted only after its own
 * uninstall succeeds), or deletes at once a Platform whose install was never approved; each outcome
 * keeps the plan it proposed and whether it waits, so the caller can send the user to Change Plans
 * only for plans that wait.
 * `runDelete` deletes only the records and takes no uninstall options, so a record-only Delete can
 * never carry them. When at least one request was accepted a single toast summarises the result;
 * when every request failed no toast is shown, because the dialog stays open and lists each
 * failure.
 */
export function usePlatformBulkActions() {
  const { platforms } = useApp()
  const { showToast } = useToast()
  const [running, setRunning] = useState(false)

  const fanOut = useCallback(
    async (
      targets: readonly Platform[],
      request: (platform: Platform) => Promise<PlatformChangeResult | void>,
      copy: BulkToastCopy,
      partialNote = '',
    ): Promise<PlatformBulkOutcome[]> => {
      setRunning(true)
      try {
        const settled = await Promise.allSettled(targets.map(request))
        const outcomes: PlatformBulkOutcome[] = settled.map((result, index) => {
          const target = targets[index]
          if (result.status === 'fulfilled') {
            const planId = result.value?.planId
            return {
              platformId: target.id,
              platformName: target.name,
              accepted: true,
              ...(planId ? { planId, awaitingApproval: result.value?.awaitingApproval ?? false } : {}),
            }
          }
          return {
            platformId: target.id,
            platformName: target.name,
            accepted: false,
            message: failureText(failureOf(result.reason, 'The action failed.')),
          }
        })

        const failures = outcomes.filter((outcome) => !outcome.accepted)
        const accepted = outcomes.filter((outcome) => outcome.accepted)
        const text = copy(accepted)
        if (failures.length === 0) {
          showToast({ tone: 'success', title: text.title, description: text.description })
        } else if (accepted.length > 0) {
          showToast({
            tone: 'warning',
            title: text.partialTitle,
            description: `${accepted.length} of ${outcomes.length} requests were accepted; ${failures.length} failed. Failed: ${failures.map((failure) => failure.platformName).join(', ')}.${partialNote}`,
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
      (platform) => platforms.deletePlatform(platform.id, options),
      (accepted) => {
        const proposed = accepted.filter((outcome) => outcome.planId)
        const waiting = proposed.filter((outcome) => outcome.awaitingApproval).length
        return platformBulkUninstallToast({ releaseServers: options.releaseServers }, {
          started: proposed.length - waiting, waiting, deletedAtOnce: accepted.length - proposed.length,
        })
      },
    ),
    [fanOut, platforms],
  )

  const runDelete = useCallback(
    (targets: readonly Platform[]) => fanOut(
      targets,
      (platform) => platforms.deletePlatformRecord(platform.id),
      (accepted) => platformBulkDeleteToast(accepted.length),
    ),
    [fanOut, platforms],
  )

  return { runUninstall, runDelete, running }
}
