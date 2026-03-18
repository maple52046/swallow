import { MockMissionRepository } from '@/infrastructure/mock/repos/MockMissionRepository'
import { MockRunRepository } from '@/infrastructure/mock/repos/MockRunRepository'
import { MockObservabilityRepository } from '@/infrastructure/mock/repos/MockObservabilityRepository'
import { MockAssetRepository } from '@/infrastructure/mock/repos/MockAssetRepository'
import { MockProvisioningRepository } from '@/infrastructure/mock/repos/MockProvisioningRepository'
import { MockAccessRepository } from '@/infrastructure/mock/repos/MockAccessRepository'
import { MockPlaneRepository } from '@/infrastructure/mock/repos/MockPlaneRepository'
import { MockPlatformRepository } from '@/infrastructure/mock/repos/MockPlatformRepository'
import { MockServerRepository } from '@/infrastructure/mock/repos/MockServerRepository'
import { MockTeamRepository } from '@/infrastructure/mock/repos/MockTeamRepository'

import { ListMissionsUseCase } from '@/application/usecases/missions/ListMissionsUseCase'
import { GetMissionUseCase } from '@/application/usecases/missions/GetMissionUseCase'
import { CreateMissionUseCase } from '@/application/usecases/missions/CreateMissionUseCase'
import { UpdateMissionPlanUseCase } from '@/application/usecases/missions/UpdateMissionPlanUseCase'
import { PauseMissionUseCase } from '@/application/usecases/missions/PauseMissionUseCase'
import { ResumeMissionUseCase } from '@/application/usecases/missions/ResumeMissionUseCase'
import { ArchiveMissionUseCase } from '@/application/usecases/missions/ArchiveMissionUseCase'
import { CloneMissionUseCase } from '@/application/usecases/missions/CloneMissionUseCase'
import { RunMissionNowUseCase } from '@/application/usecases/missions/RunMissionNowUseCase'

import { ListRunsUseCase } from '@/application/usecases/runs/ListRunsUseCase'
import { GetRunUseCase } from '@/application/usecases/runs/GetRunUseCase'
import { CancelRunUseCase } from '@/application/usecases/runs/CancelRunUseCase'
import { RerunUseCase } from '@/application/usecases/runs/RerunUseCase'

import { ListGPUDevicesUseCase } from '@/application/usecases/observability/ListGPUDevicesUseCase'
import { GetGPUMetricsUseCase } from '@/application/usecases/observability/GetGPUMetricsUseCase'
import { ListAlertsUseCase } from '@/application/usecases/observability/ListAlertsUseCase'
import { AckAlertUseCase } from '@/application/usecases/observability/AckAlertUseCase'
import { ResolveAlertUseCase } from '@/application/usecases/observability/ResolveAlertUseCase'
import { CreateMissionFromAlertUseCase } from '@/application/usecases/observability/CreateMissionFromAlertUseCase'
import { ListGPUProfilesUseCase } from '@/application/usecases/observability/ListGPUProfilesUseCase'
import { GetTopCriticalGPUsUseCase } from '@/application/usecases/observability/GetTopCriticalGPUsUseCase'

import { ListHostsUseCase } from '@/application/usecases/datacenter/ListHostsUseCase'
import { GetHostUseCase } from '@/application/usecases/datacenter/GetHostUseCase'
import { ListStorageUseCase } from '@/application/usecases/datacenter/ListStorageUseCase'
import { ListSwitchesUseCase } from '@/application/usecases/datacenter/ListSwitchesUseCase'
import { ListProvisioningImagesUseCase } from '@/application/usecases/datacenter/ListProvisioningImagesUseCase'
import { ListProvisioningProfilesUseCase } from '@/application/usecases/datacenter/ListProvisioningProfilesUseCase'
import { ListProvisioningJobsUseCase } from '@/application/usecases/datacenter/ListProvisioningJobsUseCase'
import { ListConnectionsUseCase } from '@/application/usecases/datacenter/ListConnectionsUseCase'
import { UpsertConnectionUseCase } from '@/application/usecases/datacenter/UpsertConnectionUseCase'
import { ListSSHKeysUseCase } from '@/application/usecases/datacenter/ListSSHKeysUseCase'

import { ListPlanesUseCase } from '@/application/usecases/planes/ListPlanesUseCase'
import { GetPlaneUseCase } from '@/application/usecases/planes/GetPlaneUseCase'
import { RegisterPlaneUseCase } from '@/application/usecases/planes/RegisterPlaneUseCase'

import { ListAgentsUseCase } from '@/application/usecases/platform/ListAgentsUseCase'
import { EnableDisableAgentUseCase } from '@/application/usecases/platform/EnableDisableAgentUseCase'
import { ListModelsUseCase } from '@/application/usecases/platform/ListModelsUseCase'
import { SetDefaultModelUseCase } from '@/application/usecases/platform/SetDefaultModelUseCase'
import { AddModelUseCase } from '@/application/usecases/platform/AddModelUseCase'
import { DeleteModelUseCase } from '@/application/usecases/platform/DeleteModelUseCase'
import { ListPluginsUseCase } from '@/application/usecases/platform/ListPluginsUseCase'
import { EnableDisablePluginUseCase } from '@/application/usecases/platform/EnableDisablePluginUseCase'
import { ListAuditEventsUseCase } from '@/application/usecases/platform/ListAuditEventsUseCase'

import { GetOverviewUseCase } from '@/application/usecases/overview/GetOverviewUseCase'

import type { Server, ServerStatus } from '@/domain/server/types'
import type { Team, CreateTeamInput, UpdateTeamInput } from '@/domain/team/types'
import type { ListServersFilters } from '@/application/ports/ServerRepository'

export interface AppContainer {
  missions: {
    list: ListMissionsUseCase
    get: GetMissionUseCase
    create: CreateMissionUseCase
    updatePlan: UpdateMissionPlanUseCase
    pause: PauseMissionUseCase
    resume: ResumeMissionUseCase
    archive: ArchiveMissionUseCase
    clone: CloneMissionUseCase
    runNow: RunMissionNowUseCase
  }
  runs: {
    list: ListRunsUseCase
    get: GetRunUseCase
    cancel: CancelRunUseCase
    rerun: RerunUseCase
  }
  observability: {
    listGPUDevices: ListGPUDevicesUseCase
    getGPUMetrics: GetGPUMetricsUseCase
    listAlerts: ListAlertsUseCase
    ackAlert: AckAlertUseCase
    resolveAlert: ResolveAlertUseCase
    createMissionFromAlert: CreateMissionFromAlertUseCase
    listGPUProfiles: ListGPUProfilesUseCase
    getTopCriticalGPUs: GetTopCriticalGPUsUseCase
  }
  datacenter: {
    listHosts: ListHostsUseCase
    getHost: GetHostUseCase
    listStorage: ListStorageUseCase
    listSwitches: ListSwitchesUseCase
    listProvisioningImages: ListProvisioningImagesUseCase
    listProvisioningProfiles: ListProvisioningProfilesUseCase
    listProvisioningJobs: ListProvisioningJobsUseCase
    listConnections: ListConnectionsUseCase
    upsertConnection: UpsertConnectionUseCase
    listSSHKeys: ListSSHKeysUseCase
  }
  planes: {
    list: ListPlanesUseCase
    get: GetPlaneUseCase
    register: RegisterPlaneUseCase
  }
  platform: {
    listAgents: ListAgentsUseCase
    enableDisableAgent: EnableDisableAgentUseCase
    listModels: ListModelsUseCase
    setDefaultModel: SetDefaultModelUseCase
    addModel: AddModelUseCase
    deleteModel: DeleteModelUseCase
    listPlugins: ListPluginsUseCase
    enableDisablePlugin: EnableDisablePluginUseCase
    listAuditEvents: ListAuditEventsUseCase
  }
  overview: { get: GetOverviewUseCase }
  servers: {
    list: { execute: (filters?: ListServersFilters) => Promise<Server[]> }
    get: { execute: (id: string) => Promise<Server | null> }
    assignToTeam: { execute: (id: string, teamId: string) => Promise<Server> }
    assignToUser: { execute: (id: string, userId: string) => Promise<Server> }
    unassign: { execute: (id: string) => Promise<Server> }
    updateStatus: { execute: (id: string, status: ServerStatus) => Promise<Server> }
  }
  teams: {
    list: { execute: () => Promise<Team[]> }
    get: { execute: (id: string) => Promise<Team | null> }
    create: { execute: (input: CreateTeamInput) => Promise<Team> }
    update: { execute: (id: string, input: UpdateTeamInput) => Promise<Team> }
    deleteTeam: { execute: (id: string) => Promise<void> }
    addMember: { execute: (teamId: string, userId: string) => Promise<Team> }
    removeMember: { execute: (teamId: string, userId: string) => Promise<Team> }
    addOwner: { execute: (teamId: string, userId: string) => Promise<Team> }
    removeOwner: { execute: (teamId: string, userId: string) => Promise<Team> }
  }
}

export function createContainer(): AppContainer {
  const missionRepo = new MockMissionRepository()
  const runRepo = new MockRunRepository()
  const obsRepo = new MockObservabilityRepository()
  const assetRepo = new MockAssetRepository()
  const provisioningRepo = new MockProvisioningRepository()
  const accessRepo = new MockAccessRepository()
  const planeRepo = new MockPlaneRepository()
  const platformRepo = new MockPlatformRepository()
  const serverRepo = new MockServerRepository()
  const teamRepo = new MockTeamRepository()

  return {
    missions: {
      list: new ListMissionsUseCase(missionRepo),
      get: new GetMissionUseCase(missionRepo),
      create: new CreateMissionUseCase(missionRepo, platformRepo),
      updatePlan: new UpdateMissionPlanUseCase(missionRepo, platformRepo),
      pause: new PauseMissionUseCase(missionRepo, platformRepo),
      resume: new ResumeMissionUseCase(missionRepo, platformRepo),
      archive: new ArchiveMissionUseCase(missionRepo, platformRepo),
      clone: new CloneMissionUseCase(missionRepo, platformRepo),
      runNow: new RunMissionNowUseCase(missionRepo, runRepo, platformRepo),
    },
    runs: {
      list: new ListRunsUseCase(runRepo),
      get: new GetRunUseCase(runRepo),
      cancel: new CancelRunUseCase(runRepo, platformRepo),
      rerun: new RerunUseCase(missionRepo, runRepo, platformRepo),
    },
    observability: {
      listGPUDevices: new ListGPUDevicesUseCase(obsRepo),
      getGPUMetrics: new GetGPUMetricsUseCase(obsRepo),
      listAlerts: new ListAlertsUseCase(obsRepo),
      ackAlert: new AckAlertUseCase(obsRepo, platformRepo),
      resolveAlert: new ResolveAlertUseCase(obsRepo, platformRepo),
      createMissionFromAlert: new CreateMissionFromAlertUseCase(obsRepo, missionRepo, platformRepo),
      listGPUProfiles: new ListGPUProfilesUseCase(obsRepo),
      getTopCriticalGPUs: new GetTopCriticalGPUsUseCase(obsRepo),
    },
    datacenter: {
      listHosts: new ListHostsUseCase(assetRepo),
      getHost: new GetHostUseCase(assetRepo),
      listStorage: new ListStorageUseCase(assetRepo),
      listSwitches: new ListSwitchesUseCase(assetRepo),
      listProvisioningImages: new ListProvisioningImagesUseCase(provisioningRepo),
      listProvisioningProfiles: new ListProvisioningProfilesUseCase(provisioningRepo),
      listProvisioningJobs: new ListProvisioningJobsUseCase(provisioningRepo),
      listConnections: new ListConnectionsUseCase(accessRepo),
      upsertConnection: new UpsertConnectionUseCase(accessRepo),
      listSSHKeys: new ListSSHKeysUseCase(accessRepo),
    },
    planes: {
      list: new ListPlanesUseCase(planeRepo),
      get: new GetPlaneUseCase(planeRepo),
      register: new RegisterPlaneUseCase(planeRepo, platformRepo),
    },
    platform: {
      listAgents: new ListAgentsUseCase(platformRepo),
      enableDisableAgent: new EnableDisableAgentUseCase(platformRepo),
      listModels: new ListModelsUseCase(platformRepo),
      setDefaultModel: new SetDefaultModelUseCase(platformRepo),
      addModel: new AddModelUseCase(platformRepo),
      deleteModel: new DeleteModelUseCase(platformRepo),
      listPlugins: new ListPluginsUseCase(platformRepo),
      enableDisablePlugin: new EnableDisablePluginUseCase(platformRepo),
      listAuditEvents: new ListAuditEventsUseCase(platformRepo),
    },
    overview: {
      get: new GetOverviewUseCase(missionRepo, runRepo, obsRepo, planeRepo),
    },
    servers: {
      list: { execute: (filters?) => serverRepo.listServers(filters) },
      get: { execute: (id) => serverRepo.getServer(id) },
      assignToTeam: { execute: (id, teamId) => serverRepo.assignToTeam(id, teamId) },
      assignToUser: { execute: (id, userId) => serverRepo.assignToUser(id, userId) },
      unassign: { execute: (id) => serverRepo.unassign(id) },
      updateStatus: { execute: (id, status) => serverRepo.updateStatus(id, status) },
    },
    teams: {
      list: { execute: () => teamRepo.listTeams() },
      get: { execute: (id) => teamRepo.getTeam(id) },
      create: { execute: (input) => teamRepo.createTeam(input) },
      update: { execute: (id, input) => teamRepo.updateTeam(id, input) },
      deleteTeam: { execute: (id) => teamRepo.deleteTeam(id) },
      addMember: { execute: (teamId, userId) => teamRepo.addMember(teamId, userId) },
      removeMember: { execute: (teamId, userId) => teamRepo.removeMember(teamId, userId) },
      addOwner: { execute: (teamId, userId) => teamRepo.addOwner(teamId, userId) },
      removeOwner: { execute: (teamId, userId) => teamRepo.removeOwner(teamId, userId) },
    },
  }
}
