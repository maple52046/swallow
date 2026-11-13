import { ChangePlansPage } from '@/presentation/pages/changePlans/ChangePlansPage'
import { createBrowserRouter, Navigate } from 'react-router-dom'
import { ConsoleLayout } from './presentation/app/layout/ConsoleLayout'
import { NotFoundPage } from './presentation/pages/NotFoundPage'
import { LoginPage } from './presentation/pages/auth/LoginPage'
import { ForbiddenPage } from './presentation/pages/errors/ForbiddenPage'
import { UnexpectedErrorPage } from './presentation/pages/errors/UnexpectedErrorPage'
import { HomeRoute } from './presentation/pages/overview/HomeRoute'
import { MonitoringPage } from './presentation/pages/monitoring/MonitoringPage'
import { ServersPage } from './presentation/pages/servers/ServersPage'
import { ServerDetailPage } from './presentation/pages/servers/ServerDetailPage'
import { ServerSummaryTab } from './presentation/pages/servers/ServerSummaryTab'
import { ServerMonitoringTab } from './presentation/pages/servers/ServerMonitoringTab'
import { ServerActivityTab } from './presentation/pages/servers/ServerActivityTab'
import { ServerContainersTab } from './presentation/pages/servers/ServerContainersTab'
import { ServerNetworkTab, ServerStorageTab, ServerPciTab } from './presentation/pages/servers/ServerDetailTableTab'
import { PlatformsPage } from './presentation/pages/platforms/PlatformsPage'
import { PlatformWorkspaceIndexRedirect, PlatformWorkspacePage } from './presentation/pages/platforms/PlatformWorkspacePage'
import {
  PlatformActivityRoute,
  PlatformConfigurationRoute,
  PlatformModuleRoute,
  PlatformMonitoringRoute,
  PlatformOverviewRoute,
  PlatformWorkspaceNotFound,
} from './presentation/pages/platforms/PlatformWorkspaceRoutes'
import {
  DeployKubernetesPlatformPage,
  DeploySlurmPlatformPage,
} from './presentation/pages/platforms/DeployPlatformWizardPage'
import { DeployPlatformCatalogPage } from './presentation/pages/platforms/DeployPlatformCatalogPage'
import { PlatformSettingsPage } from './presentation/pages/platforms/PlatformSettingsPage'
import { LegacyPlatformRedirect } from './presentation/pages/platforms/LegacyPlatformRedirect'
import { SoftwarePage } from './presentation/pages/software/SoftwarePage'
import { SoftwareDetailPage } from './presentation/pages/software/SoftwareDetailPage'
import { SoftwareSettingsPage } from './presentation/pages/software/SoftwareSettingsPage'
import { ConsoleOperationsPage } from './presentation/pages/operations/ConsoleOperationsPage'
import { ConsoleOperationDetailPage } from './presentation/pages/operations/ConsoleOperationDetailPage'
import { LegacyWorkflowRedirect } from './presentation/pages/operations/LegacyWorkflowRedirect'
import { ProtectedRoute } from './presentation/components/ProtectedRoute'
import { DeploymentTemplatesPage } from './presentation/pages/provisioning/DeploymentTemplatesPage'
import { OSImagesPage } from './presentation/pages/provisioning/OSImagesPage'
import { BootISOsPage } from './presentation/pages/provisioning/BootISOsPage'
import {
  LegacyBootISOsRedirect,
  LegacyDeployOSRedirect,
  ProvisioningRedirect,
} from './presentation/pages/provisioning/ProvisioningRedirect'
import { InfrastructureRedirect } from './presentation/pages/infrastructure/InfrastructureRedirect'
import { SitesPage } from './presentation/pages/infrastructure/SitesPage'
import { IntegrationsPage } from './presentation/pages/infrastructure/IntegrationsPage'
import {
  IntegrationSettingsIndexRedirect,
  IntegrationSettingsPage,
} from './presentation/pages/infrastructure/IntegrationSettingsPage'
import {
  IntegrationConnectionTab,
  IntegrationCredentialTab,
  IntegrationGeneralTab,
  IntegrationHttpProxyTab,
  IntegrationSubnetDnsTab,
} from './presentation/pages/infrastructure/IntegrationSettingsTabs'
import { ZonesPage } from './presentation/pages/infrastructure/ZonesPage'
import { LocalRepositoryPage } from './presentation/pages/infrastructure/LocalRepositoryPage'
import { SSHKeysPage } from './presentation/pages/account/SSHKeysPage'
import { ApiKeysPage } from './presentation/pages/account/ApiKeysPage'
import { ChangePasswordPage } from './presentation/pages/account/ChangePasswordPage'
import { AdminRoute } from './presentation/components/AdminRoute'
import { UsersPage } from './presentation/pages/users/UsersPage'
import { PoolsPage } from './presentation/pages/pools/PoolsPage'
import { PoolAccessPage } from './presentation/pages/pools/PoolAccessPage'
import { FeatureRoute } from './presentation/components/FeatureRoute'
import { ValidationRedirect } from './presentation/pages/validation/ValidationRedirect'
import { ValidationRunsPage } from './presentation/pages/validation/ValidationRunsPage'
import { ValidationRunDetailPage } from './presentation/pages/validation/ValidationRunDetailPage'
import { ValidationSuitesPage } from './presentation/pages/validation/ValidationSuitesPage'
import { ValidationSuiteDetailPage } from './presentation/pages/validation/ValidationSuiteDetailPage'
import { ValidationSchedulesPage } from './presentation/pages/validation/ValidationSchedulesPage'

/**
 * Stable Dashboard routes, each backed by active `/api/v1` provider contracts.
 *
 * Routes of in-development features sit behind `FeatureRoute`: while a feature is hidden
 * (always, in release builds) a typed or bookmarked URL shows Not Found or redirects to
 * the nearest available page instead of mounting the unfinished screen.
 */
export const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  { path: '/403', element: <ProtectedRoute><ForbiddenPage /></ProtectedRoute> },
  {
    path: '/',
    element: <ProtectedRoute><ConsoleLayout /></ProtectedRoute>,
    errorElement: <UnexpectedErrorPage />,
    children: [
      // The Overview for admins; everyone else lands on the Servers they may read.
      { index: true, element: <HomeRoute /> },
      { path: 'monitoring', element: <AdminRoute><MonitoringPage /></AdminRoute> },
      { path: 'servers', element: <ServersPage /> },
      {
        path: 'servers/:id',
        element: <ServerDetailPage />,
        children: [
          { index: true, element: <Navigate to="summary" replace /> },
          { path: 'summary', element: <ServerSummaryTab /> },
          { path: 'activity', element: <ServerActivityTab /> },
          { path: 'monitoring', element: <ServerMonitoringTab /> },
          { path: 'containers', element: <ServerContainersTab /> },
          { path: 'network', element: <ServerNetworkTab /> },
          { path: 'storage', element: <ServerStorageTab /> },
          { path: 'pci', element: <ServerPciTab /> },
        ],
      },
      { path: 'platforms', element: <PlatformsPage /> },
      { path: 'platforms/settings', element: <AdminRoute><PlatformSettingsPage /></AdminRoute> },
      { path: 'platforms/deploy', element: <DeployPlatformCatalogPage /> },
      { path: 'platforms/deploy/kubernetes', element: <DeployKubernetesPlatformPage /> },
      { path: 'platforms/deploy/slurm', element: <DeploySlurmPlatformPage /> },
      {
        path: 'platforms/:id',
        element: <PlatformWorkspacePage />,
        children: [
          { index: true, element: <PlatformWorkspaceIndexRedirect /> },
          { path: 'overview', element: <PlatformOverviewRoute /> },
          { path: 'monitoring', element: <PlatformMonitoringRoute /> },
          { path: 'configuration', element: <PlatformConfigurationRoute /> },
          { path: 'activity', element: <PlatformActivityRoute /> },
          { path: 'applications', element: <PlatformModuleRoute route="applications" /> },
          { path: 'namespaces', element: <PlatformModuleRoute route="namespaces" /> },
          { path: 'cluster', element: <PlatformModuleRoute route="cluster" /> },
          { path: 'nodes', element: <PlatformModuleRoute route="nodes" /> },
          { path: 'partitions', element: <PlatformModuleRoute route="partitions" /> },
          { path: '*', element: <PlatformWorkspaceNotFound /> },
        ],
      },
      { path: 'software', element: <SoftwarePage /> },
      { path: 'software/settings', element: <AdminRoute><SoftwareSettingsPage /></AdminRoute> },
      { path: 'software/:kind/settings', element: <AdminRoute><SoftwareSettingsPage /></AdminRoute> },
      { path: 'software/:kind', element: <SoftwareDetailPage /> },
      { path: 'clusters', element: <LegacyPlatformRedirect /> },
      { path: 'clusters/deploy', element: <LegacyPlatformRedirect deploy /> },
      { path: 'clusters/:id', element: <LegacyPlatformRedirect /> },
      { path: 'workflows', element: <ConsoleOperationsPage /> },
      { path: 'change-plans', element: <ChangePlansPage /> },
      { path: 'validation', element: <ValidationRedirect /> },
      { path: 'validation/runs', element: <ValidationRunsPage /> },
      { path: 'validation/runs/:id', element: <ValidationRunDetailPage /> },
      { path: 'validation/suites', element: <ValidationSuitesPage /> },
      { path: 'validation/suites/:id', element: <ValidationSuiteDetailPage /> },
      { path: 'validation/schedules', element: <AdminRoute><ValidationSchedulesPage /></AdminRoute> },
      { path: 'workflows/:id', element: <ConsoleOperationDetailPage /> },
      { path: 'operations', element: <LegacyWorkflowRedirect /> },
      { path: 'operations/:id', element: <LegacyWorkflowRedirect /> },
      { path: 'provisioning', element: <ProvisioningRedirect /> },
      { path: 'provisioning/deploy', element: <LegacyDeployOSRedirect /> },
      {
        path: 'provisioning/templates',
        element: (
          <AdminRoute>
            <FeatureRoute feature="deploymentTemplates" redirectTo="/provisioning/images">
              <DeploymentTemplatesPage />
            </FeatureRoute>
          </AdminRoute>
        ),
      },
      { path: 'provisioning/images', element: <OSImagesPage /> },
      { path: 'provisioning/boot-isos', element: <LegacyBootISOsRedirect /> },
      { path: 'infrastructure', element: <AdminRoute><InfrastructureRedirect /></AdminRoute> },
      { path: 'infrastructure/sites', element: <AdminRoute><SitesPage /></AdminRoute> },
      { path: 'infrastructure/integrations', element: <AdminRoute><IntegrationsPage /></AdminRoute> },
      // One Integration's settings, a tab per route (General, Connection, Credential, and for a
      // MAAS provisioner its HTTP proxy and Subnet DNS).
      {
        path: 'infrastructure/integrations/:id',
        element: <AdminRoute><IntegrationSettingsPage /></AdminRoute>,
        children: [
          { index: true, element: <IntegrationSettingsIndexRedirect /> },
          { path: 'general', element: <IntegrationGeneralTab /> },
          { path: 'connection', element: <IntegrationConnectionTab /> },
          { path: 'credential', element: <IntegrationCredentialTab /> },
          { path: 'http-proxy', element: <IntegrationHttpProxyTab /> },
          { path: 'subnet-dns', element: <IntegrationSubnetDnsTab /> },
        ],
      },
      { path: 'infrastructure/zones', element: <AdminRoute><ZonesPage /></AdminRoute> },
      // Pools belong to the installation, not to a Site's infrastructure (decision 082); the old
      // address opens the Pools page, where they are managed now.
      { path: 'infrastructure/pools', element: <Navigate to="/pools" replace /> },
      // One Site's Local Repository (decision 078): Site-scoped through the URL like the pages above.
      { path: 'infrastructure/local-repository', element: <AdminRoute><LocalRepositoryPage /></AdminRoute> },
      { path: 'infrastructure/boot-isos', element: <AdminRoute><BootISOsPage /></AdminRoute> },
      // Pools (decision 082): every user's own Pools, an admin's every Pool (which admins also create,
      // rename, and delete here), and one Pool's members and Sudo Policy, which its members read and
      // its owners and admins change.
      { path: 'pools', element: <PoolsPage /> },
      { path: 'pools/:id', element: <PoolAccessPage /> },
      // Account administration (decision 082), admins only.
      { path: 'users', element: <AdminRoute><UsersPage /></AdminRoute> },
      // Account settings for the signed-in User; not Site-scoped (SSH keys are per User).
      { path: 'account/ssh-keys', element: <SSHKeysPage /> },
      // API Keys for non-interactive clients belong to the signed-in User (decision 042).
      { path: 'account/api-keys', element: <ApiKeysPage /> },
      { path: 'account/password', element: <ChangePasswordPage /> },
    ],
  },
  { path: '*', element: <ProtectedRoute><NotFoundPage /></ProtectedRoute> },
])
