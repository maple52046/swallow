import { Navigate, useLocation } from 'react-router-dom'
import { isAdmin } from '@/domain/user/access'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { ConsoleOverviewPage } from './ConsoleOverviewPage'

/**
 * The console's index route. The Overview summarizes the whole installation and its API requires
 * the `admin` User Role, so an admin lands on it while every other user lands on the Servers it may
 * read (the list the API already narrows to the user's Pools). The query string (Site scope) is kept.
 */
export function HomeRoute() {
  const { currentUser } = useAuth()
  const location = useLocation()
  if (isAdmin(currentUser)) return <ConsoleOverviewPage />
  return <Navigate to={{ pathname: '/servers', search: location.search }} replace />
}
