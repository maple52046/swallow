import { Flex, Spinner } from '@radix-ui/themes'
import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '@/presentation/contexts/AuthContext'

interface ProtectedRouteProps {
  children?: React.ReactNode
}

/**
 * Route guard that gates everything behind an authenticated session.
 *
 * While the session is being restored it shows a spinner rather than redirecting, so a
 * deep link is not bounced to login before the check resolves. Once resolved, an
 * unauthenticated visitor is sent to `/login` with the intended path (including search and
 * hash) preserved in navigation state, so a filtered or anchored link survives the login.
 */
export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const location = useLocation()
  const { isAuthenticated, initializing } = useAuth()

  if (initializing) {
    return (
      <Flex justify="center" py="6">
        <Spinner size="3" />
      </Flex>
    )
  }

  if (!isAuthenticated) {
    // Search and hash are kept so that a filtered or anchored link survives a login,
    // not just the path.
    const from = `${location.pathname}${location.search}${location.hash}`
    return <Navigate to="/login" replace state={{ from }} />
  }

  return children ? <>{children}</> : <Outlet />
}
