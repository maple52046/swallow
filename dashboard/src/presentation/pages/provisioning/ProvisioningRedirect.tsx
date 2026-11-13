import { Navigate, useLocation } from 'react-router-dom'

/** Redirects the retired Provisioning root to OS Images while preserving Site scope. */
export function ProvisioningRedirect() {
  const location = useLocation()
  return <Navigate to={{ pathname: '/provisioning/images', search: location.search }} replace />
}

/**
 * Keeps bookmarks and older links to `/provisioning/boot-isos` working now that Boot ISOs is an
 * Infrastructure tab: it replaces the history entry with `/infrastructure/boot-isos`, keeping the
 * query string (the Site scope) and the hash.
 */
export function LegacyBootISOsRedirect() {
  const location = useLocation()
  return <Navigate to={{ pathname: '/infrastructure/boot-isos', search: location.search, hash: location.hash }} replace />
}

/** Sends retired full-page deploy links back to Servers without carrying obsolete draft inputs. */
export function LegacyDeployOSRedirect() {
  const location = useLocation()
  const current = new URLSearchParams(location.search)
  const next = new URLSearchParams()
  const siteId = current.get('site')
  if (siteId) next.set('site', siteId)
  return <Navigate to={{ pathname: '/servers', search: next.toString() }} replace />
}
