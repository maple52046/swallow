import { useEffect, useMemo, useRef } from 'react'
import { operationStatus, type Operation } from '@/domain/operation/types'
import { isWorkflowChangingStatus } from '@/presentation/pages/operations/workflowListPresentation'
import { useOperations } from '@/presentation/pages/operations/useOperations'
import {
  osImageVerificationKey,
  type OSImageTarget,
  type OSImageVerificationActivity,
  type OSImageVerificationActivityMap,
} from './osImageListPresentation'

interface PreviousChangingKeys {
  scopeKey: string
  initialized: boolean
  keys: ReadonlySet<string>
}

/**
 * Projects changing or attention-blocked verification Workflows onto exact provider image targets.
 * Callers decide whether they need live polling or a one-shot snapshot; malformed legacy intent is
 * ignored instead of being attached to the wrong provider artifact.
 */
export function projectOSImageVerificationActivities(
  operations: readonly Operation[],
): OSImageVerificationActivityMap {
  const next = new Map<string, OSImageVerificationActivity>()
  for (const operation of operations) {
    if (operation.kind !== 'verify-os-image') continue
    const status = operationStatus(operation)
    if (!isWorkflowChangingStatus(status) && status !== 'requires_attention') continue
    const request = (operation.intentSnapshot?.request ?? {}) as Record<string, unknown>
    const integrationId = String(request.integrationId ?? request.IntegrationID ?? '')
    const imageId = String(request.imageId ?? request.ImageID ?? '')
    const architecture = String(request.architecture ?? request.Architecture ?? '')
    const target = String(request.deployTarget ?? request.DeployTarget ?? '')
    if (!integrationId || !imageId || (target !== 'disk' && target !== 'ram')) continue
    const activity: OSImageVerificationActivity = {
      operationId: operation.id,
      integrationId,
      imageId,
      architecture,
      target: target as OSImageTarget,
      status,
      statusReason: operation.statusReason ?? operation.execution.statusReason,
      requestedAt: operation.requestedAt,
    }
    const key = osImageVerificationKey(integrationId, imageId, architecture, target)
    const current = next.get(key)
    if (!current || current.requestedAt < activity.requestedAt) next.set(key, activity)
  }
  return next
}

/**
 * Projects active image-verification Workflows onto exact provider image targets.
 *
 * The shared Workflow hook owns adaptive five-second polling and last-good error behavior. When a
 * changing target disappears from the active result, the catalog is reloaded once so its durable
 * verified/failed outcome replaces transient activity without adding a new API surface.
 */
export function useOSImageVerificationActivity(
  siteId: string | undefined,
  onWorkflowSettled: () => void,
): {
  activities: OSImageVerificationActivityMap
  refreshError?: string
  reload: () => void
} {
  const { state, reload } = useOperations({
    siteId,
    kind: 'verify-os-image',
    active: true,
    page: 1,
    pageSize: 100,
  })
  const scopeKey = siteId ?? ''
  const previous = useRef<PreviousChangingKeys>({ scopeKey, initialized: false, keys: new Set() })

  const activities = useMemo(
    () => state.status === 'ready'
      ? projectOSImageVerificationActivities(state.operations)
      : new Map<string, OSImageVerificationActivity>(),
    [state],
  )

  useEffect(() => {
    if (state.status !== 'ready') return
    const changing = new Set(
      [...activities.entries()]
        .filter(([, activity]) => isWorkflowChangingStatus(activity.status))
        .map(([key]) => key),
    )
    if (previous.current.scopeKey !== scopeKey) {
      previous.current = { scopeKey, initialized: true, keys: changing }
      return
    }
    if (previous.current.initialized) {
      const settled = [...previous.current.keys].some((key) => !changing.has(key))
      if (settled) onWorkflowSettled()
    }
    previous.current = { scopeKey, initialized: true, keys: changing }
  }, [activities, onWorkflowSettled, scopeKey, state.status])

  useEffect(() => {
    if (state.status !== 'error') return
    const retry = setTimeout(reload, 5000)
    return () => clearTimeout(retry)
  }, [reload, state])

  return {
    activities,
    refreshError: state.status === 'ready' ? state.refreshError : state.status === 'error' ? state.message : undefined,
    reload,
  }
}
