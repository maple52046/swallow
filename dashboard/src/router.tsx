import { createBrowserRouter, Navigate } from 'react-router-dom'
import { OperatorLayout } from './presentation/app/layout/OperatorLayout'
import { NotFoundPage } from './presentation/pages/NotFoundPage'
import { LoginPage } from './presentation/pages/auth/LoginPage'
import { ForbiddenPage } from './presentation/pages/errors/ForbiddenPage'
import { OperatorOverviewPage } from './presentation/pages/overview/OperatorOverviewPage'
import { MonitoringPage } from './presentation/pages/monitoring/MonitoringPage'
import { ServersPage } from './presentation/pages/servers/ServersPage'
import { ServerDetailPage } from './presentation/pages/servers/ServerDetailPage'
import { ServerSummaryTab } from './presentation/pages/servers/ServerSummaryTab'
import { ServerMonitoringTab } from './presentation/pages/servers/ServerMonitoringTab'
import { ServerActivityTab } from './presentation/pages/servers/ServerActivityTab'
import { ServerNetworkTab, ServerStorageTab, ServerPciTab } from './presentation/pages/servers/ServerDetailTableTab'
import { ClustersPage } from './presentation/pages/clusters/ClustersPage'
import { ClusterDetailPage } from './presentation/pages/clusters/ClusterDetailPage'
import { DeployClusterWizardPage } from './presentation/pages/clusters/DeployClusterWizardPage'
import { OperatorOperationsPage } from './presentation/pages/operations/OperatorOperationsPage'
import { OperatorOperationDetailPage } from './presentation/pages/operations/OperatorOperationDetailPage'
import { ProtectedRoute } from './presentation/components/ProtectedRoute'
import { DeployOSWizardPage } from './presentation/pages/provisioning/DeployOSWizardPage'
import { DeploymentTemplatesPage } from './presentation/pages/provisioning/DeploymentTemplatesPage'
import { OSImagesPage } from './presentation/pages/provisioning/OSImagesPage'
import { ProvisioningRedirect } from './presentation/pages/provisioning/ProvisioningRedirect'
import { InfrastructureRedirect } from './presentation/pages/infrastructure/InfrastructureRedirect'
import { SitesPage } from './presentation/pages/infrastructure/SitesPage'
import { IntegrationsPage } from './presentation/pages/infrastructure/IntegrationsPage'

/** Stable Dashboard routes, each backed by active `/api/v1` provider contracts. */
export const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  { path: '/403', element: <ProtectedRoute><ForbiddenPage /></ProtectedRoute> },
  {
    path: '/',
    element: <ProtectedRoute><OperatorLayout /></ProtectedRoute>,
    children: [
      { index: true, element: <OperatorOverviewPage /> },
      { path: 'monitoring', element: <MonitoringPage /> },
      { path: 'servers', element: <ServersPage /> },
      {
        path: 'servers/:id',
        element: <ServerDetailPage />,
        children: [
          { index: true, element: <Navigate to="summary" replace /> },
          { path: 'summary', element: <ServerSummaryTab /> },
          { path: 'activity', element: <ServerActivityTab /> },
          { path: 'monitoring', element: <ServerMonitoringTab /> },
          { path: 'network', element: <ServerNetworkTab /> },
          { path: 'storage', element: <ServerStorageTab /> },
          { path: 'pci', element: <ServerPciTab /> },
        ],
      },
      { path: 'clusters', element: <ClustersPage /> },
      { path: 'clusters/deploy', element: <DeployClusterWizardPage /> },
      { path: 'clusters/:id', element: <ClusterDetailPage /> },
      { path: 'operations', element: <OperatorOperationsPage /> },
      { path: 'operations/:id', element: <OperatorOperationDetailPage /> },
      { path: 'provisioning', element: <ProvisioningRedirect /> },
      { path: 'provisioning/deploy', element: <DeployOSWizardPage /> },
      { path: 'provisioning/templates', element: <DeploymentTemplatesPage /> },
      { path: 'provisioning/images', element: <OSImagesPage /> },
      { path: 'infrastructure', element: <InfrastructureRedirect /> },
      { path: 'infrastructure/sites', element: <SitesPage /> },
      { path: 'infrastructure/integrations', element: <IntegrationsPage /> },
    ],
  },
  { path: '*', element: <ProtectedRoute><NotFoundPage /></ProtectedRoute> },
])
