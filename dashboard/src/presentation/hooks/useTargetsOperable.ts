import { useEffect, useMemo, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import { canOperatePool, isAdmin, ownsAnyPool } from '@/domain/user/access'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { usePlatformPools } from './usePlatformPools'

const NO_IDS: string[] = []

/**
 * Whether the signed-in user may operate every given target — the Pool operate class a Workflow's
 * `cancel`, `rerun`, and Task retry need on each Server and Platform it targets (workflows.md).
 *
 * An admin may always; a user who owns no Pool never may, and neither check reads anything. Anyone
 * else needs to own the Pool of every target, so the targets' Pools are read: each Server once and
 * the Platforms in one list. Until they answer, after a failed read, or with no target at all (only
 * admins act on such a Workflow) the answer is `false`, so a control that would be refused stays
 * hidden; the API judges the request either way.
 */
export function useTargetsOperable(serverIds: readonly string[], platformIds: readonly string[]): boolean {
  const { servers } = useApp()
  const { currentUser } = useAuth()
  const admin = isAdmin(currentUser)
  const resolves = !admin && ownsAnyPool(currentUser)
  const serverKey = useMemo(() => (resolves ? [...new Set(serverIds)].sort().join(',') : ''), [resolves, serverIds])
  const platformPools = usePlatformPools(resolves ? platformIds : NO_IDS)
  const [serverPools, setServerPools] = useState<{ key: string; pools: ReadonlyArray<string | null> | null }>({ key: '', pools: null })

  useEffect(() => {
    if (!serverKey) return
    let cancelled = false
    Promise.all(serverKey.split(',').map((id) => servers.getServer(id)))
      .then((targets) => {
        if (!cancelled) setServerPools({ key: serverKey, pools: targets.map((server) => server?.spec.poolId ?? null) })
      })
      .catch(() => {
        if (!cancelled) setServerPools({ key: serverKey, pools: null })
      })
    return () => {
      cancelled = true
    }
  }, [servers, serverKey])

  if (admin) return true
  if (!resolves || (serverIds.length === 0 && platformIds.length === 0)) return false
  const serversOperable = serverIds.length === 0 ||
    (serverPools.key === serverKey && serverPools.pools !== null &&
      serverPools.pools.every((poolId) => canOperatePool(currentUser, poolId)))
  const platformsOperable = platformIds.every((id) => platformPools.has(id) && canOperatePool(currentUser, platformPools.get(id)))
  return serversOperable && platformsOperable
}
