import { useCallback, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'

/**
 * The bulk operations the OS image catalog offers over a multi-selection.
 *
 * `delete` removes provider-owned custom images; `reset-overrides` clears the swallow-owned
 * overlay (docs/decisions/025) and reverts each image's name, OS, and release to its provider
 * values. Deploy and edit are deliberately absent: each needs a per-image target or distinct
 * values, so they are not batchable.
 */
export type OSImageBulkAction = 'delete' | 'reset-overrides'

/** One image a bulk action runs against, identified by the catalog's overlay-key identity. */
export interface OSImageBulkTarget {
  integrationId: string
  imageId: string
  architecture: string
  /** Human label used only in result messages. */
  name: string
}

/** The result of running a bulk action against one target. */
export interface OSImageBulkOutcome extends OSImageBulkTarget {
  accepted: boolean
  /** Present only on failure: the client-safe reason to show the operator. */
  message?: string
}

/** A concise, human label for an action, used in toast titles. */
function actionLabel(action: OSImageBulkAction): string {
  return action === 'delete' ? 'Delete images' : 'Reset overrides'
}

/**
 * Fans a catalog action out across many images and reports per-image outcomes.
 *
 * Swallow exposes no bulk image endpoint, so each target is called independently and failures
 * are collected rather than aborting the rest — one provider refusal must not hide the images
 * that succeeded. The toast is a concise summary; the returned outcomes let the caller decide
 * whether to keep its dialog open (every target failed) or hand control back to the page.
 */
export function useOSImageBulkActions() {
  const { provisioning } = useApp()
  const { showToast } = useToast()
  const [running, setRunning] = useState(false)

  const run = useCallback(
    async (
      action: OSImageBulkAction,
      targets: readonly OSImageBulkTarget[],
    ): Promise<OSImageBulkOutcome[]> => {
      setRunning(true)
      try {
        const settled = await Promise.allSettled(
          targets.map((target) =>
            action === 'delete'
              ? provisioning.deleteOSImage(target.integrationId, target.imageId, target.architecture)
              : provisioning.clearOSImageOverlay(target.integrationId, target.imageId, target.architecture),
          ),
        )

        const outcomes: OSImageBulkOutcome[] = settled.map((outcome, index) => {
          const target = targets[index]
          if (outcome.status === 'fulfilled') return { ...target, accepted: true }
          return {
            ...target,
            accepted: false,
            message: outcome.reason instanceof Error ? outcome.reason.message : 'The request failed.',
          }
        })

        const succeeded = outcomes.filter((outcome) => outcome.accepted).length
        const failures = outcomes.filter((outcome) => !outcome.accepted)
        const label = actionLabel(action)

        if (failures.length === 0) {
          showToast({ tone: 'success', title: `${label}: ${succeeded} done` })
        } else {
          showToast({
            tone: succeeded > 0 ? 'warning' : 'error',
            title: succeeded > 0 ? `${label} partially applied` : `${label} failed`,
            description:
              failures.length === 1
                ? `${failures[0].name}: ${failures[0].message}`
                : `${succeeded} done; ${failures.length} failed.`,
          })
        }

        return outcomes
      } finally {
        setRunning(false)
      }
    },
    [provisioning, showToast],
  )

  return { run, running }
}
