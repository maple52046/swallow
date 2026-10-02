import { ApiAuthRepository } from '@/infrastructure/api/ApiAuthRepository'
import { ApiServerRepository } from '@/infrastructure/api/ApiServerRepository'
import { ApiServerEventStream } from '@/infrastructure/api/ApiServerEventStream'
import { ApiSiteRepository } from '@/infrastructure/api/ApiSiteRepository'
import { ApiPlatformRepository } from '@/infrastructure/api/ApiPlatformRepository'
import { ApiOperationRepository } from '@/infrastructure/api/ApiOperationRepository'
import { ApiMonitoringRepository } from '@/infrastructure/api/ApiMonitoringRepository'
import { ApiOverviewRepository } from '@/infrastructure/api/ApiOverviewRepository'
import { ApiProvisioningRepository } from '@/infrastructure/api/ApiProvisioningRepository'
import { ApiInfrastructureRepository } from '@/infrastructure/api/ApiInfrastructureRepository'
import { ApiSoftwareRepository } from '@/infrastructure/api/ApiSoftwareRepository'
import { ApiDockerHostRepository } from '@/infrastructure/api/ApiDockerHostRepository'
import { ApiRegistryCredentialRepository } from '@/infrastructure/api/ApiRegistryCredentialRepository'
import { ApiSSHKeyRepository } from '@/infrastructure/api/ApiSSHKeyRepository'
import { ApiApiKeyRepository } from '@/infrastructure/api/ApiApiKeyRepository'
import {
  DisabledExperimentalFeatureSettings,
  LocalExperimentalFeatureSettings,
} from '@/infrastructure/persistence/ExperimentalFeatureSettings'
import type { AuthRepository } from '@/application/ports/AuthRepository'
import type { ServerRepository } from '@/application/ports/ServerRepository'
import type { ServerEventStream } from '@/application/ports/ServerEventStream'
import type { SiteRepository } from '@/application/ports/SiteRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'
import type { OperationRepository } from '@/application/ports/OperationRepository'
import type { MonitoringRepository } from '@/application/ports/MonitoringRepository'
import type { OverviewRepository } from '@/application/ports/OverviewRepository'
import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'
import type { InfrastructureRepository } from '@/application/ports/InfrastructureRepository'
import type { SoftwareRepository } from '@/application/ports/SoftwareRepository'
import type { DockerHostRepository } from '@/application/ports/DockerHostRepository'
import type { RegistryCredentialRepository } from '@/application/ports/RegistryCredentialRepository'
import type { SSHKeyRepository } from '@/application/ports/SSHKeyRepository'
import type { ApiKeyRepository } from '@/application/ports/ApiKeyRepository'
import type { ExperimentalFeatureSettings } from '@/application/ports/ExperimentalFeatureSettings'

/** Browser composition contract exposing provider ports to presentation workflows. */
export interface AppContainer {
  auth: AuthRepository
  overview: OverviewRepository
  provisioning: ProvisioningRepository
  servers: ServerRepository
  /** Live Server projection changes, so the list patches rows instead of re-reading. */
  serverEvents: ServerEventStream
  sites: SiteRepository
  /** swallow-owned Zone/Pool management and Server placement (decision 029). */
  infrastructure: InfrastructureRepository
  platforms: PlatformRepository
  /** Managed Software install/uninstall and Software Assignment reads (decision 038). */
  software: SoftwareRepository
  /**
   * Live Docker Engine management for Servers where swallow installed Docker CE with `enableApi`
   * (decision 043); api-server mediates every call, the browser never reaches a host.
   */
  dockerHosts: DockerHostRepository
  /** Installation-wide, sealed registry credentials for private image pulls (decision 044). */
  registryCredentials: RegistryCredentialRepository
  /** The Deployment Key and the signed-in admin's Access Keys (decision 039). */
  sshKeys: SSHKeyRepository
  /** The signed-in user's API Keys for non-interactive clients (decision 042). */
  apiKeys: ApiKeyRepository
  operations: OperationRepository
  monitoring: MonitoringRepository
  /** Which in-development dashboard features are shown; adjustable only in development builds. */
  experimentalFeatures: ExperimentalFeatureSettings
}

/**
 * Builds the production HTTP adapters once at the React composition root.
 *
 * This is the only place that reads the build mode: the Vite dev server (and the
 * Playwright suite running on it) gets adjustable, browser-persisted experimental
 * feature settings, while `vite build` output binds the fixed all-off settings so
 * release builds hide unfinished features regardless of what a browser stored.
 */
export function createContainer(): AppContainer {
  return {
    auth: new ApiAuthRepository(),
    overview: new ApiOverviewRepository(),
    provisioning: new ApiProvisioningRepository(),
    servers: new ApiServerRepository(),
    serverEvents: new ApiServerEventStream(),
    sites: new ApiSiteRepository(),
    infrastructure: new ApiInfrastructureRepository(),
    platforms: new ApiPlatformRepository(),
    software: new ApiSoftwareRepository(),
    dockerHosts: new ApiDockerHostRepository(),
    registryCredentials: new ApiRegistryCredentialRepository(),
    sshKeys: new ApiSSHKeyRepository(),
    apiKeys: new ApiApiKeyRepository(),
    operations: new ApiOperationRepository(),
    monitoring: new ApiMonitoringRepository(),
    experimentalFeatures: import.meta.env.DEV
      ? new LocalExperimentalFeatureSettings()
      : new DisabledExperimentalFeatureSettings(),
  }
}
