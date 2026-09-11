import { Center, Spinner } from '@chakra-ui/react'
import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '@/presentation/contexts/AuthContext'

interface ProtectedRouteProps {
  children?: React.ReactNode
}

/**
 * Authenticated route guard.
 *
 * Session restoration finishes before any redirect decision (a full-screen spinner
 * covers the async restore), and the complete intended URL — path, query, and hash
 * — is preserved in navigation state so filtered deep links survive the login
 * round-trip.
 */
export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const location = useLocation()
  const { isAuthenticated, initializing } = useAuth()
  if (initializing) {
    return (
      <Center minH="100dvh">
        <Spinner size="xl" color="brand.solid" aria-label="Restoring session" />
      </Center>
    )
  }
  if (!isAuthenticated) {
    const from = `${location.pathname}${location.search}${location.hash}`
    return <Navigate to="/login" replace state={{ from }} />
  }
  return children ? <>{children}</> : <Outlet />
}
