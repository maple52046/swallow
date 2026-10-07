import { useCallback, useEffect, useRef, useState } from 'react'
import { loadOSImageCatalog, type OSImageCatalog } from '@/application/usecases/provisioning/loadOSImageCatalog'
import type { Integration } from '@/domain/site/types'
import { useApp } from '@/di/AppProvider'

export type OSImageCatalogState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | {
      status: 'ready'
      catalog: OSImageCatalog
      integrations: Integration[]
      refreshedAt: string
      refreshError?: string
    }

interface StoredCatalogState {
  scopeKey: string
  value: OSImageCatalogState
}

/**
 * Loads every live provisioner catalog in the current Site scope.
 *
 * Scope-keyed state prevents a previous Site from remaining visible, while manual or
 * verification-triggered refresh failures retain the last-good catalog. Individual provider
 * failures are successful partial results owned by `OSImageCatalog.failures`, not hook errors.
 */
export function useOSImageCatalog(siteId?: string): {
  state: OSImageCatalogState
  reload: () => void
  isRefreshing: boolean
} {
  const { sites, provisioning } = useApp()
  const scopeKey = siteId ?? ''
  const [stored, setStored] = useState<StoredCatalogState>({ scopeKey, value: { status: 'loading' } })
  const [nonce, setNonce] = useState(0)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const reloadInFlight = useRef(false)

  const reload = useCallback(() => {
    if (reloadInFlight.current) return
    reloadInFlight.current = true
    setIsRefreshing(true)
    setNonce((value) => value + 1)
  }, [])

  useEffect(() => {
    let cancelled = false
    sites
      .listIntegrations({ siteId, kind: 'provisioner' })
      .then(async (integrations) => ({ integrations, catalog: await loadOSImageCatalog(provisioning, integrations) }))
      .then(({ integrations, catalog }) => {
        if (cancelled) return
        setStored({
          scopeKey,
          value: {
            status: 'ready',
            catalog,
            integrations,
            refreshedAt: new Date().toISOString(),
          },
        })
      })
      .catch((caught: unknown) => {
        if (cancelled) return
        const message = caught instanceof Error ? caught.message : 'Could not load OS images.'
        setStored((current) => current.scopeKey === scopeKey && current.value.status === 'ready'
          ? { scopeKey, value: { ...current.value, refreshError: message } }
          : { scopeKey, value: { status: 'error', message } })
      })
      .finally(() => {
        reloadInFlight.current = false
        if (!cancelled) setIsRefreshing(false)
      })

    return () => {
      cancelled = true
    }
  }, [nonce, provisioning, scopeKey, siteId, sites])

  const state = stored.scopeKey === scopeKey ? stored.value : { status: 'loading' as const }
  return { state, reload, isRefreshing }
}
