import { useEffect, useMemo, useState } from 'react'
import { useApp } from '@/di/AppProvider'

const NO_POOLS: ReadonlyMap<string, string | null> = new Map()

/**
 * The Pool of each given Platform (`status.poolId`), keyed by Platform id, for a view that holds only
 * Platform ids (a Change Plan's resource, a Workflow's target) and must decide whether the user may
 * act on them.
 *
 * One `listPlatforms` read resolves every id; it runs only while there is an id and again only when
 * the set of ids changes. Until it answers, after it fails, or for a Platform the list lacks, the id
 * has no entry, which callers treat as a Platform the user may not operate: the API judges every
 * request anyway, and the dashboard errs towards hiding what it would refuse.
 */
export function usePlatformPools(platformIds: readonly string[]): ReadonlyMap<string, string | null> {
  const { platforms } = useApp()
  const idKey = useMemo(() => [...new Set(platformIds)].sort().join(','), [platformIds])
  const [pools, setPools] = useState<ReadonlyMap<string, string | null>>(NO_POOLS)

  // The `cancelled` flag drops a read for an older id set, so it cannot overwrite a newer one.
  useEffect(() => {
    if (!idKey) return
    let cancelled = false
    platforms
      .listPlatforms()
      .then((list) => {
        if (!cancelled) setPools(new Map(list.map((platform) => [platform.id, platform.status.poolId ?? null])))
      })
      .catch(() => {
        if (!cancelled) setPools(NO_POOLS)
      })
    return () => {
      cancelled = true
    }
  }, [platforms, idKey])

  return idKey ? pools : NO_POOLS
}
