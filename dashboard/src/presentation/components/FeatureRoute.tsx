import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import type { ExperimentalFeature } from '@/application/ports/ExperimentalFeatureSettings'
import { useExperimentalFeature } from '@/presentation/contexts/ExperimentalFeaturesContext'

type FeatureRouteProps = {
  feature: ExperimentalFeature
  children: ReactNode
} & (
  | {
    /** Route to send a disabled URL to; the query string (Site scope) is kept. May be relative. */
    redirectTo: string
    fallback?: never
  }
  | {
    /** Rendered in place of a disabled route, for example the Not Found page. */
    fallback: ReactNode
    redirectTo?: never
  }
)

/**
 * Route guard for in-development features.
 *
 * Hiding a navigation entry is not enough: bookmarks and typed URLs still reach the
 * route. When the feature is off (always, in release builds) the route renders
 * `fallback` or redirects with `replace`, so the unfinished page never mounts and
 * issues none of its API requests. It is presentation-only; server authorization
 * is unaffected.
 */
export function FeatureRoute({ feature, children, redirectTo, fallback }: FeatureRouteProps) {
  const location = useLocation()
  const enabled = useExperimentalFeature(feature)
  if (enabled) return children
  if (redirectTo !== undefined) return <Navigate to={{ pathname: redirectTo, search: location.search }} replace />
  return fallback
}
