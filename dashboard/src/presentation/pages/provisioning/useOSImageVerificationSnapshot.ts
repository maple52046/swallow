import { useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import { isWorkflowChangingStatus } from '@/presentation/pages/operations/workflowListPresentation'
import { projectOSImageVerificationActivities } from './useOSImageVerificationActivity'

interface VerificationSnapshot {
  scopeKey: string
  verifyingTargets: ReadonlySet<string>
}

const EMPTY_VERIFYING_TARGETS: ReadonlySet<string> = new Set()

/**
 * Reads active image-verification Workflows once for an opened image picker.
 *
 * This deliberately does not use the live Workflow hook: the selection flow only needs to explain
 * that a target was being verified when it opened. It never polls, retries, refreshes the catalog,
 * or exposes a read failure; durable image capability remains the safe fallback in those cases.
 */
export function useOSImageVerificationSnapshot(siteId: string | undefined): ReadonlySet<string> {
  const { operations } = useApp()
  const scopeKey = siteId ?? ''
  const [snapshot, setSnapshot] = useState<VerificationSnapshot>({
    scopeKey: '',
    verifyingTargets: EMPTY_VERIFYING_TARGETS,
  })

  useEffect(() => {
    if (!siteId) return
    let cancelled = false
    operations
      .listOperations({
        siteId,
        kind: 'verify-os-image',
        active: true,
        page: 1,
        pageSize: 100,
      })
      .then((result) => {
        if (cancelled) return
        const activities = projectOSImageVerificationActivities(result.items)
        const verifyingTargets = new Set(
          [...activities.entries()]
            .filter(([, activity]) => isWorkflowChangingStatus(activity.status))
            .map(([key]) => key),
        )
        setSnapshot({ scopeKey, verifyingTargets })
      })
      .catch(() => {
        if (!cancelled) setSnapshot({ scopeKey, verifyingTargets: EMPTY_VERIFYING_TARGETS })
      })
    return () => {
      cancelled = true
    }
  }, [operations, scopeKey, siteId])

  return snapshot.scopeKey === scopeKey
    ? snapshot.verifyingTargets
    : EMPTY_VERIFYING_TARGETS
}
