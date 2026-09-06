import { Navigate, useLocation, useParams } from 'react-router-dom'

/**
 * Redirects former Operation URLs to the canonical Workflow routes (ADR 017),
 * preserving detail identity, query scope, and hash.
 */
export function LegacyWorkflowRedirect() {
  const location = useLocation()
  const { id } = useParams()
  const path = id ? `/workflows/${encodeURIComponent(id)}` : '/workflows'
  return <Navigate to={path + location.search + location.hash} replace />
}
