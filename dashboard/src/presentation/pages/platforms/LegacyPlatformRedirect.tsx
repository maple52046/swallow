import { Navigate, useLocation, useParams } from 'react-router-dom'

/**
 * LegacyPlatformRedirect redirects former Cluster deep links (/clusters,
 * /clusters/deploy, /clusters/:id) to their canonical Platform routes.
 *
 * The Cluster -> Platform rename keeps these routes working for one release so
 * existing bookmarks and links do not break. The current query string and hash are
 * preserved so site scope (?site=) and in-page anchors survive the redirect, and the
 * detail identity is carried across when present.
 */
export function LegacyPlatformRedirect({ deploy = false }: { deploy?: boolean }) {
  const location = useLocation()
  const { id } = useParams()
  const path = deploy ? '/platforms/deploy' : id ? `/platforms/${encodeURIComponent(id)}` : '/platforms'
  return <Navigate to={path + location.search + location.hash} replace />
}
