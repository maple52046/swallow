/* eslint-disable react-refresh/only-export-components */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Site } from '@/domain/site/types'
import { useToast } from '@/presentation/components/toast/toastContext'

interface SiteScopeValue {
  sites: Site[]
  siteId?: string
  loading: boolean
  setSite: (siteId?: string) => void
  scopedHref: (path: string) => string
}

const SiteScopeContext = createContext<SiteScopeValue | null>(null)

function detailListPath(pathname: string): string | null {
  if (/^\/servers\/[^/]+/.test(pathname)) return '/servers'
  if (/^\/clusters\/[^/]+/.test(pathname) && pathname !== '/clusters/deploy') return '/clusters'
  if (/^\/operations\/[^/]+/.test(pathname)) return '/operations'
  return null
}

/**
 * Owns the one global resource scope. The URL is authoritative so deep links,
 * back/forward navigation, and page refresh all preserve the same Site.
 */
export function SiteScopeProvider({ children }: { children: ReactNode }) {
  const { sites: repository } = useApp()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const [sites, setSites] = useState<Site[]>([])
  const [loading, setLoading] = useState(true)
  const requestedSiteId = searchParams.get('site') ?? undefined

  useEffect(() => {
    let cancelled = false
    repository
      .listSites()
      .then((items) => {
        if (!cancelled) setSites(items)
      })
      .catch((error: Error) => {
        if (!cancelled) {
          showToast({
            title: 'Sites unavailable',
            description: error.message,
            tone: 'warning',
          })
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [repository, showToast])

  useEffect(() => {
    if (loading || !requestedSiteId) return
    if (sites.some((site) => site.id === requestedSiteId)) return

    const next = new URLSearchParams(searchParams)
    next.delete('site')
    navigate(
      { pathname: location.pathname, search: next.toString() ? `?${next}` : '' },
      { replace: true },
    )
    showToast({
      title: 'Unknown Site',
      description: 'The selected Site no longer exists. Showing all Sites.',
      tone: 'warning',
    })
  }, [
    loading,
    location.pathname,
    navigate,
    requestedSiteId,
    searchParams,
    showToast,
    sites,
  ])

  const siteId = sites.some((site) => site.id === requestedSiteId)
    ? requestedSiteId
    : undefined

  const setSite = useCallback(
    (nextSiteId?: string) => {
      const listPath = detailListPath(location.pathname)
      const next = listPath ? new URLSearchParams() : new URLSearchParams(searchParams)
      if (nextSiteId) next.set('site', nextSiteId)
      else next.delete('site')
      navigate({
        pathname: listPath ?? location.pathname,
        search: next.toString() ? `?${next}` : '',
      })
    },
    [location.pathname, navigate, searchParams],
  )

  const scopedHref = useCallback(
    (path: string) => {
      if (!siteId) return path
      const target = new URL(path, window.location.origin)
      target.searchParams.set('site', siteId)
      return `${target.pathname}${target.search}${target.hash}`
    },
    [siteId],
  )

  const value = useMemo(
    () => ({ sites, siteId, loading, setSite, scopedHref }),
    [loading, scopedHref, setSite, siteId, sites],
  )

  return <SiteScopeContext.Provider value={value}>{children}</SiteScopeContext.Provider>
}

/** Returns the URL-backed global Site scope. */
export function useSiteScope(): SiteScopeValue {
  const value = useContext(SiteScopeContext)
  if (!value) throw new Error('useSiteScope must be used within SiteScopeProvider')
  return value
}
