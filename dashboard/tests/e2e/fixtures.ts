import type { Page, Route } from 'playwright/test'
import type { Server } from '@/domain/server/types'

const now = '2026-08-27T03:00:00Z'

/** Builds fleet fixtures; Server four deliberately models an unobserved inventory. */
function makeServer(index: number): Server {
  const ordinal = index + 1
  const named = ordinal <= 4
  const hasInventory = ordinal !== 4
  return {
    id: `srv-${ordinal}`,
    source: { siteId: 'site-a', integrationId: 'maas-a', providerMachineId: `machine-${ordinal}` },
    hostname: named ? `gpu-node-0${ordinal}` : `compute-node-${String(ordinal).padStart(3, '0')}`,
    fqdn: named ? `gpu-node-0${ordinal}.lab.example` : `compute-node-${String(ordinal).padStart(3, '0')}.lab.example`,
    addresses: hasInventory ? [named ? `192.168.40.${20 + ordinal}` : `10.20.${Math.floor(index / 250)}.${(index % 250) + 1}`] : [],
    architecture: hasInventory ? 'amd64/generic' : '', cpuCores: hasInventory ? 64 : 0, cpuModel: hasInventory ? 'AMD EPYC 9554' : '', memoryMiB: hasInventory ? 524288 : 0, storageGB: hasInventory ? 3840 : 0,
    gpus: named && hasInventory
      ? [
          { vendor: 'AMD', model: 'MI300X', count: 8, kind: 'compute' },
          { vendor: 'ASPEED Technology, Inc.', model: 'ASPEED Graphics Family', count: 1, kind: 'display' },
        ]
      : [],
    systemVendor: hasInventory ? 'Supermicro' : '', systemProduct: hasInventory ? 'AS-8125GS-TNHR' : '', providerZone: hasInventory ? index < 2 ? 'rack-a' : 'rack-b' : '',
    providerResourcePool: hasInventory ? named ? 'accelerators' : 'compute' : '', providerPod: '', tags: hasInventory ? named ? ['gpu', 'production'] : ['compute'] : [],
    hardware: { systemUuid: `uuid-${ordinal}`, serialNumber: `SN${String(ordinal).padStart(4, '0')}`, macAddresses: hasInventory ? [`02:00:00:00:${String(Math.floor(index / 250)).padStart(2, '0')}:${String((index % 250) + 1).padStart(2, '0')}`] : [] },
    deployment: ordinal === 4
      ? { state: 'failed', operationId: 'op-deploy-failed', stepId: 'provision-srv-4', attempt: 1, code: 'deployment_address_unavailable', stage: 'ssh_readiness', statusReason: 'No provider address was observed after OS installation.', startedAt: now, finishedAt: now, updatedAt: now }
      : { state: 'succeeded', operationId: `op-os-${ordinal}`, stepId: `provision-srv-${ordinal}`, attempt: 1, code: '', stage: '', statusReason: '', startedAt: now, finishedAt: now, updatedAt: now },
    provisioning: { state: 'deployed', providerState: 'deployed', powerState: 'on', osSystem: 'ubuntu', distroSeries: '24.04', deployedImageName: 'Ubuntu 24.04 LTS', deployedImageDefaultUser: 'ubuntu', ephemeral: false, hweKernel: 'ga-24.04', locked: false, commissioningStatus: 'passed', testingStatus: 'passed', integrationId: 'maas-a', observedAt: now },
    membership: ordinal <= 3 ? { platformId: 'platform-a', nodeName: `gpu-node-0${ordinal}`, role: 'control-plane', state: 'ready', observedAt: now } : null,
    health: ordinal === 4 ? { state: 'down', observedAt: now } : { state: 'up', observedAt: now },
    defaultUser: { user: 'ubuntu', source: 'os_image' },
    absent: false, lastSeenAt: now, createdAt: '2026-08-01T00:00:00Z', updatedAt: now,
  }
}

const servers = Array.from({ length: 4 }, (_, index) => makeServer(index))
const operations = [
  { id: 'op-running', kind: 'deploy-kubernetes', intent: 'Deploy production k0s platform', siteId: 'site-a', platformId: 'platform-a', targetServerIds: servers.slice(0, 3).map((server) => server.id), retryOfOperationId: null, execution: { runId: 'run-1024', playbook: 'deploy-k0s.yml', status: 'running', statusReason: null, startedAt: '2026-08-27T02:54:00Z', finishedAt: null }, requestedBy: 'admin', requestedAt: '2026-08-27T02:53:00Z', updatedAt: now },
  { id: 'op-failed', kind: 'exporter.install', intent: 'Install GPU exporters', siteId: 'site-a', platformId: 'platform-a', targetServerIds: ['srv-4'], retryOfOperationId: null, execution: { runId: 'run-1023', playbook: 'install-exporters.yml', status: 'failed', statusReason: 'Host unreachable', startedAt: '2026-08-27T01:10:00Z', finishedAt: '2026-08-27T01:12:00Z' }, requestedBy: 'admin', requestedAt: '2026-08-27T01:09:00Z', updatedAt: '2026-08-27T01:12:00Z' },
  {
    id: 'op-deploy-failed', schemaVersion: 3, kind: 'deploy-kubernetes',
    intent: 'Deploy edge-staging k0s platform', intentSnapshot: {},
    definition: 'platform-deployment', definitionVersion: 1,
    status: 'requires_attention', statusReason: 'A failed Step requires operator attention.',
    startState: 'started', temporal: { workflowId: 'swallow-operation/op-deploy-failed', runId: 'run-deploy-failed' },
    siteId: 'site-a', platformId: 'platform-b',
    targetResources: [{ kind: 'platform', id: 'platform-b' }, { kind: 'server', id: 'srv-4' }],
    targetServerIds: ['srv-4'], retryOfOperationId: null,
    steps: [
      {
        id: 'provision-srv-4', kind: 'provision-os', name: 'Provision and verify operating system on srv-4',
        executor: 'maas', dependsOn: null, targets: [{ kind: 'server', id: 'srv-4' }],
        status: 'failed', attempt: 1, progress: 0,
        error: {
          code: 'deployment_address_unavailable',
          message: 'MAAS installed the requested OS on gpu-node-04, but no provider address was observed. Swallow did not verify this deployment. Retry this Step to release and redeploy the Server with the same frozen network settings.',
          retryable: true,
          stage: 'ssh_readiness',
        },
        externalExecution: null, artifacts: null,
        startedAt: '2026-08-27T00:30:00Z', finishedAt: '2026-08-27T00:34:00Z',
      },
      {
        id: 'install-platform', kind: 'ansible-playbook', name: 'Install k0s Platform',
        executor: 'ansible', dependsOn: ['provision-srv-4'], targets: [{ kind: 'server', id: 'srv-4' }],
        status: 'pending', attempt: 1, progress: 0, error: null,
        externalExecution: null, artifacts: [], startedAt: null, finishedAt: null,
      },
    ],
    leases: [], requestCorrelation: 'req-op-deploy-failed',
    execution: {
      runId: 'run-deploy-failed', playbook: '', status: 'requires_attention',
      statusReason: 'A failed Step requires operator attention.',
      startedAt: '2026-08-27T00:30:00Z', finishedAt: null,
    },
    requestedBy: 'admin', requestedAt: '2026-08-27T00:29:00Z',
    startedAt: '2026-08-27T00:30:00Z', finishedAt: null, updatedAt: '2026-08-27T00:35:00Z',
  },
  {
    id: 'op-succeeded', schemaVersion: 3, kind: 'inventory.reconcile',
    intent: 'Reconcile accelerator inventory', intentSnapshot: {},
    definition: 'inventory-reconciliation', definitionVersion: 1,
    status: 'succeeded', statusReason: null,
    startState: 'started', temporal: { workflowId: 'swallow-operation/op-succeeded', runId: 'run-succeeded' },
    siteId: 'site-a', platformId: null,
    targetResources: [{ kind: 'server', id: 'srv-2' }],
    targetServerIds: ['srv-2'], retryOfOperationId: null,
    steps: [
      {
        id: 'reconcile-inventory', kind: 'internal', name: 'Reconcile accelerator inventory',
        executor: 'internal', dependsOn: [], targets: [{ kind: 'server', id: 'srv-2' }],
        status: 'succeeded', attempt: 1, progress: 100, error: null,
        externalExecution: null, artifacts: [],
        startedAt: '2026-08-26T23:30:00Z', finishedAt: '2026-08-26T23:31:00Z',
      },
    ],
    leases: [], requestCorrelation: 'req-op-succeeded',
    execution: {
      runId: 'run-succeeded', playbook: '', status: 'succeeded', statusReason: null,
      startedAt: '2026-08-26T23:30:00Z', finishedAt: '2026-08-26T23:31:00Z',
    },
    requestedBy: 'system', requestedAt: '2026-08-26T23:29:00Z',
    startedAt: '2026-08-26T23:30:00Z', finishedAt: '2026-08-26T23:31:00Z',
    updatedAt: '2026-08-26T23:31:00Z',
  },
]
const platforms = [
  {
    id: 'platform-a', siteId: 'site-a', name: 'production-k0s', type: 'kubernetes',
    integrationId: 'k8s-a', origin: 'deployed', lifecycleState: 'active',
    lifecycleOperationId: 'op-running',
    deployment: {
      topology: 'high-availability',
      roleAssignments: [
        { serverId: 'srv-1', role: 'control-plane', runWorkloads: true },
        { serverId: 'srv-2', role: 'control-plane', runWorkloads: false },
        { serverId: 'srv-3', role: 'control-plane', runWorkloads: false },
      ],
    },
    gpuStackOwner: 'gpu-operator', exporterOwner: 'k8s',
    sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null, memberCount: 5, matchedCount: 4 },
    createdAt: '2026-08-10T00:00:00Z', updatedAt: now,
  },
  {
    id: 'platform-b', siteId: 'site-a', name: 'edge-staging', type: 'kubernetes',
    integrationId: null, origin: 'deployed', lifecycleState: 'deploy_failed',
    lifecycleOperationId: 'op-deploy-failed',
    deployment: {
      topology: 'standalone',
      roleAssignments: [
        { serverId: 'srv-4', role: 'control-plane', runWorkloads: true },
      ],
    },
    gpuStackOwner: 'provisioning', exporterOwner: 'ansible',
    sync: { lastStartedAt: null, lastSucceededAt: null, lastError: null, memberCount: 0, matchedCount: 0 },
    createdAt: '2026-08-20T00:00:00Z', updatedAt: now,
  },
  {
    id: 'platform-slurm', siteId: 'site-a', name: 'research-slurm', type: 'slurm',
    integrationId: 'slurm-a', origin: 'registered', lifecycleState: 'registered',
    lifecycleOperationId: null, deployment: null,
    gpuStackOwner: 'provisioning', exporterOwner: 'ansible',
    sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null, memberCount: 0, matchedCount: 0 },
    createdAt: '2026-08-22T00:00:00Z', updatedAt: now,
  },
]
const sites = [
  { id: 'site-a', name: 'Taipei Lab', description: 'Primary accelerator lab', createdAt: now, updatedAt: now },
  { id: 'site-b', name: 'Hsinchu Edge', description: 'Edge validation', createdAt: now, updatedAt: now },
]
const integrations = [
  { id: 'maas-a', siteId: 'site-a', kind: 'provisioner', providerKind: 'maas', name: 'MAAS Taipei', endpoint: 'https://maas.example', enabled: true, settings: {}, hasCredential: true, sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null }, createdAt: now, updatedAt: now },
  { id: 'maas-b', siteId: 'site-a', kind: 'provisioner', providerKind: 'maas', name: 'MAAS Edge', endpoint: 'https://maas-edge.example', enabled: true, settings: {}, hasCredential: true, sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null }, createdAt: now, updatedAt: now },
  { id: 'prom-a', siteId: 'site-a', kind: 'metrics', providerKind: 'prometheus', name: 'Prometheus Taipei', endpoint: 'https://prom.example', enabled: true, settings: {}, hasCredential: true, sync: { lastStartedAt: now, lastSucceededAt: now, lastError: 'Alertmanager timeout' }, createdAt: now, updatedAt: now },
]
const osImages = [
  { id: 'ubuntu/jammy', name: 'Ubuntu 22.04 LTS', providerName: 'Ubuntu 22.04 LTS', osSystem: 'ubuntu', providerOsSystem: 'ubuntu', release: 'jammy', providerRelease: 'jammy', tags: [], defaultUser: 'ubuntu', architecture: 'amd64', sizeBytes: 4294967296, verifiedDeployTargets: [], failedDeployTargets: [] },
  { id: 'ubuntu/noble', name: 'Ubuntu 24.04 LTS', providerName: 'Ubuntu 24.04 LTS', osSystem: 'ubuntu', providerOsSystem: 'ubuntu', release: 'noble', providerRelease: 'noble', tags: [], defaultUser: 'ubuntu', architecture: 'amd64', sizeBytes: 5368709120, verifiedDeployTargets: [], failedDeployTargets: [] },
  { id: 'ubuntu-24.04-rocm', name: 'Ubuntu 24.04 ROCm', providerName: 'Ubuntu 24.04 ROCm', osSystem: 'custom', providerOsSystem: 'custom', release: 'ubuntu-24.04-rocm', providerRelease: 'ubuntu-24.04-rocm', tags: [], architecture: 'amd64', verifiedDeployTargets: ['ram'], failedDeployTargets: ['disk'] },
]
const baseDeploymentTemplates = [
  { id: 'template-a', siteId: 'site-a', integrationId: 'maas-a', name: 'GPU compute baseline', description: 'Ubuntu baseline for accelerator nodes', imageId: 'ubuntu/jammy', ephemeral: false, network: { mode: 'dhcp', subnetId: 'subnet-a', defaultGateway: false }, hasUserData: true, createdAt: now, updatedAt: now },
]

const alerts = [
  { fingerprint: 'alert-1', name: 'NodeDown', severity: 'critical', state: 'firing', summary: 'gpu-node-04 stopped reporting', description: 'No scrape data for five minutes', labels: { alertname: 'NodeDown', server_id: 'srv-4' }, startsAt: '2026-08-27T02:50:00Z', serverId: 'srv-4', siteId: 'site-a', platformId: 'platform-a' },
  { fingerprint: 'alert-2', name: 'GpuTemperatureHigh', severity: 'warning', state: 'firing', summary: 'GPU temperature exceeds threshold', description: '', labels: { alertname: 'GpuTemperatureHigh', server_id: 'srv-2' }, startsAt: '2026-08-27T02:45:00Z', serverId: 'srv-2', siteId: 'site-a', platformId: 'platform-a' },
  { fingerprint: 'alert-3', name: 'ExporterMissing', severity: 'warning', state: 'suppressed', summary: 'Exporter rollout pending', description: '', labels: { alertname: 'ExporterMissing', server_id: 'srv-3' }, startsAt: '2026-08-27T01:45:00Z', serverId: 'srv-3', siteId: 'site-a', platformId: 'platform-a' },
]

/** Controls for large-fleet, concurrency, and failure-path browser fixtures. */
export interface FixtureOptions {
  /**
   * Whether the browser starts with a Session (as if the HttpOnly refresh cookie were present), so
   * the app's startup refresh succeeds. Defaults to true; sign-in specs pass false. Login and logout
   * fixtures then flip it for the rest of the page, like the real cookie.
   */
  signedIn?: boolean
  /** Called for every POST /auth/refresh, in order. */
  onSessionRefresh?: () => void
  fleetSize?: number
  metricsDelayMs?: number
  failMetricsBatchIndex?: number
  acknowledgeFails?: boolean
  readyServerCount?: number
  changingServerIds?: string[]
  /**
   * Overrides the non-terminal deployment stage and reason of `changingServerIds`, e.g. the provider
   * stage the backend projects while a deploy is in progress ("Provider stage: Configuring OS ...").
   */
  deployingStage?: string
  deployingStatusReason?: string
  deploymentAttentionServerIds?: string[]
  absentServerIds?: string[]
  unobservedHealthServerIds?: string[]
  cpuOnlyServerIds?: string[]
  multiGpuServerIds?: string[]
  /** Replaces inventory with a management/display GPU and no compute accelerator. */
  displayOnlyServerIds?: string[]
  /** Replaces compute inventory with a provider-generic AMD model plus an ASPEED controller. */
  genericComputeServerIds?: string[]
  /** Replaces inventory with a Cirrus Logic GD 5446 display controller. */
  cirrusDisplayOnlyServerIds?: string[]
  /** Overrides installed image labels for truncation and responsive presentation scenarios. */
  deployedImageNames?: Record<string, string>
  extraTags?: Record<string, string[]>
  serverListFailuresAfterInitial?: number
  serverListFailureRequestNumbers?: number[]
  onServerListRequest?: (requestCount: number) => void
  staticNetworkServerIds?: string[]
  networkSubnetName?: string
  ephemeralServerIds?: string[]
  lockedServerIds?: string[]
  /** Servers whose OS swallow did not deploy: provisioning `deployed` with no `deployment` record. */
  existingServerIds?: string[]
  failedServerIds?: string[]
  brokenServerIds?: string[]
  rescueServerIds?: string[]
  /**
   * Provider-only running work: sets the OS Provisioning State and leaves the Swallow deployment
   * record alone, so no Swallow Workflow is running for the Server (unlike `changingServerIds`).
   */
  providerWorkServerIds?: Record<string, 'releasing' | 'inspecting' | 'testing' | 'deploying'>

  secondReadyServerIntegrationId?: string
  failImageIntegrationIds?: string[]
  deploymentFailureIds?: string[]
  serverActionFailureIds?: string[]
  deploymentReadinessIssues?: Record<string, string>
  onOSImageCatalogRequest?: (integrationId: string) => void
  /** Called with the target integration when the OS image upload endpoint receives a POST. */
  onOSImageUploadRequest?: (integrationId: string) => void
  /** Called with the multipart `defaultUser` field (or '') of an OS image upload. */
  onOSImageUploadDefaultUser?: (defaultUser: string) => void
  /** Called with the body of an OS image overlay PATCH. */
  onOSImageOverlayRequest?: (body: Record<string, unknown>) => void
  /** Called for every SSH Keys request with its method, path, and JSON body (if any). */
  onSSHKeyRequest?: (method: string, path: string, body: Record<string, unknown> | null) => void
  onDeploymentRequest?: (body: Record<string, unknown>) => void
  onPlatformDeploymentRequest?: (body: Record<string, unknown>) => void
  onServerReleaseRequest?: (serverId: string, body: Record<string, unknown> | null) => void
  onServerRecoverRequest?: (serverId: string, body: Record<string, unknown> | null) => void
  onServerRefreshRequest?: (serverId: string) => void
  onPlatformUninstallRequest?: (platformId: string, body: Record<string, unknown> | null) => void
  releaseConvergesAfterRefreshes?: number
  deploymentConvergesAfterRefreshes?: number
  releaseCleanupFails?: boolean
  /**
   * Leaves a released Server in its "deployed" axis at accept time instead of flipping it to
   * "releasing" synchronously, modelling the real durable release that dispatches
   * asynchronously. Subsequent refreshes still converge it, so it exercises the list's
   * follow-after-release polling rather than the active-axis polling.
   */
  deferReleaseProjection?: boolean
  /**
   * Makes a cancel request drive the Operation to the terminal `canceled` status instead of
   * the default non-terminal `canceling`, so a poll that waits for a Server to clear its
   * active work converges. Off by default to preserve the `canceling`-visible assertion.
   */
  cancelMarksTerminal?: boolean
  providerFailureRetryable?: boolean
  /**
   * Makes the canonical Task-retry endpoint answer `409 conflict`, modelling a durable Workflow
   * whose Temporal execution was lost (a host restart or execution timeout) so a Step retry can
   * no longer be delivered. It drives the platform Repair action into its rerun recovery path.
   */
  deployExecutionLost?: boolean
  onNetworkLinkRequest?: (method: string, serverId: string, interfaceId: string, linkId: string | null, body: Record<string, unknown> | null) => void
  onMetricsRequest?: (serverIds: string[]) => void
  /** Removes Platform membership and deployment target claims for wizard success paths. */
  freePlatformCandidates?: boolean
  // Gives srv-1 exactly 4C/24GiB/80GB and srv-2 4C/16GiB/80GB.
  minimumResourceCandidates?: boolean
  /** Makes the Slurm requirement GET fail so the wizard fail-closed path can be verified. */
  slurmRequirementFails?: boolean
  onMetricsActive?: (active: number) => void
  /**
   * Adds a Swallow-deployed HA Slurm platform (`platform-slurm-ha`) with two controller-only
   * nodes and two compute nodes. The controllers report no membership (slurmrestd lists only
   * slurmd scheduler nodes), so this exercises the deployment-intent projection that surfaces
   * managers and the controller merge in the member list. Opt-in so other tests are unaffected.
   */
  slurmDeployed?: boolean
  /**
   * Docker CE Software Assignments by serverId (decision 043): `enabled` records `enableApi: true`
   * and serves the Docker Host Explorer routes; `disabled` models an installation without the API
   * (a legacy record with no spec). Servers not listed have no Docker CE, so their Containers tab
   * is not offered. Opt-in so other tests are unaffected.
   */
  dockerAssignments?: Record<string, 'enabled' | 'disabled'>
  /** Called with the body of every POST /software/assignments (a Docker CE re-apply from the tab). */
  onSoftwareInstallRequest?: (body: Record<string, unknown>) => void
  /** Called for every Docker Host Explorer write with its method, explorer sub-path, and JSON body. */
  onDockerRequest?: (method: string, path: string, body: Record<string, unknown> | null) => void
  /** Holds every image pull response until it resolves, to exercise a long-running pull. */
  dockerPullGate?: Promise<void>
  /** Registry Credentials present at page load (decision 044); passwords are never part of a fixture. */
  registryCredentials?: Array<{ registry: string; username: string }>
  /** Called for every Registry Credential write with its method, path, and JSON body (passwords included). */
  onRegistryCredentialRequest?: (method: string, path: string, body: Record<string, unknown> | null) => void
  /** Called for every Server Default User write (decision 045) with its method and JSON body. */
  onDefaultUserRequest?: (method: string, body: Record<string, unknown> | null) => void
  /** Sudo access the default-user PUT reports; `passwordless` when omitted. */
  defaultUserSudo?: 'passwordless' | 'password_required' | 'unavailable'
  /** Makes the default-user PUT fail with this API error instead of saving. */
  defaultUserError?: { status: number; code: string; message: string }
  /** Called for every Boot Media write (decision 047) with its method, path, and JSON body. */
  onBootMediaRequest?: (method: string, path: string, body: Record<string, unknown> | null) => void
  /** Makes the boot-media PUT fail with this API error instead of saving. */
  bootMediaError?: { status: number; code: string; message: string }
  /** Makes a Boot Media disable report that the BMC was not reset. */
  bootMediaRevertError?: string
  /** srv-1's Boot Media setting at page load (`null`, never set, when omitted). */
  bootMediaSetting?: Record<string, unknown>
  /**
   * Holds an enable preflight open until it resolves; meanwhile the Boot Media read reports it as
   * running (`apply`) in the settle phase, as the real API does for about three minutes.
   */
  bootMediaApplyGate?: Promise<void>
  /**
   * Boot ISOs at page load (boot-isos.md, decision 049). Omitted: `taipei-rack` for MAAS Taipei
   * (srv-1's provisioner) and `edge-rack` for MAAS Edge.
   */
  bootISOs?: Array<{ id: string; name: string; integrationId: string; rackAddress: string }>
  /** Makes the Boot ISO list report the builder unavailable with this reason. */
  bootISOBuilderUnavailable?: string
  /** Makes the Boot ISO build fail with this API error instead of storing an ISO. */
  bootISOBuildError?: { status: number; code: string; message: string }
  /** Called for every Boot ISO write with its method, path, and JSON body. */
  onBootISORequest?: (method: string, path: string, body: Record<string, unknown> | null) => void
  /** Removes every provisioner Integration, modelling a Site that has none yet. */
  noProvisioners?: boolean
  /** Servers whose inspect-hardware Workflow waits in `requires_attention` (decision 053). */
  inspectionAttentionServerIds?: string[]
  /** Called for every enroll-bundle read with its method, path, and the swallowUrl it sent. */
  onEnrollmentRequest?: (method: string, path: string, swallowUrl: string) => void
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

/** Builds one SSH Key fixture in the `ssh-keys.md` shape, synced to MAAS Taipei and failed on MAAS Edge. */
function sshKey(id: string, name: string, purpose: 'deployment' | 'access', publicKey: string) {
  return {
    id,
    name,
    purpose,
    keyType: 'ssh-ed25519',
    fingerprint: `SHA256:${id}`,
    publicKey,
    ...(purpose === 'access' ? { ownerUserId: 'admin-1' } : {}),
    createdAt: now,
    updatedAt: now,
    providerSync: [
      { integrationId: 'maas-a', siteId: 'site-a', state: 'synced', syncedAt: now },
      { integrationId: 'maas-b', siteId: 'site-a', state: 'failed', error: 'Could not reach MAAS.' },
    ],
  }
}

/** Installs deterministic network fixtures; no backend or provider is contacted. */
export async function installApiFixtures(page: Page, options: FixtureOptions = {}) {
  // Mirrors the refresh cookie: the startup refresh succeeds only while a Session exists.
  let signedIn = options.signedIn ?? true
  // Per-page SSH Key state so one test's changes never leak into another.
  // Per-page API Key state; secrets are fixture strings, never real keys.
  let apiKeyItems = [
    { id: 'api-1', name: 'laptop-cli', prefix: 'swk_Lap1op00', createdAt: '2026-08-01T00:00:00Z', expiresAt: null as string | null, lastUsedAt: '2026-08-27T02:00:00Z' as string | null },
    { id: 'api-2', name: 'old-ci', prefix: 'swk_0ldC1000', createdAt: '2026-05-01T00:00:00Z', expiresAt: '2026-08-01T00:00:00Z' as string | null, lastUsedAt: null as string | null },
  ]
  let sshKeyItems = [
    sshKey('deploy-1', 'swallow-deployment', 'deployment', 'ssh-ed25519 AAAADEPLOY swallow-deployment'),
    sshKey('key-laptop', 'work-laptop', 'access', 'ssh-ed25519 AAAALAPTOP alice@laptop'),
  ]
  // Per-page Boot Media state (decision 047): a supported AMI BMC, Boot Media not yet enabled.
  const bootMediaState: { setting: Record<string, unknown> | null; redfish: Record<string, unknown>; apply: Record<string, unknown> | null } = {
    setting: options.bootMediaSetting ?? null,
    apply: null,
    redfish: {
      support: 'supported', serviceRoot: 'https://192.0.2.20/redfish/v1', vendor: 'AMI', product: 'AMI Redfish Server',
      redfishVersion: '1.15.1', firmwareVersion: '13.06.10', systemId: 'Self', virtualMedia: true,
      bootOverrideModes: ['Once', 'Continuous'], probedAt: now,
    },
  }
  // Per-page Boot ISOs (decision 049). Only srv-1 has Boot Media in the fixture, so an ISO's
  // inUseBy is whether srv-1's enabled setting names it.
  let bootISOItems = (options.bootISOs ?? [
    { id: 'iso-taipei', name: 'taipei-rack', integrationId: 'maas-a', rackAddress: '10.0.0.2' },
    { id: 'iso-edge', name: 'edge-rack', integrationId: 'maas-b', rackAddress: '10.9.0.2' },
  ]).map((iso) => ({ ...iso, siteId: 'site-a', createdAt: now, createdBy: 'admin' }))
  const bootISOURL = (id: string) => `http://192.0.2.1/boot-media/ipxe/${id}/swallow-ipxe.iso`
  const bootISOView = (iso: (typeof bootISOItems)[number]) => {
    const chainUrl = `http://${/:\d+$/.test(iso.rackAddress) ? iso.rackAddress : `${iso.rackAddress}:5248`}/ipxe.cfg`
    return {
      ...iso, chainUrl, ipxeVersion: 'v2.0.0 (12798ec)', sizeBytes: 2_402_304, sha256: `sha256-of-${iso.id}`,
      script: `#!ipxe\n\nset maas_rack ${iso.rackAddress.replace(/:\d+$/, '')}\n\n:start\ndhcp || goto retry\nset next-server \${maas_rack}\nchain ${chainUrl.replace(/\/\/[^:/]+/, '//${next-server}')} || goto returned\n`,
      url: bootISOURL(iso.id),
      inUseBy: bootMediaState.setting?.enabled === true && bootMediaState.setting?.isoId === iso.id ? 1 : 0,
    }
  }
  const bootMediaView = (live: boolean) => {
    const isoId = typeof bootMediaState.setting?.isoId === 'string' ? bootMediaState.setting.isoId : ''
    const iso = bootISOItems.find((item) => item.id === isoId)
    const image = !isoId
      ? null
      : iso
        ? { id: iso.id, name: iso.name, url: bootISOURL(iso.id), available: true }
        : { id: isoId, url: '', available: false, reason: 'The Boot ISO no longer exists; choose another.' }
    return {
      serverId: 'srv-1',
      image,
      setting: bootMediaState.setting,
      redfish: bootMediaState.redfish,
      apply: bootMediaState.apply,
      live: live
        ? { mediaInserted: Boolean(bootMediaState.setting?.enabled), overrideEnabled: 'Once', overrideTarget: 'UefiBootNext', ready: Boolean(bootMediaState.setting?.enabled) }
        : null,
    }
  }
  // A newly created key starts pending in every provisioner and settles on its first single-key
  // read, modelling the backend sync pass that finishes moments after the create returns.
  const settlingKeyIds = new Set<string>()
  const pendingKey = (key: ReturnType<typeof sshKey>) => {
    settlingKeyIds.add(key.id)
    return { ...key, providerSync: key.providerSync.map(({ integrationId, siteId }) => ({ integrationId, siteId, state: 'pending' })) }
  }
  // Per-page Docker CE assignments and Docker Engine objects (software.md, servers-docker.md).
  const dockerAssignments = new Map(
    Object.entries(options.dockerAssignments ?? {}).map(([serverId, mode]) => [serverId, {
      serverId, kind: 'docker-ce', roles: [] as string[],
      spec: mode === 'enabled' ? ({ enableApi: true } as Record<string, unknown>) : null,
      state: 'installed', lastWorkflowId: 'op-docker-install', lastAppliedAt: '2026-08-20T00:00:00Z' as string | null,
      createdAt: '2026-08-20T00:00:00Z', updatedAt: '2026-08-20T00:00:00Z',
    }]),
  )
  let dockerImages = [
    { id: 'sha256:1111111111111111111111111111111111111111111111111111111111111111', repoTags: ['nginx:1.27'], repoDigests: ['nginx@sha256:aaaa'], sizeBytes: 192004589, createdAt: '2026-08-20T00:00:00Z', dangling: false },
    { id: 'sha256:2222222222222222222222222222222222222222222222222222222222222222', repoTags: [] as string[], repoDigests: [] as string[], sizeBytes: 5000000, createdAt: '2026-08-10T00:00:00Z', dangling: true },
  ]
  const dockerContainers = [
    {
      id: 'c0ffee000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', name: 'web', image: 'nginx:1.27',
      imageId: 'sha256:1111111111111111111111111111111111111111111111111111111111111111', command: "/docker-entrypoint.sh nginx -g 'daemon off;'",
      state: 'running', status: 'Up 2 hours', createdAt: '2026-08-27T01:00:00Z',
      ports: [{ ip: '0.0.0.0', privatePort: 80, publicPort: 8080 as number | null, protocol: 'tcp' }],
      networks: ['bridge'], mounts: [{ type: 'volume', source: 'web-data', destination: '/usr/share/nginx/html', readOnly: false }],
    },
  ]
  let registryCredentialItems = (options.registryCredentials ?? []).map((item, index) => ({
    id: `cred-${index + 1}`, registry: item.registry, username: item.username,
    createdAt: '2026-08-20T00:00:00Z', updatedAt: '2026-08-20T00:00:00Z', updatedBy: 'admin',
  }))
  const dockerVolumes = [
    { name: 'web-data', driver: 'local', mountpoint: '/var/lib/docker/volumes/web-data/_data', scope: 'local', createdAt: '2026-08-27T00:59:00Z' as string | null, labels: {} },
  ]
  const dockerNetworks = [
    { id: 'net-bridge', name: 'bridge', driver: 'bridge', scope: 'local', internal: false, attachable: false, predefined: true, subnets: [{ subnet: '172.17.0.0/16', gateway: '172.17.0.1' }], createdAt: null as string | null },
    { id: 'net-host', name: 'host', driver: 'host', scope: 'local', internal: false, attachable: false, predefined: true, subnets: [] as Array<{ subnet: string; gateway: string }>, createdAt: null as string | null },
  ]
  await page.addInitScript(() => {
    class FixtureEventSource {
      static readonly CONNECTING = 0
      static readonly OPEN = 1
      static readonly CLOSED = 2
      readonly url: string
      readonly withCredentials = false
      readyState = FixtureEventSource.CONNECTING
      onopen: ((event: Event) => void) | null = null
      onmessage: ((event: MessageEvent) => void) | null = null
      onerror: ((event: Event) => void) | null = null

      constructor(url: string) {
        this.url = url
        const sources = (window as typeof window & { __serverEventSources?: FixtureEventSource[] }).__serverEventSources ?? []
        sources.push(this)
        ;(window as typeof window & { __serverEventSources?: FixtureEventSource[] }).__serverEventSources = sources
        setTimeout(() => this.emitOpen(), 0)
      }

      close() {
        this.readyState = FixtureEventSource.CLOSED
      }

      emitOpen() {
        this.readyState = FixtureEventSource.OPEN
        this.onopen?.(new Event('open'))
      }

      emitError(closed = false) {
        this.readyState = closed ? FixtureEventSource.CLOSED : FixtureEventSource.CONNECTING
        this.onerror?.(new Event('error'))
      }

      emitMessage(data: string) {
        this.onmessage?.(new MessageEvent('message', { data }))
      }
    }

    Object.defineProperty(window, 'EventSource', { configurable: true, value: FixtureEventSource })
  })
  await page.unroute('**/api/v1/**')
  const fleet = Array.from({ length: options.fleetSize ?? 4 }, (_, index) => makeServer(index))
  let serverListRequestCount = 0
  let serverListFailuresRemaining = options.serverListFailuresAfterInitial ?? 0
  if (options.freePlatformCandidates) {
    for (const server of fleet) {
      server.membership = null
    }
  }
  if (options.minimumResourceCandidates && fleet[0] && fleet[1]) {
    Object.assign(fleet[0], { cpuCores: 4, memoryMiB: 24576, storageGB: 80 })
    Object.assign(fleet[1], { cpuCores: 4, memoryMiB: 16384, storageGB: 80 })
  }
  for (let index = 0; index < (options.readyServerCount ?? 0) && index < fleet.length; index++) {
    fleet[index].provisioning.state = 'ready'
    fleet[index].provisioning.providerState = 'Ready'
    fleet[index].provisioning.osSystem = ''
    fleet[index].provisioning.distroSeries = ''
    fleet[index].deployment = null
  }
  for (const serverId of options.changingServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) {
      server.provisioning.state = 'inspecting'
      server.provisioning.stateSince = '2026-08-27T02:58:30Z'
      server.provisioning.providerState = 'Commissioning'
      server.deployment = {
        state: 'deploying', operationId: 'op-running', stepId: `provision-${server.id}`,
        attempt: 1, code: '', stage: options.deployingStage ?? 'provisioning', statusReason: options.deployingStatusReason ?? 'Provisioning operating system.',
        startedAt: now, finishedAt: null, updatedAt: now,
      }
    }
  }
  for (const serverId of options.deploymentAttentionServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) {
      server.deployment = {
        state: 'requires_attention', operationId: 'op-deploy-failed', stepId: `provision-${server.id}`,
        attempt: 1, code: 'deployment_failed', stage: 'verification', statusReason: 'Swallow could not verify the deployment.',
        startedAt: now, finishedAt: null, updatedAt: now,
      }
    }
  }
  for (const serverId of options.absentServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.absent = true
  }
  for (const serverId of options.unobservedHealthServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.health = null
  }
  for (const serverId of options.cpuOnlyServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.gpus = []
  }
  for (const serverId of options.multiGpuServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.gpus = [
      { vendor: 'AMD', model: 'MI300X', count: 8, kind: 'compute' },
      { vendor: 'NVIDIA', model: 'H100', count: 4, kind: 'compute' },
      { vendor: 'ASPEED Technology, Inc.', model: 'ASPEED Graphics Family', count: 16, kind: 'display' },
    ]
  }
  for (const serverId of options.displayOnlyServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.gpus = [
      { vendor: 'ASPEED Technology, Inc.', model: 'ASPEED Graphics Family', count: 1, kind: 'display' },
    ]
  }
  for (const serverId of options.genericComputeServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.gpus = [
      { vendor: 'AMD', model: 'AMD GPU', count: 8, kind: 'compute' },
      { vendor: 'ASPEED Technology, Inc.', model: 'ASPEED Graphics Family', count: 1, kind: 'display' },
    ]
  }
  for (const serverId of options.cirrusDisplayOnlyServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.gpus = [
      { vendor: 'Cirrus Logic', model: 'GD 5446', count: 1, kind: 'display' },
    ]
  }
  for (const [serverId, imageName] of Object.entries(options.deployedImageNames ?? {})) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.provisioning.deployedImageName = imageName
  }
  for (const [serverId, tags] of Object.entries(options.extraTags ?? {})) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.tags = [...server.tags, ...tags]
  }
  for (const serverId of options.ephemeralServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) {
      server.provisioning.ephemeral = true
    }
  }
  for (const serverId of options.lockedServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.provisioning.locked = true
  }
  for (const serverId of options.existingServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.deployment = null
  }
  // Seed the not-usable provider states the recovery policy acts on (decision 033), so tests
  // can exercise Recover / Release gating and the Failed vs Broken vs Rescue badges.
  const seedProviderState = (ids: string[] | undefined, state: string, providerState: string) => {
    for (const serverId of ids ?? []) {
      const server = fleet.find((item) => item.id === serverId)
      if (server) {
        server.provisioning.state = state
        server.provisioning.providerState = providerState
      }
    }
  }
  seedProviderState(options.failedServerIds, 'failed', 'Failed deployment')
  seedProviderState(options.brokenServerIds, 'broken', 'Broken')
  seedProviderState(options.rescueServerIds, 'rescue', 'Rescue mode')
  const providerWorkLabels = { releasing: 'Releasing', inspecting: 'Commissioning', testing: 'Testing', deploying: 'Deploying' }
  for (const [serverId, state] of Object.entries(options.providerWorkServerIds ?? {})) {
    seedProviderState([serverId], state, providerWorkLabels[state])
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.provisioning.stateSince = '2026-08-27T02:58:30Z'
  }
  if (options.secondReadyServerIntegrationId && fleet[1]) {
    fleet[1].source.integrationId = options.secondReadyServerIntegrationId
    fleet[1].provisioning.integrationId = options.secondReadyServerIntegrationId
  }
  let deploymentTemplates = baseDeploymentTemplates.map((template) => ({ ...template }))
  let siteItems = sites.map((site) => ({ ...site }))
  let integrationItems = integrations
    .filter((integration) => !options.noProvisioners || integration.kind !== 'provisioner')
    .map((integration) => ({
      ...integration,
      settings: { ...integration.settings },
      sync: { ...integration.sync },
    }))
  let metricBatchIndex = 0
  let activeMetricRequests = 0
  let slurmRequirement: {
    platformType: 'slurm'
    minimumResources: { cpuCores: number; memoryMiB: number; storageGB: number } | null
    updatedAt: string | null
  } = { platformType: 'slurm', minimumResources: null, updatedAt: null }
  const releaseRefreshesRemaining = new Map<string, number>()
  const deploymentRefreshesRemaining = new Map<string, number>()
  const releaseCleanupRequested = new Set<string>()
  const provisioningTasks: Array<Record<string, unknown>> = []
  const networkTargets = new Map(fleet.map((server) => {
    const interfaceId = `nic-${server.id}`
    const linkId = `link-${server.id}`
    const hasStaticBinding = options.staticNetworkServerIds?.includes(server.id) ?? false
    const subnetName = options.networkSubnetName ?? 'lab-network'
    return [server.id, {
      serverId: server.id,
      editable: server.provisioning.state === 'ready' && !server.provisioning.locked,
      disabledReason: server.provisioning.locked
        ? 'Unlock the Server before changing network configuration.'
        : server.provisioning.state === 'ready' ? '' : 'Network configuration can only be changed while the Server is Ready.',
      suggestion: { mode: hasStaticBinding ? 'static' : 'dhcp', interfaceId, subnetId: 'subnet-a', ipAddress: hasStaticBinding ? server.addresses[0] ?? '' : '', defaultGateway: hasStaticBinding },
      network: {
        interfaces: [{
          id: interfaceId,
          name: 'eno1',
          macAddress: server.hardware.macAddresses[0] ?? '',
          boot: true,
          physicalState: 'up',
          configurationState: hasStaticBinding ? 'static' : 'provider_managed',
          rawProviderMode: hasStaticBinding ? 'STATIC' : 'AUTO',
          links: [{
            id: linkId,
            configurationState: hasStaticBinding ? 'static' : 'provider_managed',
            rawProviderMode: hasStaticBinding ? 'STATIC' : 'AUTO',
            subnetId: 'subnet-a',
            subnetName,
            cidr: '192.168.40.0/24',
            ipAddress: server.addresses[0] ?? '',
            defaultGateway: true,
          }],
          availableSubnets: [{
            id: 'subnet-a',
            name: subnetName,
            cidr: '192.168.40.0/24',
            gatewayAddress: '192.168.40.1',
            managed: true,
          }],
        }],
      },
    }] as const
  }))
  const observeRelease = (serverId: string) => {
    const remaining = releaseRefreshesRemaining.get(serverId)
    if (remaining === undefined) return
    const server = fleet.find((item) => item.id === serverId)
    if (!server) {
      releaseRefreshesRemaining.delete(serverId)
      return
    }
    if (remaining <= 1) {
      server.provisioning.state = 'ready'
      server.provisioning.stateSince = new Date().toISOString()
      server.provisioning.providerState = 'Ready'
      server.provisioning.osSystem = ''
      server.provisioning.distroSeries = ''
      server.provisioning.ephemeral = false
      server.deployment = null
      if (releaseCleanupRequested.has(serverId) && !options.releaseCleanupFails) {
        server.addresses = []
        const target = networkTargets.get(serverId)
        const iface = target?.network.interfaces[0]
        if (iface) {
          iface.links = iface.links.filter((link) => link.configurationState !== 'static')
          iface.configurationState = iface.links[0]?.configurationState ?? 'unconfigured'
          iface.rawProviderMode = iface.links[0]?.rawProviderMode ?? ''
        }
      }
      releaseRefreshesRemaining.delete(serverId)
    } else {
      releaseRefreshesRemaining.set(serverId, remaining - 1)
    }
  }
  const observeDeployment = (serverId: string) => {
    const remaining = deploymentRefreshesRemaining.get(serverId)
    if (remaining === undefined) return
    const server = fleet.find((item) => item.id === serverId)
    if (!server) {
      deploymentRefreshesRemaining.delete(serverId)
      return
    }
    if (remaining <= 1) {
      server.provisioning.state = 'deployed'
      server.provisioning.providerState = 'Deployed'
      server.provisioning.osSystem = 'ubuntu'
      server.provisioning.distroSeries = 'ubuntu/jammy'
      if (server.deployment) {
        server.deployment.state = 'succeeded'
        server.deployment.statusReason = ''
        server.deployment.stage = ''
        server.deployment.finishedAt = now
        server.deployment.updatedAt = now
      }
      deploymentRefreshesRemaining.delete(serverId)
    } else {
      deploymentRefreshesRemaining.set(serverId, remaining - 1)
    }
  }
  const platformItems = platforms.map((platform) => ({ ...platform }))
  if (options.slurmDeployed) {
    // Controller-only nodes carry no membership (they are not slurmd scheduler nodes); compute
    // nodes do. getServer reads from the fleet, so the controllers must live here for the
    // detail page's controller merge to resolve them.
    const makeSlurmNode = (id: string, hostname: string, octet: number, member: boolean): Server => ({
      ...makeServer(0),
      id,
      source: { siteId: 'site-a', integrationId: 'maas-a', providerMachineId: id },
      hostname,
      fqdn: `${hostname}.lab.example`,
      addresses: [`192.168.60.${octet}`],
      membership: member
        ? { platformId: 'platform-slurm-ha', nodeName: hostname, role: 'main', state: 'idle', observedAt: now }
        : null,
      // The cluster has no exporter coverage, so the health axis reads "down" for every node.
      // Controllers must not surface that monitoring "down" as their operational state.
      health: { state: 'down', observedAt: now },
    })
    fleet.push(
      makeSlurmNode('slurm-ctl-1', 'slurm-ctl-01', 11, false),
      makeSlurmNode('slurm-ctl-2', 'slurm-ctl-02', 12, false),
      makeSlurmNode('slurm-cpt-1', 'slurm-cpt-01', 21, true),
      makeSlurmNode('slurm-cpt-2', 'slurm-cpt-02', 22, true),
    )
    platformItems.push({
      id: 'platform-slurm-ha', siteId: 'site-a', name: 'lab-slurm-ha', type: 'slurm',
      integrationId: 'slurm-ha', origin: 'deployed', lifecycleState: 'active',
      lifecycleOperationId: 'op-slurm-ha-deploy',
      deployment: {
        topology: 'high-availability',
        roleAssignments: [
          { serverId: 'slurm-ctl-1', role: 'control-plane', runWorkloads: false },
          { serverId: 'slurm-ctl-2', role: 'control-plane', runWorkloads: false },
          { serverId: 'slurm-cpt-1', role: 'worker', runWorkloads: false },
          { serverId: 'slurm-cpt-2', role: 'worker', runWorkloads: false },
        ],
      },
      gpuStackOwner: 'provisioning', exporterOwner: 'ansible',
      sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null, memberCount: 2, matchedCount: 2 },
      createdAt: now, updatedAt: now,
    })
  }
  const operationItems = operations.map((operation) => ({ ...operation }))
  // inspect-hardware Workflows waiting for attention (server-enrollment.md): the inspect Task ran
  // out of attempts because the Server never network-booted into MAAS.
  for (const serverId of options.inspectionAttentionServerIds ?? []) {
    const inspection = {
      id: `op-inspect-${serverId}`, schemaVersion: 3, kind: 'inspect-hardware',
      intent: `Inspect hardware of ${serverId}`, intentSnapshot: { serverId, origin: 'automatic' },
      definition: 'hardware-inspection', definitionVersion: 1,
      status: 'requires_attention', statusReason: 'Job ensure-inspected requires operator attention.',
      startState: 'started', temporal: { workflowId: `swallow-operation/op-inspect-${serverId}`, runId: 'run-inspect' },
      siteId: 'site-a', platformId: null,
      targetResources: [{ kind: 'server', id: serverId }], targetServerIds: [serverId], retryOfOperationId: null,
      steps: [
        {
          id: `inspect-${serverId}`, kind: 'inspect', name: `Inspect hardware of ${serverId}`, job: 'ensure-inspected',
          executor: 'maas', dependsOn: [], targets: [{ kind: 'server', id: serverId }],
          status: 'requires_attention', attempt: 1, progress: 0,
          error: {
            code: 'inspect_pxe_unreached', retryable: true, stage: 'inspection',
            message: `Hardware inspection of ${serverId} did not complete in 3 attempts: no provider progress for 15m0s after powering it on. If its network is not served by the provisioner's DHCP (an external network), build a Boot ISO and enable Boot Media on the Server, then retry this Task.`,
          },
          externalExecution: null, artifacts: [], startedAt: now, finishedAt: now,
        },
      ],
      execution: { runId: 'run-inspect', playbook: '', status: 'running', statusReason: null, startedAt: now, finishedAt: null },
      requestedBy: 'system', requestedAt: now, updatedAt: now,
    }
    operationItems.push(inspection as unknown as (typeof operationItems)[number])
  }
  let operationSequence = 0
  const createProvisioningOperation = (
    kind: 'deploy-os' | 'release-os',
    serverIds: string[],
    request: Record<string, unknown>,
  ) => {
    operationSequence += 1
    const id = `op-${kind}-${operationSequence}`
    const failedIds = new Set(
      kind === 'deploy-os'
        ? options.deploymentFailureIds ?? []
        : options.serverActionFailureIds ?? [],
    )
    const failedCount = serverIds.filter((serverId) => failedIds.has(serverId)).length
    const steps = serverIds.map((serverId) => {
      const failed = failedIds.has(serverId)
      const retryable = failed && Boolean(options.providerFailureRetryable)
      const succeeded = !failed && failedCount > 0
      return {
        id: `${kind === 'deploy-os' ? 'provision' : 'release'}-${serverId}`,
        kind: kind === 'deploy-os' ? 'provision-os' : 'release-os',
        name: `${kind === 'deploy-os' ? 'Provision operating system on' : 'Release'} ${serverId}`,
        executor: 'maas',
        dependsOn: [],
        targets: [{ kind: 'server', id: serverId }],
        status: failed ? retryable ? 'requires_attention' : 'failed' : succeeded ? 'succeeded' : 'waiting_external',
        attempt: 1,
        progress: 0,
        waitingReason: failed || succeeded ? '' : 'Waiting for maas execution.',
        error: failed ? {
          code: retryable ? 'provider_unavailable' : 'provider_rejected',
          message: retryable
            ? 'MAAS is temporarily unavailable.'
            : kind === 'deploy-os'
              ? 'Machine reservation changed.'
              : 'MAAS refused the request: Machine cannot be released while a hosted VM is running.',
          retryable,
          stage: kind === 'deploy-os' ? 'deployment' : 'release',
        } : null,
        externalExecution: failed ? { provider: 'maas', id: serverId, generation: 1 } : null,
        artifacts: [],
        startedAt: now,
        finishedAt: failed || succeeded ? now : null,
      }
    })
    if (kind === 'deploy-os') {
      for (const serverId of serverIds) {
        const server = fleet.find((candidate) => candidate.id === serverId)
        const failed = failedIds.has(serverId)
        if (!server) continue
        server.deployment = {
          state: failed
            ? options.providerFailureRetryable ? 'requires_attention' : 'failed'
            : 'deploying',
          operationId: id,
          stepId: `provision-${serverId}`,
          attempt: 1,
          code: failed
            ? options.providerFailureRetryable ? 'provider_unavailable' : 'provider_rejected'
            : '',
          stage: failed ? 'deployment' : '',
          statusReason: failed
            ? options.providerFailureRetryable ? 'MAAS is temporarily unavailable.' : 'Machine reservation changed.'
            : '',
          startedAt: now,
          finishedAt: failed ? now : null,
          updatedAt: now,
        }
      }
    }
    const retryableFailure = steps.some((step) => step.status === 'requires_attention')
    const status = retryableFailure ? 'requires_attention' : failedCount === 0
      ? 'waiting_external'
      : failedCount === steps.length ? 'failed' : 'partially_succeeded'
    const item = {
      ...operations[0],
      id,
      schemaVersion: 3,
      kind,
      intent: kind === 'deploy-os'
        ? `Deploy operating system to ${serverIds.length} Server(s)`
        : `Release ${serverIds.length} Server(s)`,
      intentSnapshot: { request },
      definition: kind === 'deploy-os' ? 'os-deployment' : 'os-release',
      definitionVersion: 1,
      status,
      statusReason: failedCount ? 'One or more provider Steps failed.' : 'Waiting for provider observation.',
      startState: 'started',
      temporal: { workflowId: `swallow-operation/${id}`, runId: `run-${id}` },
      platformId: null,
      targetResources: serverIds.map((serverId) => ({ kind: 'server', id: serverId })),
      targetServerIds: serverIds,
      steps,
      leases: serverIds.map((serverId, index) => ({
        resourceKey: `server:${serverId}`,
        owner: `swallow-operation/${id}`,
        fencingToken: index + 1,
        expiresAt: now,
        updatedAt: now,
      })),
      retryOfOperationId: null,
      requestCorrelation: `req-${id}`,
      execution: {
        runId: `run-${id}`,
        playbook: '',
        status,
        statusReason: failedCount ? 'One or more provider Steps failed.' : 'Waiting for provider observation.',
        startedAt: now,
        finishedAt: failedCount ? now : null,
      },
      requestedBy: 'admin',
      requestedAt: now,
      startedAt: now,
      finishedAt: failedCount ? now : null,
      updatedAt: now,
    }
    operationItems.unshift(item as unknown as (typeof operationItems)[number])
    return id
  }
  if (options.freePlatformCandidates) {
    for (const operation of operationItems) {
      if (operation.kind === 'deploy-kubernetes') {
        operation.targetServerIds = []
      }
    }
  }
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (path === '/api/v1/auth/me') return json(route, { id: 'admin-1', username: 'admin', role: 'admin', authMethod: 'session' })
    if (path === '/api/v1/auth/login') {
      signedIn = true
      return json(route, { accessToken: 'e2e-token', accessTokenExpiresAt: '2099-01-01T00:00:00Z' })
    }
    if (path === '/api/v1/auth/refresh' && request.method() === 'POST') {
      options.onSessionRefresh?.()
      if (!signedIn) return json(route, { error: { code: 'unauthorized', message: 'Session expired. Sign in again.', requestId: 'req-refresh' } }, 401)
      return json(route, { accessToken: 'e2e-token', accessTokenExpiresAt: '2099-01-01T00:00:00Z' })
    }
    if (path === '/api/v1/auth/logout' && request.method() === 'POST') {
      signedIn = false
      return route.fulfill({ status: 204 })
    }
    if (path === '/api/v1/api-keys' && request.method() === 'GET') return json(route, apiKeyItems)
    if (path === '/api/v1/api-keys' && request.method() === 'POST') {
      const body = request.postDataJSON() as { name?: string; expiresAt?: string } | null
      const name = String(body?.name ?? '')
      if (apiKeyItems.some((key) => key.name.toLowerCase() === name.toLowerCase())) {
        return json(route, { error: { code: 'conflict', message: 'You already have an API key with this name.', requestId: 'req-api-key-conflict' } }, 409)
      }
      const key = { id: `api-${apiKeyItems.length + 1}`, name, prefix: 'swk_N3wK3y00', createdAt: now, expiresAt: body?.expiresAt ?? null, lastUsedAt: null }
      apiKeyItems = [...apiKeyItems, key]
      return json(route, { key, secret: 'swk_N3wK3y00FIXTURE-SECRET' }, 201)
    }
    const apiKeyMatch = path.match(/^\/api\/v1\/api-keys\/([^/]+)$/)
    if (apiKeyMatch && request.method() === 'DELETE') {
      apiKeyItems = apiKeyItems.filter((key) => key.id !== decodeURIComponent(apiKeyMatch[1]))
      return route.fulfill({ status: 204 })
    }
    if (path === '/api/v1/software/catalog') {
      return json(route, { items: [
        { kind: 'docker-ce', label: 'Docker CE', roles: [], mutuallyExclusiveWith: ['podman'], refusedForKubernetesMembers: true, specFields: ['version', 'enableApi'] },
        { kind: 'podman', label: 'Podman', roles: [], mutuallyExclusiveWith: ['docker-ce'], refusedForKubernetesMembers: true, specFields: ['version'] },
        { kind: 'nfs', label: 'NFS', roles: ['server', 'client'], mutuallyExclusiveWith: [], refusedForKubernetesMembers: false, specFields: ['exportPath', 'exportOptions', 'source', 'mountPath', 'mountOptions'] },
      ] })
    }
    // Registry Credentials (registry-credentials.md): responses never carry a password.
    if (path === '/api/v1/software/docker-ce/registry-credentials' && request.method() === 'GET') {
      return json(route, { items: [...registryCredentialItems].sort((a, b) => a.registry.localeCompare(b.registry)) })
    }
    if (path === '/api/v1/software/docker-ce/registry-credentials' && request.method() === 'POST') {
      const body = (request.postDataJSON() ?? {}) as { registry?: string; username?: string; password?: string }
      options.onRegistryCredentialRequest?.('POST', path, body as Record<string, unknown>)
      const host = String(body.registry ?? '').trim().toLowerCase().replace(/^https?:\/\//, '').replace(/\/$/, '')
      const dockerHubNames = ['index.docker.io', 'registry-1.docker.io', 'hub.docker.com', 'registry.hub.docker.com', 'hub.docker.io', 'index.docker.io/v1']
      const registry = dockerHubNames.includes(host) ? 'docker.io' : host
      if (registryCredentialItems.some((item) => item.registry === registry)) {
        return json(route, { error: { code: 'conflict', message: 'A credential for this registry already exists. Replace it instead.', requestId: 'req-cred-conflict' } }, 409)
      }
      const created = { id: `cred-${registryCredentialItems.length + 1}`, registry, username: String(body.username ?? ''), createdAt: now, updatedAt: now, updatedBy: 'admin' }
      registryCredentialItems = [...registryCredentialItems, created]
      return json(route, created, 201)
    }
    const registryCredentialMatch = path.match(/^\/api\/v1\/software\/docker-ce\/registry-credentials\/([^/]+)$/)
    if (registryCredentialMatch && request.method() === 'PUT') {
      const body = (request.postDataJSON() ?? {}) as { username?: string }
      options.onRegistryCredentialRequest?.('PUT', path, body as Record<string, unknown>)
      const item = registryCredentialItems.find((entry) => entry.id === decodeURIComponent(registryCredentialMatch[1]))
      if (!item) return json(route, { error: { code: 'not_found', message: 'Registry credential not found.' } }, 404)
      Object.assign(item, { username: String(body.username ?? ''), updatedAt: now })
      return json(route, item)
    }
    if (registryCredentialMatch && request.method() === 'DELETE') {
      options.onRegistryCredentialRequest?.('DELETE', path, null)
      registryCredentialItems = registryCredentialItems.filter((entry) => entry.id !== decodeURIComponent(registryCredentialMatch[1]))
      return route.fulfill({ status: 204 })
    }
    // Managed Software assignments: only the Docker CE records the explorer tests opt into.
    if (path === '/api/v1/software/assignments' && request.method() === 'GET') {
      const serverId = url.searchParams.get('serverId')
      const kind = url.searchParams.get('kind')
      const items = [...dockerAssignments.values()].filter((item) => (!serverId || item.serverId === serverId) && (!kind || item.kind === kind))
      return json(route, { items })
    }
    if (path === '/api/v1/software/assignments' && request.method() === 'POST') {
      const body = (request.postDataJSON() ?? {}) as { assignments?: Array<{ serverId: string }>; spec?: Record<string, unknown> }
      options.onSoftwareInstallRequest?.(body as Record<string, unknown>)
      for (const target of body.assignments ?? []) {
        const existing = dockerAssignments.get(target.serverId)
        if (existing) Object.assign(existing, { state: 'pending', spec: body.spec ?? null, lastWorkflowId: 'op-docker-reapply' })
      }
      return json(route, { operationId: 'op-docker-reapply' }, 202)
    }
    // Docker Host Explorer: eligible only for an installed Docker CE with enableApi (servers-docker.md).
    const dockerMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/docker(\/.*)?$/)
    if (dockerMatch) {
      const assignment = dockerAssignments.get(decodeURIComponent(dockerMatch[1]))
      if (!assignment || assignment.state !== 'installed' || assignment.spec?.enableApi !== true) {
        return json(route, { error: { code: 'conflict', message: 'The Docker Engine API is not enabled for this Server.', requestId: 'req-docker-ineligible' } }, 409)
      }
      const sub = dockerMatch[2] ?? ''
      const method = request.method()
      const body = method === 'GET' ? null : ((request.postDataJSON() ?? null) as Record<string, unknown> | null)
      if (method !== 'GET') options.onDockerRequest?.(method, sub, body)
      if (sub === '') {
        return json(route, {
          endpoint: 'tcp://192.168.40.21:2375', serverVersion: '27.3.1', apiVersion: '1.47', operatingSystem: 'Ubuntu 24.04.1 LTS',
          osType: 'linux', architecture: 'x86_64', kernelVersion: '6.8.0-45-generic', storageDriver: 'overlay2', cpus: 64,
          memoryBytes: 549755813888, containers: dockerContainers.length, containersRunning: dockerContainers.filter((item) => item.state === 'running').length,
          containersPaused: 0, containersStopped: dockerContainers.filter((item) => item.state !== 'running').length, images: dockerImages.length,
        })
      }
      if (sub === '/images' && method === 'GET') return json(route, { items: dockerImages })
      if (sub === '/images/pull' && method === 'POST') {
        await options.dockerPullGate
        const reference = String(body?.reference ?? '')
        const canonical = reference.lastIndexOf(':') > reference.lastIndexOf('/') ? reference : `${reference}:latest`
        const first = reference.split('/')[0]
        const registry = reference.includes('/') && (/[.:]/.test(first) || first === 'localhost') ? first : 'docker.io'
        dockerImages = [{ id: `sha256:${'3'.repeat(64)}`, repoTags: [canonical], repoDigests: [], sizeBytes: 25874, createdAt: now, dangling: false }, ...dockerImages]
        return json(route, {
          reference: canonical, status: `Status: Downloaded newer image for ${canonical}`,
          registry, authenticated: registryCredentialItems.some((item) => item.registry === registry),
        })
      }
      const imageMatch = sub.match(/^\/images\/([^/]+)$/)
      if (imageMatch && method === 'DELETE') {
        dockerImages = dockerImages.filter((item) => item.id !== decodeURIComponent(imageMatch[1]))
        return json(route, { success: true })
      }
      if (sub === '/containers' && method === 'GET') return json(route, { items: dockerContainers })
      const containerAction = sub.match(/^\/containers\/([^/]+)\/(start|stop|restart)$/)
      if (containerAction && method === 'POST') {
        const container = dockerContainers.find((item) => item.id === decodeURIComponent(containerAction[1]))
        if (container) {
          container.state = containerAction[2] === 'stop' ? 'exited' : 'running'
          container.status = containerAction[2] === 'stop' ? 'Exited (0) 1 second ago' : 'Up 1 second'
        }
        return json(route, { success: true })
      }
      if (sub.match(/^\/containers\/[^/]+\/logs$/)) return json(route, { logs: 'nginx: ready\n' })
      if (sub === '/volumes' && method === 'GET') return json(route, { items: dockerVolumes })
      if (sub === '/networks' && method === 'GET') return json(route, { items: dockerNetworks })
      return json(route, { error: { code: 'not_found', message: `No Docker fixture for ${method} ${sub}` } }, 404)
    }
    if (path === '/api/v1/sites' && request.method() === 'GET') return json(route, siteItems)
    if (path === '/api/v1/sites' && request.method() === 'POST') {
      const body = request.postDataJSON() as { name: string; description: string }
      const created = { id: `site-${siteItems.length + 1}`, name: body.name, description: body.description, createdAt: now, updatedAt: now }
      siteItems.push(created)
      return json(route, created, 201)
    }
    const siteMatch = path.match(/^\/api\/v1\/sites\/([^/]+)$/)
    if (siteMatch) {
      const site = siteItems.find((item) => item.id === siteMatch[1])
      if (!site) return json(route, { error: { code: 'not_found', message: 'Site not found.' } }, 404)
      if (request.method() === 'GET') return json(route, site)
      if (request.method() === 'PATCH') {
        Object.assign(site, request.postDataJSON(), { updatedAt: now })
        return json(route, site)
      }
      if (request.method() === 'DELETE') {
        if (integrationItems.some((integration) => integration.siteId === site.id)) {
          return json(route, { error: { code: 'conflict', message: 'This site still has integrations. Delete them first.' } }, 409)
        }
        siteItems = siteItems.filter((item) => item.id !== site.id)
        return json(route, { success: true })
      }
    }
    // Server Enrollment (server-enrollment.md): the existing-OS bundle carries the provisioner's
    // API key, and its command fetches the enrollment script from the swallowUrl it was given.
    const enrollmentMatch = path.match(/^\/api\/v1\/provisioning\/integrations\/([^/]+)\/enroll-bundle$/)
    if (enrollmentMatch && request.method() === 'POST') {
      const body = (request.postDataJSON() ?? {}) as { swallowUrl?: string }
      options.onEnrollmentRequest?.(request.method(), path, body.swallowUrl ?? '')
      const integration = integrationItems.find((item) => item.id === enrollmentMatch[1] && item.kind === 'provisioner')
      if (!integration) return json(route, { error: { code: 'not_found', message: 'Integration not found.' } }, 404)
      const endpoint = `${integration.endpoint}/MAAS`
      const token = `consumer-${integration.id}:token:secret`
      return json(route, {
        integrationId: integration.id, providerKind: 'maas', endpoint, token,
        command: `curl -fsSL '${body.swallowUrl}/downloads/swallow-enroll.sh' | sudo sh -s -- --provisioner=maas --endpoint '${endpoint}' --token '${token}'`,
      })
    }
    if (path === '/api/v1/integrations' && request.method() === 'GET') {
      const siteId = url.searchParams.get('siteId')
      const kind = url.searchParams.get('kind')
      return json(route, integrationItems.filter((item) => (
        (!siteId || item.siteId === siteId) &&
        (!kind || item.kind === kind)
      )))
    }
    if (path === '/api/v1/integrations' && request.method() === 'POST') {
      const body = request.postDataJSON() as {
        siteId: string
        kind: string
        providerKind: string
        name: string
        endpoint: string
        credential: string
        settings: Record<string, string>
        enabled: boolean
      }
      const created = {
        id: `integration-${integrationItems.length + 1}`,
        siteId: body.siteId,
        kind: body.kind,
        providerKind: body.providerKind,
        name: body.name,
        endpoint: body.endpoint,
        enabled: body.enabled,
        settings: body.settings,
        hasCredential: Boolean(body.credential),
        sync: { lastStartedAt: null, lastSucceededAt: null, lastError: null },
        createdAt: now,
        updatedAt: now,
      }
      integrationItems.push(created)
      return json(route, created, 201)
    }
    const credentialMatch = path.match(/^\/api\/v1\/integrations\/([^/]+)\/credential$/)
    if (credentialMatch && request.method() === 'PUT') {
      const integration = integrationItems.find((item) => item.id === credentialMatch[1])
      if (!integration) return json(route, { error: { code: 'not_found', message: 'Integration not found.' } }, 404)
      integration.hasCredential = true
      integration.updatedAt = now
      return json(route, { success: true })
    }
    const integrationMatch = path.match(/^\/api\/v1\/integrations\/([^/]+)$/)
    if (integrationMatch) {
      const integration = integrationItems.find((item) => item.id === integrationMatch[1])
      if (!integration) return json(route, { error: { code: 'not_found', message: 'Integration not found.' } }, 404)
      if (request.method() === 'GET') return json(route, integration)
      if (request.method() === 'PATCH') {
        Object.assign(integration, request.postDataJSON(), { updatedAt: now })
        return json(route, integration)
      }
      if (request.method() === 'DELETE') {
        const referenced = fleet.some((server) => server.source.integrationId === integration.id) ||
          deploymentTemplates.some((template) => template.integrationId === integration.id)
        if (referenced) {
          return json(route, { error: { code: 'conflict', message: 'Resources still reference this Integration.' } }, 409)
        }
        integrationItems = integrationItems.filter((item) => item.id !== integration.id)
        return json(route, { success: true })
      }
    }

    if (path === '/api/v1/provisioning/images') {
      if (request.method() === 'POST') {
        // Upload is multipart, so the integration id is a form field rather than a query param.
        // The provider classifies an uploaded artifact (here as the custom osystem), which is what
        // the created row reflects.
        const uploadedIntegrationId = request.postData()?.match(/name="integrationId"\r?\n\r?\n([^\r\n]+)/)?.[1] ?? ''
        options.onOSImageUploadRequest?.(uploadedIntegrationId)
        options.onOSImageUploadDefaultUser?.(request.postData()?.match(/name="defaultUser"\r?\n\r?\n([^\r\n]+)/)?.[1] ?? '')
        return json(
          route,
          {
            id: 'ubuntu-24.04-rocm',
            name: 'Ubuntu 24.04 ROCm',
            providerName: 'Ubuntu 24.04 ROCm',
            osSystem: 'custom',
            providerOsSystem: 'custom',
            release: 'ubuntu-24.04-rocm',
            providerRelease: 'ubuntu-24.04-rocm',
            tags: [],
            architecture: 'amd64',
            sizeBytes: 16,
          },
          201,
        )
      }
      const integrationId = url.searchParams.get('integrationId') ?? ''
      options.onOSImageCatalogRequest?.(integrationId)
      if (options.failImageIntegrationIds?.includes(integrationId)) {
        return json(route, { error: { code: 'provider_unavailable', message: 'Image provider is unavailable' } }, 503)
      }
      return json(route, osImages)
    }
    if (path === '/api/v1/provisioning/images/overlay' && request.method() === 'PATCH') {
      options.onOSImageOverlayRequest?.(request.postDataJSON() as Record<string, unknown>)
      return route.fulfill({ status: 204 })
    }
    if (path.startsWith('/api/v1/ssh-keys')) {
      const body = request.postData() ? (request.postDataJSON() as Record<string, unknown>) : null
      options.onSSHKeyRequest?.(request.method(), path, body)
      if (path === '/api/v1/ssh-keys' && request.method() === 'GET') return json(route, sshKeyItems)
      if (path === '/api/v1/ssh-keys' && request.method() === 'POST') {
        const publicKey = String(body?.publicKey ?? '')
        if (!publicKey.startsWith('ssh-')) {
          return json(route, { error: { code: 'validation_error', message: 'invalid ssh key: not a public key' } }, 400)
        }
        const created = pendingKey(sshKey(`key-${sshKeyItems.length + 1}`, String(body?.name ?? ''), 'access', publicKey))
        sshKeyItems.push(created)
        return json(route, created, 201)
      }
      if (path === '/api/v1/ssh-keys/generate' && request.method() === 'POST') {
        const created = pendingKey(sshKey(`key-${sshKeyItems.length + 1}`, String(body?.name ?? ''), 'access', `ssh-ed25519 AAAAGENERATED ${String(body?.name ?? '')}`))
        sshKeyItems.push(created)
        return json(route, { key: created, privateKey: '-----BEGIN OPENSSH PRIVATE KEY-----\nFIXTURE\n-----END OPENSSH PRIVATE KEY-----\n' }, 201)
      }
      if (path === '/api/v1/ssh-keys/sync' && request.method() === 'POST') return route.fulfill({ status: 202 })
      if (path === '/api/v1/ssh-keys/deployment/regenerate' && request.method() === 'POST') {
        sshKeyItems[0] = { ...sshKeyItems[0], fingerprint: 'SHA256:regenerated', publicKey: 'ssh-ed25519 AAAAREGENERATED swallow-deployment', providerSync: sshKeyItems[0].providerSync.map((entry) => ({ ...entry, state: 'pending' })) }
        return json(route, sshKeyItems[0])
      }
      const keyMatch = path.match(/^\/api\/v1\/ssh-keys\/([^/]+)$/)
      if (keyMatch && request.method() === 'GET') {
        const index = sshKeyItems.findIndex((item) => item.id === keyMatch[1])
        if (index < 0) return json(route, { error: { code: 'not_found', message: 'SSH key not found.' } }, 404)
        if (settlingKeyIds.delete(sshKeyItems[index].id)) {
          sshKeyItems[index] = {
            ...sshKeyItems[index],
            providerSync: sshKeyItems[index].providerSync.map(({ integrationId, siteId }) => ({ integrationId, siteId, state: 'synced', syncedAt: now })),
          }
        }
        return json(route, sshKeyItems[index])
      }
      if (keyMatch && request.method() === 'DELETE') {
        const target = sshKeyItems.find((item) => item.id === keyMatch[1])
        if (target?.purpose === 'deployment') {
          return json(route, { error: { code: 'conflict', message: 'The deployment key cannot be deleted; replace or regenerate it instead.' } }, 409)
        }
        sshKeyItems = sshKeyItems.filter((item) => item.id !== keyMatch[1])
        return route.fulfill({ status: 204 })
      }
    }
    // Boot ISOs (boot-isos.md): the real API packages an iPXE ISO with genfsimg; the fixture
    // stores the record, derives the chain URL and script as the API does, and records writes.
    if (path === '/api/v1/provisioning/boot-isos' && request.method() === 'GET') {
      const siteId = url.searchParams.get('siteId')
      const integrationId = url.searchParams.get('integrationId')
      const builder = options.bootISOBuilderUnavailable
        ? { available: false, reason: options.bootISOBuilderUnavailable }
        : { available: true }
      return json(route, {
        builder,
        items: bootISOItems
          .filter((item) => (!siteId || item.siteId === siteId) && (!integrationId || item.integrationId === integrationId))
          .sort((a, b) => a.name.localeCompare(b.name))
          .map(bootISOView),
      })
    }
    if (path === '/api/v1/provisioning/boot-isos' && request.method() === 'POST') {
      const body = (request.postDataJSON() ?? {}) as Record<string, unknown>
      options.onBootISORequest?.('POST', path, body)
      if (options.bootISOBuildError) {
        const { status, code, message } = options.bootISOBuildError
        return json(route, { error: { code, message, requestId: 'req-boot-iso' } }, status)
      }
      const name = String(body.name ?? '').trim()
      const integrationId = String(body.integrationId ?? '')
      if (bootISOItems.some((item) => item.integrationId === integrationId && item.name.toLowerCase() === name.toLowerCase())) {
        return json(route, { error: { code: 'conflict', message: 'A Boot ISO with this name already exists for the integration.' } }, 409)
      }
      const created = { id: `iso-${bootISOItems.length + 1}`, name, integrationId, rackAddress: String(body.rackAddress ?? '').trim(), siteId: 'site-a', createdAt: now, createdBy: 'admin' }
      bootISOItems = [...bootISOItems, created]
      return json(route, bootISOView(created), 201)
    }
    const bootISOMatch = path.match(/^\/api\/v1\/provisioning\/boot-isos\/([^/]+)$/)
    if (bootISOMatch) {
      const iso = bootISOItems.find((item) => item.id === decodeURIComponent(bootISOMatch[1]))
      if (!iso) return json(route, { error: { code: 'not_found', message: 'Boot ISO not found.' } }, 404)
      if (request.method() === 'DELETE') {
        options.onBootISORequest?.('DELETE', path, null)
        if (bootISOView(iso).inUseBy > 0) {
          return json(route, { error: { code: 'conflict', message: 'boot ISO in use: 1 Server(s) have Boot Media enabled with it' } }, 409)
        }
        bootISOItems = bootISOItems.filter((item) => item.id !== iso.id)
        return route.fulfill({ status: 204 })
      }
      return json(route, bootISOView(iso))
    }
    if (path === '/api/v1/provisioning/templates' && request.method() === 'GET') {
      const siteId = url.searchParams.get('siteId')
      const integrationId = url.searchParams.get('integrationId')
      return json(route, deploymentTemplates.filter((item) => (
        (!siteId || item.siteId === siteId) &&
        (!integrationId || item.integrationId === integrationId)
      )))
    }
    if (path === '/api/v1/provisioning/templates' && request.method() === 'POST') {
      const body = request.postDataJSON() as Record<string, unknown>
      const created = {
        id: `template-${deploymentTemplates.length + 1}`,
        siteId: 'site-a',
        integrationId: String(body.integrationId),
        name: String(body.name),
        description: String(body.description ?? ''),
        imageId: String(body.imageId),
        network: body.network ?? { mode: 'dhcp', defaultGateway: false },
        ephemeral: Boolean(body.ephemeral),
        hasUserData: Boolean(body.userData),
        createdAt: now,
        updatedAt: now,
      }
      deploymentTemplates.push(created)
      return json(route, created, 201)
    }
    const templateUserDataMatch = path.match(/^\/api\/v1\/provisioning\/templates\/([^/]+)\/user-data$/)
    if (templateUserDataMatch) {
      const template = deploymentTemplates.find((item) => item.id === templateUserDataMatch[1])
      if (!template) return json(route, { error: { code: 'not_found', message: 'Template not found' } }, 404)
      template.hasUserData = request.method() === 'PUT'
      template.updatedAt = now
      return route.fulfill({ status: 204 })
    }
    const templateMatch = path.match(/^\/api\/v1\/provisioning\/templates\/([^/]+)$/)
    if (templateMatch) {
      const template = deploymentTemplates.find((item) => item.id === templateMatch[1])
      if (!template) return json(route, { error: { code: 'not_found', message: 'Template not found' } }, 404)
      if (request.method() === 'GET') return json(route, template)
      if (request.method() === 'PATCH') {
        const body = request.postDataJSON() as Record<string, unknown>
        Object.assign(template, body, { updatedAt: now })
        return json(route, template)
      }
      if (request.method() === 'DELETE') {
        deploymentTemplates = deploymentTemplates.filter((item) => item.id !== template.id)
        return route.fulfill({ status: 204 })
      }
    }
    if (path === '/api/v1/provisioning/deployments/preflight' && request.method() === 'POST') {
      const body = request.postDataJSON() as { serverIds: string[] }
      const issues = body.serverIds.flatMap((serverId) => {
        const message = options.deploymentReadinessIssues?.[serverId]
        const locked = fleet.find((server) => server.id === serverId)?.provisioning.locked
        if (locked) return [{ serverId, code: "locked", message: "Unlock the Server before deployment." }]
        return message ? [{
          serverId,
          code: 'provider_not_ready',
          message,
        }] : []
      })
      return json(route, {
        valid: issues.length === 0,
        integrationId: fleet.find((server) => body.serverIds.includes(server.id))?.source.integrationId ?? '',
        issues,
      })
    }

    if (path === '/api/v1/provisioning/networks/inspect' && request.method() === 'POST') {
      const body = request.postDataJSON() as { serverIds: string[] }
      const targets = body.serverIds.flatMap((serverId) => {
        const target = networkTargets.get(serverId)
        return target ? [target] : []
      })
      return json(route, {
        valid: targets.length === body.serverIds.length,
        targets,
        issues: body.serverIds
          .filter((serverId) => !networkTargets.has(serverId))
          .map((serverId) => ({ serverId, code: 'not_found', message: 'Server not found.' })),
      })
    }

    if (path === '/api/v1/provisioning/deployment-operations' && request.method() === 'POST') {
      const body = request.postDataJSON() as Record<string, unknown>
      options.onDeploymentRequest?.(body)
      const serverIds = body.serverIds as string[]
      const operationId = createProvisioningOperation('deploy-os', serverIds, body)
      return json(route, { operationId }, 202)
    }

    if (path === '/api/v1/provisioning/release-operations' && request.method() === 'POST') {
      const body = request.postDataJSON() as Record<string, unknown>
      const serverIds = body.serverIds as string[]
      const releaseBody = { ...body }
      delete releaseBody.serverIds
      for (const serverId of serverIds) {
        options.onServerReleaseRequest?.(serverId, releaseBody)
        if (options.serverActionFailureIds?.includes(serverId)) continue
        const server = fleet.find((item) => item.id === serverId)
        if (server) {
          if (!options.deferReleaseProjection) {
            server.provisioning.state = 'releasing'
            server.provisioning.stateSince = new Date().toISOString()
            server.provisioning.providerState = 'Releasing'
          }
          releaseRefreshesRemaining.set(serverId, options.releaseConvergesAfterRefreshes ?? 2)
        }
        if (body.unbindStaticIPs) {
          releaseCleanupRequested.add(serverId)
          provisioningTasks.unshift({
            id: `task-${serverId}`,
            serverId,
            integrationId: server?.source.integrationId ?? '',
            kind: 'release_network_cleanup',
            status: options.releaseCleanupFails ? 'failed' : 'running',
            phase: options.releaseCleanupFails ? 'cleaning_network' : 'waiting_for_ready',
            error: options.releaseCleanupFails ? 'MAAS refused to unlink the captured Static address.' : '',
            requestId: `req-release-${serverId}`,
            retryable: Boolean(options.releaseCleanupFails),
            createdAt: now,
            updatedAt: now,
          })
        }
      }
      const operationId = createProvisioningOperation('release-os', serverIds, body)
      return json(route, { operationId }, 202)
    }

    if (path === '/api/v1/provisioning/recover-operations' && request.method() === 'POST') {
      const body = request.postDataJSON() as Record<string, unknown>
      const serverIds = body.serverIds as string[]
      const recoverBody = { ...body }
      delete recoverBody.serverIds
      for (const serverId of serverIds) {
        options.onServerRecoverRequest?.(serverId, recoverBody)
        const server = fleet.find((item) => item.id === serverId)
        if (server) {
          // Recovery converges to the ready pool; model the same releasing -> ready path a
          // real Recover Operation drives so the list can follow the Server in place.
          server.provisioning.state = 'releasing'
          server.provisioning.stateSince = new Date().toISOString()
          server.provisioning.providerState = 'Releasing'
          releaseRefreshesRemaining.set(serverId, options.releaseConvergesAfterRefreshes ?? 2)
        }
      }
      const operationId = createProvisioningOperation('recover-server', serverIds, body)
      return json(route, { operationId }, 202)
    }

    if (path === '/api/v1/provisioning/deployments' && request.method() === 'POST') {
      const body = request.postDataJSON() as Record<string, unknown>
      options.onDeploymentRequest?.(body)
      const serverIds = body.serverIds as string[]
      const failedIds = new Set(options.deploymentFailureIds ?? [])
      const accepted = serverIds.filter((id) => !failedIds.has(id)).map((serverId) => {
        const server = fleet.find((item) => item.id === serverId)
        if (server) {
          server.provisioning.state = 'deploying'
          server.provisioning.providerState = 'Deploying'
          server.provisioning.observedAt = now
          if (options.deploymentConvergesAfterRefreshes !== undefined) {
            deploymentRefreshesRemaining.set(serverId, Math.max(1, options.deploymentConvergesAfterRefreshes))
          }
        }
        return {
          serverId,
          state: 'deploying',
          providerState: 'Deploying',
          powerState: server?.provisioning.powerState ?? 'unknown',
          osSystem: 'ubuntu',
          distroSeries: 'ubuntu/jammy',
          ephemeral: false,
          hweKernel: '',
          locked: false,
          commissioningStatus: 'passed',
          testingStatus: 'passed',
          observedAt: now,
        }
      })
      return json(route, {
        requested: serverIds.length,
        accepted,
        failed: serverIds.filter((id) => failedIds.has(id)).map((serverId) => ({
          serverId,
          code: retryable ? 'provider_unavailable' : 'provider_rejected',
          message: 'Machine reservation changed.',
          stage: 'deployment',
        })),
      }, 202)
    }

    if (path === '/api/v1/overview') {
      const siteId = url.searchParams.get('siteId')
      return json(route, {
        generatedAt: now, scope: { siteId },
        inventory: { sites: siteId ? 1 : 2, servers: 4, absent: 0, deployed: 4, platformed: 4, gpuDevices: 24, health: { up: 3, down: 1, unknown: 0 } },
        integrations: { total: integrationItems.length, failing: 1, items: integrationItems.map((item) => ({ id: item.id, siteId: item.siteId, name: item.name, kind: item.kind, providerKind: item.providerKind, enabled: item.enabled, lastSucceededAt: item.sync.lastSucceededAt, lastError: item.sync.lastError })) },
        platforms: { total: 2, unreachable: 1, unmatchedMembers: 1 }, operations: { active: 1, failedLast24Hours: 1, recent: operations },
        monitoring: { available: true, error: null, firing: { critical: 1, warning: 1, items: alerts.filter((alert) => alert.state === 'firing') } },
      })
    }

    if (path === '/api/v1/servers') {
      serverListRequestCount += 1
      options.onServerListRequest?.(serverListRequestCount)
      if (options.serverListFailureRequestNumbers?.includes(serverListRequestCount)) {
        return json(route, { error: { code: 'unavailable', message: 'Server inventory refresh failed.' } }, 503)
      }
      if (serverListRequestCount > 1 && serverListFailuresRemaining > 0) {
        serverListFailuresRemaining -= 1
        return json(route, { error: { code: 'unavailable', message: 'Server inventory refresh failed.' } }, 503)
      }
      let items = [...fleet]
      const siteId = url.searchParams.get('siteId'); const platformId = url.searchParams.get('platformId'); const provisioningState = url.searchParams.get('provisioningState'); const keyword = url.searchParams.get('keyword')?.toLowerCase()
      const includeAbsent = url.searchParams.get('includeAbsent') === 'true'
      if (!includeAbsent) items = items.filter((item) => !item.absent)
      if (siteId) items = items.filter((item) => item.source.siteId === siteId)
      if (platformId) items = items.filter((item) => item.membership?.platformId === platformId)
      if (provisioningState) items = items.filter((item) => item.provisioning?.state === provisioningState)
      if (keyword) items = items.filter((item) => [item.hostname, item.fqdn, item.id, ...item.addresses].some((value) => value?.toLowerCase().includes(keyword)))
      const pageNumber = Number(url.searchParams.get('page') ?? 1); const pageSize = Number(url.searchParams.get('pageSize') ?? 100); const start = (pageNumber - 1) * pageSize
      return json(route, { items: items.slice(start, start + pageSize), total: items.length, page: pageNumber, pageSize })
    }
    const serverEventsMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/events$/)
    if (serverEventsMatch && request.method() === 'GET') {
      return json(route, {
        supported: true,
        events: [{
          id: '4812', level: 'audit', type: 'Request from user',
          message: 'Started releasing machine.', actor: 'admin',
          occurredAt: '2026-08-27T02:58:00Z',
        }],
      })
    }
    const serverTasksMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/provisioning-tasks$/)
    if (serverTasksMatch && request.method() === 'GET') {
      return json(route, provisioningTasks.filter((task) => task.serverId === serverTasksMatch[1]))
    }
    const taskMatch = path.match(/^\/api\/v1\/provisioning\/tasks\/([^/]+)$/)
    if (taskMatch && request.method() === 'GET') {
      const task = provisioningTasks.find((item) => item.id === taskMatch[1])
      if (!task) return json(route, { error: { code: 'not_found', message: 'Provisioning task not found.' } }, 404)
      const server = fleet.find((item) => item.id === task.serverId)
      if (task.status === 'running' && server?.provisioning.state === 'ready') {
        Object.assign(task, {
          status: 'succeeded',
          phase: 'complete',
          error: '',
          retryable,
          updatedAt: now,
        })
        server.addresses = []
        const target = networkTargets.get(server.id)
        const iface = target?.network.interfaces[0]
        if (iface) {
          iface.links = iface.links.filter((link) => link.configurationState !== 'static')
          iface.configurationState = iface.links[0]?.configurationState ?? 'unconfigured'
          iface.rawProviderMode = iface.links[0]?.rawProviderMode ?? ''
        }
      }
      return json(route, task)
    }
    const taskRetryMatch = path.match(/^\/api\/v1\/provisioning\/tasks\/([^/]+)\/retry$/)
    if (taskRetryMatch && request.method() === 'POST') {
      const task = provisioningTasks.find((item) => item.id === taskRetryMatch[1])
      if (!task) return json(route, { error: { code: 'not_found', message: 'Provisioning task not found.' } }, 404)
      Object.assign(task, { status: 'pending', phase: 'waiting_for_ready', error: '', retryable: false, updatedAt: now })
      return json(route, task, 202)
    }
    const serverNetworkMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/network$/)
    if (serverNetworkMatch && request.method() === 'GET') {
      const target = networkTargets.get(serverNetworkMatch[1])
      return target ? json(route, target) : json(route, { error: { code: 'not_found', message: 'Server not found.' } }, 404)
    }
    const networkLinkMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/network\/interfaces\/([^/]+)\/links(?:\/([^/]+))?$/)
    if (networkLinkMatch) {
      const [, serverId, interfaceId, linkId] = networkLinkMatch
      const target = networkTargets.get(serverId)
      if (!target) return json(route, { error: { code: 'not_found', message: 'Server not found.' } }, 404)
      const body = request.postData() ? request.postDataJSON() as Record<string, unknown> : null
      options.onNetworkLinkRequest?.(request.method(), serverId, interfaceId, linkId ?? null, body)
      const iface = target.network.interfaces.find((item) => item.id === interfaceId)
      if (!iface) return json(route, { error: { code: 'not_found', message: 'Interface not found.' } }, 404)
      if (request.method() === 'DELETE') {
        iface.links = iface.links.filter((link) => link.id !== linkId)
        iface.configurationState = iface.links.length ? iface.links[0].configurationState : 'unconfigured'
        iface.rawProviderMode = iface.links.length ? iface.links[0].rawProviderMode : ''
        return json(route, target)
      }
      const mode = String(body?.mode ?? 'dhcp')
      const configurationState = mode === 'link_only' ? 'link_only' : mode
      const replacement = {
        id: linkId ?? `link-${serverId}-${iface.links.length + 1}`,
        configurationState,
        rawProviderMode: mode === 'dhcp' ? 'DHCP' : mode === 'static' ? 'STATIC' : 'LINK_UP',
        subnetId: String(body?.subnetId ?? ''),
        subnetName: 'lab-network',
        cidr: '192.168.40.0/24',
        ipAddress: mode === 'static' ? String(body?.ipAddress ?? '') : '',
        defaultGateway: Boolean(body?.defaultGateway),
      }
      if (linkId) iface.links = iface.links.map((link) => link.id === linkId ? replacement : link)
      else iface.links = [...iface.links, replacement]
      iface.configurationState = configurationState
      iface.rawProviderMode = replacement.rawProviderMode
      return json(route, target)
    }
    const serverRefreshMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/refresh$/)
    if (serverRefreshMatch && request.method() === "POST") {
      const serverId = serverRefreshMatch[1]
      options.onServerRefreshRequest?.(serverId)
      observeRelease(serverId)
      observeDeployment(serverId)
      const server = fleet.find((item) => item.id === serverId)
      if (!server) return json(route, { error: { code: "not_found", message: "Server not found" } }, 404)
      return json(route, {
        serverId, state: server.provisioning.state, providerState: server.provisioning.providerState,
        powerState: server.provisioning.powerState, osSystem: server.provisioning.osSystem,
        distroSeries: server.provisioning.distroSeries, ephemeral: server.provisioning.ephemeral,
        hweKernel: server.provisioning.hweKernel, locked: server.provisioning.locked,
        commissioningStatus: server.provisioning.commissioningStatus,
        testingStatus: server.provisioning.testingStatus, observedAt: now,
      })
    }
    const serverActionMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/([^/]+)$/)
    if (serverActionMatch && request.method() === "POST") {
      const [serverId, action] = serverActionMatch.slice(1)
      if (action === "release") {
        const body = request.postData() ? request.postDataJSON() as Record<string, unknown> : null
        options.onServerReleaseRequest?.(serverId, body)
      }
      const actionServer = fleet.find((item) => item.id === serverId)
      if (actionServer && action === "lock") actionServer.provisioning.locked = true
      if (actionServer && action === "unlock") actionServer.provisioning.locked = false
      if (options.serverActionFailureIds?.includes(serverId)) {
        return json(route, {
          error: {
            code: "validation_error",
            message: "MAAS refused the request: Machine cannot be released while a hosted VM is running.",
            requestId: "req-" + action + "-" + serverId,
          },
        }, 400)
      }
      if (action === "release") {
        const server = fleet.find((item) => item.id === serverId)
        if (server) {
          server.provisioning.state = "releasing"
          server.provisioning.stateSince = new Date().toISOString()
          server.provisioning.providerState = "Releasing"
          releaseRefreshesRemaining.set(serverId, options.releaseConvergesAfterRefreshes ?? 2)
        }
        const body = request.postData() ? request.postDataJSON() as Record<string, unknown> : null
        if (body?.unbindStaticIPs) {
          provisioningTasks.unshift({
            id: `task-${serverId}`,
            serverId,
            integrationId: server?.source.integrationId ?? '',
            kind: 'release_network_cleanup',
            status: options.releaseCleanupFails ? 'failed' : 'running',
            phase: options.releaseCleanupFails ? 'cleaning_network' : 'waiting_for_ready',
            error: options.releaseCleanupFails ? 'MAAS refused to unlink the captured Static address.' : '',
            requestId: `req-release-${serverId}`,
            retryable: Boolean(options.releaseCleanupFails),
            createdAt: now,
            updatedAt: now,
          })
        }
      }
      const releaseBody = action === 'release' && request.postData()
        ? request.postDataJSON() as Record<string, unknown>
        : null
      return json(route, {
        serverId, state: action === "release" ? "releasing" : "deployed", providerState: "Accepted",
        powerState: "off", osSystem: "ubuntu", distroSeries: "jammy", ephemeral: false,
        hweKernel: "", locked: actionServer?.provisioning.locked ?? false, commissioningStatus: "", testingStatus: "", observedAt: now,
        ...(releaseBody?.unbindStaticIPs ? { taskId: `task-${serverId}` } : {}),
      }, 202)
    }
    if (/\/api\/v1\/servers\/[^/]+\/provisioner-detail$/.test(path)) return json(route, { capabilities: { ephemeralDeploy: true, power: true, hardwareValidation: true, operatorState: true, machineDetail: true, hardwareInventory: true, machineRemoval: true, releaseOptions: true, networkConfiguration: true }, sections: [{ title: 'System', fields: [{ label: 'System vendor', value: 'Supermicro' }, { label: 'Serial', value: 'SN0001' }] }, { title: 'BMC', fields: [{ label: 'Protocol', value: 'IPMI' }, { label: 'Address', value: '192.0.2.20' }, { label: 'Username', value: 'bmc-admin' }, { label: 'Password', value: 'bmc-secret' }, { label: 'Driver', value: 'LAN_2_0' }, { label: 'Boot type', value: 'efi' }, { label: 'Privilege level', value: 'OPERATOR' }, { label: 'Cipher suite', value: '17' }, { label: 'Power MAC', value: 'aa:bb:cc:dd:ee:ff' }] }], tables: [{ title: 'Storage', columns: ['Device', 'Size', 'Model'], rows: [['nvme0n1', '3.84 TB', 'PM1733']] }, { title: 'PCI devices', columns: ['Address', 'Device', 'Vendor'], rows: [['03:00.0', 'MI300X', 'AMD']] }] })
    // Boot Media (server-detail-actions.md "Boot Media"): the real API drives the BMC over Redfish;
    // the fixture records writes and keeps one per-page setting so the Summary re-reads it.
    const bootMediaMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/(boot-media|redfish\/probe)$/)
    if (bootMediaMatch) {
      const body = request.method() === 'GET' ? null : ((request.postDataJSON() ?? {}) as Record<string, unknown>)
      if (request.method() !== 'GET') options.onBootMediaRequest?.(request.method(), path, body)
      if (bootMediaMatch[2] === 'redfish/probe') {
        bootMediaState.redfish = { ...bootMediaState.redfish, probedAt: now }
        return json(route, { redfish: bootMediaState.redfish })
      }
      if (request.method() === 'PUT') {
        if (options.bootMediaError) {
          const { status, code, message } = options.bootMediaError
          return json(route, { error: { code, message, requestId: 'req-boot-media' } }, status)
        }
        const enabled = body?.enabled === true
        // The API's enable gates (server-detail-actions.md): an isoId is required and must name a
        // Boot ISO of srv-1's own provisioner (MAAS Taipei).
        const isoId = typeof body?.isoId === 'string' ? body.isoId : ''
        if (enabled) {
          const iso = bootISOItems.find((item) => item.id === isoId)
          if (!isoId) return json(route, { error: { code: 'validation_error', message: 'Choose a Boot ISO to enable Boot Media.' } }, 400)
          if (!iso) return json(route, { error: { code: 'not_found', message: 'Boot ISO not found.' } }, 404)
          if (iso.integrationId !== 'maas-a') {
            return json(route, { error: { code: 'validation_error', message: "The Boot ISO was built for another provisioner than the Server's." } }, 400)
          }
        }
        if (enabled && options.bootMediaApplyGate) {
          // Real timestamps: the dashboard counts the settle wait down against the browser clock.
          const at = (offsetMs: number) => new Date(Date.now() + offsetMs).toISOString()
          bootMediaState.apply = { isoId, phase: 'settling', startedAt: at(-60_000), phaseStartedAt: at(-10_000), phaseEndsAt: at(170_000) }
          await options.bootMediaApplyGate
          bootMediaState.apply = null
        }
        const keptISO = enabled ? isoId : bootMediaState.setting?.isoId
        bootMediaState.setting = enabled
          ? { enabled: true, isoId, updatedAt: now, lastAppliedAt: now, lastAppliedBy: 'preflight', bootOverride: 'Continuous', lastErrorAt: null }
          : { enabled: false, ...(keptISO ? { isoId: keptISO } : {}), updatedAt: now, lastAppliedAt: now, lastAppliedBy: 'preflight', bootOverride: 'Continuous', lastErrorAt: null }
        const disableOutcome = enabled ? {} : options.bootMediaRevertError ? { reverted: false, revertError: options.bootMediaRevertError } : { reverted: true }
        return json(route, { ...bootMediaView(false), ...disableOutcome })
      }
      return json(route, bootMediaView(url.searchParams.get('live') === 'true'))
    }
    // Server Default User (server-detail-actions.md): PUT verifies on the host in the real API; the
    // fixture just records the request and applies the result to the projection.
    const defaultUserMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/default-user$/)
    if (defaultUserMatch) {
      const item = fleet.find((entry) => entry.id === decodeURIComponent(defaultUserMatch[1]))
      if (!item) return json(route, { error: { code: 'not_found', message: 'Server not found.' } }, 404)
      if (request.method() === 'PUT') {
        const body = (request.postDataJSON() ?? {}) as { user?: string; password?: string }
        options.onDefaultUserRequest?.('PUT', body)
        if (options.defaultUserError) {
          const { status, code, message } = options.defaultUserError
          return json(route, { error: { code, message, requestId: 'req-default-user' } }, status)
        }
        item.defaultUser = { user: String(body.user ?? ''), source: 'server' }
        return json(route, { defaultUser: item.defaultUser, keyInstalled: Boolean(body.password), sudo: options.defaultUserSudo ?? 'passwordless' })
      }
      if (request.method() === 'DELETE') {
        options.onDefaultUserRequest?.('DELETE', null)
        const imageUser = item.provisioning?.deployedImageDefaultUser
        item.defaultUser = imageUser ? { user: imageUser, source: 'os_image' } : undefined
        return route.fulfill({ status: 204 })
      }
    }
    const serverMatch = path.match(/^\/api\/v1\/servers\/([^/]+)$/)
    if (serverMatch) {
      const index = fleet.findIndex((item) => item.id === serverMatch[1])
      if (request.method() === 'DELETE') {
        if (index < 0) return json(route, { error: { code: 'not_found', message: 'Server not found' } }, 404)
        fleet.splice(index, 1)
        return route.fulfill({ status: 204 })
      }
      return index >= 0 ? json(route, fleet[index]) : json(route, { error: { code: 'not_found', message: 'Server not found' } }, 404)
    }

    if (path === '/api/v1/monitoring/alerts' && request.method() === 'GET') {
      const severity = url.searchParams.get('severity'); const state = url.searchParams.get('state'); const serverId = url.searchParams.get('serverId')
      return json(route, alerts.filter((alert) => (!severity || alert.severity === severity) && (!state || alert.state === state) && (!serverId || alert.serverId === serverId)))
    }
    if (/\/api\/v1\/monitoring\/alerts\/[^/]+\/acknowledge$/.test(path) && request.method() === 'POST') {
      if (options.acknowledgeFails) return json(route, { error: { code: 'provider_unavailable', message: 'Alertmanager is unavailable' } }, 503)
      return json(route, { silenceId: 'silence-e2e' })
    }
    if (path === '/api/v1/monitoring/metrics') {
      const ids = (url.searchParams.get('serverIds') ?? '').split(',').filter(Boolean)
      const batch = metricBatchIndex++
      options.onMetricsRequest?.(ids)
      activeMetricRequests += 1; options.onMetricsActive?.(activeMetricRequests)
      if (options.metricsDelayMs) await new Promise((resolve) => setTimeout(resolve, options.metricsDelayMs))
      activeMetricRequests -= 1
      if (batch === options.failMetricsBatchIndex) return json(route, { error: { code: 'provider_unavailable', message: 'Prometheus batch failed' } }, 503)
      return json(route, { items: ids.map((serverId, index) => ({ serverId, metrics: { cpuUsagePercent: 30 + (index % 20), memoryUsedPercent: 60 + (index % 10), ...(Number(serverId.split('-')[1]) <= 4 ? { gpuUtilizationPercent: 88.1, gpuTemperatureCelsius: 71.3 } : {}) } })), grafana: 'https://grafana.example' })
    }

    if (path === '/api/v1/platforms/deployment-requirements/slurm') {
      if (options.slurmRequirementFails && request.method() === 'GET') {
        return json(route, { error: { code: 'provider_error', message: 'Requirement store unavailable' } }, 503)
      }
      if (request.method() === 'PUT') {
        const body = request.postDataJSON() as { minimumResources: typeof slurmRequirement.minimumResources }
        slurmRequirement = { platformType: 'slurm', minimumResources: body.minimumResources, updatedAt: now }
      }
      return json(route, slurmRequirement)
    }

    if (path === '/api/v1/platforms/deploy' && request.method() === 'POST') {
      const rawBody = request.postDataJSON() as Record<string, unknown>
      options.onPlatformDeploymentRequest?.(rawBody)
      const body = rawBody as {
        name: string
        type?: 'kubernetes' | 'slurm'
        gpuStackOwner?: string
        roleAssignments?: Array<{
          serverId: string
          role: 'control-plane' | 'worker'
          runWorkloads?: boolean
        }>
        slurm?: {
          nodeAssignments: Array<{ serverId: string; controller: boolean; compute: boolean }>
        }
      }
      const operationId = 'op-platform-new'
      if (body.type === 'slurm') {
        // Slurm deploy uses per-daemon node assignments and starts a configure-slurm Operation.
        const targetServerIds = (body.slurm?.nodeAssignments ?? []).map((node) => node.serverId)
        operationItems.unshift({
          ...operations[0],
          id: operationId,
          kind: 'configure-slurm',
          intent: 'Deploy ' + body.name,
          platformId: 'platform-new',
          targetServerIds,
        })
        platformItems.push({
          id: 'platform-new',
          siteId: 'site-a',
          name: body.name,
          type: 'slurm',
          integrationId: null,
          origin: 'deployed',
          lifecycleState: 'deploying',
          lifecycleOperationId: operationId,
          deployment: null,
          gpuStackOwner: body.gpuStackOwner ?? 'provisioning',
          exporterOwner: 'ansible',
          sync: { lastStartedAt: null, lastSucceededAt: null, lastError: null, memberCount: 0, matchedCount: 0 },
          createdAt: now,
          updatedAt: now,
        })
        return json(route, { platformId: 'platform-new', operationId }, 202)
      }
      const roleAssignments = body.roleAssignments ?? []
      const controllerCount = roleAssignments.filter(
        (assignment) => assignment.role === 'control-plane',
      ).length
      const topology: 'standalone' | 'multi-node' | 'high-availability' =
        controllerCount >= 3
          ? 'high-availability'
          : roleAssignments.length === 1
            ? 'standalone'
            : 'multi-node'
      operationItems.unshift({
        ...operations[0],
        id: operationId,
        intent: 'Deploy ' + body.name,
        platformId: 'platform-new',
        targetServerIds: roleAssignments.map((assignment) => assignment.serverId),
      })
      platformItems.push({
        id: 'platform-new',
        siteId: 'site-a',
        name: body.name,
        type: 'kubernetes',
        integrationId: null,
        origin: 'deployed',
        lifecycleState: 'deploying',
        lifecycleOperationId: operationId,
        deployment: { topology, roleAssignments },
        gpuStackOwner: body.gpuStackOwner ?? 'provisioning',
        exporterOwner: 'ansible',
        sync: { lastStartedAt: null, lastSucceededAt: null, lastError: null, memberCount: 0, matchedCount: 0 },
        createdAt: now,
        updatedAt: now,
      })
      return json(route, { platformId: 'platform-new', operationId }, 202)
    }
    if (path === '/api/v1/platforms') {
      const siteId = url.searchParams.get('siteId')
      return json(route, platformItems.filter((item) => !siteId || item.siteId === siteId))
    }
    // Live Slurm cluster read. Returns a Slurm-native cluster only for the deployed HA fixture;
    // any other platform answers 422 so the Slurm view exercises its degrade path.
    const slurmClusterMatch = path.match(/^\/api\/v1\/platforms\/([^/]+)\/slurm$/)
    if (slurmClusterMatch && request.method() === 'GET') {
      if (options.slurmDeployed && slurmClusterMatch[1] === 'platform-slurm-ha') {
        return json(route, {
          controllers: [
            { hostname: 'slurm-ctl-01', primary: true, status: 'up' },
            { hostname: 'slurm-ctl-02', primary: false, status: 'up' },
          ],
          partitions: [{ name: 'main', state: 'up', nodeSpec: 'slurm-cpt-0[1-2]', totalNodes: 2 }],
          nodes: [
            { name: 'slurm-cpt-01', state: 'idle', cpus: 64, realMemoryMiB: 524288, gres: 'gpu:8', partitions: ['main'], address: '192.168.60.21' },
            { name: 'slurm-cpt-02', state: 'allocated', cpus: 64, realMemoryMiB: 524288, gres: 'gpu:8', partitions: ['main'], address: '192.168.60.22' },
          ],
        })
      }
      return json(route, { error: { code: 'validation_error', message: 'no live Slurm state' } }, 422)
    }
    // Live Kubernetes cluster explorer. Answers for the deployed Kubernetes fixture (platform-a)
    // with canned live data so the explorer tabs render; any other platform 404s so the tabs are
    // hidden (matching the deployed + credential eligibility gate).
    const kubernetesMatch = path.match(/^\/api\/v1\/platforms\/([^/]+)\/kubernetes(\/.*)?$/)
    if (kubernetesMatch) {
      const platformId = kubernetesMatch[1]
      const sub = kubernetesMatch[2] ?? ''
      if (platformId !== 'platform-a') {
        return json(route, { error: { code: 'not_found', message: 'no cluster explorer' } }, 404)
      }
      if (sub === '' && request.method() === 'GET') {
        return json(route, { version: 'v1.30.2+k0s', nodeCount: 3, readyNodeCount: 3, namespaceCount: 6 })
      }
      if (sub === '/nodes' && request.method() === 'GET') {
        return json(route, {
          items: [
            { name: 'gpu-node-01', role: 'control-plane', ready: true, unschedulable: false, serverId: 'srv-1', addresses: ['192.168.40.21'], kubeletVersion: 'v1.30.2+k0s' },
            { name: 'gpu-node-02', role: 'worker', ready: true, unschedulable: false, serverId: 'srv-2', addresses: ['192.168.40.22'], kubeletVersion: 'v1.30.2+k0s' },
          ],
        })
      }
      const cordonMatch = sub.match(/^\/nodes\/([^/]+)\/(cordon|uncordon)$/)
      if (cordonMatch && request.method() === 'POST') {
        return json(route, { name: cordonMatch[1], role: 'worker', ready: true, unschedulable: cordonMatch[2] === 'cordon', serverId: null, addresses: [], kubeletVersion: 'v1.30.2+k0s' })
      }
      if (sub === '/namespaces' && request.method() === 'GET') {
        return json(route, {
          items: [
            { name: 'default', phase: 'Active', system: false },
            { name: 'web', phase: 'Active', system: false },
            { name: 'kube-system', phase: 'Active', system: true },
          ],
        })
      }
      if (sub === '/namespaces' && request.method() === 'POST') {
        const body = request.postDataJSON() as { name: string }
        return json(route, { name: body.name, phase: 'Active', system: false }, 201)
      }
      if (/^\/namespaces\/[^/]+$/.test(sub) && request.method() === 'DELETE') {
        return json(route, { success: true })
      }
      if (sub === '/applications' && request.method() === 'GET') {
        return json(route, {
          items: [
            { namespace: 'web', name: 'nginx', kind: 'Deployment', images: ['nginx:1.27'], replicas: 3, readyReplicas: 3, createdAt: now },
            { namespace: 'web', name: 'debug', kind: 'Pod', images: ['busybox'], replicas: 1, readyReplicas: 1, createdAt: now },
          ],
        })
      }
      const appMatch = sub.match(/^\/applications\/([^/]+)\/([^/]+)\/([^/]+)(\/(scale|restart))?$/)
      if (appMatch) {
        if (appMatch[5] === 'scale' && request.method() === 'POST') {
          const body = request.postDataJSON() as { replicas: number }
          return json(route, { namespace: appMatch[1], name: appMatch[3], kind: appMatch[2], images: [], replicas: body.replicas, readyReplicas: body.replicas, createdAt: now })
        }
        if (appMatch[5] === 'restart' && request.method() === 'POST') {
          return json(route, { success: true })
        }
        if (!appMatch[5] && request.method() === 'DELETE') {
          return json(route, { success: true })
        }
        if (!appMatch[5] && request.method() === 'GET') {
          return json(route, {
            namespace: appMatch[1], name: appMatch[3], kind: appMatch[2], images: ['nginx:1.27'], replicas: 3, readyReplicas: 3, createdAt: now,
            pods: [{ namespace: appMatch[1], name: `${appMatch[3]}-abcde`, phase: 'Running', ready: true, nodeName: 'gpu-node-02', restarts: 0, containers: ['nginx'], startedAt: now }],
          })
        }
      }
      const logsMatch = sub.match(/^\/pods\/([^/]+)\/([^/]+)\/logs$/)
      if (logsMatch && request.method() === 'GET') {
        return json(route, { container: 'nginx', logs: 'listening on :80\nready' })
      }
      if (sub === '/apply' && request.method() === 'POST') {
        return json(route, { results: [{ kind: 'Deployment', namespace: 'web', name: 'nginx', action: 'configured' }] })
      }
      // Subsidiary resource lists default to empty in the fixture.
      if (['/services', '/ingresses', '/configmaps', '/secrets', '/persistentvolumeclaims', '/pods'].includes(sub) && request.method() === 'GET') {
        return json(route, { items: [] })
      }
      return json(route, { items: [] })
    }
    const uninstallMatch = path.match(/^\/api\/v1\/platforms\/([^/]+)\/uninstall$/)
    if (uninstallMatch && request.method() === 'POST') {
      options.onPlatformUninstallRequest?.(
        uninstallMatch[1],
        (request.postDataJSON() as Record<string, unknown> | null) ?? null,
      )
      return json(route, { platformId: uninstallMatch[1], operationId: 'op-uninstall' }, 202)
    }
    const platformMatch = path.match(/^\/api\/v1\/platforms\/([^/]+)$/)
    if (platformMatch && request.method() === 'DELETE') {
      const index = platformItems.findIndex((platform) => platform.id === platformMatch[1])
      if (index < 0) return json(route, { error: { code: 'not_found', message: 'Platform not found' } }, 404)
      platformItems.splice(index, 1)
      return json(route, { success: true })
    }
    if (platformMatch) {
      const platform = platformItems.find((item) => item.id === platformMatch[1])
      return platform
        ? json(route, platform)
        : json(route, { error: { code: 'not_found', message: 'Platform not found' } }, 404)
    }
    // The dashboard reads a single Operation as a Workflow (ADR 017 rename). Serve it by id so
    // deployment claim resolution can read a Platform's target history.
    // Canonical Workflow listing: filtering and pagination stay server-owned so dashboard
    // behavior tests exercise the same contract as production rather than client-side narrowing.
    if (path === '/api/v1/workflows' && request.method() === 'GET') {
      let items = [...operationItems]
      const siteId = url.searchParams.get('siteId')
      const status = url.searchParams.get('status')
      const platformId = url.searchParams.get('platformId')
      const serverId = url.searchParams.get('serverId')
      const kind = url.searchParams.get('kind')
      const active = url.searchParams.get('active') === 'true'
      const terminal = new Set(['succeeded', 'failed', 'partially_succeeded', 'canceled', 'indeterminate'])
      if (siteId) items = items.filter((item) => item.siteId === siteId)
      if (status) items = items.filter((item) => (item.status ?? item.execution.status) === status)
      if (platformId) items = items.filter((item) => item.platformId === platformId)
      if (serverId) items = items.filter((item) => item.targetServerIds.includes(serverId))
      if (kind) items = items.filter((item) => item.kind === kind)
      if (active) items = items.filter((item) => !terminal.has(item.status ?? item.execution.status))
      items.sort((left, right) => right.requestedAt.localeCompare(left.requestedAt))
      const total = items.length
      const page = Math.max(1, Number(url.searchParams.get('page')) || 1)
      const pageSize = Math.max(1, Number(url.searchParams.get('pageSize')) || 30)
      const offset = (page - 1) * pageSize
      return json(route, { items: items.slice(offset, offset + pageSize), total, page, pageSize })
    }
    const workflowByIdMatch = path.match(/^\/api\/v1\/workflows\/([^/]+)$/)
    if (workflowByIdMatch && request.method() === 'GET') {
      const operation = operationItems.find((item) => item.id === workflowByIdMatch[1])
      return operation
        ? json(route, operation)
        : json(route, { error: { code: 'not_found', message: 'Workflow not found' } }, 404)
    }
    // Canonical Task-retry (ADR 017 rename of /operations/{id}/steps/{stepId}/retry). When
    // deployExecutionLost is set it answers 409 to model a lost Workflow execution, which drives
    // the Repair action into its rerun fallback.
    const workflowTaskRetryMatch = path.match(/^\/api\/v1\/workflows\/([^/]+)\/tasks\/([^/]+)\/retry$/)
    if (workflowTaskRetryMatch && request.method() === 'POST') {
      if (options.deployExecutionLost) {
        return json(route, { error: { code: 'conflict', message: 'The operation\'s workflow execution is no longer running, so this Step cannot be retried.' } }, 409)
      }
      const operation = operationItems.find((item) => item.id === workflowTaskRetryMatch[1]) as unknown as Record<string, unknown> | undefined
      const steps = operation?.steps as Array<Record<string, unknown>> | undefined
      const step = steps?.find((item) => item.id === workflowTaskRetryMatch[2])
      if (!operation || !step) return json(route, { error: { code: 'not_found', message: 'Operation Step not found' } }, 404)
      step.attempt = Number(step.attempt) + 1
      step.status = 'pending'
      step.progress = 0
      step.error = null
      step.finishedAt = null
      operation.status = 'running'
      operation.execution = { ...(operation.execution as Record<string, unknown>), status: 'running' }
      operation.updatedAt = now
      if (operation.kind === 'deploy-kubernetes') {
        const platform = platformItems.find((item) => item.id === operation.platformId)
        if (platform) {
          platform.lifecycleState = 'deploying'
          platform.updatedAt = now
        }
      }
      return json(route, { workflowId: workflowTaskRetryMatch[1], taskId: workflowTaskRetryMatch[2] }, 202)
    }
    // Rerun recovery: launch a new Operation for the same intent on the same Platform, linked to
    // the original, and hand the platform lifecycle to it (mirrors the backend Rerun contract).
    const rerunMatch = path.match(/^\/api\/v1\/workflows\/([^/]+)\/rerun$/)
    if (rerunMatch && request.method() === 'POST') {
      const original = operationItems.find((item) => item.id === rerunMatch[1])
      if (!original) return json(route, { error: { code: 'not_found', message: 'Operation not found' } }, 404)
      const rerun = {
        ...original,
        id: 'op-rerun',
        retryOfOperationId: original.id,
        status: 'pending',
        statusReason: null,
        execution: { ...original.execution, runId: 'run-rerun', status: 'pending', statusReason: null, startedAt: now, finishedAt: null },
        requestedAt: now,
        updatedAt: now,
      }
      operationItems.unshift(rerun)
      const platform = platformItems.find((item) => item.id === original.platformId)
      if (platform) {
        platform.lifecycleState = 'deploying'
        platform.lifecycleOperationId = rerun.id
        platform.updatedAt = now
      }
      return json(route, rerun, 202)
    }
    if (path === '/api/v1/operations') {
      let items = [...operationItems]
      const status = url.searchParams.get('status')
      const platformId = url.searchParams.get('platformId')
      const serverId = url.searchParams.get('serverId')
      if (status) items = items.filter((item) => item.execution.status === status)
      if (platformId) items = items.filter((item) => item.platformId === platformId)
      if (serverId) items = items.filter((item) => item.targetServerIds.includes(serverId))
      return json(route, { items, total: items.length, page: 1, pageSize: 30 })
    }
    const timelineMatch = path.match(/^\/api\/v1\/operations\/([^/]+)\/timeline$/)
    if (timelineMatch) {
      return json(route, [{
        id: `timeline-${timelineMatch[1]}`,
        operationId: timelineMatch[1],
        type: 'operation_requested',
        message: 'Operation accepted and waiting for durable workflow start.',
        createdAt: now,
      }])
    }
    if (path.endsWith('/events')) return json(route, { runId: 'run-1024', status: 'running', okCount: 4, changedCount: 2, failedCount: 1, events: [{ play: 'Prepare hosts', task: 'Gather facts', host: 'gpu-node-01', status: 'ok', changed: false, startedAt: now, endedAt: now }, { play: 'Install k0s', task: 'Write configuration', host: 'gpu-node-02', status: 'changed', changed: true, startedAt: now, endedAt: now }, { play: 'Install k0s', task: 'Start controller', host: 'gpu-node-04', status: 'failed', changed: false, startedAt: now, endedAt: now }] })
    if (path.endsWith('/logs')) return route.fulfill({ status: 200, contentType: 'text/plain', body: 'PLAY [Prepare hosts]\nTASK [Gather facts]\nok: [gpu-node-01]\nTASK [Write configuration]\nchanged: [gpu-node-02]\nTASK [Start controller]\nfatal: [gpu-node-04]: UNREACHABLE\n' })
    if (path.endsWith('/artifacts')) return json(route, null)
    const cancelMatch = path.match(/^\/api\/v1\/workflows\/([^/]+)\/cancel$/)
    if (cancelMatch && request.method() === 'POST') {
      const operation = operationItems.find((item) => item.id === cancelMatch[1]) as unknown as Record<string, unknown> | undefined
      if (!operation) return json(route, { error: { code: 'not_found', message: 'Operation not found' } }, 404)
      const cancelStatus = options.cancelMarksTerminal ? 'canceled' : 'canceling'
      const cancelReason = options.cancelMarksTerminal ? 'Canceled by operator.' : 'Canceling active work.'
      operation.status = cancelStatus
      operation.statusReason = cancelReason
      if (options.cancelMarksTerminal) operation.finishedAt = now
      operation.execution = {
        ...(operation.execution as Record<string, unknown>),
        status: cancelStatus,
        statusReason: cancelReason,
        ...(options.cancelMarksTerminal ? { finishedAt: now } : {}),
      }
      return json(route, { operationId: cancelMatch[1] }, 202)
    }
    const stepRetryMatch = path.match(/^\/api\/v1\/operations\/([^/]+)\/steps\/([^/]+)\/retry$/)
    if (stepRetryMatch && request.method() === 'POST') {
      const operation = operationItems.find((item) => item.id === stepRetryMatch[1]) as unknown as Record<string, unknown> | undefined
      const steps = operation?.steps as Array<Record<string, unknown>> | undefined
      const step = steps?.find((item) => item.id === stepRetryMatch[2])
      if (!operation || !step) return json(route, { error: { code: 'not_found', message: 'Operation Step not found' } }, 404)
      step.attempt = Number(step.attempt) + 1
      step.status = 'pending'
      step.progress = 0
      step.error = null
      step.finishedAt = null
      operation.status = 'running'
      operation.statusReason = 'Retrying the selected Step.'
      operation.execution = {
        ...(operation.execution as Record<string, unknown>),
        status: 'running',
        statusReason: 'Retrying the selected Step.',
      }
      operation.updatedAt = now
      if (operation.kind === 'deploy-kubernetes') {
        const platform = platformItems.find((item) => item.id === operation.platformId)
        if (platform) {
          platform.lifecycleState = 'deploying'
          platform.updatedAt = now
        }
      }
      return json(route, { operationId: stepRetryMatch[1], stepId: stepRetryMatch[2] }, 202)
    }
    const retryMatch = path.match(/^\/api\/v1\/operations\/([^/]+)\/retry$/)
    if (retryMatch && request.method() === 'POST') {
      const original = operationItems.find((item) => item.id === retryMatch[1])
      if (!original) {
        return json(route, { error: { code: 'not_found', message: 'Operation not found' } }, 404)
      }
      const isDeploymentRepair = original.kind === 'deploy-kubernetes'
      const retry = {
        ...original,
        id: isDeploymentRepair ? 'op-repair' : 'op-retry',
        retryOfOperationId: original.id,
        execution: {
          ...original.execution,
          runId: isDeploymentRepair ? 'run-repair' : 'run-retry',
          status: 'running',
          statusReason: null,
          startedAt: now,
          finishedAt: null,
        },
        requestedAt: now,
        updatedAt: now,
      }
      operationItems.unshift(retry)
      if (isDeploymentRepair) {
        const platform = platformItems.find((item) => item.id === original.platformId)
        if (platform) {
          platform.lifecycleState = 'deploying'
          platform.lifecycleOperationId = retry.id
          platform.updatedAt = now
        }
      }
      return json(route, retry, 202)
    }
    const operationMatch = path.match(/^\/api\/v1\/operations\/([^/]+)$/)
    if (operationMatch) return json(route, operationItems.find((item) => item.id === operationMatch[1]) ?? operationItems[0])
    return json(route, { error: { code: 'not_found', message: `No fixture for ${path}` } }, 404)
  })
}
