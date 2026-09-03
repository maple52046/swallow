import type { Page, Route } from 'playwright/test'

const now = '2026-08-27T03:00:00Z'

/** Builds fleet fixtures; Server four deliberately models an unobserved inventory. */
function makeServer(index: number) {
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
    gpus: named && hasInventory ? [{ vendor: 'AMD', model: 'MI300X', count: 8 }] : [],
    systemVendor: hasInventory ? 'Supermicro' : '', systemProduct: hasInventory ? 'AS-8125GS-TNHR' : '', providerZone: hasInventory ? index < 2 ? 'rack-a' : 'rack-b' : '',
    providerResourcePool: hasInventory ? named ? 'accelerators' : 'compute' : '', providerPod: '', tags: hasInventory ? named ? ['gpu', 'production'] : ['compute'] : [],
    hardware: { systemUuid: `uuid-${ordinal}`, serialNumber: `SN${String(ordinal).padStart(4, '0')}`, macAddresses: hasInventory ? [`02:00:00:00:${String(Math.floor(index / 250)).padStart(2, '0')}:${String((index % 250) + 1).padStart(2, '0')}`] : [] },
    provisioning: { state: 'deployed', providerState: 'deployed', powerState: 'on', osSystem: 'ubuntu', distroSeries: '24.04', ephemeral: false, hweKernel: 'ga-24.04', locked: false, commissioningStatus: 'passed', testingStatus: 'passed', integrationId: 'maas-a', observedAt: now },
    membership: ordinal <= 3 ? { clusterId: 'cluster-a', nodeName: `gpu-node-0${ordinal}`, role: 'control-plane', state: 'ready', observedAt: now } : null,
    health: ordinal === 4 ? { state: 'down', observedAt: now } : { state: 'up', observedAt: now },
    absent: false, lastSeenAt: now, createdAt: '2026-08-01T00:00:00Z', updatedAt: now,
  }
}

const servers = Array.from({ length: 4 }, (_, index) => makeServer(index))
const operations = [
  { id: 'op-running', kind: 'deploy-kubernetes', intent: 'Deploy production k0s cluster', siteId: 'site-a', clusterId: 'cluster-a', targetServerIds: servers.slice(0, 3).map((server) => server.id), retryOfOperationId: null, execution: { runId: 'run-1024', playbook: 'deploy-k0s.yml', status: 'running', statusReason: null, startedAt: '2026-08-27T02:54:00Z', finishedAt: null }, requestedBy: 'admin', requestedAt: '2026-08-27T02:53:00Z', updatedAt: now },
  { id: 'op-failed', kind: 'exporter.install', intent: 'Install GPU exporters', siteId: 'site-a', clusterId: 'cluster-a', targetServerIds: ['srv-4'], retryOfOperationId: null, execution: { runId: 'run-1023', playbook: 'install-exporters.yml', status: 'failed', statusReason: 'Host unreachable', startedAt: '2026-08-27T01:10:00Z', finishedAt: '2026-08-27T01:12:00Z' }, requestedBy: 'admin', requestedAt: '2026-08-27T01:09:00Z', updatedAt: '2026-08-27T01:12:00Z' },
  { id: 'op-deploy-failed', kind: 'deploy-kubernetes', intent: 'Deploy edge-staging k0s cluster', siteId: 'site-a', clusterId: 'cluster-b', targetServerIds: ['srv-4'], retryOfOperationId: null, execution: { runId: 'run-deploy-failed', playbook: 'deploy-kubernetes', status: 'failed', statusReason: 'Worker join failed', startedAt: '2026-08-27T00:30:00Z', finishedAt: '2026-08-27T00:35:00Z' }, requestedBy: 'admin', requestedAt: '2026-08-27T00:29:00Z', updatedAt: '2026-08-27T00:35:00Z' },
]
const clusters = [
  {
    id: 'cluster-a', siteId: 'site-a', name: 'production-k0s', type: 'kubernetes',
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
    id: 'cluster-b', siteId: 'site-a', name: 'edge-staging', type: 'kubernetes',
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
    id: 'cluster-slurm', siteId: 'site-a', name: 'research-slurm', type: 'slurm',
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
  { id: 'ubuntu/jammy', name: 'Ubuntu 22.04 LTS', osSystem: 'ubuntu', release: 'jammy', architecture: 'amd64' },
  { id: 'ubuntu/noble', name: 'Ubuntu 24.04 LTS', osSystem: 'ubuntu', release: 'noble', architecture: 'amd64' },
  { id: 'ubuntu-24.04-rocm', name: 'Ubuntu 24.04 ROCm', osSystem: 'custom', release: 'ubuntu-24.04-rocm', architecture: 'amd64' },
]
const baseDeploymentTemplates = [
  { id: 'template-a', siteId: 'site-a', integrationId: 'maas-a', name: 'GPU compute baseline', description: 'Ubuntu baseline for accelerator nodes', imageId: 'ubuntu/jammy', ephemeral: false, network: { mode: 'dhcp', subnetId: 'subnet-a', defaultGateway: false }, hasUserData: true, createdAt: now, updatedAt: now },
]

const alerts = [
  { fingerprint: 'alert-1', name: 'NodeDown', severity: 'critical', state: 'firing', summary: 'gpu-node-04 stopped reporting', description: 'No scrape data for five minutes', labels: { alertname: 'NodeDown', server_id: 'srv-4' }, startsAt: '2026-08-27T02:50:00Z', serverId: 'srv-4', siteId: 'site-a', clusterId: 'cluster-a' },
  { fingerprint: 'alert-2', name: 'GpuTemperatureHigh', severity: 'warning', state: 'firing', summary: 'GPU temperature exceeds threshold', description: '', labels: { alertname: 'GpuTemperatureHigh', server_id: 'srv-2' }, startsAt: '2026-08-27T02:45:00Z', serverId: 'srv-2', siteId: 'site-a', clusterId: 'cluster-a' },
  { fingerprint: 'alert-3', name: 'ExporterMissing', severity: 'warning', state: 'suppressed', summary: 'Exporter rollout pending', description: '', labels: { alertname: 'ExporterMissing', server_id: 'srv-3' }, startsAt: '2026-08-27T01:45:00Z', serverId: 'srv-3', siteId: 'site-a', clusterId: 'cluster-a' },
]

/** Controls for large-fleet, concurrency, and failure-path browser fixtures. */
export interface FixtureOptions {
  fleetSize?: number
  metricsDelayMs?: number
  failMetricsBatchIndex?: number
  acknowledgeFails?: boolean
  readyServerCount?: number
  staticNetworkServerIds?: string[]
  networkSubnetName?: string
  ephemeralServerIds?: string[]
  secondReadyServerIntegrationId?: string
  failImageIntegrationIds?: string[]
  deploymentFailureIds?: string[]
  serverActionFailureIds?: string[]
  deploymentReadinessIssues?: Record<string, string>
  onOSImageCatalogRequest?: (integrationId: string) => void
  onDeploymentRequest?: (body: Record<string, unknown>) => void
  onServerReleaseRequest?: (serverId: string, body: Record<string, unknown> | null) => void
  onServerRefreshRequest?: (serverId: string) => void
  releaseConvergesAfterRefreshes?: number
  deploymentConvergesAfterRefreshes?: number
  releaseCleanupFails?: boolean
  onNetworkLinkRequest?: (method: string, serverId: string, interfaceId: string, linkId: string | null, body: Record<string, unknown> | null) => void
  onMetricsRequest?: (serverIds: string[]) => void
  /** Removes Cluster membership and deployment target claims for wizard success paths. */
  freeClusterCandidates?: boolean
  onMetricsActive?: (active: number) => void
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

/** Installs deterministic network fixtures; no backend or provider is contacted. */
export async function installApiFixtures(page: Page, options: FixtureOptions = {}) {
  await page.unroute('**/api/v1/**')
  const fleet = Array.from({ length: options.fleetSize ?? 4 }, (_, index) => makeServer(index))
  if (options.freeClusterCandidates) {
    for (const server of fleet) {
      server.membership = null
    }
  }
  for (let index = 0; index < (options.readyServerCount ?? 0) && index < fleet.length; index++) {
    fleet[index].provisioning.state = 'ready'
    fleet[index].provisioning.providerState = 'Ready'
    fleet[index].provisioning.osSystem = ''
    fleet[index].provisioning.distroSeries = ''
  }
  for (const serverId of options.ephemeralServerIds ?? []) {
    const server = fleet.find((item) => item.id === serverId)
    if (server) {
      server.provisioning.ephemeral = true
    }
  }
  if (options.secondReadyServerIntegrationId && fleet[1]) {
    fleet[1].source.integrationId = options.secondReadyServerIntegrationId
    fleet[1].provisioning.integrationId = options.secondReadyServerIntegrationId
  }
  let deploymentTemplates = baseDeploymentTemplates.map((template) => ({ ...template }))
  let siteItems = sites.map((site) => ({ ...site }))
  let integrationItems = integrations.map((integration) => ({
    ...integration,
    settings: { ...integration.settings },
    sync: { ...integration.sync },
  }))
  let metricBatchIndex = 0
  let activeMetricRequests = 0
  const releaseRefreshesRemaining = new Map<string, number>()
  const deploymentRefreshesRemaining = new Map<string, number>()
  const provisioningTasks: Array<Record<string, unknown>> = []
  const networkTargets = new Map(fleet.map((server) => {
    const interfaceId = `nic-${server.id}`
    const linkId = `link-${server.id}`
    const hasStaticBinding = options.staticNetworkServerIds?.includes(server.id) ?? false
    const subnetName = options.networkSubnetName ?? 'lab-network'
    return [server.id, {
      serverId: server.id,
      editable: server.provisioning.state === 'ready',
      disabledReason: server.provisioning.state === 'ready' ? '' : 'Network configuration can only be changed while the Server is Ready.',
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
      server.provisioning.providerState = 'Ready'
      server.provisioning.osSystem = ''
      server.provisioning.distroSeries = ''
      server.provisioning.ephemeral = false
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
      deploymentRefreshesRemaining.delete(serverId)
    } else {
      deploymentRefreshesRemaining.set(serverId, remaining - 1)
    }
  }
  const clusterItems = clusters.map((cluster) => ({ ...cluster }))
  const operationItems = operations.map((operation) => ({ ...operation }))
  if (options.freeClusterCandidates) {
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

    if (path === '/api/v1/auth/me') return json(route, { id: 'admin-1', username: 'admin', role: 'admin' })
    if (path === '/api/v1/auth/login') return json(route, { accessToken: 'e2e-token' })
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
      const integrationId = url.searchParams.get('integrationId') ?? ''
      options.onOSImageCatalogRequest?.(integrationId)
      if (options.failImageIntegrationIds?.includes(integrationId)) {
        return json(route, { error: { code: 'provider_unavailable', message: 'Image provider is unavailable' } }, 503)
      }
      return json(route, osImages)
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
          code: 'provider_rejected',
          message: 'Machine reservation changed.',
          stage: 'deployment',
        })),
      }, 202)
    }

    if (path === '/api/v1/overview') {
      const siteId = url.searchParams.get('siteId')
      return json(route, {
        generatedAt: now, scope: { siteId },
        inventory: { sites: siteId ? 1 : 2, servers: 4, absent: 0, deployed: 4, clustered: 4, gpuDevices: 24, health: { up: 3, down: 1, unknown: 0 } },
        integrations: { total: integrationItems.length, failing: 1, items: integrationItems.map((item) => ({ id: item.id, siteId: item.siteId, name: item.name, kind: item.kind, providerKind: item.providerKind, enabled: item.enabled, lastSucceededAt: item.sync.lastSucceededAt, lastError: item.sync.lastError })) },
        clusters: { total: 2, unreachable: 1, unmatchedMembers: 1 }, operations: { active: 1, failedLast24Hours: 1, recent: operations },
        monitoring: { available: true, error: null, firing: { critical: 1, warning: 1, items: alerts.filter((alert) => alert.state === 'firing') } },
      })
    }

    if (path === '/api/v1/servers') {
      let items = [...fleet]
      const siteId = url.searchParams.get('siteId'); const clusterId = url.searchParams.get('clusterId'); const provisioningState = url.searchParams.get('provisioningState'); const keyword = url.searchParams.get('keyword')?.toLowerCase()
      if (siteId) items = items.filter((item) => item.source.siteId === siteId)
      if (clusterId) items = items.filter((item) => item.membership?.clusterId === clusterId)
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
          retryable: false,
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
        hweKernel: "", locked: false, commissioningStatus: "", testingStatus: "", observedAt: now,
        ...(releaseBody?.unbindStaticIPs ? { taskId: `task-${serverId}` } : {}),
      }, 202)
    }
    if (/\/api\/v1\/servers\/[^/]+\/provisioner-detail$/.test(path)) return json(route, { capabilities: { ephemeralDeploy: true, power: true, hardwareValidation: true, operatorState: true, machineDetail: true, hardwareInventory: true, machineRemoval: true, releaseOptions: true, networkConfiguration: true }, sections: [{ title: 'System', fields: [{ label: 'System vendor', value: 'Supermicro' }, { label: 'Serial', value: 'SN0001' }] }], tables: [{ title: 'Storage', columns: ['Device', 'Size', 'Model'], rows: [['nvme0n1', '3.84 TB', 'PM1733']] }, { title: 'PCI devices', columns: ['Address', 'Device', 'Vendor'], rows: [['03:00.0', 'MI300X', 'AMD']] }] })
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

    if (path === '/api/v1/clusters/deploy' && request.method() === 'POST') {
      const body = request.postDataJSON() as {
        name: string
        gpuStackOwner: string
        roleAssignments: Array<{
          serverId: string
          role: 'control-plane' | 'worker'
          runWorkloads?: boolean
        }>
      }
      const controllerCount = body.roleAssignments.filter(
        (assignment) => assignment.role === 'control-plane',
      ).length
      const topology: 'standalone' | 'multi-node' | 'high-availability' =
        controllerCount >= 3
          ? 'high-availability'
          : body.roleAssignments.length === 1
            ? 'standalone'
            : 'multi-node'
      const operationId = 'op-cluster-new'
      operationItems.unshift({
        ...operations[0],
        id: operationId,
        intent: 'Deploy ' + body.name,
        clusterId: 'cluster-new',
        targetServerIds: body.roleAssignments.map((assignment) => assignment.serverId),
      })
      clusterItems.push({
        id: 'cluster-new',
        siteId: 'site-a',
        name: body.name,
        type: 'kubernetes',
        integrationId: null,
        origin: 'deployed',
        lifecycleState: 'deploying',
        lifecycleOperationId: operationId,
        deployment: { topology, roleAssignments: body.roleAssignments },
        gpuStackOwner: body.gpuStackOwner,
        exporterOwner: 'ansible',
        sync: { lastStartedAt: null, lastSucceededAt: null, lastError: null, memberCount: 0, matchedCount: 0 },
        createdAt: now,
        updatedAt: now,
      })
      return json(route, { clusterId: 'cluster-new', operationId }, 202)
    }
    if (path === '/api/v1/clusters') {
      const siteId = url.searchParams.get('siteId')
      return json(route, clusterItems.filter((item) => !siteId || item.siteId === siteId))
    }
    const uninstallMatch = path.match(/^\/api\/v1\/clusters\/([^/]+)\/uninstall$/)
    if (uninstallMatch && request.method() === 'POST') {
      return json(route, { clusterId: uninstallMatch[1], operationId: 'op-uninstall' }, 202)
    }
    const clusterMatch = path.match(/^\/api\/v1\/clusters\/([^/]+)$/)
    if (clusterMatch && request.method() === 'DELETE') {
      const index = clusterItems.findIndex((cluster) => cluster.id === clusterMatch[1])
      if (index < 0) return json(route, { error: { code: 'not_found', message: 'Cluster not found' } }, 404)
      clusterItems.splice(index, 1)
      return json(route, { success: true })
    }
    if (clusterMatch) {
      const cluster = clusterItems.find((item) => item.id === clusterMatch[1])
      return cluster
        ? json(route, cluster)
        : json(route, { error: { code: 'not_found', message: 'Cluster not found' } }, 404)
    }
    if (path === '/api/v1/operations') {
      let items = [...operationItems]
      const status = url.searchParams.get('status')
      const clusterId = url.searchParams.get('clusterId')
      const serverId = url.searchParams.get('serverId')
      if (status) items = items.filter((item) => item.execution.status === status)
      if (clusterId) items = items.filter((item) => item.clusterId === clusterId)
      if (serverId) items = items.filter((item) => item.targetServerIds.includes(serverId))
      return json(route, { items, total: items.length, page: 1, pageSize: 30 })
    }
    if (path.endsWith('/events')) return json(route, { runId: 'run-1024', status: 'running', okCount: 4, changedCount: 2, failedCount: 1, events: [{ play: 'Prepare hosts', task: 'Gather facts', host: 'gpu-node-01', status: 'ok', changed: false, startedAt: now, endedAt: now }, { play: 'Install k0s', task: 'Write configuration', host: 'gpu-node-02', status: 'changed', changed: true, startedAt: now, endedAt: now }, { play: 'Install k0s', task: 'Start controller', host: 'gpu-node-04', status: 'failed', changed: false, startedAt: now, endedAt: now }] })
    if (path.endsWith('/logs')) return route.fulfill({ status: 200, contentType: 'text/plain', body: 'PLAY [Prepare hosts]\nTASK [Gather facts]\nok: [gpu-node-01]\nTASK [Write configuration]\nchanged: [gpu-node-02]\nTASK [Start controller]\nfatal: [gpu-node-04]: UNREACHABLE\n' })
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
        const cluster = clusterItems.find((item) => item.id === original.clusterId)
        if (cluster) {
          cluster.lifecycleState = 'deploying'
          cluster.lifecycleOperationId = retry.id
          cluster.updatedAt = now
        }
      }
      return json(route, retry, 202)
    }
    const operationMatch = path.match(/^\/api\/v1\/operations\/([^/]+)$/)
    if (operationMatch) return json(route, operationItems.find((item) => item.id === operationMatch[1]) ?? operationItems[0])
    return json(route, { error: { code: 'not_found', message: `No fixture for ${path}` } }, 404)
  })
}
