import { createBrowserRouter, Navigate } from 'react-router-dom'
import { OperatorLayout } from './presentation/app/layout/OperatorLayout'
import { NotFoundPage } from './presentation/pages/NotFoundPage'
import { LoginPage } from './presentation/pages/auth/LoginPage'
import { ForbiddenPage } from './presentation/pages/errors/ForbiddenPage'
import { UnexpectedErrorPage } from './presentation/pages/errors/UnexpectedErrorPage'
import { OperatorOverviewPage } from './presentation/pages/overview/OperatorOverviewPage'
import { MonitoringPage } from './presentation/pages/monitoring/MonitoringPage'
import { ServersPage } from './presentation/pages/servers/ServersPage'
import { ServerDetailPage } from './presentation/pages/servers/ServerDetailPage'
import { ServerSummaryTab } from './presentation/pages/servers/ServerSummaryTab'
import { ServerMonitoringTab } from './presentation/pages/servers/ServerMonitoringTab'
import { ServerActivityTab } from './presentation/pages/servers/ServerActivityTab'
import { ServerNetworkTab, ServerStorageTab, ServerPciTab } from './presentation/pages/servers/ServerDetailTableTab'
import { PlatformsPage } from './presentation/pages/platforms/PlatformsPage'
import { PlatformDetailPage } from './presentation/pages/platforms/PlatformDetailPage'
import { DeployPlatformWizardPage } from './presentation/pages/platforms/DeployPlatformWizardPage'
import { PlatformSettingsPage } from './presentation/pages/platforms/PlatformSettingsPage'
import { LegacyPlatformRedirect } from './presentation/pages/platforms/LegacyPlatformRedirect'
import { SoftwarePage } from './presentation/pages/software/SoftwarePage'
import { OperatorOperationsPage } from './presentation/pages/operations/OperatorOperationsPage'
import { OperatorOperationDetailPage } from './presentation/pages/operations/OperatorOperationDetailPage'
import { LegacyWorkflowRedirect } from './presentation/pages/operations/LegacyWorkflowRedirect'
import { ProtectedRoute } from './presentation/components/ProtectedRoute'
import { DeployOSWizardPage } from './presentation/pages/provisioning/DeployOSWizardPage'
import { DeploymentTemplatesPage } from './presentation/pages/provisioning/DeploymentTemplatesPage'
import { OSImagesPage } from './presentation/pages/provisioning/OSImagesPage'
import { ProvisioningRedirect } from './presentation/pages/provisioning/ProvisioningRedirect'
import { InfrastructureRedirect } from './presentation/pages/infrastructure/InfrastructureRedirect'
import { SitesPage } from './presentation/pages/infrastructure/SitesPage'
import { IntegrationsPage } from './presentation/pages/infrastructure/IntegrationsPage'
import { GroupingPage } from './presentation/pages/infrastructure/GroupingPage'
import { SSHKeysPage } from './presentation/pages/account/SSHKeysPage'

/** Stable Dashboard routes, each backed by active `/api/v1` provider contracts. */
export const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  { path: '/403', element: <ProtectedRoute><ForbiddenPage /></ProtectedRoute> },
  {
    path: '/',
    element: <ProtectedRoute><OperatorLayout /></ProtectedRoute>,
    errorElement: <UnexpectedErrorPage />,
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
      { path: 'platforms', element: <PlatformsPage /> },
      { path: 'platforms/settings', element: <PlatformSettingsPage /> },
      { path: 'platforms/deploy', element: <DeployPlatformWizardPage /> },
      { path: 'platforms/:id', element: <PlatformDetailPage /> },
      { path: 'software', element: <SoftwarePage /> },
      { path: 'clusters', element: <LegacyPlatformRedirect /> },
      { path: 'clusters/deploy', element: <LegacyPlatformRedirect deploy /> },
      { path: 'clusters/:id', element: <LegacyPlatformRedirect /> },
      { path: 'workflows', element: <OperatorOperationsPage /> },
      { path: 'workflows/:id', element: <OperatorOperationDetailPage /> },
      { path: 'operations', element: <LegacyWorkflowRedirect /> },
      { path: 'operations/:id', element: <LegacyWorkflowRedirect /> },
      { path: 'provisioning', element: <ProvisioningRedirect /> },
      { path: 'provisioning/deploy', element: <DeployOSWizardPage /> },
      { path: 'provisioning/templates', element: <DeploymentTemplatesPage /> },
      { path: 'provisioning/images', element: <OSImagesPage /> },
      { path: 'infrastructure', element: <InfrastructureRedirect /> },
      { path: 'infrastructure/sites', element: <SitesPage /> },
      { path: 'infrastructure/integrations', element: <IntegrationsPage /> },
      { path: 'infrastructure/zones', element: <GroupingPage kind="zone" /> },
      { path: 'infrastructure/pools', element: <GroupingPage kind="pool" /> },
      // Account settings for the signed-in admin; not Site-scoped (SSH keys are per User).
      { path: 'account/ssh-keys', element: <SSHKeysPage /> },
    ],
  },
  { path: '*', element: <ProtectedRoute><NotFoundPage /></ProtectedRoute> },
])
