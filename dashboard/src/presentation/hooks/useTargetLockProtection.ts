import { useEffect, useMemo, useState } from 'react'
import { useApp } from '@/di/AppProvider'

/**
 * Current read-only lock check for a set of Operation or Platform targets. `lockedTargets`
 * keeps each locked Server's id beside its display name so a caller checking several Platforms
 * at once (`usePlatformUninstallEligibility`) can attribute a lock to the Platform that owns it;
 * `lockedNames` is the same list as display names only.
 */
export interface TargetLockProtection {
  checking: boolean
  lockedNames: string[]
  lockedTargets: ReadonlyArray<{ serverId: string; name: string }>
  error?: string
}

/**
 * Resolves provider-owned Server Lock projections for controls that mutate several targets.
 * A failed or incomplete read is returned explicitly so callers can fail closed; the hook
 * never treats missing protection data as permission to enable an action.
 */
export function useTargetLockProtection(
  targetServerIds: readonly string[] | undefined,
): TargetLockProtection {
  const { servers } = useApp()
  const targetKey = useMemo(
    () => [...(targetServerIds ?? [])].sort().join(','),
    [targetServerIds],
  )
  const [state, setState] = useState<{
    targetKey: string
    lockedTargets: ReadonlyArray<{ serverId: string; name: string }>
    error?: string
  }>({ targetKey: '', lockedTargets: [] })

  useEffect(() => {
    const ids = targetKey ? targetKey.split(',') : []
    let cancelled = false
    Promise.all(ids.map((id) => servers.getServer(id)))
      .then((targets) => {
        if (cancelled) return
        setState({
          targetKey,
          lockedTargets: targets.flatMap((server) =>
            server?.provisioning?.locked ? [{ serverId: server.id, name: server.hostname || server.id }] : [],
          ),
        })
      })
      .catch(() => {
        if (!cancelled) {
          setState({
            targetKey,
            lockedTargets: [],
            error: 'Target protection could not be checked. Refresh and try again.',
          })
        }
      })
    return () => {
      cancelled = true
    }
  }, [servers, targetKey])

  return {
    checking: state.targetKey !== targetKey,
    lockedNames: state.lockedTargets.map((target) => target.name),
    lockedTargets: state.lockedTargets,
    error: state.error,
  }
}
