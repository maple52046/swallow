import { Bullseye, Spinner } from '@patternfly/react-core'
import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '@/presentation/contexts/AuthContext'

interface ProtectedRouteProps {
  children?: React.ReactNode
}

/**
 * Authenticated route guard. Session restoration finishes before redirect decisions, and
 * the complete intended URL is retained so filtered deep links survive login.
 */
export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const location = useLocation()
  const { isAuthenticated, initializing } = useAuth()
  if (initializing) return <Bullseye><Spinner size="xl" aria-label="Restoring session" /></Bullseye>
  if (!isAuthenticated) {
    const from = `${location.pathname}${location.search}${location.hash}`
    return <Navigate to="/login" replace state={{ from }} />
  }
  return children ? <>{children}</> : <Outlet />
}
