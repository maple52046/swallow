import { useEffect, useMemo, useState } from 'react'
import { useApp } from '@/di/AppProvider'

/** Current read-only lock check for a set of Operation or Platform targets. */
export interface TargetLockProtection {
  checking: boolean
  lockedNames: string[]
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
    lockedNames: string[]
    error?: string
  }>({ targetKey: '', lockedNames: [] })

  useEffect(() => {
    const ids = targetKey ? targetKey.split(',') : []
    let cancelled = false
    Promise.all(ids.map((id) => servers.getServer(id)))
      .then((targets) => {
        if (cancelled) return
        setState({
          targetKey,
          lockedNames: targets.flatMap((server) =>
            server?.provisioning?.locked ? [server.hostname || server.id] : [],
          ),
        })
      })
      .catch(() => {
        if (!cancelled) {
          setState({
            targetKey,
            lockedNames: [],
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
    lockedNames: state.lockedNames,
    error: state.error,
  }
}
