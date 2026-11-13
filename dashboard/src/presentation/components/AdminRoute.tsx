import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
import { isAdmin } from '@/domain/user/access'
import { useAuth } from '@/presentation/contexts/AuthContext'

/**
 * Route guard for screens whose every API requires the `admin` User Role (identity-and-access.md §4).
 *
 * The side navigation already hides these destinations from other users, but bookmarks and typed
 * URLs still reach them; the guard sends those to the 403 page before the screen mounts, so a user
 * sees "Access denied" instead of a screen of failed requests. It is presentation-only: the API
 * refuses the requests regardless. It sits inside `ProtectedRoute`, so the user is already known.
 */
export function AdminRoute({ children }: { children: ReactNode }) {
  const { currentUser } = useAuth()
  if (!isAdmin(currentUser)) return <Navigate to="/403" replace />
  return children
}
