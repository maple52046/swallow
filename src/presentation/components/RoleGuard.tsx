import { Navigate, Outlet } from 'react-router-dom'
import { useAuth, type UserRole } from '@/presentation/contexts/AuthContext'

interface RoleGuardProps {
  allowedRoles: UserRole[]
  children?: React.ReactNode
}

export function RoleGuard({ allowedRoles, children }: RoleGuardProps) {
  const { currentUser } = useAuth()

  if (!currentUser) {
    return <Navigate to="/login" replace />
  }

  if (!allowedRoles.includes(currentUser.role)) {
    return <Navigate to="/403" replace />
  }

  return children ? <>{children}</> : <Outlet />
}
