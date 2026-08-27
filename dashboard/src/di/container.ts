import { ApiAuthRepository } from '@/infrastructure/api/ApiAuthRepository'
import { ApiServerRepository } from '@/infrastructure/api/ApiServerRepository'
import { ApiSiteRepository } from '@/infrastructure/api/ApiSiteRepository'
import { ApiClusterRepository } from '@/infrastructure/api/ApiClusterRepository'
import { ApiOperationRepository } from '@/infrastructure/api/ApiOperationRepository'
import { ApiMonitoringRepository } from '@/infrastructure/api/ApiMonitoringRepository'
import { ApiOverviewRepository } from '@/infrastructure/api/ApiOverviewRepository'
import type { AuthRepository } from '@/application/ports/AuthRepository'
import type { ServerRepository } from '@/application/ports/ServerRepository'
import type { SiteRepository } from '@/application/ports/SiteRepository'
import type { ClusterRepository } from '@/application/ports/ClusterRepository'
import type { OperationRepository } from '@/application/ports/OperationRepository'
import type { MonitoringRepository } from '@/application/ports/MonitoringRepository'
import type { OverviewRepository } from '@/application/ports/OverviewRepository'

/** Browser composition contract exposing provider ports to presentation workflows. */
export interface AppContainer {
  auth: AuthRepository
  overview: OverviewRepository
  servers: ServerRepository
  sites: SiteRepository
  clusters: ClusterRepository
  operations: OperationRepository
  monitoring: MonitoringRepository
}

/** Builds the production HTTP adapters once at the React composition root. */
export function createContainer(): AppContainer {
  return {
    auth: new ApiAuthRepository(),
    overview: new ApiOverviewRepository(),
    servers: new ApiServerRepository(),
    sites: new ApiSiteRepository(),
    clusters: new ApiClusterRepository(),
    operations: new ApiOperationRepository(),
    monitoring: new ApiMonitoringRepository(),
  }
}
