import { createBrowserRouter, Navigate } from 'react-router-dom'
import { AppLayout } from './presentation/app/layout/AppLayout'
import { NotFoundPage } from './presentation/pages/NotFoundPage'
import { LoginPage } from './presentation/pages/auth/LoginPage'
import { ForbiddenPage } from './presentation/pages/errors/ForbiddenPage'
import { OverviewPage } from './presentation/pages/overview/OverviewPage'
import { ServersPage } from './presentation/pages/servers/ServersPage'
import { ServerDetailPage } from './presentation/pages/servers/ServerDetailPage'
import { ServerSummaryTab } from './presentation/pages/servers/ServerSummaryTab'
import {
  ServerNetworkTab,
  ServerStorageTab,
  ServerPciTab,
} from './presentation/pages/servers/ServerDetailTableTab'
import { ClustersPage } from './presentation/pages/clusters/ClustersPage'
import { ClusterDetailPage } from './presentation/pages/clusters/ClusterDetailPage'
import { DeployClusterPage } from './presentation/pages/clusters/DeployClusterPage'
import { OperationsPage } from './presentation/pages/operations/OperationsPage'
import { OperationDetailPage } from './presentation/pages/operations/OperationDetailPage'
import { ProtectedRoute } from './presentation/components/ProtectedRoute'

/**
 * Every route here is backed by a real endpoint.
 *
 * Alerts and metrics still have no screen: they are absent rather than mocked, because a
 * screen that appears to work is worse than one that is missing. Clusters and operations
 * now have screens, backed by the clusters and operations contracts.
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
      {
        path: 'servers/:id',
        element: <ServerDetailPage />,
        children: [
          { index: true, element: <Navigate to="summary" replace /> },
          { path: 'summary', element: <ServerSummaryTab /> },
          { path: 'network', element: <ServerNetworkTab /> },
          { path: 'storage', element: <ServerStorageTab /> },
          { path: 'pci', element: <ServerPciTab /> },
        ],
      },
      { path: 'clusters', element: <ClustersPage /> },
      { path: 'clusters/deploy', element: <DeployClusterPage /> },
      { path: 'clusters/:id', element: <ClusterDetailPage /> },
      { path: 'operations', element: <OperationsPage /> },
      { path: 'operations/:id', element: <OperationDetailPage /> },
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
