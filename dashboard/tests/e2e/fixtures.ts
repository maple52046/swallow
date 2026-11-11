import { expect, type Page, type Route } from 'playwright/test'
import type { HealthState, Server, ServerHealthCounts } from '@/domain/server/types'
import type { SoftwareAssignment } from '@/domain/software/types'

const now = '2026-08-27T03:00:00Z'

/** The id of the Workflow `controllerWorkflow` seeds; uuid-shaped like the ids the API assigns. */
export const CONTROLLER_WORKFLOW_ID = '7c9e6679-7425-40de-944b-e07fc1f90ae7'

/**
 * Builds fleet fixtures; Server four deliberately models an unobserved inventory. The stored
 * `health` is what the Site Prometheus answers for a monitored Server (srv-4's scrape fails); the
 * API fixture classifies it at read time (see `classifiedServer` in `installApiFixtures`).
 */
function makeServer(index: number): Server {
  const ordinal = index + 1
  const named = ordinal <= 4
  const hasInventory = ordinal !== 4
  const tags = hasInventory ? named ? ['gpu', 'production'] : ['compute'] : []
  return {
    id: `srv-${ordinal}`,
    origin: 'discovered',
    generation: 1,
    spec: { tags, zoneId: null, poolId: null },
    status: {
    observedGeneration: 1,
    conditions: realizedConditions(1),
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
    providerResourcePool: hasInventory ? named ? 'accelerators' : 'compute' : '', providerPod: '', tags, automaticTags: [],
    hardware: { systemUuid: `uuid-${ordinal}`, serialNumber: `SN${String(ordinal).padStart(4, '0')}`, macAddresses: hasInventory ? [`02:00:00:00:${String(Math.floor(index / 250)).padStart(2, '0')}:${String((index % 250) + 1).padStart(2, '0')}`] : [] },
    deployment: ordinal === 4
      ? { state: 'failed', operationId: 'op-deploy-failed', stepId: 'provision-srv-4', attempt: 1, code: 'deployment_address_unavailable', stage: 'ssh_readiness', statusReason: 'No provider address was observed after OS installation.', startedAt: now, finishedAt: now, updatedAt: now }
      : { state: 'succeeded', operationId: `op-os-${ordinal}`, stepId: `provision-srv-${ordinal}`, attempt: 1, code: '', stage: '', statusReason: '', startedAt: now, finishedAt: now, updatedAt: now },
    provisioning: { state: 'deployed', providerState: 'deployed', powerState: 'on', osSystem: 'ubuntu', distroSeries: '24.04', deployedImageName: 'Ubuntu 24.04 LTS', deployedImageDefaultUser: 'ubuntu', ephemeral: false, hweKernel: 'ga-24.04', locked: false, commissioningStatus: 'passed', testingStatus: 'passed', integrationId: 'maas-a', observedAt: now },
    membership: ordinal <= 3 ? { platformId: 'platform-a', nodeName: `gpu-node-0${ordinal}`, role: 'control-plane', state: 'ready', observedAt: now } : null,
    health: ordinal === 4 ? { state: 'down', observedAt: now } : { state: 'up', observedAt: now },
    defaultUser: { user: 'ubuntu', source: 'os_image' },
    absent: false, lastSeenAt: now, createdAt: '2026-08-01T00:00:00Z', updatedAt: now,
    },
  }
}

/** A Zone or Pool of site-a realized on its provisioner, as the infrastructure list returns it. */
function groupFixture(id: string, name: string) {
  return {
    id, siteId: 'site-a', generation: 1,
    spec: { name, description: '' },
    status: {
      observedGeneration: 1, conditions: realizedConditions(1).filter((condition) => condition.type !== 'held'),
      provisioners: [{ integrationId: 'maas-a', providerName: name, observedGeneration: 1, observedAt: now, conditions: realizedConditions(1).slice(0, 1) }],
      createdAt: now, updatedAt: now,
    },
  }
}

/**
 * The conditions of a provisioner setting realized at generation `applied`: current when it equals
 * `generation`, otherwise still being applied, or refused with `refused` as the provisioner's reason.
 */
function settingConditions(applied: number, generation: number, refused: string) {
  const condition = (type: string, status: 'true' | 'false', reason: string, message: string, observed: number) =>
    ({ type, status, reason, message, lastTransitionTime: now, observedGeneration: observed })
  if (refused) {
    return [
      condition('realized', 'false', 'failed', refused, generation),
      condition('drifted', 'false', 'in_sync', '', generation),
      condition('requires_attention', 'true', 'provider_rejected', refused, generation),
    ]
  }
  return [
    condition('realized', 'true', 'converged', '', applied),
    condition('drifted', 'false', 'in_sync', '', applied),
    condition('requires_attention', 'false', 'none', '', applied),
  ]
}

/** The conditions of a Server whose provisioner matches its spec at the given generation. */
function realizedConditions(generation: number): Server['status']['conditions'] {
  const condition = (type: string, status: 'true' | 'false', reason: string) =>
    ({ type, status, reason, message: '', lastTransitionTime: now, observedGeneration: generation })
  return [
    condition('realized', 'true', 'converged'),
    condition('drifted', 'false', 'in_sync'),
    condition('requires_attention', 'false', 'none'),
    condition('held', 'false', 'not_held'),
  ]
}

/** The conditions of a Server whose provisioner refused its spec or drifted from it at generation 1. */
function serverSyncConditions(state: 'requires_attention' | 'drifted'): Server['status']['conditions'] {
  const condition = (type: string, status: 'true' | 'false', reason: string, message = '') =>
    ({ type, status, reason, message, lastTransitionTime: now, observedGeneration: 1 })
  if (state === 'requires_attention') {
    return [
      condition('realized', 'false', 'failed', 'MAAS refused the tag change.'),
      condition('drifted', 'false', 'in_sync'),
      condition('requires_attention', 'true', 'provider_rejected', 'MAAS refused the tag change.'),
      condition('held', 'false', 'not_held'),
    ]
  }
  return [
    condition('realized', 'true', 'converged'),
    condition('drifted', 'true', 'tags_differ', 'MAAS tags differ from the swallow spec.'),
    condition('requires_attention', 'false', 'none'),
    condition('held', 'false', 'not_held'),
  ]
}

const servers = Array.from({ length: 4 }, (_, index) => makeServer(index))
const operations = [
  {
    id: 'op-running', schemaVersion: 3, kind: 'deploy-kubernetes',
    intent: 'Deploy Kubernetes Platform production-k0s', intentSnapshot: {},
    definition: 'platform-deployment', definitionVersion: 1,
    status: 'running', statusReason: null,
    startState: 'started', temporal: { workflowId: 'swallow-operation/op-running', runId: 'temporal-run-1024' },
    siteId: 'site-a', platformId: 'platform-a',
    targetResources: [{ kind: 'platform', id: 'platform-a' }, ...servers.slice(0, 3).map((server) => ({ kind: 'server', id: server.id }))],
    targetServerIds: servers.slice(0, 3).map((server) => server.id), retryOfOperationId: null,
    steps: [
      {
        id: 'install-platform', kind: 'ansible-playbook', name: 'Install k0s Platform', job: 'configure-k0s',
        executor: 'ansible', dependsOn: [], targets: servers.slice(0, 3).map((server) => ({ kind: 'server', id: server.id })),
        status: 'running', attempt: 1, progress: 0, error: null,
        externalExecution: { provider: 'ansible-runner', id: 'run-1024', generation: 1 }, artifacts: [],
        startedAt: '2026-08-27T02:54:00Z', finishedAt: null,
      },
    ],
    jobs: [{ name: 'configure-k0s', label: 'Configure Kubernetes', retry: 'job' }],
    leases: [], requestCorrelation: null,
    execution: { runId: 'run-1024', playbook: 'ansible-playbook', status: 'running', statusReason: null, startedAt: '2026-08-27T02:54:00Z', finishedAt: null },
    requestedBy: 'admin', requestedAt: '2026-08-27T02:53:00Z',
    startedAt: '2026-08-27T02:54:00Z', finishedAt: null, updatedAt: now,
  },
  {
    id: 'op-failed', schemaVersion: 3, kind: 'install-exporters',
    intent: 'Install GPU exporters', intentSnapshot: {},
    definition: 'ansible-operation', definitionVersion: 1,
    status: 'failed', statusReason: 'Host unreachable',
    startState: 'started', temporal: { workflowId: 'swallow-operation/op-failed', runId: 'temporal-run-1023' },
    siteId: 'site-a', platformId: 'platform-a',
    targetResources: [{ kind: 'platform', id: 'platform-a' }, { kind: 'server', id: 'srv-4' }],
    targetServerIds: ['srv-4'], retryOfOperationId: null,
    steps: [
      {
        id: 'ansible', kind: 'ansible-playbook', name: 'Run install-exporters',
        executor: 'ansible', dependsOn: [], targets: [{ kind: 'server', id: 'srv-4' }],
        status: 'failed', attempt: 1, progress: 0,
        error: { code: 'ansible_failed', message: 'Host unreachable', retryable: true },
        externalExecution: { provider: 'ansible-runner', id: 'run-1023', generation: 1 }, artifacts: [],
        startedAt: '2026-08-27T01:10:00Z', finishedAt: '2026-08-27T01:12:00Z',
      },
    ],
    leases: [], requestCorrelation: null,
    execution: { runId: 'run-1023', playbook: 'ansible-playbook', status: 'failed', statusReason: 'Host unreachable', startedAt: '2026-08-27T01:10:00Z', finishedAt: '2026-08-27T01:12:00Z' },
    requestedBy: 'admin', requestedAt: '2026-08-27T01:09:00Z',
    startedAt: '2026-08-27T01:10:00Z', finishedAt: '2026-08-27T01:12:00Z', updatedAt: '2026-08-27T01:12:00Z',
  },
  {
    id: 'op-deploy-failed', schemaVersion: 3, kind: 'deploy-kubernetes',
    intent: 'Deploy Kubernetes Platform edge-staging', intentSnapshot: {},
    definition: 'platform-deployment', definitionVersion: 1,
    status: 'requires_attention', statusReason: 'A failed Step requires user attention.',
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
      statusReason: 'A failed Step requires user attention.',
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
    gpuStackOwner: 'gpu-operator',
    // Mirrors platforms.md "Platform Monitoring Dashboards": a Kubernetes Platform's Grafana is linked
    // at node port 30300 of its first workload node, on its landing dashboard (Servers, since the GPU
    // Operator's metricsExporter is off).
    monitoringLinks: [{ name: 'Platform Grafana', url: 'http://192.168.40.21:30300/d/swallow-node-exporter/servers' }],
    gpuOperator: {
      enabled: true, version: 'v1.5.1', allocationMode: 'device-plugin', draDriverVersion: null,
      resourceNaming: 'single', nodeLabeller: true, metricsExporter: false,
      state: 'ready', lastWorkflowId: 'op-gpu-operator', updatedAt: now,
    } as Record<string, unknown> | null,
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
    gpuStackOwner: 'provisioning', monitoringLinks: [], gpuOperator: null,
    sync: { lastStartedAt: null, lastSucceededAt: null, lastError: null, memberCount: 0, matchedCount: 0 },
    createdAt: '2026-08-20T00:00:00Z', updatedAt: now,
  },
  {
    id: 'platform-slurm', siteId: 'site-a', name: 'research-slurm', type: 'slurm',
    integrationId: 'slurm-a', origin: 'registered', lifecycleState: 'registered',
    lifecycleOperationId: null, deployment: null,
    gpuStackOwner: 'provisioning', gpuOperator: null,
    monitoringLinks: [{ name: 'Platform Grafana', url: 'http://10.0.0.11:3000/d/edqnz7pce7togc-rms/global-rms' }],
    omnistat: {
      enabled: true, version: '1.15.0', installMethod: 'container', observabilityServerId: 'srv-1', state: 'ready',
      hardwareCounters: { requested: false, nodes: [] },
      lastWorkflowId: 'op-slurm-omnistat', updatedAt: now,
    },
    containerRuntime: 'podman',
    sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null, memberCount: 0, matchedCount: 0 },
    createdAt: '2026-08-22T00:00:00Z', updatedAt: now,
  },
]
const sites = [
  { id: 'site-a', name: 'Taipei Lab', description: 'Primary accelerator lab', createdAt: now, updatedAt: now },
  { id: 'site-b', name: 'Hsinchu Edge', description: 'Edge validation', createdAt: now, updatedAt: now },
]
const integrations = [
  { id: 'maas-a', siteId: 'site-a', kind: 'provisioner', providerKind: 'maas', name: 'MAAS Taipei', endpoint: 'https://maas.example', enabled: true, settings: {}, hasCredential: true, hasOperatorCredential: false, sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null }, createdAt: now, updatedAt: now },
  { id: 'maas-b', siteId: 'site-b', kind: 'provisioner', providerKind: 'maas', name: 'MAAS Edge', endpoint: 'https://maas-edge.example', enabled: true, settings: {}, hasCredential: true, hasOperatorCredential: false, sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null }, createdAt: now, updatedAt: now },
  { id: 'prom-a', siteId: 'site-a', kind: 'metrics', providerKind: 'prometheus', name: 'Prometheus Taipei', endpoint: 'https://prom.example', enabled: true, settings: {}, hasCredential: true, hasOperatorCredential: false, sync: { lastStartedAt: now, lastSucceededAt: now, lastError: 'Alertmanager timeout' }, createdAt: now, updatedAt: now },
  // The credential Integration a successful deployment of production-k0s created (sites-integrations.md,
  // kind `platform`). research-slurm's legacy reader Integration `slurm-a` is deliberately absent, so
  // its Platform page shows the Integration id it cannot resolve.
  { id: 'k8s-a', siteId: 'site-a', kind: 'platform', providerKind: 'kubernetes', name: 'production-k0s (deployed)', endpoint: 'https://192.168.40.200:6443', enabled: true, settings: {}, hasCredential: true, hasOperatorCredential: true, sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null }, createdAt: '2026-08-10T00:42:00Z', updatedAt: now },
]
/** The provider-observed facts and swallow metadata of one fixture OS image catalog entry. */
export interface CatalogImageFixture {
  id: string
  architecture?: string
  /** The provider's own name, OS family, and release. */
  name: string
  osSystem: string
  release: string
  spec?: Partial<{ name: string; osSystem: string; release: string; tags: string[]; defaultUser: string }>
  /** The built-in default user the backend derives for the provider OS family, if any. */
  builtinDefaultUser?: string
  sizeBytes?: number
  verifiedDeployTargets?: string[]
  failedDeployTargets?: string[]
}

/** Builds a catalog entry the way the backend does: each metadata label wins over the provider value. */
export function catalogImage(fixture: CatalogImageFixture) {
  const spec = { name: '', osSystem: '', release: '', tags: [] as string[], defaultUser: '', ...fixture.spec }
  const defaultUser = spec.defaultUser || fixture.builtinDefaultUser
  return {
    id: fixture.id,
    architecture: fixture.architecture ?? 'amd64',
    spec,
    status: {
      name: spec.name || fixture.name,
      osSystem: spec.osSystem || fixture.osSystem,
      release: spec.release || fixture.release,
      ...(defaultUser ? { defaultUser } : {}),
      providerName: fixture.name,
      providerOsSystem: fixture.osSystem,
      providerRelease: fixture.release,
      ...(fixture.sizeBytes === undefined ? {} : { sizeBytes: fixture.sizeBytes }),
      verifiedDeployTargets: fixture.verifiedDeployTargets ?? [],
      failedDeployTargets: fixture.failedDeployTargets ?? [],
    },
  }
}

const osImages = [
  catalogImage({ id: 'ubuntu/jammy', name: 'Ubuntu 22.04 LTS', osSystem: 'ubuntu', release: 'jammy', spec: { tags: ['general-purpose', 'lts', 'production'] }, builtinDefaultUser: 'ubuntu', sizeBytes: 4294967296 }),
  catalogImage({ id: 'ubuntu/noble', name: 'Ubuntu 24.04 LTS', osSystem: 'ubuntu', release: 'noble', spec: { tags: ['latest', 'lts'] }, builtinDefaultUser: 'ubuntu', sizeBytes: 5368709120 }),
  catalogImage({ id: 'ubuntu-24.04-rocm', name: 'Ubuntu 24.04 ROCm', osSystem: 'custom', release: 'ubuntu-24.04-rocm', spec: { tags: ['gpu', 'rocm'] }, verifiedDeployTargets: ['ram'], failedDeployTargets: ['disk'] }),
]
const baseDeploymentTemplates = [
  { id: 'template-a', siteId: 'site-a', integrationId: 'maas-a', name: 'GPU compute baseline', description: 'Ubuntu baseline for accelerator nodes', imageId: 'ubuntu/jammy', ephemeral: false, network: { mode: 'dhcp', subnetId: 'subnet-a', defaultGateway: false }, hasUserData: true, createdAt: now, updatedAt: now },
]

const alerts = [
  { fingerprint: 'alert-1', name: 'NodeDown', severity: 'critical', state: 'firing', summary: 'gpu-node-04 stopped reporting', description: 'No scrape data for five minutes', labels: { alertname: 'NodeDown', server_id: 'srv-4' }, startsAt: '2026-08-27T02:50:00Z', serverId: 'srv-4', siteId: 'site-a', platformId: 'platform-a' },
  { fingerprint: 'alert-2', name: 'GpuTemperatureHigh', severity: 'warning', state: 'firing', summary: 'GPU temperature exceeds threshold', description: '', labels: { alertname: 'GpuTemperatureHigh', server_id: 'srv-2' }, startsAt: '2026-08-27T02:45:00Z', serverId: 'srv-2', siteId: 'site-a', platformId: 'platform-a' },
  { fingerprint: 'alert-3', name: 'ExporterMissing', severity: 'warning', state: 'suppressed', summary: 'Exporter rollout pending', description: '', labels: { alertname: 'ExporterMissing', server_id: 'srv-3' }, startsAt: '2026-08-27T01:45:00Z', serverId: 'srv-3', siteId: 'site-a', platformId: 'platform-a' },
]

interface BootISOFixture {
  id: string
  name: string
  integrationId: string
  rackAddress: string
  /** Undefined uses the fixture's served URL; null models an ISO record that is not currently served. */
  url?: string | null
  /** An ISO built before the UEFI hand-off (decision 047): no `uefiChainUrl` until rebuilt. */
  legacyTemplate?: boolean
  rebuiltAt?: string
  rebuiltBy?: string
}

/**
 * One Server's Power Configuration as the provisioner holds it (decision 054). The password itself
 * is never part of a fixture; only whether one is set.
 */
export interface PowerConfigurationFixture {
  driver: string
  address?: string
  powerId?: string
  username?: string
  passwordSet?: boolean
  /** A provisioner VM host whose own configuration powers the Server, making it read-only. */
  managedBy?: string
}

/** Controls for large-fleet, concurrency, and failure-path browser fixtures. */
export interface FixtureOptions {
  /** Whether GET /api/v1/modules fails, so a generated settings form cannot load. */
  modulesUnavailable?: boolean
  /**
   * Whether an approved Change Plan is applied the way its controller would apply it: the first
   * list read after approval still answers `approved`, and a later one answers `applied` with the
   * Workflow it produced (`op-<planId>`).
   */
  applyApprovedChangePlans?: boolean
  /**
   * Starts the installation's Change Plan policy with the safety switch on (`approveOnSubmit:
   * false`), so a confirmed spec write's plan waits in Change Plans the way change-plans.md
   * ("Approval On Submit") says. By default the policy was never stored, and a write sent with
   * `approve=true` is approved at once and carried out as its controller would.
   */
  requireSeparateApproval?: boolean
  /**
   * How many reads of a Platform still answer it as it was before an uninstall its user approved,
   * as a controller that has not acted yet by the time the page re-reads: the Platform stays active
   * with a `realized` Condition still awaiting approval, and the uninstall starts on the read after.
   * Unset, the mock starts an approved uninstall at once.
   */
  controllerLagReads?: number
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
  /**
   * Overrides the health the list and detail reads classify for these Servers (`observedAt` is set
   * only for `up` and `down`). Without one, an absent Server or one not `deployed` reads
   * `not_applicable` and a deployed one reads its stored scrape answer, like the API's policy.
   */
  serverHealth?: Record<string, HealthState>
  /** Replaces the Overview aggregate's `inventory.health`; `inventory.servers` follows its total. */
  overviewHealth?: ServerHealthCounts
  cpuOnlyServerIds?: string[]
  multiGpuServerIds?: string[]
  /** Replaces inventory with a management/display GPU and no compute accelerator. */
  displayOnlyServerIds?: string[]
  /** Overrides observed storage in GB, e.g. a provider's unrounded `2000.398934016`. */
  serverStorageGB?: Record<string, number>
  /**
   * Seeds one Workflow a swallow controller requested (`requestedBy` `swallow:node-exporter-controller`)
   * with a uuid-style id, `CONTROLLER_WORKFLOW_ID`: `succeeded` (ended, so it owns nothing anymore) or
   * `pending` (accepted but not started yet). Long requester and id values exercise truncation.
   */
  controllerWorkflow?: 'succeeded' | 'pending'
  /** Replaces compute inventory with a provider-generic AMD model plus an ASPEED controller. */
  genericComputeServerIds?: string[]
  /** Replaces inventory with a Cirrus Logic GD 5446 display controller. */
  cirrusDisplayOnlyServerIds?: string[]
  /** Overrides installed image labels for truncation and responsive presentation scenarios. */
  deployedImageNames?: Record<string, string>
  extraTags?: Record<string, string[]>
  /** Adds provisioner-computed tags: observed (`status.tags`, `status.automaticTags`), never in the spec. */
  automaticTags?: Record<string, string[]>
  /** Servers whose manual tags were changed outside swallow: `drifted` true until the next resync. */
  driftedServerIds?: string[]
  /** Overrides a Server's placement spec (Zone and Pool ids; null is the provisioner's default). */
  serverPlacements?: Record<string, { zoneId?: string | null; poolId?: string | null }>
  serverListFailuresAfterInitial?: number
  serverListFailureRequestNumbers?: number[]
  onServerListRequest?: (requestCount: number) => void
  staticNetworkServerIds?: string[]
  networkSubnetName?: string
  ephemeralServerIds?: string[]
  lockedServerIds?: string[]
  /** Projected power states by serverId, in place of `on` (the list's power button shows them). */
  powerStates?: Record<string, 'on' | 'off' | 'error' | 'unknown'>
  /**
   * What `GET /servers/{id}/power-state` reads live by serverId. Omitted: `on` through an automatic
   * driver, `unknown` without one, as the provisioner reads it.
   */
  livePowerStates?: Record<string, string>
  /**
   * Servers whose projection carries no `powerControl` (stored before the field existed), so the
   * Server list's power dialog must wait for the live Power Configuration read.
   */
  powerControlUnobservedServerIds?: string[]
  /** Servers whose OS swallow did not deploy: provisioning `deployed` with no `deployment` record. */
  existingServerIds?: string[]
  failedServerIds?: string[]
  brokenServerIds?: string[]
  rescueServerIds?: string[]
  /**
   * Servers whose provisioner storage layout needs a new inspection before a disk deployment
   * (`provisioning.inspectionRequired` with the MAAS UEFI-without-ESP reason). An accepted Inspect
   * moves them to `inspecting`; the flag stays until the inspection would finish.
   */
  inspectionRequiredServerIds?: string[]
  /** Called for every per-Server provisioner action POST (inspect, power-on, lock, ...). */
  onServerActionRequest?: (serverId: string, action: string) => void
  /**
   * Overrides a seeded Platform's lifecycle state by id, e.g. `{ 'platform-a': 'deploying' }`; its
   * status follows (`fixturePlatformStatus`), so `planned` awaits approval with nothing applied.
   */
  platformLifecycleStates?: Record<string, 'planned' | 'deploying' | 'uninstalling' | 'active' | 'deploy_failed' | 'uninstall_failed' | 'uninstalled'>
  /**
   * Marks a seeded Platform absent at generation 2 with its uninstall not started yet, judged with
   * the given `realized` reason (`pendingRemoval`), e.g. `{ 'platform-a': 'plan_required' }`.
   */
  platformRemovals?: Record<string, PendingRemovalReason>
  /** Removes platform-a's seeded GPU Operator, modelling a Platform that never had one. */
  withoutPlatformGPUOperator?: boolean
  /**
   * Seeds no Platform Claims, modelling Servers no Platform's deployment targets, for journeys
   * that exercise Server Release or Delete mechanics rather than Platform ownership. By default
   * every non-uninstalled deployed Platform claims its deployment targets (decision 067), so the
   * API refuses to release or delete them with 409.
   */
  withoutPlatformClaims?: boolean
  /** Receives every `PUT /platforms/{id}/gpu-operator` body. */
  onGPUOperatorRequest?: (platformId: string, body: Record<string, unknown>) => void
  /** Makes `PUT .../gpu-operator` answer 409 as if a target were held by another Workflow. */
  gpuOperatorBusy?: boolean
  /**
   * Receives each Slurm Platform spec write (`PUT /platforms/{id}/spec`): the written
   * `settings.omnistat.hardwareCounters` and the generation the write names.
   */
  onHardwareCountersRequest?: (platformId: string, hardwareCounters: unknown, generation: number) => void
  /**
   * Makes the first Slurm spec write answer `409 conflict` as if another write had changed the spec
   * since it was read; the stored generation moves on, so a retry that reads again succeeds.
   */
  hardwareCountersConflict?: boolean
  /**
   * Provider-only running work: sets the OS Provisioning State and leaves the swallow deployment
   * record alone, so no swallow Workflow is running for the Server (unlike `changingServerIds`).
   */
  providerWorkServerIds?: Record<string, 'releasing' | 'inspecting' | 'testing' | 'deploying'>
  /** Replaces a Server's conditions: its provisioner refused the spec, or drifted from it. */
  serverSync?: Record<string, 'requires_attention' | 'drifted'>
  /**
   * The swallow work behind a Releasing Server (pair with `providerWorkServerIds`): seeds one
   * `release-os` Workflow per listed Server with that status, finished after the seeded
   * `stateSince` when terminal. `null` instead removes the Server from every Workflow, modelling a
   * release started in the provisioner.
   */
  releaseWorkflows?: Record<string, 'waiting_external' | 'requires_attention' | 'canceled' | null>
  /** Populates each persisted Server Activity source with this many rows for pagination coverage. */
  serverActivityItemCount?: number

  secondReadyServerIntegrationId?: string
  failImageIntegrationIds?: string[]
  deploymentFailureIds?: string[]
  serverActionFailureIds?: string[]
  deploymentReadinessIssues?: Record<string, string>
  onOSImageCatalogRequest?: (integrationId: string) => void
  /** Adds one large synthetic OS family so grouping, page-level scrolling, and search can be exercised. */
  extraOSImageCount?: number
  /** Called with the target integration when an OS image upload is created (`POST /image-uploads`). */
  onOSImageUploadRequest?: (integrationId: string) => void
  /** Called with the `defaultUser` (or '') of a created OS image upload. */
  onOSImageUploadDefaultUser?: (defaultUser: string) => void
  /** Called with the whole JSON intent of a created OS image upload (size and SHA-256 included). */
  onOSImageUploadIntent?: (intent: Record<string, unknown>) => void
  /**
   * Called when an OS image upload's content arrives (`PUT /image-uploads/{id}/content`) with the
   * number of bytes received and the request's `Content-Type`.
   */
  onOSImageUploadContent?: (receivedBytes: number, contentType: string) => void
  /** Called with the body of an OS image metadata PUT. */
  onOSImageMetadataRequest?: (body: Record<string, unknown>) => void
  /** Called for every SSH Keys request with its method, path, and JSON body (if any). */
  onSSHKeyRequest?: (method: string, path: string, body: Record<string, unknown> | null) => void
  onDeploymentRequest?: (body: Record<string, unknown>) => void
  /** Holds an OS deployment response open so dismissal locking can be exercised. */
  deploymentRequestGate?: Promise<void>
  onPlatformDeploymentRequest?: (body: Record<string, unknown>) => void
  onServerReleaseRequest?: (serverId: string, body: Record<string, unknown> | null) => void
  onServerRecoverRequest?: (serverId: string, body: Record<string, unknown> | null) => void
  onServerRefreshRequest?: (serverId: string) => void
  onPlatformUninstallRequest?: (platformId: string, body: Record<string, unknown> | null) => void
  releaseConvergesAfterRefreshes?: number
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
  /**
   * Makes the Workflow cancel endpoint answer `409 conflict` with this message and leave the
   * Operation active, modelling a cancel the backend refuses. Unset by default, so cancels are
   * accepted.
   */
  cancelRefusedMessage?: string
  /** Called for every `DELETE /servers/{id}`, in order. */
  onServerDeleteRequest?: (serverId: string) => void
  /** Servers whose deletion the provisioner refuses with `409 conflict`. */
  serverDeleteFailureIds?: string[]
  /** Called with each Operation a Release, Recover, or Platform uninstall with `cancelActiveWorkflows` canceled. */
  onWorkflowSuperseded?: (operationId: string) => void
  providerFailureRetryable?: boolean
  /**
   * Makes the canonical Task-retry endpoint answer `409 conflict`, modelling a durable Workflow
   * whose Temporal execution was lost (a host restart or execution timeout) so a Step retry can
   * no longer be delivered. It drives the platform Repair action into its rerun recovery path.
   */
  deployExecutionLost?: boolean
  onNetworkLinkRequest?: (method: string, serverId: string, interfaceId: string, linkId: string | null, body: Record<string, unknown> | null) => void
  onMetricsRequest?: (serverIds: string[]) => void
  /**
   * Removes Platform membership, Platform Claims, and deployment target history for wizard
   * success paths.
   */
  freePlatformCandidates?: boolean
  /**
   * Gives research-slurm the Device Metrics Exporter instead of Omnistat (they are mutually
   * exclusive): ready, with the GPU identity the lab's MI308X virtual functions report (collapsed).
   */
  slurmDeviceMetricsExporter?: boolean
  // Gives srv-1 exactly 4C/24GiB/80GB and srv-2 4C/16GiB/80GB.
  minimumResourceCandidates?: boolean
  /** Makes the Slurm requirement GET fail so the wizard fail-closed path can be verified. */
  slurmRequirementFails?: boolean
  onMetricsActive?: (active: number) => void
  /**
   * The overview's alert-source state: `not-configured` models a Site without an Alertmanager
   * (normal), `unavailable` a configured one that could not be read. Defaults to alerts read.
   */
  overviewAlertSource?: 'not-configured' | 'unavailable'
  /** Makes every metric read answer `provider_unavailable`, as when the Site has no metrics Integration. */
  metricsBackendMissing?: boolean
  /** Called with the query string of every usage-history (range) request. */
  onMetricRangeRequest?: (params: URLSearchParams) => void
  /**
   * Adds a swallow-deployed HA Slurm platform (`platform-slurm-ha`) with two controller-only
   * nodes and two compute nodes. The controllers report no membership (slurmrestd lists only
   * slurmd scheduler nodes), so this exercises the deployment-intent projection that surfaces
   * managers and the controller merge in the member list. Opt-in so other tests are unaffected.
   */
  slurmDeployed?: boolean
  /**
   * With `slurmDeployed`, lab-slurm-ha's spec and Omnistat record ask for no hardware counters, as a
   * Platform deployed without them or turned off afterwards.
   */
  slurmHardwareCountersOff?: boolean
  /**
   * Seeds the succeeded deployment Workflow of each deployed Platform: production-k0s's
   * `deploy-kubernetes` (op-k0s-deploy), and with `slurmDeployed` lab-slurm-ha's `configure-slurm`
   * (op-slurm-ha-deploy), so the Platform details card can name and link the last deployment.
   * Opt-in because the Workflow list and Server Activity tests count the seeded Workflows.
   */
  platformDeploymentWorkflows?: boolean
  /**
   * Docker CE Software Assignments by serverId (decision 043): `enabled` records `enableApi: true`
   * and serves the Docker Host Explorer routes; `disabled` models an installation without the API
   * (a legacy record with no spec). Servers not listed have no Docker CE, so their Containers tab
   * is not offered. Opt-in so other tests are unaffected.
   */
  dockerAssignments?: Record<string, 'enabled' | 'disabled'>
  /** Generic Managed Software assignments used by catalog/detail and lifecycle tests. */
  softwareAssignments?: SoftwareAssignment[]
  /** GET request ordinals that should fail, allowing initial and background failure coverage. */
  softwareAssignmentFailureRequestNumbers?: number[]
  /** Called with the body of every POST /software/assignments (a Docker CE re-apply from the tab). */
  onSoftwareInstallRequest?: (body: Record<string, unknown>) => void
  /** Makes the Managed Software install endpoint reject the submitted request. */
  softwareInstallError?: { status: number; code: string; message: string }
  /** Called with every POST /software/uninstall request. */
  onSoftwareUninstallRequest?: (body: Record<string, unknown>) => void
  /** Called for every Docker Host Explorer write with its method, explorer sub-path, and JSON body. */
  onDockerRequest?: (method: string, path: string, body: Record<string, unknown> | null) => void
  /** Holds every image pull response until it resolves, to exercise a long-running pull. */
  dockerPullGate?: Promise<void>
  /** Registry Credentials present at page load (decision 044); passwords are never part of a fixture. */
  registryCredentials?: Array<{ registry: string; username: string }>
  /** Called for every Registry Credential write with its method, path, and JSON body (passwords included). */
  onRegistryCredentialRequest?: (method: string, path: string, body: Record<string, unknown> | null) => void
  /** Called for every Server Default User write (decision 039) with its method and JSON body. */
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
   * Power Configurations by serverId at page load (decision 054). Servers not listed have the IPMI
   * BMC the provisioner detail reports (192.0.2.20, bmc-admin, a password set).
   */
  powerConfigurations?: Record<string, PowerConfigurationFixture>
  /** Called for every Power Configuration write with its JSON body (a password included). */
  onPowerConfigurationRequest?: (serverId: string, body: Record<string, unknown>) => void
  /** Makes the Power Configuration PUT fail with this API error instead of saving. */
  powerConfigurationError?: { status: number; code: string; message: string }
  /**
   * Holds an enable preflight open until it resolves; meanwhile the Boot Media read reports it as
   * running (`apply`) in the settle phase, as the real API does for about three minutes.
   */
  bootMediaApplyGate?: Promise<void>
  /**
   * Boot ISOs at page load (boot-isos.md, decision 047). Omitted: `taipei-rack` for MAAS Taipei
   * (srv-1's provisioner) and `edge-rack` for MAAS Edge.
   */
  bootISOs?: BootISOFixture[]
  /** Makes the Boot ISO list report the builder unavailable with this reason. */
  bootISOBuilderUnavailable?: string
  /** Makes the Boot ISO build fail with this API error instead of storing an ISO. */
  bootISOBuildError?: { status: number; code: string; message: string }
  /** Called for every Boot ISO write with its method, path, and JSON body. */
  onBootISORequest?: (method: string, path: string, body: Record<string, unknown> | null) => void
  /** Removes every provisioner Integration, modelling a Site that has none yet. */
  noProvisioners?: boolean
  /** Adds a second provisioner to site-a, modelling an invalid Site configuration. */
  multipleProvisioners?: boolean
  /** Overrides provisioner endpoints to exercise Boot ISO rack-address suggestion parsing. */
  provisionerEndpoints?: Record<string, string>
  /** Disables site-a's only provisioner. */
  disabledProvisioner?: boolean
  /**
   * Gives production-k0s's credential Integration (`k8s-a`) no operator credential
   * (`hasOperatorCredential: false`), as a Kubernetes Platform last deployed successfully before
   * 2026-11-05 still has its single cluster-admin token (platforms-kubernetes.md "Cluster
   * Credentials"). By default it has both credentials.
   */
  singleClusterCredential?: boolean
  /** Limits the installation to site-a so global deployment can auto-select it. */
  singleSite?: boolean
  /** Servers whose inspect-hardware Workflow waits in `requires_attention` (decision 053). */
  inspectionAttentionServerIds?: string[]
  /**
   * Servers whose inspect-hardware Workflow stopped at the enrollment wait with
   * `power_configuration_required` (decision 054): enrollment ended without a readable power driver.
   */
  powerAttentionServerIds?: string[]
  /** Called for every enroll-bundle read with its method, path, and the swallowUrl it sent. */
  onEnrollmentRequest?: (method: string, path: string, swallowUrl: string) => void
  /**
   * libvirt domains by hypervisor Server id for `GET /servers/{id}/virtual-machines` (decision 055).
   * Omitted: srv-1 (gpu-node-01) hosts three lab domains, one of which is already srv-3.
   */
  virtualMachines?: Record<string, VirtualMachineFixture[]>
  /** Called for every virtual-machine enrollment POST with its JSON body. */
  onVirtualMachineEnrollmentRequest?: (body: Record<string, unknown>) => void
  /** Servers whose Boot Media goes through a swallow hypervisor (method `libvirt`), on srv-1. */
  libvirtBootMediaServerIds?: string[]
  /**
   * Provisioner HTTP Proxy by Integration id as MAAS holds it (decision 071). Omitted Integrations
   * start on the MAAS default, the built-in proxy.
   */
  httpProxies?: Record<string, { mode: string; httpProxy: string }>
  /** Called for every Provisioner HTTP Proxy PUT with the Integration id and its JSON body. */
  onHttpProxyRequest?: (integrationId: string, body: Record<string, unknown>) => void
  /**
   * Makes MAAS refuse every Provisioner HTTP Proxy change the way a non-administrator MAAS account
   * does: the spec is stored, and the controller then reports requires_attention.
   */
  httpProxyWriteForbidden?: boolean
  /**
   * Servers whose inspected subnet has no provisioner DHCP (`provisionerDhcp` false). The API
   * still suggests `automatic` there; the dashboard warns about the site DHCP pool (decision 058).
   */
  siteDhcpNetworkServerIds?: string[]
  /**
   * Provisioner subnets with their Subnet DNS by Integration id (decision 058). Omitted
   * provisioner Integrations list a MAAS-DHCP lab subnet and a site-DHCP subnet, both without DNS.
   */
  provisionerSubnets?: Record<string, ProvisionerSubnetFixture[]>
  /** Called for every Subnet DNS PUT with the Integration id, subnet id, and its JSON body. */
  onSubnetDnsRequest?: (integrationId: string, subnetId: string, body: Record<string, unknown>) => void
  /**
   * Gives site-a a Local Repository (decision 078) through a `repo-a` Pulp Integration, in one of
   * the {@link LocalRepositoryScenario}s. Omitted: no Site has one, so every local-repository route
   * answers 404 as it does for a Site without a `repository` Integration.
   */
  localRepository?: LocalRepositoryScenario
  /** In the `syncing` scenario, the number of repository reads after which every source, file, and image is ready. */
  localRepositorySettleAfterReads?: number
  /**
   * Makes the first spec write answer 409 the way a concurrent write by another admin would: the
   * stored generation moves on and gains that admin's `ports` source (its `serveDeployments` is kept),
   * and nothing of this write lands.
   */
  localRepositorySpecConflict?: boolean
  /** Makes every content read fail with this API error, as when Pulp cannot be reached. */
  localRepositoryContentError?: { status: number; code: string; message: string }
  /** Called for every local-repository write (spec PUT, sync POST) with its method, path, and JSON body. */
  onLocalRepositoryRequest?: (method: string, path: string, body: Record<string, unknown> | null) => void
}

/**
 * site-a's Local Repository in the local-repository.md read shape. Every scenario but `not-serving`
 * starts with a spec that serves deployments (`serveDeployments: true`):
 * - `ready`: every source, file, and image is ready and deployments are accepted;
 * - `syncing`: the grafana archive syncs, the docker.io cache, the k0s binary, and the docker.io
 *   images wait, and the kube-proxy image is being fetched;
 * - `failed`: the docker-ce archive, the k0s binary, and the kube-proxy image failed, with the
 *   provider's reasons;
 * - `attention`: Pulp refused the Integration's credential, so the last pass stopped and the items
 *   keep their last observation;
 * - `not-serving`: the spec does not serve deployments, so they fetch from the upstreams and the
 *   worker converges nothing. The items keep the last observation from when it did: the docker-ce
 *   archive failed, the grafana archive was syncing, and the docker.io cache, the k0s binary, and the
 *   images had not been fetched.
 *
 * A spec write stores `serveDeployments` as sent. While it is `false` the API's `ready` is false, the
 * `ready` condition reads `not_serving`, and the mock worker converges nothing; once a write turns it
 * on, the worker converges the spec as for any write, and the scenario's items stay as they are.
 */
export type LocalRepositoryScenario = 'ready' | 'syncing' | 'failed' | 'attention' | 'not-serving'

/** One effective Repository Source as the fixture stores it. */
interface RepositorySourceFixture {
  name: string
  format: string
  url: string
  suites: string[]
  components: string[]
  architectures: string[]
  declaredBy: string[]
  state: string
  message: string | null
  lastSyncedAt: string | null
  contentCount: number
}

/** One Repository Source of the user's spec. */
interface RepositorySourceSpecFixture {
  name: string
  format: string
  url: string
  suites: string[]
  components: string[]
  architectures: string[]
}

const repositorySyncedAt = '2026-08-27T02:40:00Z'

/** The Ubuntu archive the installation writes as site-a's first user source (generation 1). */
const ubuntuSourceSpec: RepositorySourceSpecFixture = {
  name: 'ubuntu', format: 'deb', url: 'http://archive.ubuntu.com/ubuntu',
  suites: ['noble', 'noble-updates', 'noble-security'], components: ['main', 'universe'], architectures: ['amd64'],
}

/** The sources the compiled modules declare, and the pull-through caches derived from their images and requirements. */
function moduleRepositorySources(scenario: LocalRepositoryScenario): RepositorySourceFixture[] {
  // The not-serving scenario keeps a mix of last observations: a failure and items not fetched yet.
  const syncing = scenario === 'syncing' || scenario === 'not-serving'
  const failed = scenario === 'failed' || scenario === 'not-serving'
  const ready = (contentCount: number) => ({ state: 'ready', message: null, lastSyncedAt: repositorySyncedAt, contentCount })
  const none = { suites: [] as string[], components: [] as string[], architectures: [] as string[] }
  return [
    {
      name: 'docker-ce', format: 'deb', url: 'https://download.docker.com/linux/ubuntu', suites: ['noble'], components: ['stable'], architectures: ['amd64'],
      declaredBy: ['module:docker-ce'],
      ...(failed
        ? { state: 'failed', message: 'sync failed: 404 Not Found fetching https://download.docker.com/linux/ubuntu/dists/noble/Release', lastSyncedAt: '2026-08-20T01:00:00Z', contentCount: 312 }
        : ready(312)),
    },
    {
      name: 'docker.io', format: 'oci', url: 'https://docker.io', ...none, declaredBy: ['module:grafana', 'module:amd'],
      ...(syncing ? { state: 'pending', message: null, lastSyncedAt: null, contentCount: 0 } : ready(9)),
    },
    {
      name: 'grafana', format: 'deb', url: 'https://apt.grafana.com', suites: ['stable'], components: ['main'], architectures: ['amd64'],
      declaredBy: ['module:grafana'],
      ...(syncing ? { state: 'syncing', message: 'Downloading package indexes', lastSyncedAt: null, contentCount: 0 } : ready(1840)),
    },
    { name: 'pypi', format: 'python', url: 'https://pypi.org/', ...none, declaredBy: ['module:slurm'], ...ready(27) },
    { name: 'quay.io', format: 'oci', url: 'https://quay.io', ...none, declaredBy: ['module:kubernetes'], ...ready(6) },
  ]
}

/** The files the modules declare, with the k0s binary following the scenario. */
function repositoryFiles(scenario: LocalRepositoryScenario) {
  const file = (path: string, url: string, sha256: string, module: string, sizeBytes: number) => ({
    path, url, sha256, declaredBy: [`module:${module}`], state: 'ready', message: null as string | null, sizeBytes,
  })
  const k0s = file(
    'k0s/v1.36.3+k0s.2/k0s-v1.36.3+k0s.2-amd64', 'https://github.com/k0sproject/k0s/releases/download/v1.36.3+k0s.2/k0s-v1.36.3+k0s.2-amd64',
    'f8b278ba5a9938ced226381484a0f31b6bfa2938703949df235474e03c774acc', 'kubernetes', 248_512_512,
  )
  if (scenario === 'syncing' || scenario === 'not-serving') Object.assign(k0s, { state: 'pending', sizeBytes: 0 })
  if (scenario === 'failed') {
    Object.assign(k0s, { state: 'failed', sizeBytes: 0, message: 'checksum mismatch: the download has sha256 3b1c0d9a52e4, the declaration f8b278ba5a99' })
  }
  return [
    file(
      'device-metrics-exporter/1.5.3/amdgpu-exporter_1.5.3~24.04_amd64.deb',
      'https://repo.radeon.com/device-metrics-exporter/apt/1.5.3/pool/main/a/amdgpu-exporter/amdgpu-exporter_1.5.3~24.04_amd64.deb',
      'fa5cc3f949c89f3e944798bf6326cded16de7aca6449e80de7376d2b824886b7', 'device-metrics-exporter', 23_456_789,
    ),
    file('helm/v3.22.1/helm-v3.22.1-linux-amd64.tar.gz', 'https://get.helm.sh/helm-v3.22.1-linux-amd64.tar.gz',
      '896cf63684a42a723d61c980369d6e588330927deb13676ca0d85006dd6da0ea', 'kubernetes', 16_384_000),
    k0s,
    file('omnistat/v1.15.0.tar.gz', 'https://github.com/ROCm/omnistat/archive/refs/tags/v1.15.0.tar.gz',
      '899c681179b8dfe5b3ccebde360c7a39ef2fe13cd0fdebb34d088963ad32f96f', 'slurm', 1_048_576),
    file('victoriametrics/1.155.0/victoria-metrics-linux-amd64-v1.155.0.tar.gz',
      'https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/v1.155.0/victoria-metrics-linux-amd64-v1.155.0.tar.gz',
      '7180e65913d55ab99c0be8f4a3ad17a0fafe5598a21ff6327fecba3190d7b176', 'victoriametrics', 14_891_085),
  ]
}

/** The images the modules declare, with what the worker observed of each in the scenario. */
function repositoryImages(scenario: LocalRepositoryScenario) {
  const image = (reference: string, module: string, sizeBytes: number) => ({
    reference, declaredBy: [`module:${module}`], state: 'ready', message: null as string | null, sizeBytes,
  })
  const grafana = image('docker.io/grafana/grafana:12.4.12', 'grafana', 186_413_056)
  const gpuOperator = image('docker.io/rocm/gpu-operator:v1.5.1', 'amd', 64_225_280)
  const kubeProxy = image('quay.io/k0sproject/kube-proxy:v1.36.3-1', 'kubernetes', 30_408_704)
  if (scenario === 'syncing' || scenario === 'not-serving') {
    // The docker.io cache is not ready yet, so its images wait; kube-proxy's layers are downloading.
    for (const waiting of [grafana, gpuOperator]) Object.assign(waiting, { state: 'pending', sizeBytes: 0 })
    Object.assign(kubeProxy, { state: 'syncing', sizeBytes: 0 })
  }
  if (scenario === 'failed') {
    Object.assign(kubeProxy, {
      state: 'failed', sizeBytes: 0,
      message: 'Pulp could not fetch the image quay.io/k0sproject/kube-proxy:v1.36.3-1 from its upstream registry (HTTP 502); it is tried again later.',
    })
  }
  return [grafana, gpuOperator, kubeProxy, image('quay.io/k0sproject/pause:3.10.2-0', 'kubernetes', 320_512)]
}

/** The controller's conditions for a scenario (local-repository.md, Conditions), judged for `generation`. */
function repositoryConditions(scenario: LocalRepositoryScenario, generation: number) {
  const condition = (type: string, status: string, reason: string, message = '') => ({
    type, status, reason, message, lastTransitionTime: '2026-08-27T02:58:00Z', observedGeneration: generation,
  })
  switch (scenario) {
    case 'ready':
      return [condition('ready', 'true', 'ready'), condition('realized', 'true', 'converged'), condition('requires_attention', 'false', 'none')]
    case 'syncing':
      return [
        condition('ready', 'false', 'syncing', 'source docker.io is pending; source grafana is syncing; file k0s/v1.36.3+k0s.2/k0s-v1.36.3+k0s.2-amd64 is pending; '
          + 'image docker.io/grafana/grafana:12.4.12 is pending; image docker.io/rocm/gpu-operator:v1.5.1 is pending; image quay.io/k0sproject/kube-proxy:v1.36.3-1 is syncing'),
        condition('realized', 'false', 'syncing', 'grafana is syncing'),
        condition('requires_attention', 'false', 'none'),
      ]
    case 'failed':
      return [
        condition('ready', 'false', 'source_failed', 'source docker-ce failed: 404 Not Found; file k0s/v1.36.3+k0s.2/k0s-v1.36.3+k0s.2-amd64 failed: checksum mismatch; '
          + 'image quay.io/k0sproject/kube-proxy:v1.36.3-1 failed: Pulp could not fetch the image from its upstream registry (HTTP 502)'),
        condition('realized', 'false', 'source_failed', 'docker-ce: 404 Not Found fetching the Release file'),
        condition('requires_attention', 'false', 'none'),
      ]
    case 'attention': {
      const refused = "Pulp refused the Integration's credential (401 Unauthorized); set the Pulp admin password again."
      return [
        condition('ready', 'false', 'credential_rejected', refused),
        condition('realized', 'false', 'credential_rejected', refused),
        condition('requires_attention', 'true', 'credential_rejected', refused),
      ]
    }
    case 'not-serving':
      // What the worker judges of these items once the spec serves deployments; while it does not,
      // the body replaces the `ready` condition with `not_serving`.
      return [
        condition('ready', 'false', 'source_failed', 'source docker-ce failed: 404 Not Found; source docker.io is pending; source grafana is syncing; '
          + 'file k0s/v1.36.3+k0s.2/k0s-v1.36.3+k0s.2-amd64 is pending; image quay.io/k0sproject/kube-proxy:v1.36.3-1 is syncing'),
        condition('realized', 'false', 'source_failed', 'docker-ce: 404 Not Found fetching the Release file'),
        condition('requires_attention', 'false', 'none'),
      ]
  }
}

/** One content item in the browse read shape; the fields a format does not have are null. */
interface RepositoryContentFixture {
  name: string
  version: string | null
  architecture: string | null
  source: string | null
  path: string | null
  sizeBytes: number | null
}

/** Ubuntu archive packages the `ubuntu` source serves, enough for two content pages. */
const ubuntuPackageNames = [
  'acl', 'adduser', 'apparmor', 'apt', 'base-files', 'bash', 'bsdutils', 'ca-certificates', 'chrony', 'coreutils',
  'curl', 'dash', 'dbus', 'debconf', 'dpkg', 'e2fsprogs', 'gpgv', 'grep', 'gzip', 'hostname',
  'init-system-helpers', 'iproute2', 'iptables', 'jq', 'kmod', 'libc6', 'libssl3t64', 'login', 'mount', 'netplan.io',
  'openssh-server', 'passwd', 'procps', 'python3', 'sed', 'systemd', 'tar', 'tzdata', 'util-linux', 'zstd',
]

/** What the provider serves per format, ordered by name as the provider pages it. */
function repositoryContent(format: string): RepositoryContentFixture[] {
  const items: RepositoryContentFixture[] = (() => {
    switch (format) {
      case 'deb':
        return [
          { name: 'containerd.io', version: '1.7.28-1', architecture: 'amd64', source: 'docker-ce', path: 'pool/stable/amd64/containerd.io_1.7.28-1_amd64.deb', sizeBytes: 31_457_280 },
          { name: 'docker-ce', version: '5:28.5.1-1~ubuntu.24.04~noble', architecture: 'amd64', source: 'docker-ce', path: 'pool/stable/amd64/docker-ce_28.5.1-1~ubuntu.24.04~noble_amd64.deb', sizeBytes: 20_971_520 },
          { name: 'docker-ce-cli', version: '5:28.5.1-1~ubuntu.24.04~noble', architecture: 'amd64', source: 'docker-ce', path: 'pool/stable/amd64/docker-ce-cli_28.5.1-1~ubuntu.24.04~noble_amd64.deb', sizeBytes: 15_728_640 },
          { name: 'grafana', version: '12.4.12', architecture: 'amd64', source: 'grafana', path: 'pool/main/g/grafana/grafana_12.4.12_amd64.deb', sizeBytes: 125_829_120 },
          ...ubuntuPackageNames.map((name, index) => ({
            name, version: `1.${index}.0-1ubuntu1`, architecture: 'amd64', source: 'ubuntu',
            path: `pool/main/${name.charAt(0)}/${name}/${name}_1.${index}.0-1ubuntu1_amd64.deb`, sizeBytes: 65_536 * (index + 3),
          })),
        ]
      case 'oci':
        return [
          { name: 'docker.io/grafana/grafana', version: '12.4.12', architecture: 'amd64', source: 'docker.io', path: null, sizeBytes: 152_043_520 },
          { name: 'docker.io/rocm/gpu-operator', version: 'v1.5.1', architecture: 'amd64', source: 'docker.io', path: null, sizeBytes: 98_566_144 },
          { name: 'quay.io/k0sproject/pause', version: '3.10.2-0', architecture: 'amd64', source: 'quay.io', path: null, sizeBytes: 327_680 },
        ]
      case 'python':
        return [
          { name: 'Flask', version: '3.1.3', architecture: null, source: 'pypi', path: null, sizeBytes: 103_424 },
          { name: 'numpy', version: '2.5.4', architecture: null, source: 'pypi', path: null, sizeBytes: 16_777_216 },
        ]
      case 'file':
        return repositoryFiles('ready').map((file) => ({
          name: file.path.split('/').pop() ?? file.path, version: null, architecture: null, source: null, path: file.path, sizeBytes: file.sizeBytes,
        }))
      default:
        return []
    }
  })()
  return items.sort((left, right) => left.name.localeCompare(right.name))
}

/** One subnet in the provisioner-subnet-dns.md read shape. */
export interface ProvisionerSubnetFixture {
  id: string
  name: string
  cidr: string
  gatewayAddress: string
  managed: boolean
  provisionerDhcp: boolean
  attached: boolean
  dnsServers: string[]
}

/**
 * The subnets a provisioner Integration lists when a test does not choose its own: two attached
 * networks and one host-internal Docker bridge MAAS recorded from an enrolled host.
 */
function defaultProvisionerSubnets(): ProvisionerSubnetFixture[] {
  return [
    { id: '1', name: 'lab-network', cidr: '192.168.40.0/24', gatewayAddress: '192.168.40.1', managed: true, provisionerDhcp: true, attached: true, dnsServers: [] },
    { id: '7', name: 'site-network', cidr: '10.140.168.0/23', gatewayAddress: '10.140.169.254', managed: true, provisionerDhcp: false, attached: true, dnsServers: [] },
    { id: '12', name: '172.17.0.0/16', cidr: '172.17.0.0/16', gatewayAddress: '', managed: true, provisionerDhcp: false, attached: false, dnsServers: [] },
  ]
}

/** One libvirt domain in the `virtual-machines` read shape. */
interface VirtualMachineFixture {
  name: string
  uuid: string
  state: string
  architecture: string
  macAddresses: string[]
  serverId: string | null
}

/** srv-1's lab domains when a test names none: two to enroll (one running) and one already a Server. */
const defaultVirtualMachines: Record<string, VirtualMachineFixture[]> = {
  'srv-1': [
    { name: 'lab-vm-1', uuid: '5d685113-0078-444c-8707-000000000001', state: 'shut off', architecture: 'x86_64', macAddresses: ['52:54:00:af:de:01'], serverId: null },
    { name: 'lab-vm-2', uuid: '5d685113-0078-444c-8707-000000000002', state: 'running', architecture: 'x86_64', macAddresses: ['52:54:00:af:de:02'], serverId: null },
    { name: 'lab-vm-3', uuid: '5d685113-0078-444c-8707-000000000003', state: 'shut off', architecture: 'x86_64', macAddresses: ['52:54:00:af:de:03'], serverId: 'srv-3' },
  ],
}

/**
 * The type settings and OS preparation a seeded Platform's spec carries beyond its k0s version
 * (platforms.md "Create A Platform"), keyed by Platform id. production-k0s is HA behind a virtual IP
 * with a custom Pod range and the default Service range, provisioned to disk; lab-slurm-ha pins its
 * cluster name and slurmrestd version, provisioned to RAM. Other Platforms keep the bare settings.
 */
const fixturePlatformSpecExtras: Record<string, { settings: Record<string, unknown>; machinePreparation: Record<string, unknown> }> = {
  'platform-a': {
    settings: { podCidr: '10.32.0.0/16', apiVip: '192.168.40.200', apiVipPrefix: 24 },
    machinePreparation: { mode: 'provision_os', settings: { imageId: 'ubuntu/noble', deployTarget: 'disk' }, userData: { mode: 'inherit' }, network: { mode: 'automatic' } },
  },
  'platform-slurm-ha': {
    settings: { slurm: { clusterName: 'lab-ha', apiVersion: 'v0.0.42', pmix: true }, monitoringDashboards: true, omnistat: { enabled: true, installMethod: 'container', hardwareCounters: true } },
    machinePreparation: { mode: 'provision_os', settings: { imageId: 'ubuntu-24.04-rocm', deployTarget: 'ram' }, userData: { mode: 'inherit' }, network: { mode: 'automatic' } },
  },
}

/**
 * The spec a fixture Platform reads with (platforms.md "Spec and status"): its role assignments as
 * members, a k0s version, its GPU Operator settings, and its `fixturePlatformSpecExtras`.
 */
function fixturePlatformSpec(platform: {
  id: string
  type: string
  lifecycleState: string
  deployment: { roleAssignments: Array<{ serverId: string; role: string; runWorkloads?: boolean }> } | null
  gpuOperator: Record<string, unknown> | null
}) {
  const members = (platform.deployment?.roleAssignments ?? []).map((assignment) => ({
    serverId: assignment.serverId,
    roles: assignment.role === 'control-plane' ? ['control-plane', ...(assignment.runWorkloads ? ['workloads'] : [])] : ['worker'],
  }))
  const extras = fixturePlatformSpecExtras[platform.id]
  const settings: Record<string, unknown> = {
    ...(platform.type === 'kubernetes' ? { k0sVersion: 'v1.36.3+k0s.2' } : { slurm: {} }),
    ...structuredClone(extras?.settings ?? {}),
  }
  const operator = platform.gpuOperator
  if (operator && operator.enabled) {
    settings.gpuOperator = {
      enabled: true, version: operator.version, allocationMode: operator.allocationMode, resourceNaming: operator.resourceNaming,
      nodeLabeller: operator.nodeLabeller, metricsExporter: operator.metricsExporter,
    }
  }
  return {
    desired: platform.lifecycleState === 'uninstalled' ? 'absent' : 'present', members, settings,
    machinePreparation: extras ? structuredClone(extras.machinePreparation) : null, removal: null as ReturnType<typeof removalChoice>,
  }
}

/** A Platform's shared `realized` and `drifted` Conditions (platforms.md "Spec And Status") at `generation`. */
function platformConditions(realized: 'converged' | 'awaiting_approval' | 'converging' | 'failed' | 'plan_required', generation: number, message = '') {
  const condition = (type: string, status: 'true' | 'false', reason: string, text = '') =>
    ({ type, status, reason, message: text, lastTransitionTime: now, observedGeneration: generation })
  return [
    condition('realized', realized === 'converged' ? 'true' : 'false', realized, message),
    condition('drifted', 'false', 'in_sync'),
  ]
}

/**
 * The status a seeded Platform's lifecycle implies at generation 1: nothing applied while its install
 * waits on its Change Plan, runs, or failed (with the matching `realized` reason), and the spec
 * applied once it was deployed.
 */
function fixturePlatformStatus(lifecycleState: string, spec: ReturnType<typeof fixturePlatformSpec>) {
  const pending = (reason: 'awaiting_approval' | 'converging' | 'failed', message: string) =>
    ({ observedGeneration: 0, conditions: platformConditions(reason, 1, message), applied: null, realizingWorkflowId: null })
  switch (lifecycleState) {
    case 'planned':
      return pending('awaiting_approval', 'The install waits for its Change Plan to be approved.')
    case 'deploying':
      return pending('converging', 'A deployment Workflow carries out generation 1.')
    case 'deploy_failed':
      return pending('failed', 'The deployment Workflow failed.')
    default:
      return { observedGeneration: 1, conditions: platformConditions('converged', 1), applied: spec, realizingWorkflowId: null }
  }
}

/** The legacy deploy-shaped body a spec create stands for, which the mock's creation code reads. */
function legacyPlatformBody(body: { siteId: string; name: string; type: string; spec: { members: Array<{ serverId: string; roles: string[] }>; settings: Record<string, unknown>; machinePreparation?: unknown } }) {
  const { members, settings, machinePreparation } = body.spec
  if (body.type === 'slurm') {
    const { slurm, ...rest } = settings as { slurm?: Record<string, unknown> }
    return {
      siteId: body.siteId, name: body.name, type: 'slurm', ...rest, machinePreparation,
      slurm: {
        ...(slurm ?? {}),
        nodeAssignments: members.map((member) => ({
          serverId: member.serverId, controller: member.roles.includes('controller'), compute: member.roles.includes('compute'), login: member.roles.includes('login'),
        })),
      },
    }
  }
  return {
    siteId: body.siteId, name: body.name, type: body.type, ...settings, machinePreparation,
    roleAssignments: members.map((member) => member.roles.includes('control-plane')
      ? { serverId: member.serverId, role: 'control-plane', runWorkloads: member.roles.includes('workloads') }
      : { serverId: member.serverId, role: 'worker', runWorkloads: false }),
  }
}

/** A fixture Change Plan in the change-plans.md shape; `overrides` set the parts a test cares about. */
function changePlan(id: string, platformId: string, state: string, overrides: Record<string, unknown>) {
  return {
    id, resource: { kind: 'platform', id: platformId }, generation: 3, observedAt: '2026-08-27T02:00:00Z',
    actions: [] as unknown[], effects: [] as string[], blockers: [] as string[], state, stateReason: '',
    decidedBy: '', decidedAt: null as string | null, workflowId: '',
    expiresAt: '2026-08-28T02:00:00Z', createdAt: '2026-08-27T02:00:00Z', updatedAt: '2026-08-27T02:00:00Z',
    ...overrides,
  }
}

/**
 * The effect the API gives an uninstall that releases its `members` (platforms.md "Delete A
 * Platform"), from the DELETE body's `releaseOptions`, so fixture plans read like real ones.
 */
function releaseEffect(members: number, options: Record<string, unknown>) {
  const [subject, their, systems] = members === 1 ? ['1 member is', 'its', 'operating system'] : [`${members} members are`, 'their', 'operating systems']
  const disks = options.erase || options.secureErase || options.quickErase ? 'are erased' : 'are not erased'
  const bindings = options.unbindStaticIps ? 'are removed' : 'are kept'
  return `${subject} released to the provisioner, which removes ${their} ${systems}; ${their} disks ${disks} and ${their} static IP bindings ${bindings}. The Platform record is deleted once the release succeeds.`
}

/** How the controller judged an absent Platform whose uninstall has not started (`platformRemovals`). */
type PendingRemovalReason = 'awaiting_approval' | 'plan_required' | 'converging'

/**
 * A seeded Platform marked absent at generation 2 whose uninstall has not started (platforms.md
 * "Delete A Platform"), with the `realized` Condition the controller gives it: its plan waits for
 * approval under the safety switch, was rejected, or is approved and about to start.
 */
function pendingRemoval<T extends { id: string; spec: ReturnType<typeof fixturePlatformSpec>; status: ReturnType<typeof fixturePlatformStatus> }>(
  platform: T,
  reason: PendingRemovalReason,
): T & { generation: number } {
  const planId = `plan-uninstall-${platform.id}-2`
  const message = {
    awaiting_approval: `Change Plan ${planId} waits for approval: the Change Plan policy requires a separate approval`,
    plan_required: 'the uninstall needs an approved Change Plan; delete the Platform again to propose one',
    converging: `Change Plan ${planId} is approved and is carried out next`,
  }[reason]
  return {
    ...platform, generation: 2,
    spec: { ...platform.spec, desired: 'absent' },
    status: { ...platform.status, conditions: platformConditions(reason, 2, message) },
  }
}

/**
 * The `spec.removal` a DELETE body stores (platforms.md "Delete A Platform"), in a form two
 * requests compare by: null keeps the members and cancels nothing, and the release options count
 * only when the members are released.
 */
function removalChoice(body: Record<string, unknown> | null) {
  if (!body?.releaseServers && !body?.cancelActiveWorkflows) return null
  const released = Boolean(body.releaseServers)
  const options = (released ? body.releaseOptions ?? {} : {}) as Record<string, unknown>
  return {
    releaseServers: released, erase: Boolean(options.erase), secureErase: Boolean(options.secureErase),
    quickErase: Boolean(options.quickErase), unbindStaticIps: Boolean(options.unbindStaticIps),
    cancelActiveWorkflows: Boolean(body.cancelActiveWorkflows),
  }
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

type SSHKeyRealizationFixture = { integrationId: string; siteId: string; observedGeneration: number; observedAt: string | null; conditions: ReturnType<typeof keyRealized>[] }

/** One provisioner's realized condition for an SSH Key at `generation`. */
function keyRealized(status: 'true' | 'false', reason: string, message: string, generation: number) {
  return { type: 'realized', status, reason, message, lastTransitionTime: now, observedGeneration: generation }
}

/** A provisioner that holds the key at `generation`. */
function keySynced(integrationId: string, siteId: string, generation: number): SSHKeyRealizationFixture {
  return { integrationId, siteId, observedGeneration: generation, observedAt: now, conditions: [keyRealized('true', 'converged', '', generation)] }
}

/** A provisioner the controller has not judged the key on yet. */
function keyPending(integrationId: string, siteId: string): SSHKeyRealizationFixture {
  return { integrationId, siteId, observedGeneration: 0, observedAt: null, conditions: [] }
}

/** The SSH Key status summarizing its provisioners, as the API derives it. */
function sshKeyStatus(generation: number, provisioners: SSHKeyRealizationFixture[]) {
  const allRealized = provisioners.every((entry) => entry.conditions[0]?.status === 'true' && entry.observedGeneration >= generation)
  const failing = provisioners.find((entry) => entry.conditions[0]?.status === 'false')
  return {
    observedGeneration: allRealized ? generation : Math.min(...provisioners.map((entry) => entry.observedGeneration)),
    conditions: [allRealized
      ? keyRealized('true', 'converged', '', generation)
      : keyRealized('false', failing?.conditions[0].reason ?? 'converging', failing?.conditions[0].message ?? '', generation)],
    provisioners,
    createdAt: now,
    updatedAt: now,
  }
}

/** Builds one SSH Key fixture in the `ssh-keys.md` shape, synced to MAAS Taipei and failed on MAAS Edge. */
function sshKey(id: string, name: string, purpose: 'deployment' | 'access', publicKey: string, generation = 1) {
  return {
    id,
    ...(purpose === 'access' ? { ownerUserId: 'admin-1' } : {}),
    generation,
    spec: { name, purpose, keyType: 'ssh-ed25519', fingerprint: `SHA256:${id}`, publicKey },
    status: sshKeyStatus(generation, [
      keySynced('maas-a', 'site-a', generation),
      { integrationId: 'maas-b', siteId: 'site-b', observedGeneration: 0, observedAt: now, conditions: [keyRealized('false', 'provider_unreachable', 'Could not reach MAAS.', generation)] },
    ]),
  }
}

/** The NFS module's spec schema, as api-server/modules/software/nfs/module.json declares it. */
const nfsSpecSchema = {
  type: 'object',
  additionalProperties: false,
  properties: {
    exportPath: { type: 'string', title: 'Export path (server)', description: 'Required when a Server takes the server role.', examples: ['/export/data'] },
    exportOptions: { type: 'string', title: 'Export options (server)', examples: ['rw,sync,no_subtree_check,root_squash'] },
    source: { type: 'string', title: 'Source (client)', description: 'Required when a Server takes the client role (host:/path).', examples: ['10.0.0.9:/export/data'] },
    mountPath: { type: 'string', title: 'Mount path (client)', examples: ['/shared'] },
    mountOptions: { type: 'string', title: 'Mount options (client)', examples: ['rw,_netdev'] },
  },
}

/** The spec schema the VictoriaMetrics module declares (`api-server/modules/software/victoriametrics/module.json`). */
const victoriaMetricsSpecSchema = {
  type: 'object',
  additionalProperties: false,
  properties: {
    listen: {
      type: 'string',
      title: 'Answers on',
      description: 'loopback (the default) answers only processes on this Server, such as its Grafana; management answers on the Server\'s management address. VictoriaMetrics has no login: anyone who reaches it can read and write every series.',
      enum: ['loopback', 'management'],
    },
    retention: {
      type: 'string',
      title: 'Retention',
      description: 'How long samples are kept: a whole number with d (days), w (weeks), M (months), or y (years), at most 100 years. Leave blank for VictoriaMetrics\'s default, 1M.',
      examples: ['1M'],
      pattern: '^[1-9][0-9]{0,4}[dwMy]$',
    },
  },
}

/**
 * After the deploy wizard submitted a Platform, checks that its install was approved with the
 * request and started (change-plans.md "Approval On Submit"): the wizard says so and opens the new
 * Platform, whose deploy the mock has started.
 */
export async function expectPlatformInstallStarted(page: Page, platformId = 'platform-new') {
  await expect(page).toHaveURL(`/platforms/${platformId}/overview?site=site-a`)
  await expect(page.getByText('Platform install started')).toBeVisible()
}

/**
 * With the safety switch on (`requireSeparateApproval`), approves the install plan a Platform
 * created on this page waits on, which starts its deploy in the mock, and opens the Platform.
 */
export async function approvePlatformInstall(page: Page, platformId = 'platform-new') {
  await expect(page).toHaveURL('/change-plans?site=site-a')
  // The newest plan is listed first; a page that created the Platform twice approves the latest.
  // The card is titled by the Platform's name once it loads and keeps the id in its text either way,
  // so it is found by the id without waiting for the name.
  const plan = page.getByRole('article', { name: /^Change Plan for Platform / }).filter({ hasText: platformId }).first()
  await plan.getByRole('button', { name: 'Approve plan' }).click()
  await page.getByRole('alertdialog', { name: 'Approve Change Plan' }).getByRole('button', { name: 'Approve plan' }).click()
  await expect(page.getByText('Change Plan approved')).toBeVisible()
  await page.goto(`/platforms/${platformId}?site=site-a`)
}

/** Installs deterministic network fixtures; no backend or provider is contacted. */
export async function installApiFixtures(page: Page, options: FixtureOptions = {}) {
  const imageCatalog = [
    ...osImages,
    ...Array.from({ length: options.extraOSImageCount ?? 0 }, (_, index) => {
      const release = `validation-${String(index + 1).padStart(2, '0')}`
      const name = `Validation Linux ${String(index + 1).padStart(2, '0')}`
      return catalogImage({
        id: `validation/${release}`,
        name,
        osSystem: 'validation-linux',
        release,
        spec: { tags: ['validation'], defaultUser: 'operator' },
        architecture: index % 2 === 0 ? 'amd64' : 'arm64',
        sizeBytes: 2_147_483_648 + index,
      })
    }),
  ]
  // Mirrors the refresh cookie: the startup refresh succeeds only while a Session exists.
  let signedIn = options.signedIn ?? true
  // Per-page SSH Key state so one test's changes never leak into another.
  // Per-page API Key state; secrets are fixture strings, never real keys.
  let apiKeyItems = [
    { id: 'api-1', name: 'laptop-cli', prefix: 'swk_Lap1op00', createdAt: '2026-08-01T00:00:00Z', expiresAt: null as string | null, lastUsedAt: '2026-08-27T02:00:00Z' as string | null },
    { id: 'api-2', name: 'old-ci', prefix: 'swk_0ldC1000', createdAt: '2026-05-01T00:00:00Z', expiresAt: '2026-08-01T00:00:00Z' as string | null, lastUsedAt: null as string | null },
  ]
  // The deploy Workflow of a Platform created on this page, started when its install is approved.
  const pendingPlatformDeploys = new Map<string, (typeof operations)[number]>()
  // Per-page Change Plan state (change-plans.md): one plan to decide, one blocked, one applied, and
  // an expired one whose Platform and Server were deleted since, so only their ids are known.
  let changePlanItems = [
    changePlan('plan-uninstall', 'platform-slurm', 'pending_approval', {
      actions: [
        { target: { kind: 'platform', id: 'platform-slurm' }, action: 'uninstall', from: 'active', to: 'uninstalled', destructive: true, reason: 'The Platform was marked for uninstall.' },
        { target: { kind: 'server', id: 'srv-3' }, action: 'remove-member', from: 'compute', to: '', destructive: true, reason: 'srv-3 leaves the Platform with it.' },
      ],
      effects: ['srv-3 stops running Slurm jobs', 'The research-slurm read Integration is removed'],
    }),
    changePlan('plan-blocked', 'platform-a', 'pending_approval', {
      actions: [{ target: { kind: 'server', id: 'srv-1' }, action: 'change-node-role', from: 'worker', to: 'control-plane', destructive: true, reason: 'The spec gives srv-1 a new Node Role.' }],
      blockers: ['srv-1 is locked in the provisioner'],
    }),
    changePlan('plan-applied', 'platform-a', 'applied', {
      actions: [{ target: { kind: 'server', id: 'srv-2' }, action: 'remove-member', from: 'worker', to: '', destructive: true, reason: 'srv-2 was removed from the spec.' }],
      decidedBy: 'admin', decidedAt: '2026-08-26T09:00:00Z', workflowId: 'op-remove-member',
    }),
    changePlan('plan-retired', 'platform-retired', 'expired', {
      actions: [{ target: { kind: 'server', id: 'srv-retired' }, action: 'remove-member', from: 'worker', to: '', destructive: true, reason: 'srv-retired was removed from the spec.' }],
      stateReason: 'Nobody decided the plan before it expired.',
      createdAt: '2026-08-20T02:00:00Z', expiresAt: '2026-08-21T02:00:00Z', updatedAt: '2026-08-21T02:00:00Z',
    }),
  ]
  // List reads each approved plan has seen since its approval, for `applyApprovedChangePlans`.
  const approvedPlanReads = new Map<string, number>()
  // Per-page Change Plan policy (change-plans.md "Change Plan Policy"); never stored by default.
  let changePlanPolicy = options.requireSeparateApproval
    ? { approveOnSubmit: false, updatedAt: '2026-08-27T01:00:00Z' as string | null, updatedBy: 'admin' }
    : { approveOnSubmit: true, updatedAt: null as string | null, updatedBy: '' }
  /**
   * How a spec write's new plan is stored (change-plans.md "Approval On Submit"): approved by the
   * requesting admin when the request sent `approve=true`, the policy allows it, and the plan has no
   * blockers; otherwise waiting, with the reason the API gives.
   */
  const submittedPlan = (requestUrl: URL, blockers: readonly string[] = []) => {
    if (blockers.length > 0) return { state: 'pending_approval', stateReason: 'it has blockers and cannot be approved' }
    if (requestUrl.searchParams.get('approve') !== 'true') return { state: 'pending_approval', stateReason: 'submitted without approval' }
    if (!changePlanPolicy.approveOnSubmit) return { state: 'pending_approval', stateReason: 'the Change Plan policy requires a separate approval' }
    return { state: 'approved', stateReason: 'approved by admin with the change', decidedBy: 'admin', decidedAt: now }
  }
  // Approved uninstalls the mock controller has not carried out yet, by Platform (`controllerLagReads`).
  const laggingUninstalls = new Map<string, { reads: number; planId: string }>()
  /** The `realized` Condition message of a Platform whose plan waits, as the controller words it. */
  const waitingMessage = (plan: { id: string; stateReason: string }) => `Change Plan ${plan.id} waits for approval: ${plan.stateReason}`
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
      redfishVersion: '1.15.1', firmwareVersion: '13.06.10', profile: 'ami', systemId: 'Self', virtualMedia: true,
      bootOverrideModes: ['Once', 'Continuous'], probedAt: now,
    },
  }
  // Per-page Power Configurations (decision 054), keyed by serverId; a write replaces one the way
  // the API does, keeping the password for an unchanged driver when the body omits it.
  const powerConfigurations: Record<string, PowerConfigurationFixture> = { ...(options.powerConfigurations ?? {}) }
  const powerFamily = (driver: string) => (driver === 'ipmi' || driver === 'redfish' ? 'bmc' : driver === 'virsh' ? 'virsh' : '')
  const powerControl = (driver: string) => (driver === '' ? 'none' : driver === 'manual' ? 'manual' : 'automatic')
  const powerConfigurationView = (serverId: string) => {
    const config = powerConfigurations[serverId] ?? { driver: 'ipmi', address: '192.0.2.20', username: 'bmc-admin', passwordSet: true }
    return {
      serverId, driver: config.driver, family: powerFamily(config.driver), control: powerControl(config.driver),
      address: config.address ?? '', powerId: config.powerId ?? '', username: config.username ?? '',
      passwordSet: config.passwordSet ?? false, editable: !config.managedBy,
      readOnlyReason: config.managedBy ? `This virtual machine takes its power from provisioner VM host "${config.managedBy}"; change it there.` : '',
      drivers: [{ driver: 'ipmi', family: 'bmc' }, { driver: 'redfish', family: 'bmc' }, { driver: 'virsh', family: 'virsh' }],
    }
  }
  // Per-page Boot ISOs (decision 047). Only srv-1 has Boot Media in the fixture, so an ISO's
  // inUseBy is whether srv-1's enabled setting names it.
  let bootISOItems = (options.bootISOs ?? [
    { id: 'iso-taipei', name: 'taipei-rack', integrationId: 'maas-a', rackAddress: '10.0.0.2' },
    { id: 'iso-edge', name: 'edge-rack', integrationId: 'maas-b', rackAddress: '10.9.0.2' },
  ]).map((iso) => ({
    ...iso,
    siteId: integrations.find((integration) => integration.id === iso.integrationId)?.siteId ?? 'site-a',
    createdAt: now,
    createdBy: 'admin',
  }))
  const bootISOURL = (id: string) => `http://192.0.2.1/boot-media/ipxe/${id}/swallow-ipxe.iso`
  const bootISOView = (iso: (typeof bootISOItems)[number]) => {
    // legacyTemplate is fixture state, not an API field, so it is kept out of the response.
    const { legacyTemplate, ...record } = iso
    const rack = /:\d+$/.test(iso.rackAddress) ? iso.rackAddress : `${iso.rackAddress}:5248`
    const [rackHost, rackPort = '5248'] = rack.split(':')
    const chainUrl = `http://${rack}/ipxe.cfg`
    const uefiChainUrl = legacyTemplate ? '' : `http://${rack}/grub/grub.cfg-<mac>`
    const grubConfig = legacyTemplate
      ? ''
      : `set maas_rack=${rackHost}\nset maas_port=${rackPort}\nnet_bootp\nset root=(http,\${maas_rack}:\${maas_port})\nconfigfile /grub/grub.cfg-$mac\n`
    return {
      ...record, chainUrl, uefiChainUrl, grubConfig, ipxeVersion: 'v2.0.0 (12798ec)', sizeBytes: 2_402_304, sha256: `sha256-of-${iso.id}`,
      script: `#!ipxe\n\nset maas_rack ${rackHost}\n\n:start\ndhcp || goto retry\nset next-server \${maas_rack}\nchain ${chainUrl.replace(/\/\/[^:/]+/, '//${next-server}')} || goto returned\n`,
      url: iso.url === undefined ? bootISOURL(iso.id) : iso.url,
      inUseBy: bootMediaState.setting?.enabled === true && bootMediaState.setting?.isoId === iso.id ? 1 : 0,
    }
  }
  // A virtual machine on srv-1 (decision 055) has no BMC; its Hypervisor is what the probes find.
  const throughHypervisor = (serverId: string) => options.libvirtBootMediaServerIds?.includes(serverId) ?? false
  const noBMC = {
    support: 'no_bmc', reason: 'The Server is a libvirt virtual machine; its Boot Media method is libvirt.',
    virtualMedia: false, bootOverrideModes: [], probedAt: now,
  }
  const libvirtCapability = (serverId: string) => ({
    support: 'supported', hypervisorServerId: 'srv-1', account: 'ubuntu',
    domain: fleet.find((entry) => entry.id === serverId)?.status.hostname ?? serverId, pool: 'default', cdrom: true, probedAt: now,
  })
  const bootMediaView = (live: boolean, serverId = 'srv-1') => {
    const isoId = typeof bootMediaState.setting?.isoId === 'string' ? bootMediaState.setting.isoId : ''
    const iso = bootISOItems.find((item) => item.id === isoId)
    const image = !isoId
      ? null
      : iso
        ? { id: iso.id, name: iso.name, url: iso.url === undefined ? bootISOURL(iso.id) : (iso.url ?? ''), available: iso.url !== null }
        : { id: isoId, url: '', available: false, reason: 'The Boot ISO no longer exists; choose another.' }
    const enabled = Boolean(bootMediaState.setting?.enabled)
    if (throughHypervisor(serverId)) {
      return {
        serverId, method: 'libvirt', image: image && { ...image, available: Boolean(iso) },
        setting: bootMediaState.setting, redfish: noBMC, libvirt: libvirtCapability(serverId), apply: bootMediaState.apply,
        live: live
          ? { mediaInserted: enabled, mediaImage: enabled ? `/var/lib/libvirt/images/swallow-ipxe-${isoId}.iso` : '', overrideEnabled: enabled ? 'Continuous' : '', overrideTarget: enabled ? 'Cd' : '', ready: enabled }
          : null,
      }
    }
    return {
      serverId,
      method: 'redfish',
      image,
      setting: bootMediaState.setting,
      redfish: bootMediaState.redfish,
      libvirt: null,
      apply: bootMediaState.apply,
      live: live
        ? { mediaInserted: enabled, overrideEnabled: 'Once', overrideTarget: 'UefiBootNext', ready: enabled }
        : null,
    }
  }
  // A newly created key starts pending in every provisioner and settles on its first single-key
  // read, modelling the backend sync pass that finishes moments after the create returns.
  const settlingKeyIds = new Set<string>()
  const pendingKey = (key: ReturnType<typeof sshKey>) => {
    settlingKeyIds.add(key.id)
    return { ...key, status: sshKeyStatus(key.generation, key.status.provisioners.map(({ integrationId, siteId }) => keyPending(integrationId, siteId))) }
  }
  // Per-page Docker CE assignments and Docker Engine objects (software.md, servers-docker.md).
  const dockerAssignments = new Map<string, SoftwareAssignment>(
    Object.entries(options.dockerAssignments ?? {}).map(([serverId, mode]) => [serverId, {
      serverId, kind: 'docker-ce', roles: [] as string[],
      spec: mode === 'enabled' ? ({ enableApi: true } as Record<string, unknown>) : null,
      state: 'installed', lastWorkflowId: 'op-docker-install', lastAppliedAt: '2026-08-20T00:00:00Z' as string | null,
      createdAt: '2026-08-20T00:00:00Z', updatedAt: '2026-08-20T00:00:00Z',
    }]),
  )
  let softwareAssignmentItems: SoftwareAssignment[] = [
    ...dockerAssignments.values(),
    ...structuredClone(options.softwareAssignments ?? []),
  ]
  let softwareAssignmentRequestCount = 0
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
      server.status.membership = null
    }
  }
  if (options.minimumResourceCandidates && fleet[0] && fleet[1]) {
    Object.assign(fleet[0].status, { cpuCores: 4, memoryMiB: 24576, storageGB: 80 })
    Object.assign(fleet[1].status, { cpuCores: 4, memoryMiB: 16384, storageGB: 80 })
  }
  for (let index = 0; index < (options.readyServerCount ?? 0) && index < fleet.length; index++) {
    fleet[index].status.provisioning.state = 'ready'
    fleet[index].status.provisioning.providerState = 'Ready'
    fleet[index].status.provisioning.osSystem = ''
    fleet[index].status.provisioning.distroSeries = ''
    fleet[index].status.deployment = null
  }
  for (const serverId of options.changingServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) {
      server.status.provisioning.state = 'inspecting'
      server.status.provisioning.stateSince = '2026-08-27T02:58:30Z'
      server.status.provisioning.providerState = 'Commissioning'
      server.status.deployment = {
        state: 'deploying', operationId: 'op-running', stepId: `provision-${server.id}`,
        attempt: 1, code: '', stage: options.deployingStage ?? 'provisioning', statusReason: options.deployingStatusReason ?? 'Provisioning operating system.',
        startedAt: now, finishedAt: null, updatedAt: now,
      }
    }
  }
  for (const serverId of options.deploymentAttentionServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) {
      server.status.deployment = {
        state: 'requires_attention', operationId: 'op-deploy-failed', stepId: `provision-${server.id}`,
        attempt: 1, code: 'deployment_failed', stage: 'verification', statusReason: 'Swallow could not verify the deployment.',
        startedAt: now, finishedAt: null, updatedAt: now,
      }
    }
  }
  for (const serverId of options.absentServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.absent = true
  }
  // Health is classified when a Server is read, never stored (servers-list.md "Health"), so the
  // fixture works it out from the Server's current record on every list and detail read: an
  // explicit `serverHealth` wins; otherwise swallow does not monitor an absent or not-deployed
  // Server (N/A), and a deployed one reads its stored scrape answer. A release in a journey thus
  // turns a Server N/A on its next read, as it does against the API.
  const classifiedHealth = (server: Server): Server['status']['health'] => {
    const override = options.serverHealth?.[server.id]
    if (override) return { state: override, observedAt: override === 'up' || override === 'down' ? now : null }
    if (server.status.absent || server.status.provisioning?.state !== 'deployed') return { state: 'not_applicable', observedAt: null }
    return server.status.health
  }
  const classifiedServer = (server: Server): Server => ({ ...server, status: { ...server.status, health: classifiedHealth(server) } })
  for (const serverId of options.cpuOnlyServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.gpus = []
  }
  for (const serverId of options.multiGpuServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.gpus = [
      { vendor: 'AMD', model: 'MI300X', count: 8, kind: 'compute' },
      { vendor: 'NVIDIA', model: 'H100', count: 4, kind: 'compute' },
      { vendor: 'ASPEED Technology, Inc.', model: 'ASPEED Graphics Family', count: 16, kind: 'display' },
    ]
  }
  for (const [serverId, storageGB] of Object.entries(options.serverStorageGB ?? {})) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.storageGB = storageGB
  }
  for (const serverId of options.displayOnlyServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.gpus = [
      { vendor: 'ASPEED Technology, Inc.', model: 'ASPEED Graphics Family', count: 1, kind: 'display' },
    ]
  }
  for (const serverId of options.genericComputeServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.gpus = [
      // MAAS reports the PCI vendor name, not a brand.
      { vendor: 'Advanced Micro Devices, Inc. [AMD/ATI]', model: 'AMD GPU', count: 8, kind: 'compute' },
      { vendor: 'ASPEED Technology, Inc.', model: 'ASPEED Graphics Family', count: 1, kind: 'display' },
    ]
  }
  for (const serverId of options.cirrusDisplayOnlyServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.gpus = [
      { vendor: 'Cirrus Logic', model: 'GD 5446', count: 1, kind: 'display' },
    ]
  }
  for (const [serverId, imageName] of Object.entries(options.deployedImageNames ?? {})) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.provisioning.deployedImageName = imageName
  }
  for (const [serverId, tags] of Object.entries(options.extraTags ?? {})) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) {
      server.status.tags = [...server.status.tags, ...tags]
      server.spec.tags = [...server.spec.tags, ...tags]
    }
  }
  for (const [serverId, tags] of Object.entries(options.automaticTags ?? {})) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) {
      server.status.tags = [...server.status.tags, ...tags]
      server.status.automaticTags = [...server.status.automaticTags, ...tags]
    }
  }
  for (const [serverId, placement] of Object.entries(options.serverPlacements ?? {})) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.spec = { ...server.spec, ...placement }
  }
  for (const serverId of options.driftedServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) {
      server.status.tags = [...server.status.tags, 'hand-made']
      server.status.conditions = server.status.conditions.map((condition) => {
        if (condition.type === 'drifted') return { ...condition, status: 'true', reason: 'drift_detected', message: 'Manual tags differ from the spec (unexpected hand-made).' }
        if (condition.type === 'realized') return { ...condition, status: 'false', reason: 'converging', message: 'Drift is corrected at the next resync.' }
        return condition
      })
    }
  }
  for (const serverId of options.ephemeralServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) {
      server.status.provisioning.ephemeral = true
    }
  }
  for (const serverId of options.lockedServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.provisioning.locked = true
  }
  for (const [serverId, powerState] of Object.entries(options.powerStates ?? {})) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.provisioning.powerState = powerState
  }
  // The projection mirrors each Server's power control (servers-list.md) from its Power
  // Configuration, except where a test needs a projection that has not observed it yet.
  for (const server of fleet) {
    if (!(options.powerControlUnobservedServerIds ?? []).includes(server.id)) {
      server.status.provisioning.powerControl = powerConfigurationView(server.id).control
    }
  }
  for (const serverId of options.existingServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.deployment = null
  }
  // Seed the not-usable provider states the recovery policy acts on (decision 033), so tests
  // can exercise Recover / Release gating and the Failed vs Broken vs Rescue badges.
  const seedProviderState = (ids: string[] | undefined, state: string, providerState: string) => {
    for (const serverId of ids ?? []) {
      const server = fleet.find((item) => item.id === serverId)
      if (server) {
        server.status.provisioning.state = state
        server.status.provisioning.providerState = providerState
      }
    }
  }
  seedProviderState(options.failedServerIds, 'failed', 'Failed deployment')
  seedProviderState(options.brokenServerIds, 'broken', 'Broken')
  seedProviderState(options.rescueServerIds, 'rescue', 'Rescue mode')
  for (const serverId of options.inspectionRequiredServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) {
      server.status.provisioning.inspectionRequired = true
      server.status.provisioning.inspectionRequiredReason =
        'MAAS boots this Server with UEFI, but its storage layout has no EFI system partition because it was inspected while booting BIOS. Inspect the Server again before a disk deployment.'
    }
  }
  const providerWorkLabels = { releasing: 'Releasing', inspecting: 'Commissioning', testing: 'Testing', deploying: 'Deploying' }
  for (const [serverId, state] of Object.entries(options.providerWorkServerIds ?? {})) {
    seedProviderState([serverId], state, providerWorkLabels[state])
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.provisioning.stateSince = '2026-08-27T02:58:30Z'
  }
  for (const [serverId, state] of Object.entries(options.serverSync ?? {})) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) server.status.conditions = serverSyncConditions(state)
  }
  if (options.secondReadyServerIntegrationId && fleet[1]) {
    fleet[1].status.source.integrationId = options.secondReadyServerIntegrationId
    fleet[1].status.provisioning.integrationId = options.secondReadyServerIntegrationId
  }
  let deploymentTemplates = baseDeploymentTemplates.map((template) => ({ ...template }))
  // Recorded OS image upload intents by upload id; a content request consumes its upload, as the
  // contract's single-use uploads do.
  const imageUploads = new Map<string, Record<string, unknown>>()
  let siteItems = sites
    .filter((site) => !options.singleSite || site.id === 'site-a')
    .map((site) => ({ ...site }))
  let integrationItems = integrations
    .filter((integration) => !options.noProvisioners || integration.kind !== 'provisioner')
    .map((integration) => ({
      ...integration,
      endpoint: options.provisionerEndpoints?.[integration.id] ?? integration.endpoint,
      enabled:
        options.disabledProvisioner && integration.id === 'maas-a'
          ? false
          : integration.enabled,
      hasOperatorCredential: options.singleClusterCredential && integration.id === 'k8s-a' ? false : integration.hasOperatorCredential,
      settings: { ...integration.settings },
      sync: { ...integration.sync },
    }))
  // site-a's Local Repository (local-repository.md): the `repo-a` Pulp Integration, the user's spec,
  // and the scenario the worker last observed. A spec write stores the sources at once; the new
  // ones read pending, and the observed generation stays behind until the worker would converge it.
  let localRepositoryScenario = options.localRepository
  if (localRepositoryScenario) {
    const settings: Record<string, string> = { contentOrigin: 'http://10.170.168.20:8080' }
    integrationItems.push({
      id: 'repo-a', siteId: 'site-a', kind: 'repository', providerKind: 'pulp', name: 'Pulp Taipei', endpoint: 'http://127.0.0.1:24817',
      enabled: true, settings, hasCredential: true, hasOperatorCredential: false,
      sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null }, createdAt: now, updatedAt: now,
    })
  }
  const localRepositorySpec = {
    generation: 1, serveDeployments: localRepositoryScenario !== 'not-serving', sources: [structuredClone(ubuntuSourceSpec)],
  }
  let localRepositoryObservedGeneration = 1
  let localRepositoryReads = 0
  let localRepositoryConflictPending = Boolean(options.localRepositorySpecConflict)
  let hardwareCountersConflictPending = Boolean(options.hardwareCountersConflict)
  // User sources the mock worker has synced; a written spec converges on the second read after it.
  const syncedUserSources = new Set(['ubuntu'])
  let readsUntilSpecConverges = 0
  // The generation the worker last judged `not_serving`, or null when its judgement serves deployments.
  let notServingJudgedGeneration: number | null = null
  const localRepositoryBody = (scenario: LocalRepositoryScenario) => {
    const integration = integrationItems.find((item) => item.id === 'repo-a')
    const userSources: RepositorySourceFixture[] = localRepositorySpec.sources.map((source) => ({
      ...structuredClone(source),
      declaredBy: ['user'],
      ...(syncedUserSources.has(source.name)
        ? { state: 'ready', message: null, lastSyncedAt: repositorySyncedAt, contentCount: source.name === 'ubuntu' ? 64_012 : 1_204 }
        : { state: 'pending', message: null, lastSyncedAt: null, contentCount: 0 }),
    }))
    const pendingUserSources = userSources.filter((source) => source.state === 'pending').map((source) => source.name)
    const servedConditions = scenario === 'ready' && pendingUserSources.length > 0
      ? repositoryConditions('ready', localRepositoryObservedGeneration).map((condition) => condition.type === 'requires_attention'
        ? condition
        : { ...condition, status: 'false', reason: 'syncing', message: pendingUserSources.map((name) => `source ${name} is pending`).join('; ') })
      : repositoryConditions(scenario, localRepositoryObservedGeneration)
    // A spec that does not serve deployments is judged at once, for its own generation, and only
    // for `ready`; `realized` and `requires_attention` keep the last judgement. Once a write turns
    // it on, that `not_serving` judgement stays until the worker converges the new generation.
    if (!localRepositorySpec.serveDeployments) notServingJudgedGeneration = localRepositorySpec.generation
    else if (localRepositoryObservedGeneration >= localRepositorySpec.generation) notServingJudgedGeneration = null
    const judgedNotServing = notServingJudgedGeneration
    const conditions = judgedNotServing === null
      ? servedConditions
      : servedConditions.map((condition) => condition.type === 'ready'
        ? { ...condition, status: 'false', reason: 'not_serving', message: 'the spec does not serve deployments', observedGeneration: judgedNotServing }
        : condition)
    return {
      siteId: 'site-a',
      integrationId: 'repo-a',
      ready: localRepositorySpec.serveDeployments && scenario === 'ready' && pendingUserSources.length === 0,
      contentOrigin: (integration?.settings as Record<string, string> | undefined)?.contentOrigin ?? null,
      spec: structuredClone(localRepositorySpec),
      sources: [...moduleRepositorySources(scenario), ...userSources].sort((left, right) => left.name.localeCompare(right.name)),
      files: repositoryFiles(scenario),
      images: repositoryImages(scenario),
      python: [
        { requirement: 'Flask==3.1.3', declaredBy: ['module:slurm'] },
        { requirement: 'numpy==2.5.4', declaredBy: ['module:slurm'] },
      ],
      observedAt: '2026-08-27T02:58:00Z',
      observedGeneration: localRepositoryObservedGeneration,
      conditions,
    }
  }
  // Provisioner settings are swallow-owned desired state (decision 071). The fixture plays the
  // controller: a PUT stores the spec unapplied, and the next read finds it applied (or refused).
  type HttpProxyConfigFixture = { mode: string; httpProxy: string }
  type HttpProxyFixture = { generation: number; spec: HttpProxyConfigFixture; observed: HttpProxyConfigFixture; applied: number; refused: string }
  const httpProxies = new Map<string, HttpProxyFixture>(
    Object.entries(options.httpProxies ?? {}).map(([id, config]) => [id, { generation: 1, spec: { ...config }, observed: { ...config }, applied: 1, refused: '' }]),
  )
  const httpProxyOf = (integrationId: string): HttpProxyFixture => {
    let proxy = httpProxies.get(integrationId)
    if (!proxy) {
      proxy = { generation: 1, spec: { mode: 'builtin', httpProxy: '' }, observed: { mode: 'builtin', httpProxy: '' }, applied: 1, refused: '' }
      httpProxies.set(integrationId, proxy)
    }
    return proxy
  }
  const httpProxyBody = (integrationId: string, proxy: HttpProxyFixture) => ({
    integrationId, generation: proxy.generation, spec: proxy.spec,
    status: {
      observedGeneration: proxy.applied, conditions: settingConditions(proxy.applied, proxy.generation, proxy.refused),
      observed: proxy.observed, observedAt: now,
    },
  })
  // Subnet DNS is live MAAS state as well, kept per Integration so a write is read back.
  const provisionerSubnets = new Map(
    Object.entries(options.provisionerSubnets ?? {}).map(([id, subnets]) => [id, subnets.map((subnet) => ({ ...subnet }))]),
  )
  // Each subnet's DNS setting: adopted from the fixture's dnsServers (what MAAS holds) on first use.
  const subnetSettings = new Map<string, { generation: number; spec: string[]; applied: number }>()
  const subnetSettingOf = (integrationId: string, subnet: ProvisionerSubnetFixture) => {
    const key = `${integrationId}/${subnet.id}`
    let setting = subnetSettings.get(key)
    if (!setting) {
      setting = { generation: 1, spec: [...subnet.dnsServers], applied: 1 }
      subnetSettings.set(key, setting)
    }
    return setting
  }
  const subnetBody = (integrationId: string, subnet: ProvisionerSubnetFixture) => {
    const setting = subnetSettingOf(integrationId, subnet)
    const { dnsServers, ...facts } = subnet
    return {
      ...facts, generation: setting.generation, spec: { dnsServers: setting.spec },
      status: { observedGeneration: setting.applied, conditions: settingConditions(setting.applied, setting.generation, ''), dnsServers },
    }
  }
  const subnetsOf = (integrationId: string): ProvisionerSubnetFixture[] => {
    let subnets = provisionerSubnets.get(integrationId)
    if (!subnets) {
      subnets = defaultProvisionerSubnets()
      provisionerSubnets.set(integrationId, subnets)
    }
    return subnets
  }
  if (options.multipleProvisioners) {
    integrationItems.push({
      ...integrations.find((integration) => integration.id === 'maas-b')!,
      id: 'maas-a-secondary',
      siteId: 'site-a',
      name: 'MAAS Taipei Secondary',
    })
  }
  let metricBatchIndex = 0
  let activeMetricRequests = 0
  let slurmRequirement: {
    platformType: 'slurm'
    minimumResources: { cpuCores: number; memoryMiB: number; storageGB: number } | null
    updatedAt: string | null
  } = { platformType: 'slurm', minimumResources: null, updatedAt: null }
  const releaseRefreshesRemaining = new Map<string, number>()
  const releaseCleanupRequested = new Set<string>()
  const provisioningTasks: Array<Record<string, unknown>> = Array.from(
    { length: options.serverActivityItemCount ?? 0 },
    (_, index) => {
      const ordinal = index + 1
      return {
        id: `task-activity-${String(ordinal).padStart(2, '0')}`,
        kind: 'release_network_cleanup',
        serverId: 'srv-1',
        status: 'succeeded',
        phase: 'complete',
        attempt: 1,
        error: '',
        requestId: `req-task-${ordinal}`,
        retryable: false,
        createdAt: now,
        updatedAt: now,
      }
    },
  )
  const networkTargets = new Map(fleet.map((server) => {
    const interfaceId = `nic-${server.id}`
    const linkId = `link-${server.id}`
    const hasStaticBinding = options.staticNetworkServerIds?.includes(server.id) ?? false
    // A site-DHCP Server's subnet has no provisioner DHCP; like every other non-static Server it is
    // suggested `automatic`, never `dhcp` (provisioning.md, Network Inspection).
    const siteDhcp = options.siteDhcpNetworkServerIds?.includes(server.id) ?? false
    const subnetName = options.networkSubnetName ?? 'lab-network'
    return [server.id, {
      serverId: server.id,
      editable: server.status.provisioning.state === 'ready' && !server.status.provisioning.locked,
      disabledReason: server.status.provisioning.locked
        ? 'Unlock the Server before changing network configuration.'
        : server.status.provisioning.state === 'ready' ? '' : 'Network configuration can only be changed while the Server is Ready.',
      suggestion: { mode: hasStaticBinding ? 'static' : 'automatic', interfaceId, subnetId: 'subnet-a', ipAddress: hasStaticBinding ? server.status.addresses[0] ?? '' : '', defaultGateway: hasStaticBinding },
      network: {
        interfaces: [{
          id: interfaceId,
          name: 'eno1',
          macAddress: server.status.hardware.macAddresses[0] ?? '',
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
            ipAddress: server.status.addresses[0] ?? '',
            defaultGateway: true,
          }],
          availableSubnets: [{
            id: 'subnet-a',
            name: subnetName,
            cidr: '192.168.40.0/24',
            gatewayAddress: '192.168.40.1',
            managed: true,
            provisionerDhcp: !siteDhcp,
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
      server.status.provisioning.state = 'ready'
      server.status.provisioning.stateSince = new Date().toISOString()
      server.status.provisioning.providerState = 'Ready'
      server.status.provisioning.osSystem = ''
      server.status.provisioning.distroSeries = ''
      server.status.provisioning.ephemeral = false
      server.status.deployment = null
      if (releaseCleanupRequested.has(serverId) && !options.releaseCleanupFails) {
        server.status.addresses = []
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
  const platformItems = platforms.map((platform) => ({
    ...structuredClone(platform),
    lifecycleState: options.platformLifecycleStates?.[platform.id] ?? platform.lifecycleState,
    gpuOperator: options.withoutPlatformGPUOperator && platform.id === 'platform-a' ? null : structuredClone(platform.gpuOperator),
    ...(options.slurmDeviceMetricsExporter && platform.id === 'platform-slurm'
      ? {
          omnistat: null,
          monitoringLinks: [{ name: 'Platform Grafana', url: 'http://10.0.0.21:3000/d/fe0ayyeeoxvy8f/overview' }],
          deviceMetricsExporter: {
            enabled: true, version: '1.5.3', installMethod: 'native', observabilityServerId: 'srv-1',
            state: 'ready', gpuIdentity: 'collapsed', lastWorkflowId: 'op-slurm-dme', updatedAt: now,
          },
          ociContainers: true,
          pmix: true,
        }
      : {}),
  })).map((platform) => {
    const spec = fixturePlatformSpec(platform)
    // A planned Platform's install has not run, so no deployment Workflow records its topology yet.
    const deployment = platform.lifecycleState === 'planned' ? null : platform.deployment
    const seeded = { ...platform, deployment, generation: 1, spec, status: fixturePlatformStatus(platform.lifecycleState, spec) }
    const removal = options.platformRemovals?.[platform.id]
    return removal ? pendingRemoval(seeded, removal) : seeded
  })
  if (options.slurmDeployed) {
    // Controller-only nodes carry no membership (they are not slurmd scheduler nodes); compute
    // nodes do. getServer reads from the fleet, so the controllers must live here for the
    // detail page's controller merge to resolve them.
    const makeSlurmNode = (id: string, hostname: string, octet: number, member: boolean): Server => {
      const base = makeServer(0)
      return {
        ...base,
        id,
        status: {
          ...base.status,
          source: { siteId: 'site-a', integrationId: 'maas-a', providerMachineId: id },
          hostname,
          fqdn: `${hostname}.lab.example`,
          addresses: [`192.168.60.${octet}`],
          membership: member
            ? { platformId: 'platform-slurm-ha', nodeName: hostname, role: 'main', state: 'idle', observedAt: now }
            : null,
          // Every node's Node Exporter fails its scrape, so the health axis reads "down" for each.
          // Controllers must not surface that monitoring "down" as their operational state.
          health: { state: 'down', observedAt: now },
        },
      }
    }
    fleet.push(
      makeSlurmNode('slurm-ctl-1', 'slurm-ctl-01', 11, false),
      makeSlurmNode('slurm-ctl-2', 'slurm-ctl-02', 12, false),
      makeSlurmNode('slurm-cpt-1', 'slurm-cpt-01', 21, true),
      makeSlurmNode('slurm-cpt-2', 'slurm-cpt-02', 22, true),
    )
    // Deployed with Omnistat, whose `container` install method runs the Platform Grafana in a
    // container on the primary controller; no container engine was chosen for the compute nodes, so
    // that Grafana brought Docker CE (platforms.md "Platform Monitoring Dashboards"). MPI is set up.
    const slurmHa = {
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
      gpuStackOwner: 'provisioning', gpuOperator: null,
      monitoringLinks: [{ name: 'Platform Grafana', url: 'http://192.168.60.11:3000/d/edqnz7pce7togc-rms/global-rms' }],
      // It asked for the hardware counters: one compute node collects them, the other runs a ROCm
      // version swallow ships no counter extension for (glossary Omnistat Hardware Counters).
      omnistat: {
        enabled: true, version: '1.15.0', installMethod: 'container', observabilityServerId: 'slurm-ctl-1', state: 'ready',
        hardwareCounters: options.slurmHardwareCountersOff ? { requested: false, nodes: [] } : {
          requested: true,
          nodes: [
            { serverId: 'slurm-cpt-1', state: 'collecting', reason: null },
            { serverId: 'slurm-cpt-2', state: 'unavailable', reason: 'rocm_unsupported' },
          ],
        },
        lastWorkflowId: 'op-slurm-ha-deploy', updatedAt: now,
      },
      deviceMetricsExporter: null, containerRuntime: null, gpuDriver: null, gpuGresType: null, ociContainers: false, pmix: true,
      sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null, memberCount: 2, matchedCount: 2 },
      createdAt: '2026-08-18T00:00:00Z', updatedAt: now,
    }
    const slurmHaSpec = fixturePlatformSpec(slurmHa)
    if (options.slurmHardwareCountersOff) {
      slurmHaSpec.settings = { ...slurmHaSpec.settings, omnistat: { enabled: true, installMethod: 'container', hardwareCounters: false } }
    }
    platformItems.push({ ...slurmHa, generation: 1, spec: slurmHaSpec, status: fixturePlatformStatus(slurmHa.lifecycleState, slurmHaSpec) } as (typeof platformItems)[number])
    integrationItems.push({
      id: 'slurm-ha', siteId: 'site-a', kind: 'platform', providerKind: 'slurm', name: 'lab-slurm-ha (deployed)',
      endpoint: 'http://192.168.60.11:6820', enabled: true, settings: {}, hasCredential: true, hasOperatorCredential: false,
      sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null }, createdAt: '2026-08-18T01:10:00Z', updatedAt: now,
    })
  }
  // Platform Claim (decision 067): a deployed Platform claims its targets from deploy acceptance
  // until its uninstall succeeds or its record is deleted.
  const claimPlatformTargets = (platformId: string, serverIds: readonly string[]) => {
    for (const serverId of serverIds) {
      const server = fleet.find((item) => item.id === serverId)
      if (server && !server.status.platformClaim) server.status.platformClaim = { platformId, claimedAt: now }
    }
  }
  const releasePlatformClaims = (platformId: string) => {
    for (const server of fleet) {
      if (server.status.platformClaim?.platformId === platformId) server.status.platformClaim = null
    }
  }
  if (!options.withoutPlatformClaims && !options.freePlatformCandidates) {
    for (const platform of platformItems) {
      if (platform.origin !== 'deployed' || platform.lifecycleState === 'uninstalled') continue
      claimPlatformTargets(platform.id, platform.deployment?.roleAssignments.map((assignment) => assignment.serverId) ?? [])
    }
  }
  // Mirrors the backend's refusal: a claimed Server is released through its Platform's uninstall
  // and cannot be deleted until then; Recover is refused only for a deployed one, which it would
  // release.
  const platformClaimRefusal = (serverIds: readonly string[], action: 'release' | 'recover' | 'delete'): string | null => {
    const nextSteps = {
      release: 'instead of releasing the Server directly',
      recover: 'instead of recovering it, which would release the deployed Server',
      delete: 'or delete its record before deleting the Server',
    }
    for (const serverId of serverIds) {
      const server = fleet.find((item) => item.id === serverId)
      const claim = server?.status.platformClaim
      if (!server || !claim || (action === 'recover' && server.status.provisioning.state !== 'deployed')) continue
      const platform = platformItems.find((item) => item.id === claim.platformId)
      const owner = platform ? `${platform.type} Platform ${platform.name}` : `Platform ${claim.platformId}`
      return `Server ${server.status.hostname ?? server.id} belongs to ${owner}; uninstall the Platform (with releaseServers to also release its Servers) ${nextSteps[action]}`
    }
    return null
  }
  const operationItems = structuredClone(operations)
  /**
   * Starts the deploy of a Platform created on this page, as its controller does once the install
   * plan is approved: the deploy Workflow appears, and the Platform is `deploying` and converging.
   */
  const startPlatformInstall = (platformId: string) => {
    const deploy = pendingPlatformDeploys.get(platformId)
    const installing = platformItems.find((item) => item.id === platformId)
    if (!deploy || !installing) return null
    pendingPlatformDeploys.delete(platformId)
    operationItems.unshift(deploy)
    installing.lifecycleState = 'deploying'
    installing.lifecycleOperationId = deploy.id
    installing.status = {
      ...installing.status, realizingWorkflowId: deploy.id,
      conditions: platformConditions('converging', 1, `Workflow ${deploy.id} carries out generation 1`),
    }
    return deploy.id
  }
  /**
   * Starts a Platform's uninstall, as its controller does once the uninstall plan is approved: the
   * Platform is `uninstalling` and converging to the absent generation.
   */
  const startPlatformUninstall = (platform: (typeof platformItems)[number]) => {
    const workflowId = `op-uninstall-${platform.id}`
    platform.lifecycleState = 'uninstalling'
    platform.lifecycleOperationId = workflowId
    platform.status = {
      ...platform.status, realizingWorkflowId: workflowId,
      conditions: platformConditions('converging', platform.generation, `Workflow ${workflowId} carries out generation ${platform.generation}`),
    }
    return workflowId
  }
  if (options.serverActivityItemCount) {
    for (const operation of operationItems) {
      operation.targetServerIds = operation.targetServerIds.filter((serverId) => serverId !== 'srv-1')
    }
    for (let index = 0; index < options.serverActivityItemCount; index += 1) {
      const ordinal = index + 1
      const operation = {
        ...structuredClone(operations[0]),
        id: `op-activity-${String(ordinal).padStart(2, '0')}`,
        kind: 'server.maintenance',
        intent: `Maintenance operation ${String(ordinal).padStart(2, '0')}`,
        platformId: null,
        targetServerIds: ['srv-1'],
        execution: {
          runId: `run-activity-${ordinal}`,
          playbook: 'server-maintenance.yml',
          status: 'succeeded',
          statusReason: null,
          startedAt: now,
          finishedAt: now,
        },
        requestedAt: now,
        updatedAt: now,
      }
      operationItems.push(operation as unknown as (typeof operationItems)[number])
    }
  }
  // The succeeded deployment Workflows of the deployed Platforms (platforms.md "Create A Platform"):
  // production-k0s's deploy-kubernetes, and lab-slurm-ha's configure-slurm when it is seeded. They
  // were requested before every other seeded Workflow, so list order and lifecycle links stay as
  // they are.
  if (options.platformDeploymentWorkflows) {
    const succeededDeployment = (id: string, kind: string, platformId: string, intent: string, targetServerIds: string[], at: { requestedAt: string; finishedAt: string }) => {
      const base = structuredClone(operations[0])
      return {
        ...base, id, kind, intent, platformId, status: 'succeeded', targetServerIds,
        targetResources: [{ kind: 'platform', id: platformId }, ...targetServerIds.map((serverId) => ({ kind: 'server', id: serverId }))],
        temporal: { workflowId: `swallow-operation/${id}`, runId: `run-${id}` },
        steps: base.steps.map((step) => ({
          ...step, name: kind === 'configure-slurm' ? 'Install Slurm Platform' : step.name,
          status: 'succeeded', progress: 100, startedAt: at.requestedAt, finishedAt: at.finishedAt,
        })),
        execution: { ...base.execution, runId: `run-${id}`, status: 'succeeded', startedAt: at.requestedAt, finishedAt: at.finishedAt },
        requestedAt: at.requestedAt, startedAt: at.requestedAt, finishedAt: at.finishedAt, updatedAt: at.finishedAt,
      } as (typeof operationItems)[number]
    }
    operationItems.push(succeededDeployment(
      'op-k0s-deploy', 'deploy-kubernetes', 'platform-a', 'Deploy Kubernetes Platform production-k0s', ['srv-1', 'srv-2', 'srv-3'],
      { requestedAt: '2026-08-10T00:05:00Z', finishedAt: '2026-08-10T00:41:00Z' },
    ))
    if (options.slurmDeployed) {
      operationItems.push(succeededDeployment(
        'op-slurm-ha-deploy', 'configure-slurm', 'platform-slurm-ha', 'Deploy lab-slurm-ha', ['slurm-ctl-1', 'slurm-ctl-2', 'slurm-cpt-1', 'slurm-cpt-2'],
        { requestedAt: '2026-08-18T00:10:00Z', finishedAt: '2026-08-18T01:08:00Z' },
      ))
    }
  }
  // inspect-hardware Workflows waiting for attention (server-enrollment.md): the inspect Task ran
  // out of attempts because the Server never network-booted into MAAS.
  for (const serverId of options.inspectionAttentionServerIds ?? []) {
    const inspection = {
      id: `op-inspect-${serverId}`, schemaVersion: 3, kind: 'inspect-hardware',
      intent: `Inspect hardware of ${serverId}`, intentSnapshot: { serverId, origin: 'automatic' },
      definition: 'hardware-inspection', definitionVersion: 1,
      status: 'requires_attention', statusReason: 'Job ensure-inspected requires user attention.',
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
      jobs: [{ name: 'ensure-inspected', label: 'Inspect Hardware', retry: 'job' }],
      execution: { runId: 'run-inspect', playbook: '', status: 'running', statusReason: null, startedAt: now, finishedAt: null },
      requestedBy: 'system', requestedAt: now, updatedAt: now,
    }
    operationItems.push(inspection as unknown as (typeof operationItems)[number])
  }
  for (const serverId of options.powerAttentionServerIds ?? []) {
    const target = [{ kind: 'server', id: serverId }]
    const pending = (id: string, kind: string, name: string, dependsOn: string[]) => ({
      id, kind, name, job: 'ensure-inspected', executor: kind === 'ensure-boot-media' ? 'internal' : 'maas', dependsOn, targets: target,
      status: 'pending', attempt: 0, progress: 0, error: null, externalExecution: null, artifacts: [], startedAt: null, finishedAt: null,
    })
    const inspection = {
      id: `op-power-${serverId}`, schemaVersion: 3, kind: 'inspect-hardware',
      intent: `Inspect hardware of ${serverId}`, intentSnapshot: { serverId, origin: 'automatic' },
      definition: 'hardware-inspection', definitionVersion: 1,
      status: 'requires_attention', statusReason: 'Job ensure-inspected requires user attention.',
      startState: 'started', temporal: { workflowId: `swallow-operation/op-power-${serverId}`, runId: 'run-power' },
      siteId: 'site-a', platformId: null,
      targetResources: target, targetServerIds: [serverId], retryOfOperationId: null,
      steps: [
        {
          id: `wait-enrollment-${serverId}`, kind: 'wait-enrollment-settled', name: `Wait for the enrollment of ${serverId} to finish`,
          job: 'ensure-inspected', executor: 'maas', dependsOn: [], targets: target,
          status: 'requires_attention', attempt: 1, progress: 0,
          error: {
            code: 'power_configuration_required', retryable: true, stage: 'enrollment',
            message: `${serverId} has no power driver, so it finished enrolling, but no power driver can report the power-off that ends enrollment. Set the Server's Power Configuration, then retry this Task.`,
          },
          externalExecution: null, artifacts: [], startedAt: now, finishedAt: now,
        },
        pending(`ensure-boot-media-${serverId}`, 'ensure-boot-media', `Ensure Boot Media on ${serverId}`, [`wait-enrollment-${serverId}`]),
        pending(`inspect-${serverId}`, 'inspect', `Inspect hardware of ${serverId}`, [`ensure-boot-media-${serverId}`]),
      ],
      jobs: [{ name: 'ensure-inspected', label: 'Inspect Hardware', retry: 'job' }],
      execution: { runId: 'run-power', playbook: '', status: 'running', statusReason: null, startedAt: now, finishedAt: null },
      requestedBy: 'system', requestedAt: now, updatedAt: now,
    }
    operationItems.push(inspection as unknown as (typeof operationItems)[number])
  }
  if (options.controllerWorkflow) {
    const pending = options.controllerWorkflow === 'pending'
    const status = pending ? 'pending' : 'succeeded'
    const target = [{ kind: 'server', id: 'srv-2' }]
    const controllerWorkflow = {
      id: CONTROLLER_WORKFLOW_ID, schemaVersion: 3, kind: 'configure-node-exporter',
      intent: 'Install the Node Exporter on gpu-node-02', intentSnapshot: {},
      definition: 'software-install', definitionVersion: 1,
      status, statusReason: null,
      startState: pending ? 'pending' : 'started',
      temporal: { workflowId: `swallow-operation/${CONTROLLER_WORKFLOW_ID}`, runId: pending ? '' : 'run-node-exporter' },
      siteId: 'site-a', platformId: null,
      targetResources: target, targetServerIds: ['srv-2'], retryOfOperationId: null,
      steps: [
        {
          id: 'install-node-exporter-srv-2', kind: 'ansible-playbook', name: 'Install the Node Exporter on gpu-node-02',
          executor: 'ansible', dependsOn: [], targets: target, status, attempt: pending ? 0 : 1, progress: pending ? 0 : 100,
          error: null, externalExecution: null, artifacts: [],
          startedAt: pending ? null : '2026-08-27T02:40:00Z', finishedAt: pending ? null : '2026-08-27T02:42:00Z',
        },
      ],
      leases: [], requestCorrelation: null,
      execution: {
        runId: pending ? '' : 'run-node-exporter', playbook: 'ansible-playbook', status, statusReason: null,
        startedAt: pending ? null : '2026-08-27T02:40:00Z', finishedAt: pending ? null : '2026-08-27T02:42:00Z',
      },
      requestedBy: 'swallow:node-exporter-controller', requestedAt: '2026-08-27T02:39:00Z',
      startedAt: pending ? null : '2026-08-27T02:40:00Z', finishedAt: pending ? null : '2026-08-27T02:42:00Z',
      updatedAt: pending ? '2026-08-27T02:39:00Z' : '2026-08-27T02:42:00Z',
    }
    operationItems.push(controllerWorkflow as unknown as (typeof operationItems)[number])
  }
  for (const [serverId, status] of Object.entries(options.releaseWorkflows ?? {})) {
    if (status === null) {
      for (const operation of operationItems) {
        operation.targetServerIds = operation.targetServerIds.filter((target) => target !== serverId)
      }
      continue
    }
    const finished = status === 'canceled' ? now : null
    const release = {
      id: `op-release-${serverId}`, schemaVersion: 3, kind: 'release-os',
      intent: 'Release 1 Server(s)', intentSnapshot: {},
      definition: 'os-release', definitionVersion: 1,
      status, statusReason: status === 'waiting_external' ? 'Waiting for provider observation.' : null,
      startState: 'started', temporal: { workflowId: `swallow-operation/op-release-${serverId}`, runId: 'run-release' },
      siteId: 'site-a', platformId: null,
      targetResources: [{ kind: 'server', id: serverId }], targetServerIds: [serverId], retryOfOperationId: null,
      steps: [
        {
          id: `release-${serverId}`, kind: 'release-os', name: `Release ${serverId}`,
          executor: 'maas', dependsOn: [], targets: [{ kind: 'server', id: serverId }],
          status, attempt: 1, progress: 0,
          error: status === 'requires_attention'
            ? { code: 'provider_observation_timeout', message: 'Timed out waiting for the Server to return to Ready.', retryable: true, stage: 'release' }
            : null,
          externalExecution: null, artifacts: [], startedAt: '2026-08-27T02:58:00Z', finishedAt: finished,
        },
      ],
      execution: { runId: 'run-release', playbook: '', status, statusReason: null, startedAt: '2026-08-27T02:58:00Z', finishedAt: finished },
      requestedBy: 'admin', requestedAt: '2026-08-27T02:58:00Z', startedAt: '2026-08-27T02:58:00Z', finishedAt: finished, updatedAt: now,
    }
    operationItems.push(release as unknown as (typeof operationItems)[number])
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
        server.status.deployment = {
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
        owner: id,
        fencingToken: index + 1,
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
    if (path === '/api/v1/change-plans' && request.method() === 'GET') {
      if (options.applyApprovedChangePlans) {
        changePlanItems = changePlanItems.map((item) => {
          const reads = approvedPlanReads.get(item.id)
          if (item.state !== 'approved' || reads === undefined) return item
          approvedPlanReads.set(item.id, reads + 1)
          if (reads < 1) return item
          approvedPlanReads.delete(item.id)
          return { ...item, state: 'applied', workflowId: `op-${item.id}`, updatedAt: now }
        })
      }
      const states = url.searchParams.getAll('state')
      return json(route, { items: changePlanItems.filter((plan) => states.length === 0 || states.includes(plan.state)) })
    }
    const changePlanMatch = path.match(/^\/api\/v1\/change-plans\/([^/]+)(?:\/(approve|reject))?$/)
    if (changePlanMatch) {
      const plan = changePlanItems.find((item) => item.id === decodeURIComponent(changePlanMatch[1]))
      if (!plan) return json(route, { error: { code: 'not_found', message: 'Change Plan not found.', requestId: 'req-plan-missing' } }, 404)
      const decision = changePlanMatch[2]
      if (!decision) return json(route, plan)
      if (decision === 'approve' && (plan.state !== 'pending_approval' || plan.blockers.length > 0)) {
        return json(route, { error: { code: 'conflict', message: 'change plan has blockers.', requestId: 'req-plan-blocked' } }, 409)
      }
      const decided = decision === 'approve'
        ? { ...plan, state: 'approved', stateReason: 'approved by admin', decidedBy: 'admin', decidedAt: now }
        : { ...plan, state: 'rejected', stateReason: 'rejected by admin', decidedBy: 'admin', decidedAt: now }
      changePlanItems = changePlanItems.map((item) => (item.id === plan.id ? decided : item))
      if (decision === 'approve') {
        approvedPlanReads.set(plan.id, 0)
        // Approving a created Platform's install starts its deploy Workflow, as the controller does.
        startPlatformInstall(plan.resource.id)
      }
      const absent = platformItems.find((item) => item.id === plan.resource.id && item.spec?.desired === 'absent')
      if (decision === 'reject' && absent) {
        // A rejected uninstall leaves the Platform absent with no plan waiting, as the controller judges it.
        absent.status = {
          ...absent.status,
          conditions: platformConditions('plan_required', absent.generation, 'the uninstall needs an approved Change Plan; delete the Platform again to propose one'),
        }
      }
      return json(route, decided)
    }
    if (path === '/api/v1/change-plan-policy') {
      if (request.method() === 'PUT') {
        const written = request.postDataJSON() as { approveOnSubmit?: unknown }
        if (typeof written.approveOnSubmit !== 'boolean') {
          return json(route, { error: { code: 'validation_error', message: 'approveOnSubmit must be true or false.', requestId: 'req-policy-invalid' } }, 400)
        }
        changePlanPolicy = { approveOnSubmit: written.approveOnSubmit, updatedAt: now, updatedBy: 'admin' }
      }
      return json(route, changePlanPolicy)
    }
    if (path === '/api/v1/software/catalog') {
      return json(route, { items: [
        { kind: 'docker-ce', label: 'Docker CE', roles: [], mutuallyExclusiveWith: ['podman'], refusedForKubernetesMembers: true, installMethods: [], containerRuntimes: [], specFields: ['version', 'enableApi'] },
        { kind: 'podman', label: 'Podman', roles: [], mutuallyExclusiveWith: ['docker-ce'], refusedForKubernetesMembers: true, installMethods: [], containerRuntimes: [], specFields: ['version'] },
        { kind: 'nfs', label: 'NFS', roles: ['server', 'client'], mutuallyExclusiveWith: [], refusedForKubernetesMembers: false, installMethods: [], containerRuntimes: [], specFields: ['exportPath', 'exportOptions', 'source', 'mountPath', 'mountOptions'] },
        { kind: 'prometheus', label: 'Prometheus', roles: [], mutuallyExclusiveWith: [], refusedForKubernetesMembers: false, installMethods: ['container', 'native'], containerRuntimes: ['docker-ce', 'podman'], specFields: ['installMethod', 'containerRuntime', 'version', 'scrapeJob', 'scrapeTargets', 'scrapeServerIds', 'scrapePort', 'listen', 'retention'] },
        { kind: 'grafana', label: 'Grafana', roles: [], mutuallyExclusiveWith: [], refusedForKubernetesMembers: false, installMethods: ['container', 'native'], containerRuntimes: ['docker-ce', 'podman'], specFields: ['installMethod', 'containerRuntime', 'version', 'prometheusUrl', 'dashboards'] },
        { kind: 'device-metrics-exporter', label: 'AMD Device Metrics Exporter', roles: [], mutuallyExclusiveWith: [], refusedForKubernetesMembers: true, refusedForClaimedServers: true, requiresAmdComputeGpu: true, installMethods: [], containerRuntimes: [], specFields: [] },
        { kind: 'node-exporter', label: 'Node Exporter', roles: [], mutuallyExclusiveWith: [], refusedForKubernetesMembers: false, installMethods: [], containerRuntimes: [], specFields: [] },
        { kind: 'victoriametrics', label: 'VictoriaMetrics', roles: [], mutuallyExclusiveWith: [], refusedForKubernetesMembers: false, installMethods: [], containerRuntimes: [], specFields: ['listen', 'retention'] },
      ] })
    }
    // Modules (modules.md): the build's modules; NFS and VictoriaMetrics declare the spec schemas
    // their install forms read.
    if (path === '/api/v1/modules') {
      if (options.modulesUnavailable) return json(route, { error: { code: 'internal', message: 'module list unavailable' } }, 500)
      return json(route, { items: [
        { kind: 'accelerator', id: 'amd', version: '0.1.0', spec: null },
        { kind: 'platform', id: 'kubernetes', version: '0.1.0', spec: null },
        { kind: 'platform', id: 'slurm', version: '0.1.0', spec: null },
        ...['device-metrics-exporter', 'docker-ce', 'grafana'].map((id) => ({ kind: 'software', id, version: '0.1.0', spec: null })),
        { kind: 'software', id: 'nfs', version: '0.1.0', spec: nfsSpecSchema },
        ...['node-exporter', 'podman', 'prometheus'].map((id) => ({ kind: 'software', id, version: '0.1.0', spec: null })),
        { kind: 'software', id: 'victoriametrics', version: '0.1.0', spec: victoriaMetricsSpecSchema },
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
    // Managed Software assignments used by catalog, detail, install, and Docker explorer tests.
    if (path === '/api/v1/software/assignments' && request.method() === 'GET') {
      softwareAssignmentRequestCount += 1
      if (options.softwareAssignmentFailureRequestNumbers?.includes(softwareAssignmentRequestCount)) {
        return json(route, { error: { code: 'provider_unavailable', message: 'Software assignments are temporarily unavailable.', requestId: 'req-software-list' } }, 503)
      }
      const serverId = url.searchParams.get('serverId')
      const kind = url.searchParams.get('kind')
      const items = softwareAssignmentItems.filter((item) => (!serverId || item.serverId === serverId) && (!kind || item.kind === kind))
      return json(route, { items })
    }
    if (path === '/api/v1/software/assignments' && request.method() === 'POST') {
      const body = (request.postDataJSON() ?? {}) as {
        kind?: SoftwareAssignment['kind']
        assignments?: Array<{ serverId: string; roles?: SoftwareAssignment['roles'] }>
        spec?: Record<string, unknown>
      }
      options.onSoftwareInstallRequest?.(body as Record<string, unknown>)
      // Mirrors software.md: the Device Metrics Exporter needs AMD compute GPUs and is left to the
      // Platform that claims a Server.
      if (body.kind === 'device-metrics-exporter') {
        for (const target of body.assignments ?? []) {
          const server = fleet.find((item) => item.id === target.serverId)
          if (server?.status.platformClaim) {
            return json(route, { error: { code: 'conflict', message: `${server.status.hostname} belongs to Platform ${server.status.platformClaim.platformId}, which manages AMD Device Metrics Exporter on its Servers; cannot install it directly` } }, 409)
          }
          if (server && !server.status.gpus.some((gpu) => gpu.kind === 'compute' && /amd|advanced micro devices/i.test(gpu.vendor))) {
            return json(route, { error: { code: 'conflict', message: `${server.status.hostname} lists no AMD compute GPU in its GPU Inventory, which AMD Device Metrics Exporter needs` } }, 409)
          }
        }
      }
      if (options.softwareInstallError) {
        return json(route, {
          error: { code: options.softwareInstallError.code, message: options.softwareInstallError.message },
        }, options.softwareInstallError.status)
      }
      for (const target of body.assignments ?? []) {
        const existing = softwareAssignmentItems.find((item) =>
          item.serverId === target.serverId && item.kind === body.kind
        )
        if (existing) {
          Object.assign(existing, {
            roles: target.roles ?? [],
            state: 'pending',
            spec: body.spec ?? null,
            lastWorkflowId: 'op-docker-reapply',
            updatedAt: now,
          })
        } else if (body.kind) {
          const created: SoftwareAssignment = {
            serverId: target.serverId,
            kind: body.kind,
            roles: target.roles ?? [],
            spec: body.spec ?? null,
            state: 'pending',
            lastWorkflowId: 'op-docker-reapply',
            lastAppliedAt: null,
            createdAt: now,
            updatedAt: now,
          }
          softwareAssignmentItems = [...softwareAssignmentItems, created]
          if (body.kind === 'docker-ce') dockerAssignments.set(target.serverId, created)
        }
      }
      return json(route, { operationId: 'op-docker-reapply' }, 202)
    }
    if (path === '/api/v1/software/uninstall' && request.method() === 'POST') {
      const body = (request.postDataJSON() ?? {}) as { kind?: SoftwareAssignment['kind']; serverIds?: string[] }
      options.onSoftwareUninstallRequest?.(body as Record<string, unknown>)
      const claimed = body.kind === 'device-metrics-exporter'
        ? fleet.find((item) => body.serverIds?.includes(item.id) && item.status.platformClaim)
        : undefined
      if (claimed) {
        return json(route, { error: { code: 'conflict', message: `${claimed.status.hostname} belongs to Platform ${claimed.status.platformClaim?.platformId}, which manages AMD Device Metrics Exporter on its Servers; cannot uninstall it directly` } }, 409)
      }
      for (const serverId of body.serverIds ?? []) {
        const existing = softwareAssignmentItems.find((item) => item.serverId === serverId && item.kind === body.kind)
        if (existing) Object.assign(existing, { state: 'uninstalling', lastWorkflowId: 'op-software-uninstall', updatedAt: now })
      }
      return json(route, { operationId: 'op-software-uninstall' }, 202)
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
    // Local Repository (local-repository.md): only site-a has one, and only with a scenario set.
    const localRepositoryMatch = path.match(/^\/api\/v1\/sites\/([^/]+)\/local-repository(\/content|\/spec|\/sync)?$/)
    if (localRepositoryMatch) {
      const method = request.method()
      const sub = localRepositoryMatch[2] ?? ''
      if (localRepositoryMatch[1] !== 'site-a' || !localRepositoryScenario) {
        return json(route, { error: { code: 'not_found', message: 'The site has no repository integration.', requestId: 'req-no-local-repository' } }, 404)
      }
      if (sub === '' && method === 'GET') {
        localRepositoryReads += 1
        const settleAfter = options.localRepositorySettleAfterReads
        if (localRepositoryScenario === 'syncing' && settleAfter !== undefined && localRepositoryReads > settleAfter) localRepositoryScenario = 'ready'
        // The worker converges nothing while the spec does not serve deployments.
        if (readsUntilSpecConverges > 0 && localRepositorySpec.serveDeployments) {
          readsUntilSpecConverges -= 1
          if (readsUntilSpecConverges === 0) {
            for (const source of localRepositorySpec.sources) syncedUserSources.add(source.name)
            localRepositoryObservedGeneration = localRepositorySpec.generation
          }
        }
        return json(route, localRepositoryBody(localRepositoryScenario))
      }
      if (sub === '/content' && method === 'GET') {
        const failure = options.localRepositoryContentError
        if (failure) return json(route, { error: { code: failure.code, message: failure.message, requestId: 'req-local-repository-content' } }, failure.status)
        const format = url.searchParams.get('format') ?? ''
        if (!['deb', 'rpm', 'oci', 'python', 'file'].includes(format)) {
          return json(route, { error: { code: 'validation_error', message: 'format is required: one of deb, rpm, oci, python, file' } }, 400)
        }
        const page = Number(url.searchParams.get('page') ?? '1')
        const pageSize = Number(url.searchParams.get('pageSize') ?? '20')
        if (!Number.isInteger(page) || page < 1 || !Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100) {
          return json(route, { error: { code: 'validation_error', message: 'page must be at least 1 and pageSize between 1 and 100' } }, 400)
        }
        const source = url.searchParams.get('source')
        const search = (url.searchParams.get('search') ?? '').toLowerCase()
        const matching = repositoryContent(format).filter((item) => (!source || item.source === source) && (!search || item.name.toLowerCase().includes(search)))
        return route.fulfill({
          status: 200, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' },
          body: JSON.stringify({ items: matching.slice((page - 1) * pageSize, page * pageSize), total: matching.length, page, pageSize }),
        })
      }
      if (sub === '/spec' && method === 'PUT') {
        const body = (request.postDataJSON() ?? {}) as { generation?: unknown; serveDeployments?: unknown; sources?: unknown }
        options.onLocalRepositoryRequest?.(method, path, body as Record<string, unknown>)
        if (localRepositoryConflictPending) {
          localRepositoryConflictPending = false
          localRepositorySpec.generation += 1
          localRepositorySpec.sources.push({ name: 'ports', format: 'deb', url: 'http://ports.ubuntu.com/ubuntu-ports', suites: ['noble'], components: ['main'], architectures: ['arm64'] })
        }
        if (body.generation !== localRepositorySpec.generation) {
          return json(route, { error: { code: 'conflict', message: `the local repository spec changed since it was read: generation ${String(body.generation)} is not ${localRepositorySpec.generation}`, requestId: 'req-local-repository-conflict' } }, 409)
        }
        const written = Array.isArray(body.sources) ? (body.sources as Array<Record<string, unknown>>) : []
        const words = (value: unknown) => (Array.isArray(value) ? value.map(String) : [])
        const sources = written.map((source) => ({
          name: String(source.name ?? ''), format: String(source.format ?? ''), url: String(source.url ?? ''),
          suites: words(source.suites), components: words(source.components), architectures: words(source.architectures),
        }))
        if (typeof body.serveDeployments !== 'boolean') {
          return json(route, { error: { code: 'validation_error', message: 'serveDeployments is required' } }, 400)
        }
        const invalid = sources.find((source) => !source.name || (source.format === 'deb' && (source.suites.length === 0 || source.components.length === 0)))
        if (!Array.isArray(body.sources) || invalid || new Set(sources.map((source) => source.name)).size !== sources.length) {
          return json(route, { error: { code: 'validation_error', message: `invalid local repository spec${invalid ? `: source ${invalid.name} needs suites and components` : ''}` } }, 400)
        }
        localRepositorySpec.serveDeployments = body.serveDeployments
        localRepositorySpec.sources = sources
        localRepositorySpec.generation += 1
        readsUntilSpecConverges = 2
        return json(route, localRepositoryBody(localRepositoryScenario))
      }
      if (sub === '/sync' && method === 'POST') {
        options.onLocalRepositoryRequest?.(method, path, null)
        return route.fulfill({ status: 202, contentType: 'text/plain', body: 'Accepted' })
      }
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
    // Provisioner HTTP Proxy (provisioner-http-proxy.md): live MAAS state, so the fixture keeps it
    // per Integration and normalizes a write the way the API does before reading it back.
    const httpProxyMatch = path.match(/^\/api\/v1\/provisioning\/integrations\/([^/]+)\/http-proxy$/)
    if (httpProxyMatch) {
      const integrationId = httpProxyMatch[1]
      const integration = integrationItems.find((item) => item.id === integrationId)
      if (!integration) return json(route, { error: { code: 'not_found', message: 'Integration not found.' } }, 404)
      if (integration.kind !== 'provisioner') {
        return json(route, { error: { code: 'validation_error', message: 'integration is not a provisioner' } }, 400)
      }
      if (request.method() === 'GET') {
        const proxy = httpProxyOf(integrationId)
        if (proxy.applied < proxy.generation && !proxy.refused) {
          if (options.httpProxyWriteForbidden) {
            proxy.refused = 'The provisioner credential may not change MAAS settings; its MAAS account must be an administrator.'
          } else {
            proxy.observed = { ...proxy.spec }
            proxy.applied = proxy.generation
          }
        }
        return json(route, httpProxyBody(integrationId, proxy))
      }
      if (request.method() === 'PUT') {
        const body = (request.postDataJSON() ?? {}) as { mode?: string; httpProxy?: string }
        options.onHttpProxyRequest?.(integrationId, body)
        const mode = body.mode ?? ''
        const needsUrl = mode === 'external' || mode === 'peer'
        if (!['none', 'builtin', 'external', 'peer'].includes(mode)) {
          return json(route, { error: { code: 'validation_error', message: `invalid provisioner HTTP proxy: mode "${mode}" is not one of none, builtin, external, peer` } }, 400)
        }
        if (needsUrl && !/^https?:\/\/[^/@?#\s]+\/?$/.test(body.httpProxy?.trim() ?? '')) {
          return json(route, { error: { code: 'validation_error', message: 'invalid provisioner HTTP proxy: httpProxy must start with http:// or https://' } }, 400)
        }
        const proxy = httpProxyOf(integrationId)
        proxy.spec = { mode, httpProxy: needsUrl ? (body.httpProxy ?? '').trim().replace(/\/$/, '') : '' }
        proxy.generation++
        proxy.refused = ''
        return json(route, httpProxyBody(integrationId, proxy))
      }
    }
    // Subnet DNS (provisioner-subnet-dns.md): the list, and a write validated the way the API does
    // before it is read back.
    const subnetListMatch = path.match(/^\/api\/v1\/provisioning\/integrations\/([^/]+)\/subnets$/)
    const subnetDnsMatch = path.match(/^\/api\/v1\/provisioning\/integrations\/([^/]+)\/subnets\/([^/]+)\/dns-servers$/)
    if (subnetListMatch || subnetDnsMatch) {
      const integrationId = (subnetListMatch ?? subnetDnsMatch)![1]
      const integration = integrationItems.find((item) => item.id === integrationId)
      if (!integration) return json(route, { error: { code: 'not_found', message: 'Integration not found.' } }, 404)
      if (integration.kind !== 'provisioner') {
        return json(route, { error: { code: 'validation_error', message: 'integration is not a provisioner' } }, 400)
      }
      if (subnetListMatch && request.method() === 'GET') {
        for (const subnet of subnetsOf(integrationId)) {
          const setting = subnetSettingOf(integrationId, subnet)
          if (setting.applied < setting.generation) {
            subnet.dnsServers = [...setting.spec]
            setting.applied = setting.generation
          }
        }
        return json(route, { integrationId, subnets: subnetsOf(integrationId).map((subnet) => subnetBody(integrationId, subnet)) })
      }
      if (subnetDnsMatch && request.method() === 'PUT') {
        const subnetId = decodeURIComponent(subnetDnsMatch[2])
        const body = (request.postDataJSON() ?? {}) as { dnsServers?: unknown }
        options.onSubnetDnsRequest?.(integrationId, subnetId, body)
        const subnet = subnetsOf(integrationId).find((item) => item.id === subnetId)
        if (!subnet) return json(route, { error: { code: 'not_found', message: 'The provisioner has no such subnet.' } }, 404)
        if (!Array.isArray(body.dnsServers)) {
          return json(route, { error: { code: 'validation_error', message: 'dnsServers is required; send [] to clear it.' } }, 400)
        }
        const servers = (body.dnsServers as unknown[]).map((entry) => String(entry).trim())
        if (servers.length > 8 || new Set(servers).size !== servers.length || servers.some((entry) => !/^[0-9a-f.:]+$/i.test(entry))) {
          return json(route, { error: { code: 'validation_error', message: 'invalid subnet DNS servers: enter up to 8 distinct IP addresses' } }, 400)
        }
        const setting = subnetSettingOf(integrationId, subnet)
        setting.spec = servers
        setting.generation++
        return json(route, subnetBody(integrationId, subnet))
      }
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
        hasOperatorCredential: false,
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
        const referenced = fleet.some((server) => server.status.source.integrationId === integration.id) ||
          deploymentTemplates.some((template) => template.integrationId === integration.id)
        if (referenced) {
          return json(route, { error: { code: 'conflict', message: 'Resources still reference this Integration.' } }, 409)
        }
        integrationItems = integrationItems.filter((item) => item.id !== integration.id)
        return json(route, { success: true })
      }
    }

    if (path === '/api/v1/provisioning/image-uploads' && request.method() === 'POST') {
      const intent = request.postDataJSON() as Record<string, unknown>
      options.onOSImageUploadRequest?.(String(intent.integrationId ?? ''))
      options.onOSImageUploadDefaultUser?.(String(intent.defaultUser ?? ''))
      options.onOSImageUploadIntent?.(intent)
      const id = `upload-${imageUploads.size + 1}`
      imageUploads.set(id, intent)
      return json(route, { id, expiresAt: '2026-05-02T16:00:00Z' }, 201)
    }
    const uploadContent = path.match(/^\/api\/v1\/provisioning\/image-uploads\/([^/]+)\/content$/)
    if (uploadContent && request.method() === 'PUT') {
      const id = decodeURIComponent(uploadContent[1])
      if (!imageUploads.delete(id)) {
        return json(route, { error: { code: 'not_found', message: 'OS image upload not found or expired.' } }, 404)
      }
      options.onOSImageUploadContent?.(request.postDataBuffer()?.length ?? 0, request.headers()['content-type'] ?? '')
      // The provider classifies an uploaded artifact (here as the custom osystem), which is what
      // the created row reflects.
      return json(
        route,
        catalogImage({
          id: 'ubuntu-24.04-rocm',
          name: 'Ubuntu 24.04 ROCm',
          osSystem: 'custom',
          release: 'ubuntu-24.04-rocm',
          sizeBytes: 16,
        }),
        201,
      )
    }
    if (path === '/api/v1/provisioning/images') {
      const integrationId = url.searchParams.get('integrationId') ?? ''
      options.onOSImageCatalogRequest?.(integrationId)
      if (options.failImageIntegrationIds?.includes(integrationId)) {
        return json(route, { error: { code: 'provider_unavailable', message: 'Image provider is unavailable' } }, 503)
      }
      return json(route, imageCatalog)
    }
    if (path === '/api/v1/provisioning/images/metadata' && request.method() === 'PUT') {
      options.onOSImageMetadataRequest?.(request.postDataJSON() as Record<string, unknown>)
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
        const generation = sshKeyItems[0].generation + 1
        sshKeyItems[0] = {
          ...sshKeyItems[0], generation,
          spec: { ...sshKeyItems[0].spec, fingerprint: 'SHA256:regenerated', publicKey: 'ssh-ed25519 AAAAREGENERATED swallow-deployment' },
          // The old material's judgements no longer count: every provisioner reads as pending.
          status: sshKeyStatus(generation, sshKeyItems[0].status.provisioners),
        }
        return json(route, sshKeyItems[0])
      }
      const keyMatch = path.match(/^\/api\/v1\/ssh-keys\/([^/]+)$/)
      if (keyMatch && request.method() === 'GET') {
        const index = sshKeyItems.findIndex((item) => item.id === keyMatch[1])
        if (index < 0) return json(route, { error: { code: 'not_found', message: 'SSH key not found.' } }, 404)
        if (settlingKeyIds.delete(sshKeyItems[index].id)) {
          const settled = sshKeyItems[index]
          sshKeyItems[index] = {
            ...settled,
            status: sshKeyStatus(settled.generation, settled.status.provisioners.map(({ integrationId, siteId }) => keySynced(integrationId, siteId, settled.generation))),
          }
        }
        return json(route, sshKeyItems[index])
      }
      if (keyMatch && request.method() === 'DELETE') {
        const target = sshKeyItems.find((item) => item.id === keyMatch[1])
        if (target?.spec.purpose === 'deployment') {
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
    // A rebuild re-renders the current template in place: same id and URL, now with the UEFI
    // hand-off, recorded as rebuilt by the signed-in admin.
    const bootISORebuildMatch = path.match(/^\/api\/v1\/provisioning\/boot-isos\/([^/]+)\/rebuild$/)
    if (bootISORebuildMatch && request.method() === 'POST') {
      options.onBootISORequest?.('POST', path, null)
      const index = bootISOItems.findIndex((item) => item.id === decodeURIComponent(bootISORebuildMatch[1]))
      if (index < 0) return json(route, { error: { code: 'not_found', message: 'Boot ISO not found.' } }, 404)
      if (options.bootISOBuildError) {
        const { status, code, message } = options.bootISOBuildError
        return json(route, { error: { code, message, requestId: 'req-boot-iso' } }, status)
      }
      const rebuilt = { ...bootISOItems[index], legacyTemplate: false, rebuiltAt: now, rebuiltBy: 'admin' }
      bootISOItems = bootISOItems.map((item, position) => (position === index ? rebuilt : item))
      return json(route, bootISOView(rebuilt))
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
        network: body.network ?? { mode: 'automatic', defaultGateway: false },
        ephemeral: body.deployTarget === 'ram',
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
        const locked = fleet.find((server) => server.id === serverId)?.status.provisioning.locked
        if (locked) return [{ serverId, code: "locked", message: "Unlock the Server before deployment." }]
        return message ? [{
          serverId,
          code: 'provider_not_ready',
          message,
        }] : []
      })
      return json(route, {
        valid: issues.length === 0,
        integrationId: fleet.find((server) => body.serverIds.includes(server.id))?.status.source.integrationId ?? '',
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
      await options.deploymentRequestGate
      const serverIds = body.serverIds as string[]
      const operationId = createProvisioningOperation('deploy-os', serverIds, body)
      return json(route, { operationId }, 202)
    }

    // Models `cancelActiveWorkflows` (decision 061): the backend cancels every unfinished Operation
    // holding a target and accepts the request, or answers 409 naming one whose cancel it refused.
    const supersedeActiveOperations = (serverIds: string[]): string | null => {
      const terminal = new Set(['succeeded', 'failed', 'partially_succeeded', 'canceled'])
      const holders = operationItems.filter((item) =>
        item.targetServerIds.some((serverId) => serverIds.includes(serverId)) &&
        !terminal.has(item.status ?? item.execution.status))
      if (holders.length > 0 && options.cancelRefusedMessage) {
        return `${holders[0].intent} could not be canceled: ${options.cancelRefusedMessage}`
      }
      for (const holder of holders) {
        const operation = holder as unknown as Record<string, unknown>
        operation.status = 'canceling'
        operation.statusReason = 'Canceling active work.'
        operation.execution = { ...(operation.execution as Record<string, unknown>), status: 'canceling', statusReason: 'Canceling active work.' }
        options.onWorkflowSuperseded?.(holder.id)
      }
      return null
    }

    if (path === '/api/v1/provisioning/release-operations' && request.method() === 'POST') {
      const body = request.postDataJSON() as Record<string, unknown>
      const serverIds = body.serverIds as string[]
      const claimed = platformClaimRefusal(serverIds, 'release')
      if (claimed) return json(route, { error: { code: 'conflict', message: claimed, requestId: 'req-release-claimed' } }, 409)
      if (body.cancelActiveWorkflows) {
        const refused = supersedeActiveOperations(serverIds)
        if (refused) return json(route, { error: { code: 'conflict', message: refused, requestId: 'req-release-busy' } }, 409)
      }
      const releaseBody = { ...body }
      delete releaseBody.serverIds
      for (const serverId of serverIds) {
        options.onServerReleaseRequest?.(serverId, releaseBody)
        if (options.serverActionFailureIds?.includes(serverId)) continue
        const server = fleet.find((item) => item.id === serverId)
        if (server) {
          if (!options.deferReleaseProjection) {
            server.status.provisioning.state = 'releasing'
            server.status.provisioning.stateSince = new Date().toISOString()
            server.status.provisioning.providerState = 'Releasing'
          }
          releaseRefreshesRemaining.set(serverId, options.releaseConvergesAfterRefreshes ?? 2)
        }
        if (body.unbindStaticIPs) {
          releaseCleanupRequested.add(serverId)
          provisioningTasks.unshift({
            id: `task-${serverId}`,
            serverId,
            integrationId: server?.status.source.integrationId ?? '',
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
      const claimed = platformClaimRefusal(serverIds, 'recover')
      if (claimed) return json(route, { error: { code: 'conflict', message: claimed, requestId: 'req-recover-claimed' } }, 409)
      if (body.cancelActiveWorkflows) {
        const refused = supersedeActiveOperations(serverIds)
        if (refused) return json(route, { error: { code: 'conflict', message: refused, requestId: 'req-recover-busy' } }, 409)
      }
      const recoverBody = { ...body }
      delete recoverBody.serverIds
      for (const serverId of serverIds) {
        options.onServerRecoverRequest?.(serverId, recoverBody)
        const server = fleet.find((item) => item.id === serverId)
        if (server) {
          // Recovery converges to the ready pool; model the same releasing -> ready path a
          // real Recover Operation drives so the list can follow the Server in place.
          server.status.provisioning.state = 'releasing'
          server.status.provisioning.stateSince = new Date().toISOString()
          server.status.provisioning.providerState = 'Releasing'
          releaseRefreshesRemaining.set(serverId, options.releaseConvergesAfterRefreshes ?? 2)
        }
      }
      const operationId = createProvisioningOperation('recover-server', serverIds, body)
      return json(route, { operationId }, 202)
    }

    if (path === '/api/v1/overview') {
      const siteId = url.searchParams.get('siteId')
      // One Server in each health state by default (a down one, an imported one whose Node
      // Exporter is not installed yet, a ready one swallow does not monitor), so the Fleet health
      // card and both health attention items render.
      const health = options.overviewHealth ?? { up: 3, down: 1, unknown: 1, notApplicable: 1 }
      const servers = health.up + health.down + health.unknown + health.notApplicable
      return json(route, {
        generatedAt: now, scope: { siteId },
        inventory: { sites: siteId ? 1 : 2, servers, absent: 0, deployed: servers - health.notApplicable, platformed: 4, gpuDevices: 24, health },
        integrations: { total: integrationItems.length, failing: 1, items: integrationItems.map((item) => ({ id: item.id, siteId: item.siteId, name: item.name, kind: item.kind, providerKind: item.providerKind, enabled: item.enabled, lastSucceededAt: item.sync.lastSucceededAt, lastError: item.sync.lastError })) },
        platforms: { total: 2, unreachable: 1, unmatchedMembers: 1 }, operations: { active: 1, failedLast24Hours: 1, recent: operations },
        monitoring: options.overviewAlertSource === 'not-configured'
          ? { configured: false, available: false, error: null, firing: { critical: 0, warning: 0, items: [] } }
          : options.overviewAlertSource === 'unavailable'
            ? { configured: true, available: false, error: { code: 'provider_unavailable', message: 'Alertmanager is configured but could not be read.' }, firing: { critical: 0, warning: 0, items: [] } }
            : { configured: true, available: true, error: null, firing: { critical: 1, warning: 1, items: alerts.filter((alert) => alert.state === 'firing') } },
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
      if (!includeAbsent) items = items.filter((item) => !item.status.absent)
      if (siteId) items = items.filter((item) => item.status.source.siteId === siteId)
      if (platformId) items = items.filter((item) => item.status.membership?.platformId === platformId)
      if (provisioningState) items = items.filter((item) => item.status.provisioning?.state === provisioningState)
      if (keyword) items = items.filter((item) => [item.status.hostname, item.status.fqdn, item.id, ...item.status.addresses].some((value) => value?.toLowerCase().includes(keyword)))
      const pageNumber = Number(url.searchParams.get('page') ?? 1); const pageSize = Number(url.searchParams.get('pageSize') ?? 100); const start = (pageNumber - 1) * pageSize
      return json(route, { items: items.slice(start, start + pageSize).map(classifiedServer), total: items.length, page: pageNumber, pageSize })
    }
    const serverEventsMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/events$/)
    if (serverEventsMatch && request.method() === 'GET') {
      const activityCount = serverEventsMatch[1] === 'srv-1' ? options.serverActivityItemCount ?? 0 : 0
      return json(route, {
        supported: true,
        events: activityCount > 0
          ? Array.from({ length: activityCount }, (_, index) => ({
              id: `event-activity-${String(index + 1).padStart(2, '0')}`,
              level: 'audit',
              type: 'Machine lifecycle',
              message: `Provider event ${String(index + 1).padStart(2, '0')}`,
              actor: 'admin',
              occurredAt: now,
            }))
          : [{
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
      if (task.status === 'running' && server?.status.provisioning.state === 'ready') {
        Object.assign(task, {
          status: 'succeeded',
          phase: 'complete',
          error: '',
          retryable,
          updatedAt: now,
        })
        server.status.addresses = []
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
      const server = fleet.find((item) => item.id === serverId)
      if (!server) return json(route, { error: { code: "not_found", message: "Server not found" } }, 404)
      // A refresh re-projects the provisioning axis, power control included.
      server.status.provisioning.powerControl = powerConfigurationView(serverId).control
      return json(route, {
        serverId, state: server.status.provisioning.state, providerState: server.status.provisioning.providerState,
        powerState: server.status.provisioning.powerState, osSystem: server.status.provisioning.osSystem,
        distroSeries: server.status.provisioning.distroSeries, ephemeral: server.status.provisioning.ephemeral,
        hweKernel: server.status.provisioning.hweKernel, locked: server.status.provisioning.locked,
        commissioningStatus: server.status.provisioning.commissioningStatus,
        testingStatus: server.status.provisioning.testingStatus, observedAt: now,
      })
    }
    const serverActionMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/([^/]+)$/)
    if (serverActionMatch && request.method() === "POST") {
      const [serverId, action] = serverActionMatch.slice(1)
      if (action === "release") {
        const body = request.postData() ? request.postDataJSON() as Record<string, unknown> : null
        options.onServerReleaseRequest?.(serverId, body)
      }
      options.onServerActionRequest?.(serverId, action)
      const actionServer = fleet.find((item) => item.id === serverId)
      if (actionServer && action === "lock") actionServer.status.provisioning.locked = true
      if (actionServer && action === "unlock") actionServer.status.provisioning.locked = false
      if (actionServer && action === "inspect" && actionServer.status.provisioning.inspectionRequired) {
        actionServer.status.provisioning.state = "inspecting"
        actionServer.status.provisioning.providerState = "Commissioning"
        actionServer.status.provisioning.stateSince = new Date().toISOString()
      }
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
          server.status.provisioning.state = "releasing"
          server.status.provisioning.stateSince = new Date().toISOString()
          server.status.provisioning.providerState = "Releasing"
          releaseRefreshesRemaining.set(serverId, options.releaseConvergesAfterRefreshes ?? 2)
        }
        const body = request.postData() ? request.postDataJSON() as Record<string, unknown> : null
        if (body?.unbindStaticIPs) {
          provisioningTasks.unshift({
            id: `task-${serverId}`,
            serverId,
            integrationId: server?.status.source.integrationId ?? '',
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
        hweKernel: "", locked: actionServer?.status.provisioning.locked ?? false, commissioningStatus: "", testingStatus: "", observedAt: now,
        ...(releaseBody?.unbindStaticIPs ? { taskId: `task-${serverId}` } : {}),
      }, 202)
    }
    if (/\/api\/v1\/servers\/[^/]+\/provisioner-detail$/.test(path)) return json(route, { capabilities: { ephemeralDeploy: true, power: true, hardwareValidation: true, stateActions: true, machineDetail: true, hardwareInventory: true, machineRemoval: true, releaseOptions: true, networkConfiguration: true, powerConfiguration: true }, sections: [{ title: 'System', fields: [{ label: 'System vendor', value: 'Supermicro' }, { label: 'Serial', value: 'SN0001' }] }, { title: 'BMC', fields: [{ label: 'Protocol', value: 'IPMI' }, { label: 'Address', value: '192.0.2.20' }, { label: 'Username', value: 'bmc-admin' }, { label: 'Password', value: 'bmc-secret' }, { label: 'Driver', value: 'LAN_2_0' }, { label: 'Boot type', value: 'efi' }, { label: 'Privilege level', value: 'OPERATOR' }, { label: 'Cipher suite', value: '17' }, { label: 'Power MAC', value: 'aa:bb:cc:dd:ee:ff' }] }], tables: [{ title: 'Storage', columns: ['Device', 'Size', 'Model'], rows: [['nvme0n1', '3.84 TB', 'PM1733']] }, { title: 'PCI devices', columns: ['Address', 'Device', 'Vendor'], rows: [['03:00.0', 'MI300X', 'AMD']] }] })
    // Power Configuration (server-detail-actions.md "Power Configuration"): the real API writes it
    // through to the provisioner; the fixture keeps one per Server for the page.
    const powerConfigurationMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/power-configuration$/)
    if (powerConfigurationMatch) {
      const serverId = decodeURIComponent(powerConfigurationMatch[1])
      if (!fleet.some((entry) => entry.id === serverId)) return json(route, { error: { code: 'not_found', message: 'Server not found.' } }, 404)
      if (request.method() === 'PUT') {
        const body = (request.postDataJSON() ?? {}) as Record<string, unknown>
        options.onPowerConfigurationRequest?.(serverId, body)
        if (options.powerConfigurationError) {
          const { status, code, message } = options.powerConfigurationError
          return json(route, { error: { code, message, requestId: 'req-power-configuration' } }, status)
        }
        const previous = powerConfigurationView(serverId)
        const driver = String(body.driver ?? '')
        const passwordSet = typeof body.password === 'string' ? body.password !== '' : driver === previous.driver && previous.passwordSet
        powerConfigurations[serverId] = {
          driver, address: String(body.address ?? ''), powerId: String(body.powerId ?? ''), username: String(body.username ?? ''), passwordSet,
        }
        // The real write is kept only after the provisioner read the power through it; a refused
        // check is a powerConfigurationError (400) that stores nothing.
        return json(route, { ...powerConfigurationView(serverId), powerState: 'on' })
      }
      return json(route, powerConfigurationView(serverId))
    }
    // Live power read (server-detail-actions.md): the provisioner reads it through the driver.
    const powerStateMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/power-state$/)
    if (powerStateMatch && request.method() === 'GET') {
      const serverId = decodeURIComponent(powerStateMatch[1])
      if (!fleet.some((entry) => entry.id === serverId)) return json(route, { error: { code: 'not_found', message: 'Server not found.' } }, 404)
      const readable = powerConfigurationView(serverId).control === 'automatic'
      return json(route, { serverId, powerState: options.livePowerStates?.[serverId] ?? (readable ? 'on' : 'unknown') })
    }
    // Boot Media (server-detail-actions.md "Boot Media"): the real API drives the BMC over Redfish;
    // the fixture records writes and keeps one per-page setting so the Summary re-reads it.
    const bootMediaMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/(boot-media|redfish\/probe)$/)
    if (bootMediaMatch) {
      const mediaServerId = decodeURIComponent(bootMediaMatch[1])
      const body = request.method() === 'GET' ? null : ((request.postDataJSON() ?? {}) as Record<string, unknown>)
      if (request.method() !== 'GET') options.onBootMediaRequest?.(request.method(), path, body)
      if (bootMediaMatch[2] === 'redfish/probe') {
        if (throughHypervisor(mediaServerId)) return json(route, { redfish: noBMC, libvirt: libvirtCapability(mediaServerId) })
        bootMediaState.redfish = { ...bootMediaState.redfish, probedAt: now }
        return json(route, { redfish: bootMediaState.redfish, libvirt: null })
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
        return json(route, { ...bootMediaView(false, mediaServerId), ...disableOutcome })
      }
      return json(route, bootMediaView(url.searchParams.get('live') === 'true', mediaServerId))
    }
    // Virtual machines (server-enrollment.md, decision 055): the real API reads libvirt over SSH on
    // the hypervisor; the fixture answers a fixed domain list per hypervisor.
    const virtualMachinesMatch = path.match(/^\/api\/v1\/servers\/([^/]+)\/virtual-machines$/)
    if (virtualMachinesMatch && request.method() === 'GET') {
      const hypervisorId = decodeURIComponent(virtualMachinesMatch[1])
      if (!fleet.some((entry) => entry.id === hypervisorId)) return json(route, { error: { code: 'not_found', message: 'Server not found.' } }, 404)
      const items = (options.virtualMachines ?? defaultVirtualMachines)[hypervisorId] ?? []
      return json(route, { hypervisorServerId: hypervisorId, account: url.searchParams.get('account') || 'ubuntu', items })
    }
    if (path === '/api/v1/provisioning/virtual-machine-enrollments' && request.method() === 'POST') {
      const body = (request.postDataJSON() ?? {}) as Record<string, unknown>
      options.onVirtualMachineEnrollmentRequest?.(body)
      if (!Array.isArray(body.domains) || body.domains.length === 0) {
        return json(route, { error: { code: 'validation_error', message: 'domains must name at least one virtual machine' } }, 400)
      }
      return json(route, { workflowId: 'op-vm-enroll' }, 202)
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
        item.status.defaultUser = { user: String(body.user ?? ''), source: 'server' }
        return json(route, { defaultUser: item.status.defaultUser, keyInstalled: Boolean(body.password), sudo: options.defaultUserSudo ?? 'passwordless' })
      }
      if (request.method() === 'DELETE') {
        options.onDefaultUserRequest?.('DELETE', null)
        const imageUser = item.status.provisioning?.deployedImageDefaultUser
        item.status.defaultUser = imageUser ? { user: imageUser, source: 'os_image' } : undefined
        return route.fulfill({ status: 204 })
      }
    }
    const serverMatch = path.match(/^\/api\/v1\/servers\/([^/]+)$/)
    if (serverMatch) {
      const index = fleet.findIndex((item) => item.id === serverMatch[1])
      if (request.method() === 'DELETE') {
        if (index < 0) return json(route, { error: { code: 'not_found', message: 'Server not found' } }, 404)
        options.onServerDeleteRequest?.(serverMatch[1])
        const claimed = platformClaimRefusal([serverMatch[1]], 'delete')
        if (claimed) return json(route, { error: { code: 'conflict', message: claimed, requestId: 'req-delete-claimed' } }, 409)
        if (options.serverDeleteFailureIds?.includes(serverMatch[1])) {
          return json(route, { error: { code: 'conflict', message: 'MAAS refused to delete the machine: it hosts running virtual machines.', requestId: 'req-delete-' + serverMatch[1] } }, 409)
        }
        fleet.splice(index, 1)
        return route.fulfill({ status: 204 })
      }
      return index >= 0 ? json(route, classifiedServer(fleet[index])) : json(route, { error: { code: 'not_found', message: 'Server not found' } }, 404)
    }

    if (path === '/api/v1/monitoring/alerts' && request.method() === 'GET') {
      const severity = url.searchParams.get('severity'); const state = url.searchParams.get('state'); const serverId = url.searchParams.get('serverId')
      return json(route, alerts.filter((alert) => (!severity || alert.severity === severity) && (!state || alert.state === state) && (!serverId || alert.serverId === serverId)))
    }
    if (/\/api\/v1\/monitoring\/alerts\/[^/]+\/acknowledge$/.test(path) && request.method() === 'POST') {
      if (options.acknowledgeFails) return json(route, { error: { code: 'provider_unavailable', message: 'Alertmanager is unavailable' } }, 503)
      return json(route, { silenceId: 'silence-e2e' })
    }
    if (path === '/api/v1/infrastructure/zones' && request.method() === 'GET') {
      return json(route, [groupFixture('zone-a', 'rack-a')])
    }
    if (path === '/api/v1/infrastructure/pools' && request.method() === 'GET') {
      return json(route, [groupFixture('pool-research', 'research')])
    }
    if (path === '/api/v1/monitoring/metrics/range') {
      options.onMetricRangeRequest?.(url.searchParams)
      if (options.metricsBackendMissing) {
        return json(route, { error: { code: 'provider_unavailable', message: 'No metrics backend is registered. Register a Prometheus metrics Integration for the Site to see usage.' } }, 503)
      }
      // A deterministic 1-hour, 30-second history ending at a fixed instant. CPU, memory, and the
      // network charts get data; disk is left without data on purpose so the "No data" card is
      // exercised.
      const end = Date.parse('2026-08-27T03:00:00Z')
      const step = 30
      const start = end - 3600_000
      const serverIds = (url.searchParams.get('serverIds') ?? '').split(',').filter(Boolean)
      const poolId = url.searchParams.get('poolId'); const zoneId = url.searchParams.get('zoneId'); const platformId = url.searchParams.get('platformId')
      const serverCount = serverIds.length || (poolId === 'pool-research' ? 3 : zoneId === 'zone-a' ? 2 : platformId ? 4 : 0)
      const requested = (url.searchParams.get('metrics') ?? '').split(',').filter(Boolean)
      const wave = (base: number, amplitude: number) => Array.from({ length: 121 }, (_, index) => ({
        timestamp: new Date(start + index * step * 1000).toISOString(),
        value: Math.max(0, base + amplitude * Math.sin(index / 9)),
      }))
      const shapes: Record<string, () => Array<{ timestamp: string; value: number }>> = {
        cpuUsagePercent: () => wave(42, 18),
        memoryUsedPercent: () => wave(63, 4),
        networkReceiveBytesPerSecond: () => wave(48_000_000, 20_000_000),
        networkTransmitBytesPerSecond: () => wave(12_000_000, 6_000_000),
      }
      const series = serverCount === 0 ? [] : requested
        .filter((metric) => shapes[metric])
        .map((metric) => ({ metric, serverId: null, points: shapes[metric]() }))
      return json(route, {
        scope: { kind: serverIds.length ? 'servers' : poolId ? 'pool' : zoneId ? 'zone' : 'platform', id: poolId ?? zoneId ?? platformId, serverCount },
        mode: 'aggregate',
        start: new Date(start).toISOString(),
        end: new Date(end).toISOString(),
        stepSeconds: step,
        series,
        grafana: 'https://grafana.example',
      })
    }
    if (path === '/api/v1/monitoring/metrics') {
      if (options.metricsBackendMissing) {
        return json(route, { error: { code: 'provider_unavailable', message: 'No metrics backend is registered. Register a Prometheus metrics Integration for the Site to see usage.' } }, 503)
      }
      const ids = (url.searchParams.get('serverIds') ?? '').split(',').filter(Boolean)
      const batch = metricBatchIndex++
      options.onMetricsRequest?.(ids)
      activeMetricRequests += 1; options.onMetricsActive?.(activeMetricRequests)
      if (options.metricsDelayMs) await new Promise((resolve) => setTimeout(resolve, options.metricsDelayMs))
      activeMetricRequests -= 1
      if (batch === options.failMetricsBatchIndex) return json(route, { error: { code: 'provider_unavailable', message: 'Prometheus batch failed' } }, 503)
      return json(route, { items: ids.map((serverId, index) => ({ serverId, metrics: { cpuUsagePercent: 30 + (index % 20), memoryUsedPercent: 60 + (index % 10) } })), grafana: 'https://grafana.example' })
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

    if (path === '/api/v1/platforms/gpu-operator/releases' && request.method() === 'GET') {
      return json(route, {
        items: [{
          version: 'v1.5.1', default: true, minKubernetesVersion: '1.29', draMinKubernetesVersion: '1.34',
          draDriverVersions: ['v1.0.1'], defaultDraDriverVersion: 'v1.0.1',
        }],
      })
    }

    // Spec write (platforms.md "Change A Platform"). The dashboard writes only the GPU Operator
    // setting: turning it on converges at once and records it as installing, the way the controller
    // does; changing or turning off a managed operator answers 202 with a Change Plan, which is
    // carried out at once when the write approved it (change-plans.md "Approval On Submit").
    const specMatch = path.match(/^\/api\/v1\/platforms\/([^/]+)\/spec$/)
    if (specMatch && request.method() === 'PUT') {
      const platformId = specMatch[1]
      const written = request.postDataJSON() as { generation: number; spec: { settings: Record<string, unknown> } }
      // A Slurm spec write turns Omnistat's hardware counters on or off (platforms.md "Omnistat"): it
      // needs no Change Plan, and the controller's configure-omnistat Workflow restarts the record as
      // installing with no nodes while the Platform stays active.
      const slurmPlatform = platformItems.find((item) => item.id === platformId && item.type === 'slurm')
      if (slurmPlatform) {
        const omnistatSetting = written.spec.settings.omnistat as { hardwareCounters?: unknown } | undefined
        options.onHardwareCountersRequest?.(platformId, omnistatSetting?.hardwareCounters, written.generation)
        if (hardwareCountersConflictPending) {
          hardwareCountersConflictPending = false
          slurmPlatform.generation += 1
          return json(route, { error: { code: 'conflict', message: 'The platform spec changed since it was read; read it again and retry.', requestId: 'req-platform-spec-conflict' } }, 409)
        }
        if (written.generation !== slurmPlatform.generation) {
          return json(route, { error: { code: 'conflict', message: 'The platform spec changed since it was read; read it again and retry.' } }, 409)
        }
        slurmPlatform.generation += 1
        slurmPlatform.spec = { ...slurmPlatform.spec, settings: written.spec.settings } as typeof slurmPlatform.spec
        const workflowId = `op-hardware-counters-${slurmPlatform.generation}`
        slurmPlatform.status = {
          ...slurmPlatform.status,
          conditions: platformConditions('converging', slurmPlatform.generation, `Workflow ${workflowId} carries out generation ${slurmPlatform.generation}`),
          realizingWorkflowId: workflowId,
        }
        if (slurmPlatform.omnistat) {
          slurmPlatform.omnistat = {
            ...slurmPlatform.omnistat, state: 'installing', lastWorkflowId: workflowId, updatedAt: now,
            hardwareCounters: { requested: omnistatSetting?.hardwareCounters === true, nodes: [] },
          }
        }
        return json(route, { platform: slurmPlatform, plan: null })
      }
      const body = (written.spec.settings.gpuOperator ?? { enabled: false }) as Record<string, unknown>
      options.onGPUOperatorRequest?.(platformId, body)
      if (options.gpuOperatorBusy) {
        return json(route, { error: { code: 'conflict', message: 'targets are busy: gpu-node-01 is held by Deploy Kubernetes Platform production-k0s' } }, 409)
      }
      const platform = platformItems.find((item) => item.id === platformId)
      if (!platform) return json(route, { error: { code: 'not_found', message: 'Platform not found.' } }, 404)
      if (written.generation !== platform.generation) {
        return json(route, { error: { code: 'conflict', message: 'The platform spec changed since it was read; read it again and retry.' } }, 409)
      }
      const previous = (platform.gpuOperator ?? {}) as Record<string, unknown>
      const managed = previous.enabled === true && previous.state !== 'absent'
      const enabled = body.enabled === true
      platform.generation += 1
      platform.spec = { ...platform.spec, settings: written.spec.settings }
      if (managed) {
        const plan = changePlan(`plan-gpu-operator-${platform.generation}`, platformId, 'pending_approval', {
          expiresAt: '2099-01-01T00:00:00Z', generation: platform.generation,
          actions: [{ target: { kind: 'platform', id: '' }, action: enabled ? 'change-gpu-operator' : 'disable-gpu-operator', from: 'enabled', to: enabled ? '' : 'disabled', destructive: true, reason: enabled ? 'pods that use GPUs may restart' : 'GPU workloads lose the device plugin' }],
          ...submittedPlan(url),
        })
        const earlier = (item: (typeof changePlanItems)[number]) => item.resource.id === platformId && (item.state === 'pending_approval' || item.state === 'approved')
        if (plan.state === 'pending_approval') {
          changePlanItems = [plan, ...changePlanItems.filter((item) => !earlier(item))]
          return json(route, { platform, plan }, 202)
        }
        // Approved with the write: the controller applies it, so the Workflow starts right away.
        const workflowId = `op-gpu-operator-${platform.generation}`
        changePlanItems = [{ ...plan, state: 'applied', workflowId }, ...changePlanItems.filter((item) => !earlier(item))]
        platform.gpuOperator = enabled
          ? { ...previous, ...body, state: 'installing', lastWorkflowId: workflowId, updatedAt: now } as typeof platform.gpuOperator
          : { ...previous, state: 'uninstalling', lastWorkflowId: workflowId, updatedAt: now } as typeof platform.gpuOperator
        return json(route, { platform, plan }, 202)
      }
      platform.gpuOperator = {
        enabled: true, version: (body.version as string | undefined) ?? 'v1.5.1',
        allocationMode: body.allocationMode ?? 'device-plugin',
        draDriverVersion: body.allocationMode === 'dra' ? (body.draDriverVersion as string | undefined) ?? 'v1.0.1' : null,
        resourceNaming: body.resourceNaming ?? 'single', nodeLabeller: body.nodeLabeller ?? true,
        metricsExporter: body.metricsExporter ?? false,
        state: 'installing', lastWorkflowId: 'op-gpu-operator-new', updatedAt: now,
      }
      platform.gpuStackOwner = 'gpu-operator'
      return json(route, { platform, plan: null })
    }

    // Create (platforms.md "Create A Platform"): the Platform is stored `planned` with its members
    // claimed, and its install is a Change Plan. A create sent with approve=true is approved and
    // carried out at once, as the controller does (`submitted`); otherwise its install waits. The
    // mock reads the body in its deploy shape, which is also what onPlatformDeploymentRequest
    // receives.
    if (path === '/api/v1/platforms' && request.method() === 'POST') {
      const created = request.postDataJSON() as Parameters<typeof legacyPlatformBody>[0]
      const rawBody = legacyPlatformBody(created) as Record<string, unknown>
      options.onPlatformDeploymentRequest?.(rawBody)
      const installPlan = changePlan('plan-platform-new', 'platform-new', 'pending_approval', {
        expiresAt: '2099-01-01T00:00:00Z', generation: 1,
        actions: [
          { target: { kind: 'platform', id: '' }, action: 'install', from: '', to: 'installed', destructive: true, reason: 'the Platform is created on its members' },
          ...created.spec.members.map((member) => ({ target: { kind: 'server', id: member.serverId }, action: 'join', from: '', to: member.roles.join(', '), destructive: true, reason: 'the Server becomes a member of the new Platform' })),
        ],
        ...submittedPlan(url),
      })
      const planned = (platform: Record<string, unknown>) => {
        const spec = { desired: 'present', members: created.spec.members, settings: created.spec.settings, machinePreparation: null, removal: null }
        return {
          ...platform, lifecycleState: 'planned', lifecycleOperationId: null, generation: 1, spec,
          status: { observedGeneration: 0, conditions: platformConditions('awaiting_approval', 1, waitingMessage(installPlan)), applied: null, realizingWorkflowId: null },
        }
      }
      /** Stores the install plan; one approved with the create is applied, starting the deploy. */
      const submitted = () => {
        if (installPlan.state === 'approved') {
          const workflowId = startPlatformInstall('platform-new')
          changePlanItems = [{ ...installPlan, state: 'applied', workflowId: workflowId ?? '' }, ...changePlanItems]
        } else {
          changePlanItems = [installPlan, ...changePlanItems]
        }
      }
      const body = rawBody as {
        name: string
        type?: 'kubernetes' | 'slurm'
        gpuStackOwner?: string
        gpuOperator?: Record<string, unknown> & { enabled?: boolean }
        containerRuntime?: { kind?: 'docker-ce' | 'podman' }
        omnistat?: { enabled?: boolean; installMethod?: 'container' | 'native'; hardwareCounters?: boolean }
        deviceMetricsExporter?: { enabled?: boolean; installMethod?: 'container' | 'native' }
        gpuDriver?: { moduleOptions?: Record<string, string> }
        roleAssignments?: Array<{
          serverId: string
          role: 'control-plane' | 'worker'
          runWorkloads?: boolean
        }>
        slurm?: {
          nodeAssignments: Array<{ serverId: string; controller: boolean; compute: boolean }>
          gpuGresType?: string
          ociContainers?: boolean
          pmix?: boolean
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
          monitoringLinks: [],
          gpuOperator: null,
          // Mirrors platforms.md "Omnistat": an enabled request records Omnistat as installing, with
          // the hardware counters request and no nodes until the install reports them.
          omnistat: body.omnistat?.enabled
            ? {
                enabled: true, version: '1.15.0', installMethod: body.omnistat.installMethod ?? 'container',
                observabilityServerId: targetServerIds[0] ?? '',
                state: 'installing', hardwareCounters: { requested: body.omnistat.hardwareCounters ?? false, nodes: [] },
                lastWorkflowId: operationId, updatedAt: now,
              }
            : null,
          // Mirrors platforms.md "Device Metrics Exporter": an enabled request records it as
          // installing, with no GPU identity until an install succeeded.
          deviceMetricsExporter: body.deviceMetricsExporter?.enabled
            ? {
                enabled: true, version: '1.5.3', installMethod: body.deviceMetricsExporter.installMethod ?? 'container',
                observabilityServerId: targetServerIds[0] ?? '', state: 'installing', gpuIdentity: null,
                lastWorkflowId: operationId, updatedAt: now,
              }
            : null,
          // Mirrors platforms.md "Container Runtime": the Platform records the chosen kind.
          containerRuntime: body.containerRuntime?.kind ?? null,
          // Mirrors platforms.md "GPU driver options" and "GPU GRES": only user-set values are recorded.
          gpuDriver: Object.keys(body.gpuDriver?.moduleOptions ?? {}).length > 0 ? body.gpuDriver : null,
          gpuGresType: body.slurm?.gpuGresType ?? null,
          // Mirrors platforms.md "Native OCI Containers" and "MPI (PMIx)": false unless requested.
          ociContainers: body.slurm?.ociContainers ?? false,
          pmix: body.slurm?.pmix ?? false,
          sync: { lastStartedAt: null, lastSucceededAt: null, lastError: null, memberCount: 0, matchedCount: 0 },
          createdAt: now,
          updatedAt: now,
        })
        claimPlatformTargets('platform-new', targetServerIds)
        pendingPlatformDeploys.set('platform-new', operationItems.shift() as (typeof operationItems)[number])
        const platform = planned(platformItems.pop() as Record<string, unknown>)
        platformItems.push(platform as (typeof platformItems)[number])
        submitted()
        return json(route, { platform, plan: installPlan }, 202)
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
        gpuStackOwner: body.gpuOperator?.enabled ? 'gpu-operator' : body.gpuStackOwner ?? 'provisioning',
        monitoringLinks: [],
        gpuOperator: body.gpuOperator?.enabled
          ? {
              enabled: true, version: 'v1.5.1', allocationMode: body.gpuOperator.allocationMode ?? 'device-plugin',
              draDriverVersion: body.gpuOperator.allocationMode === 'dra' ? 'v1.0.1' : null,
              resourceNaming: body.gpuOperator.resourceNaming ?? 'single', nodeLabeller: body.gpuOperator.nodeLabeller ?? true,
              metricsExporter: body.gpuOperator.metricsExporter ?? false,
              state: 'installing', lastWorkflowId: operationId, updatedAt: now,
            }
          : null,
        sync: { lastStartedAt: null, lastSucceededAt: null, lastError: null, memberCount: 0, matchedCount: 0 },
        createdAt: now,
        updatedAt: now,
      })
      claimPlatformTargets('platform-new', roleAssignments.map((assignment) => assignment.serverId))
      pendingPlatformDeploys.set('platform-new', operationItems.shift() as (typeof operationItems)[number])
      const platform = planned(platformItems.pop() as Record<string, unknown>)
      platformItems.push(platform as (typeof platformItems)[number])
      submitted()
      return json(route, { platform, plan: installPlan }, 202)
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
            { name: 'gpu-node-01', role: 'control-plane', ready: true, unschedulable: false, serverId: 'srv-1', addresses: ['192.168.40.21'], kubeletVersion: 'v1.30.2+k0s', gpu: { allocatable: { 'amd.com/gpu': 8 }, draDevices: 0 } },
            { name: 'gpu-node-02', role: 'worker', ready: true, unschedulable: false, serverId: 'srv-2', addresses: ['192.168.40.22'], kubeletVersion: 'v1.30.2+k0s', gpu: { allocatable: {}, draDevices: 0 } },
          ],
        })
      }
      const cordonMatch = sub.match(/^\/nodes\/([^/]+)\/(cordon|uncordon)$/)
      if (cordonMatch && request.method() === 'POST') {
        return json(route, { name: cordonMatch[1], role: 'worker', ready: true, unschedulable: cordonMatch[2] === 'cordon', serverId: null, addresses: [], kubeletVersion: 'v1.30.2+k0s', gpu: { allocatable: {}, draDevices: null } })
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
    const platformMatch = path.match(/^\/api\/v1\/platforms\/([^/]+)$/)
    if (platformMatch && request.method() === 'DELETE') {
      const index = platformItems.findIndex((platform) => platform.id === platformMatch[1])
      if (index < 0) return json(route, { error: { code: 'not_found', message: 'Platform not found' } }, 404)
      const removed = platformItems[index]
      // Record-only delete, and a planned Platform (its install never started), go at once.
      if (url.searchParams.get('recordOnly') === 'true' || removed.lifecycleState === 'planned') {
        platformItems.splice(index, 1)
        releasePlatformClaims(platformMatch[1])
        changePlanItems = changePlanItems.filter((item) => !(item.resource.id === platformMatch[1] && item.state === 'pending_approval'))
        return route.fulfill({ status: 204 })
      }
      // Delete (platforms.md "Delete A Platform"): the spec is marked absent and the uninstall is a
      // Change Plan, carried out at once when the delete approved it and waiting otherwise;
      // cancelActiveWorkflows supersedes the Workflows holding the targets (061). A running or
      // failed deployment is uninstalled the same way.
      const body = (request.postDataJSON() as Record<string, unknown> | null) ?? null
      options.onPlatformUninstallRequest?.(platformMatch[1], body)
      const requested = removalChoice(body)
      const unfinished = (item: (typeof changePlanItems)[number]) => item.resource.id === removed.id && (item.state === 'pending_approval' || item.state === 'approved')
      // Deleting an absent Platform again: nothing changes while its uninstall runs, and the same
      // choices return the plan that waits, approved when the request may approve it. Different
      // choices, or no plan waiting (rejected, expired, or a failed uninstall), propose it again.
      if (removed.spec.desired === 'absent') {
        if (removed.lifecycleState === 'uninstalling') return json(route, { platform: removed, plan: null })
        const waiting = changePlanItems.find(unfinished)
        if (waiting && JSON.stringify(removed.spec.removal) === JSON.stringify(requested)) {
          if (waiting.state !== 'pending_approval' || waiting.blockers.length > 0 || submittedPlan(url).state !== 'approved') {
            return json(route, { platform: removed, plan: waiting }, 202)
          }
          const approved = { ...waiting, ...submittedPlan(url) }
          const workflowId = startPlatformUninstall(removed)
          changePlanItems = changePlanItems.map((item) => (item.id === waiting.id ? { ...approved, state: 'applied', workflowId } : item))
          return json(route, { platform: removed, plan: approved }, 202)
        }
      }
      const targets = operationItems.find((item) => item.id === removed.lifecycleOperationId)?.targetServerIds ?? []
      if (body?.cancelActiveWorkflows) {
        const refused = supersedeActiveOperations(targets)
        if (refused) return json(route, { error: { code: 'conflict', message: refused, requestId: 'req-uninstall-busy' } }, 409)
      }
      removed.generation += 1
      removed.spec = { ...removed.spec, desired: 'absent', removal: requested }
      const plan = changePlan(`plan-uninstall-${removed.id}-${removed.generation}`, removed.id, 'pending_approval', {
        expiresAt: '2099-01-01T00:00:00Z', generation: removed.generation,
        actions: [{ target: { kind: 'platform', id: removed.id }, action: 'uninstall', from: 'installed', to: 'uninstalled', destructive: true, reason: 'the Platform is marked absent' }],
        effects: body?.releaseServers ? [releaseEffect(targets.length, (body.releaseOptions ?? {}) as Record<string, unknown>)] : [],
        ...submittedPlan(url),
      })
      const superseded = (item: (typeof changePlanItems)[number]) => (unfinished(item)
        ? { ...item, state: 'superseded', stateReason: `the spec changed to generation ${removed.generation}` }
        : item)
      if (plan.state === 'approved' && options.controllerLagReads) {
        // The controller has not acted yet: the Platform reads as it did while its plan waited.
        removed.status = { ...removed.status, conditions: platformConditions('awaiting_approval', removed.generation, waitingMessage(plan)) }
        changePlanItems = [plan, ...changePlanItems.map(superseded)]
        laggingUninstalls.set(removed.id, { reads: options.controllerLagReads, planId: plan.id })
      } else if (plan.state === 'approved') {
        const workflowId = startPlatformUninstall(removed)
        changePlanItems = [{ ...plan, state: 'applied', workflowId }, ...changePlanItems.map(superseded)]
      } else {
        removed.status = { ...removed.status, conditions: platformConditions('awaiting_approval', removed.generation, waitingMessage(plan)) }
        changePlanItems = [plan, ...changePlanItems.map(superseded)]
      }
      return json(route, { platform: removed, plan }, 202)
    }
    if (platformMatch) {
      const platform = platformItems.find((item) => item.id === platformMatch[1])
      const lagging = laggingUninstalls.get(platformMatch[1])
      if (platform && lagging) {
        // Each read before the controller acts answers the Platform unchanged (`controllerLagReads`).
        if (lagging.reads > 0) {
          lagging.reads -= 1
        } else {
          laggingUninstalls.delete(platformMatch[1])
          const workflowId = startPlatformUninstall(platform)
          changePlanItems = changePlanItems.map((item) => (item.id === lagging.planId ? { ...item, state: 'applied', workflowId } : item))
        }
      }
      return platform
        ? json(route, platform)
        : json(route, { error: { code: 'not_found', message: 'Platform not found' } }, 404)
    }
    // The dashboard reads a single Operation as a Workflow (decision 072 rename). Serve it by id so
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
      const terminal = new Set(['succeeded', 'failed', 'partially_succeeded', 'canceled'])
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
    // Task retry. When
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
    const timelineMatch = path.match(/^\/api\/v1\/workflows\/([^/]+)\/timeline$/)
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
      if (options.cancelRefusedMessage) {
        return json(route, { error: { code: 'conflict', message: options.cancelRefusedMessage, requestId: 'req-cancel-refused' } }, 409)
      }
      const cancelStatus = options.cancelMarksTerminal ? 'canceled' : 'canceling'
      const cancelReason = options.cancelMarksTerminal ? 'Canceled by user.' : 'Canceling active work.'
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
    return json(route, { error: { code: 'not_found', message: `No fixture for ${path}` } }, 404)
  })
}
