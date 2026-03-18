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
import { DashboardsPage } from './presentation/pages/observability/DashboardsPage'
import { InventoryPage } from './presentation/pages/datacenter/InventoryPage'
import { ProvisioningPage } from './presentation/pages/datacenter/ProvisioningPage'
import { AccessPage } from './presentation/pages/datacenter/AccessPage'
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
      { path: 'missions', element: <MissionsPage /> },
      { path: 'missions/new', element: <MissionCreatePage /> },
      { path: 'missions/:id', element: <MissionDetailPage /> },
      { path: 'runs', element: <RunsPage /> },
      { path: 'runs/:id', element: <RunDetailPage /> },
      { path: 'observability/gpu-metrics', element: <GPUMetricsPage /> },
      { path: 'observability/gpu-profiling', element: <GPUProfilingPage /> },
      { path: 'observability/alerts', element: <AlertsPage /> },
      { path: 'observability/dashboards', element: <DashboardsPage /> },
      { path: 'datacenter/inventory', element: <InventoryPage /> },
      { path: 'datacenter/provisioning', element: <ProvisioningPage /> },
      { path: 'datacenter/access', element: <AccessPage /> },
      { path: 'datacenter/ipmi', element: <IPMIPage /> },
      { path: 'datacenter/networking', element: <NetworkingPage /> },
      { path: 'datacenter/storage', element: <StoragePage /> },
      { path: 'planes', element: <ManagementPlanesPage /> },
      { path: 'planes/new', element: <AddPlanePage /> },
      { path: 'planes/kubernetes', element: <KubernetesPage /> },
      { path: 'planes/slurm', element: <SlurmPage /> },
      { path: 'platform/agents', element: <AgentsPage /> },
      { path: 'platform/models', element: <ModelsPage /> },
      { path: 'platform/plugins', element: <PluginsPage /> },
      { path: 'platform/audit', element: <AuditLogPage /> },
      {
        path: 'users',
        element: (
          <RoleGuard allowedRoles={['admin']}>
            <UsersPage />
          </RoleGuard>
        ),
      },
      { path: 'servers', element: <ServersPage /> },
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
      { path: 'settings', element: <SettingsPage /> },
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
