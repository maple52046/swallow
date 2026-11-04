import { useEffect, useMemo, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import { serverDisplayName } from '@/domain/server/types'

const NO_LOOKUPS: ReadonlyMap<string, string | null> = new Map()

/**
 * Display names (`serverDisplayName`: hostname, else FQDN) of the given Servers, keyed by Server
 * id, for a view that only holds Server ids — a Change Plan's action targets — and must show
 * people a name.
 *
 * The servers contract has no lookup by several ids, so each distinct id is read once through
 * `ServerRepository.getServer`: every id not looked up yet is read together in one batch, and the
 * answers are kept for the life of the view, so re-renders and polling with the same ids send no
 * further reads; only an id that appears later is read. A Server that no longer exists, or whose
 * read failed, is remembered as unresolved and has no entry (callers show the id) rather than being
 * retried on every change. Names are presentation only: nothing may be enabled or hidden on a name.
 */
export function useServerNames(serverIds: readonly string[]): ReadonlyMap<string, string> {
  const { servers } = useApp()
  // Every id looked up so far: its display name, or null when it could not be resolved.
  const [lookups, setLookups] = useState<ReadonlyMap<string, string | null>>(NO_LOOKUPS)
  const missingKey = useMemo(
    () => [...new Set(serverIds)].filter((id) => !lookups.has(id)).sort().join(','),
    [serverIds, lookups],
  )

  // Reads the ids no lookup covers yet. When another id appears while a batch is in flight, the
  // batch is dropped (`cancelled`) and the next one reads its ids again, so a slower, older batch
  // never writes after a newer one; answers are merged, never replacing earlier lookups.
  useEffect(() => {
    if (!missingKey) return
    const ids = missingKey.split(',')
    let cancelled = false
    // Each read catches its own failure, so the batch never rejects.
    void Promise.all(ids.map((id) => servers.getServer(id).catch(() => null))).then((found) => {
      if (cancelled) return
      setLookups((current) => {
        const next = new Map(current)
        ids.forEach((id, index) => {
          const server = found[index]
          next.set(id, server ? serverDisplayName(server) : null)
        })
        return next
      })
    })
    return () => {
      cancelled = true
    }
  }, [servers, missingKey])

  return useMemo(() => {
    const names = new Map<string, string>()
    for (const [id, name] of lookups) if (name !== null) names.set(id, name)
    return names
  }, [lookups])
}
