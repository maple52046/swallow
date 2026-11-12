import { Navigate, useLocation } from 'react-router-dom'

/** Redirects the Validation root to its Runs tab while preserving the Site scope and filters. */
export function ValidationRedirect() {
  const location = useLocation()
  return <Navigate to={{ pathname: '/validation/runs', search: location.search }} replace />
}
