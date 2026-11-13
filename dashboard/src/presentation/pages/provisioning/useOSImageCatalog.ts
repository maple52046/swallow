import { useCallback, useEffect, useRef, useState } from 'react'
import { loadOSImageCatalog, type OSImageCatalog } from '@/application/usecases/provisioning/loadOSImageCatalog'
import type { Integration } from '@/domain/site/types'
import { isAdmin } from '@/domain/user/access'
import { useApp } from '@/di/AppProvider'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'
import { useAuth } from '@/presentation/contexts/AuthContext'

/**
 * The scope's OS image catalog read. `error` is a failed first read, with its message and request
 * ID; a later refresh failure keeps `ready` and sets `refreshError`, one line that includes the
 * request ID, if any. `integrations` are the Site's provisioner Integrations an admin's page reads
 * to upload an image to one, and empty for every other user, who may not list them.
 */
export type OSImageCatalogState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
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

const NO_INTEGRATIONS: Integration[] = []

/**
 * Loads every live provisioner catalog in the current Site scope through the Site-wide reference
 * read (`loadOSImageCatalog`), the same for every user: OS Images are reference data, independent of
 * which Servers the user can see. An admin's page also reads the Site's provisioner Integrations,
 * which only uploading needs.
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
  const { currentUser } = useAuth()
  const admin = isAdmin(currentUser)
  const scopeKey = `${siteId ?? ''}|${admin ? 'admin' : 'user'}`
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

  // One catalog read per scope, user kind, or reload; `cancelled` drops the answer of a scope the
  // page left, and the in-flight flag lets the next manual reload start once this one settles.
  useEffect(() => {
    let cancelled = false
    Promise.all([
      loadOSImageCatalog(provisioning, siteId),
      admin ? sites.listIntegrations({ siteId, kind: 'provisioner' }) : Promise.resolve(NO_INTEGRATIONS),
    ])
      .then(([catalog, integrations]) => {
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
        const failure = failureOf(caught, 'Could not load OS images.')
        setStored((current) => current.scopeKey === scopeKey && current.value.status === 'ready'
          ? { scopeKey, value: { ...current.value, refreshError: failureText(failure) } }
          : { scopeKey, value: { status: 'error', ...failure } })
      })
      .finally(() => {
        reloadInFlight.current = false
        if (!cancelled) setIsRefreshing(false)
      })

    return () => {
      cancelled = true
    }
  }, [admin, nonce, provisioning, scopeKey, siteId, sites])

  const state = stored.scopeKey === scopeKey ? stored.value : { status: 'loading' as const }
  return { state, reload, isRefreshing }
}
