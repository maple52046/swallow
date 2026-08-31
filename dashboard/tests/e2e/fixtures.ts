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
]
const baseDeploymentTemplates = [
  { id: 'template-a', siteId: 'site-a', integrationId: 'maas-a', name: 'GPU compute baseline', description: 'Ubuntu baseline for accelerator nodes', imageId: 'ubuntu/jammy', ephemeral: false, hasUserData: true, createdAt: now, updatedAt: now },
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
  secondReadyServerIntegrationId?: string
  failImageIntegrationIds?: string[]
  deploymentFailureIds?: string[]
  deploymentReadinessIssues?: Record<string, string>
  onDeploymentRequest?: (body: Record<string, unknown>) => void
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
  if (options.secondReadyServerIntegrationId && fleet[1]) {
    fleet[1].source.integrationId = options.secondReadyServerIntegrationId
    fleet[1].provisioning.integrationId = options.secondReadyServerIntegrationId
  }
  let deploymentTemplates = baseDeploymentTemplates.map((template) => ({ ...template }))
  let metricBatchIndex = 0
  let activeMetricRequests = 0
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
    if (path === '/api/v1/sites') return json(route, sites)
    if (path === '/api/v1/integrations') {
      const siteId = url.searchParams.get('siteId')
      const kind = url.searchParams.get('kind')
      return json(route, integrations.filter((item) => (
        (!siteId || item.siteId === siteId) &&
        (!kind || item.kind === kind)
      )))
    }

    if (path === '/api/v1/provisioning/images') {
      const integrationId = url.searchParams.get('integrationId') ?? ''
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
        })),
      }, 202)
    }

    if (path === '/api/v1/overview') {
      const siteId = url.searchParams.get('siteId')
      return json(route, {
        generatedAt: now, scope: { siteId },
        inventory: { sites: siteId ? 1 : 2, servers: 4, absent: 0, deployed: 4, clustered: 4, gpuDevices: 24, health: { up: 3, down: 1, unknown: 0 } },
        integrations: { total: integrations.length, failing: 1, items: integrations.map((item) => ({ id: item.id, siteId: item.siteId, name: item.name, kind: item.kind, providerKind: item.providerKind, enabled: item.enabled, lastSucceededAt: item.sync.lastSucceededAt, lastError: item.sync.lastError })) },
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
    if (/\/api\/v1\/servers\/[^/]+\/provisioner-detail$/.test(path)) return json(route, { capabilities: { ephemeralDeploy: true, power: true, hardwareValidation: true, operatorState: true, machineDetail: true, hardwareInventory: true, machineRemoval: true }, sections: [{ title: 'System', fields: [{ label: 'System vendor', value: 'Supermicro' }, { label: 'Serial', value: 'SN0001' }] }], tables: [{ title: 'Network', columns: ['Interface', 'MAC', 'Link'], rows: [['eno1', '02:00:00:00:00:01', '100 Gbps']] }, { title: 'Storage', columns: ['Device', 'Size', 'Model'], rows: [['nvme0n1', '3.84 TB', 'PM1733']] }, { title: 'PCI devices', columns: ['Address', 'Device', 'Vendor'], rows: [['03:00.0', 'MI300X', 'AMD']] }] })
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
      if (status) items = items.filter((item) => item.execution.status === status)
      if (clusterId) items = items.filter((item) => item.clusterId === clusterId)
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
