import { createBrowserRouter } from 'react-router-dom'
import { AppLayout } from './presentation/app/layout/AppLayout'
import { NotFoundPage } from './presentation/pages/NotFoundPage'
import { LoginPage } from './presentation/pages/auth/LoginPage'
import { ForbiddenPage } from './presentation/pages/errors/ForbiddenPage'
import { OverviewPage } from './presentation/pages/overview/OverviewPage'
import { ServersPage } from './presentation/pages/servers/ServersPage'
import { ServerDetailPage } from './presentation/pages/servers/ServerDetailPage'
import { ProtectedRoute } from './presentation/components/ProtectedRoute'

/**
 * Every route here is backed by a real endpoint.
 *
 * The API also has clusters, operations, alerts, and metrics, which have no screen
 * yet. They are absent rather than mocked, because a screen that appears to work is
 * worse than one that is missing.
 */
export const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  {
    path: '/403',
    element: (
      <ProtectedRoute>
        <ForbiddenPage />
      </ProtectedRoute>
    ),
  },
  {
    path: '/',
    element: (
      <ProtectedRoute>
        <AppLayout />
      </ProtectedRoute>
    ),
    children: [
      { index: true, element: <OverviewPage /> },
      { path: 'servers', element: <ServersPage /> },
      { path: 'servers/:id', element: <ServerDetailPage /> },
    ],
  },
  {
    path: '*',
    element: (
      <ProtectedRoute>
        <NotFoundPage />
      </ProtectedRoute>
    ),
  },
])
