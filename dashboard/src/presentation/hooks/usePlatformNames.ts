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
  const { platforms } = useApp()
  const claimKey = useMemo(() => {
    const ids = new Set(servers.flatMap((server) => (server.platformClaim ? [server.platformClaim.platformId] : [])))
    return [...ids].sort().join(',')
  }, [servers])
  const [names, setNames] = useState<ReadonlyMap<string, string>>(NO_NAMES)

  useEffect(() => {
    if (!claimKey) return
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
  }, [platforms, siteId, claimKey])

  return claimKey ? names : NO_NAMES
}
