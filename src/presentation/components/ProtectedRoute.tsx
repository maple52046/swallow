import { Center, Loader } from '@mantine/core'
import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '@/presentation/contexts/AuthContext'

interface ProtectedRouteProps {
  children?: React.ReactNode
}

export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const location = useLocation()
  const { isAuthenticated, initializing } = useAuth()

  if (initializing) {
    return (
      <Center py="xl">
        <Loader size="sm" />
      </Center>
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
