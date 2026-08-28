import { Navigate, useLocation } from 'react-router-dom'

/** Redirects the Provisioning root while preserving the URL-backed Site scope. */
export function ProvisioningRedirect() {
  const location = useLocation()
  return <Navigate to={{ pathname: '/provisioning/deploy', search: location.search }} replace />
}
