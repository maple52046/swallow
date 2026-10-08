import { Navigate, useLocation } from 'react-router-dom'

/** Redirects the Provisioning root to its first workspace while preserving Site scope. */
export function ProvisioningRedirect() {
  const location = useLocation()
  return <Navigate to={{ pathname: '/provisioning/images', search: location.search }} replace />
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
