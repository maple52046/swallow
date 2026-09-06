import { ApiAuthRepository } from '@/infrastructure/api/ApiAuthRepository'
import { ApiServerRepository } from '@/infrastructure/api/ApiServerRepository'
import { ApiServerEventStream } from '@/infrastructure/api/ApiServerEventStream'
import { ApiSiteRepository } from '@/infrastructure/api/ApiSiteRepository'
import { ApiPlatformRepository } from '@/infrastructure/api/ApiPlatformRepository'
import { ApiOperationRepository } from '@/infrastructure/api/ApiOperationRepository'
import { ApiMonitoringRepository } from '@/infrastructure/api/ApiMonitoringRepository'
import { ApiOverviewRepository } from '@/infrastructure/api/ApiOverviewRepository'
import { ApiProvisioningRepository } from '@/infrastructure/api/ApiProvisioningRepository'
import type { AuthRepository } from '@/application/ports/AuthRepository'
import type { ServerRepository } from '@/application/ports/ServerRepository'
import type { ServerEventStream } from '@/application/ports/ServerEventStream'
import type { SiteRepository } from '@/application/ports/SiteRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'
import type { OperationRepository } from '@/application/ports/OperationRepository'
import type { MonitoringRepository } from '@/application/ports/MonitoringRepository'
import type { OverviewRepository } from '@/application/ports/OverviewRepository'
import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'

/** Browser composition contract exposing provider ports to presentation workflows. */
export interface AppContainer {
  auth: AuthRepository
  overview: OverviewRepository
  provisioning: ProvisioningRepository
  servers: ServerRepository
  /** Live Server projection changes, so the list patches rows instead of re-reading. */
  serverEvents: ServerEventStream
  sites: SiteRepository
  platforms: PlatformRepository
  operations: OperationRepository
  monitoring: MonitoringRepository
}

/** Builds the production HTTP adapters once at the React composition root. */
export function createContainer(): AppContainer {
  return {
    auth: new ApiAuthRepository(),
    overview: new ApiOverviewRepository(),
    provisioning: new ApiProvisioningRepository(),
    servers: new ApiServerRepository(),
    serverEvents: new ApiServerEventStream(),
    sites: new ApiSiteRepository(),
    platforms: new ApiPlatformRepository(),
    operations: new ApiOperationRepository(),
    monitoring: new ApiMonitoringRepository(),
  }
}
