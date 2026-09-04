import { Navigate, useLocation, useParams } from 'react-router-dom'

/** Redirects former Cluster URLs while preserving scope and detail identity. */
export function LegacyPlatformRedirect({ deploy = false }: { deploy?: boolean }) {
  const location = useLocation()
  const { id } = useParams()
  const path = deploy ? '/platforms/deploy' : id ? `/platforms/${encodeURIComponent(id)}` : '/platforms'
  return <Navigate to={path + location.search + location.hash} replace />
}
