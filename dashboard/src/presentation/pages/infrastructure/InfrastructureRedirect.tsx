import { Navigate, useLocation } from 'react-router-dom'

/** Redirects the Infrastructure root while preserving the URL-backed Site scope. */
export function InfrastructureRedirect() {
  const location = useLocation()
  return <Navigate to={{ pathname: '/infrastructure/sites', search: location.search }} replace />
}
