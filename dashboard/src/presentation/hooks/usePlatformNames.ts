import { useEffect, useMemo, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Server } from '@/domain/server/types'

const NO_NAMES: ReadonlyMap<string, string> = new Map()

/**
 * Display names of the Platforms that claim the given Servers, keyed by Platform id, so a
 * Release or Delete disabled by a Platform Claim can say which Platform owns the Server.
 *
 * The Platform list is read only while some Server carries a claim, and again whenever the set
 * of claiming Platforms changes. Names are presentation only: a failed read yields no names
 * (callers then show the Platform id) and never re-enables the action, which the claim alone
 * disables.
 */
export function usePlatformNames(siteId: string | undefined, servers: readonly Server[]): ReadonlyMap<string, string> {
  const claimingIds = useMemo(
    () => servers.flatMap((server) => (server.status.platformClaim ? [server.status.platformClaim.platformId] : [])),
    [servers],
  )
  return usePlatformNamesById(claimingIds, siteId)
}

/**
 * Display names of the given Platforms, keyed by Platform id, for a view that only holds Platform
 * ids (a Platform Claim, a Change Plan's resource) and must show people the Platform's name.
 *
 * One `listPlatforms` read (scoped to `siteId` when given; the list is small and unpaginated)
 * resolves every id at once. It runs only while there is an id, and again only when the set of
 * ids changes, so a view that re-renders or polls with the same ids does not read again. An id
 * the list lacks (a deleted Platform) has no entry. Names are presentation only: a failed read
 * yields no names and callers show the id; nothing may be enabled or hidden on a name.
 */
export function usePlatformNamesById(platformIds: readonly string[], siteId?: string): ReadonlyMap<string, string> {
  const { platforms } = useApp()
  const idKey = useMemo(() => [...new Set(platformIds)].sort().join(','), [platformIds])
  const [names, setNames] = useState<ReadonlyMap<string, string>>(NO_NAMES)

  // The `cancelled` flag drops a read for an older id set, so it cannot overwrite a newer one.
  useEffect(() => {
    if (!idKey) return
    let cancelled = false
    platforms
      .listPlatforms(siteId)
      .then((list) => {
        if (!cancelled) setNames(new Map(list.map((platform) => [platform.id, platform.name])))
      })
      .catch(() => {
        if (!cancelled) setNames(NO_NAMES)
      })
    return () => {
      cancelled = true
    }
  }, [platforms, siteId, idKey])

  return idKey ? names : NO_NAMES
}
