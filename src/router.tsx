import { createBrowserRouter } from 'react-router-dom'
import { AppLayout } from './presentation/app/layout/AppLayout'
import { NotFoundPage } from './presentation/pages/NotFoundPage'
import { LoginPage } from './presentation/pages/auth/LoginPage'
import { ForbiddenPage } from './presentation/pages/errors/ForbiddenPage'
import { OverviewPage } from './presentation/pages/overview/OverviewPage'
import { MissionsPage } from './presentation/pages/missions/MissionsPage'
import { MissionDetailPage } from './presentation/pages/missions/MissionDetailPage'
import { MissionCreatePage } from './presentation/pages/missions/MissionCreatePage'
import { RunsPage } from './presentation/pages/runs/RunsPage'
import { RunDetailPage } from './presentation/pages/runs/RunDetailPage'
import { GPUMetricsPage } from './presentation/pages/observability/GPUMetricsPage'
import { GPUProfilingPage } from './presentation/pages/observability/GPUProfilingPage'
import { AlertsPage } from './presentation/pages/observability/AlertsPage'
import { AnalysisPage } from './presentation/pages/analysis/AnalysisPage'
import { DashboardsPage } from './presentation/pages/observability/DashboardsPage'
import { InventoryPage } from './presentation/pages/datacenter/InventoryPage'
import { ProvisioningPage } from './presentation/pages/datacenter/ProvisioningPage'
import { IPMIPage } from './presentation/pages/datacenter/IPMIPage'
import { NetworkingPage } from './presentation/pages/datacenter/NetworkingPage'
import { StoragePage } from './presentation/pages/datacenter/StoragePage'
import { ManagementPlanesPage } from './presentation/pages/planes/ManagementPlanesPage'
import { AddPlanePage } from './presentation/pages/planes/AddPlanePage'
import { KubernetesPage } from './presentation/pages/planes/KubernetesPage'
import { SlurmPage } from './presentation/pages/planes/SlurmPage'
import { AgentsPage } from './presentation/pages/platform/AgentsPage'
import { ModelsPage } from './presentation/pages/platform/ModelsPage'
import { PluginsPage } from './presentation/pages/platform/PluginsPage'
import { AuditLogPage } from './presentation/pages/platform/AuditLogPage'
import { SettingsPage } from './presentation/pages/platform/SettingsPage'
import { UsersPage } from './presentation/pages/users/UsersPage'
import { ServersPage } from './presentation/pages/servers/ServersPage'
import { ServerDetailPage } from './presentation/pages/servers/ServerDetailPage'
import { AddServerPage } from './presentation/pages/servers/AddServerPage'
import { CreateContainerPage } from './presentation/pages/servers/containers/CreateContainerPage'
import { DatacenterTopologyPage } from './presentation/pages/datacenter/DatacenterTopologyPage'
import { TeamsPage } from './presentation/pages/teams/TeamsPage'
import { TeamDetailPage } from './presentation/pages/teams/TeamDetailPage'
import { ProtectedRoute } from './presentation/components/ProtectedRoute'
import { RoleGuard } from './presentation/components/RoleGuard'

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
      ...(import.meta.env.DEV ? [
        { path: 'deprecated/missions', element: <MissionsPage /> },
        { path: 'deprecated/missions/new', element: <MissionCreatePage /> },
        { path: 'deprecated/missions/:id', element: <MissionDetailPage /> },
        { path: 'deprecated/runs', element: <RunsPage /> },
        { path: 'deprecated/runs/:id', element: <RunDetailPage /> },
        { path: 'deprecated/observability/gpu-metrics', element: <GPUMetricsPage /> },
        { path: 'deprecated/observability/gpu-profiling', element: <GPUProfilingPage /> },
        { path: 'deprecated/observability/dashboards', element: <DashboardsPage /> },
        { path: 'deprecated/datacenter/inventory', element: <InventoryPage /> },
        { path: 'deprecated/datacenter/ipmi', element: <IPMIPage /> },
        { path: 'deprecated/datacenter/networking', element: <NetworkingPage /> },
        { path: 'deprecated/datacenter/storage', element: <StoragePage /> },
        { path: 'deprecated/platform/agents', element: <AgentsPage /> },
        { path: 'deprecated/platform/models', element: <ModelsPage /> },
        { path: 'deprecated/platform/plugins', element: <PluginsPage /> },
        { path: 'deprecated/platform/audit', element: <AuditLogPage /> },
      ] : []),
      { path: 'planes', element: <ManagementPlanesPage /> },
      { path: 'planes/new', element: <AddPlanePage /> },
      { path: 'planes/kubernetes', element: <KubernetesPage /> },
      { path: 'planes/slurm', element: <SlurmPage /> },
      {
        path: 'users',
        element: (
          <RoleGuard allowedRoles={['admin']}>
            <UsersPage />
          </RoleGuard>
        ),
      },
      { path: 'servers', element: <ServersPage /> },
      { path: 'servers/new', element: <AddServerPage /> },
      { path: 'servers/:id/containers/new', element: <CreateContainerPage /> },
      { path: 'servers/:id', element: <ServerDetailPage /> },
      { path: 'datacenter', element: <DatacenterTopologyPage /> },
      { path: 'provisioning', element: <ProvisioningPage /> },
      { path: 'alerts', element: <AlertsPage /> },
      { path: 'analysis', element: <AnalysisPage /> },
      {
        path: 'teams',
        element: (
          <RoleGuard allowedRoles={['admin']}>
            <TeamsPage />
          </RoleGuard>
        ),
      },
      {
        path: 'teams/:id',
        element: (
          <RoleGuard allowedRoles={['admin']}>
            <TeamDetailPage />
          </RoleGuard>
        ),
      },
      { path: 'account/settings', element: <SettingsPage /> },
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
