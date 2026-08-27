import type { Page, Route } from 'playwright/test'

const now = '2026-08-27T03:00:00Z'

function makeServer(index: number) {
  const ordinal = index + 1
  const named = ordinal <= 4
  return {
    id: `srv-${ordinal}`,
    source: { siteId: 'site-a', integrationId: 'maas-a', providerMachineId: `machine-${ordinal}` },
    hostname: named ? `gpu-node-0${ordinal}` : `compute-node-${String(ordinal).padStart(3, '0')}`,
    fqdn: named ? `gpu-node-0${ordinal}.lab.example` : `compute-node-${String(ordinal).padStart(3, '0')}.lab.example`,
    addresses: [named ? `192.168.40.${20 + ordinal}` : `10.20.${Math.floor(index / 250)}.${(index % 250) + 1}`],
    architecture: 'amd64/generic', cpuCores: 64, cpuModel: 'AMD EPYC 9554', memoryMiB: 524288, storageGB: 3840,
    gpus: named ? [{ vendor: 'AMD', model: 'MI300X', count: 8 }] : [],
    systemVendor: 'Supermicro', systemProduct: 'AS-8125GS-TNHR', providerZone: index < 2 ? 'rack-a' : 'rack-b',
    providerResourcePool: named ? 'accelerators' : 'compute', providerPod: '', tags: named ? ['gpu', 'production'] : ['compute'],
    hardware: { systemUuid: `uuid-${ordinal}`, serialNumber: `SN${String(ordinal).padStart(4, '0')}`, macAddresses: [`02:00:00:00:${String(Math.floor(index / 250)).padStart(2, '0')}:${String((index % 250) + 1).padStart(2, '0')}`] },
    provisioning: { state: 'deployed', providerState: 'deployed', powerState: 'on', osSystem: 'ubuntu', distroSeries: '24.04', ephemeral: false, hweKernel: 'ga-24.04', locked: false, commissioningStatus: 'passed', testingStatus: 'passed', integrationId: 'maas-a', observedAt: now },
    membership: ordinal <= 3 ? { clusterId: 'cluster-a', nodeName: `gpu-node-0${ordinal}`, role: 'control-plane', state: 'ready', observedAt: now } : ordinal === 4 ? { clusterId: 'cluster-a', nodeName: 'gpu-node-04', role: 'worker', state: 'ready', observedAt: now } : null,
    health: ordinal === 4 ? { state: 'down', observedAt: now } : { state: 'up', observedAt: now },
    absent: false, lastSeenAt: now, createdAt: '2026-08-01T00:00:00Z', updatedAt: now,
  }
}

const servers = Array.from({ length: 4 }, (_, index) => makeServer(index))
const operations = [
  { id: 'op-running', kind: 'cluster.deploy', intent: 'Deploy production k0s cluster', siteId: 'site-a', clusterId: 'cluster-a', targetServerIds: servers.map((server) => server.id), retryOfOperationId: null, execution: { runId: 'run-1024', playbook: 'deploy-k0s.yml', status: 'running', statusReason: null, startedAt: '2026-08-27T02:54:00Z', finishedAt: null }, requestedBy: 'admin', requestedAt: '2026-08-27T02:53:00Z', updatedAt: now },
  { id: 'op-failed', kind: 'exporter.install', intent: 'Install GPU exporters', siteId: 'site-a', clusterId: 'cluster-a', targetServerIds: ['srv-4'], retryOfOperationId: null, execution: { runId: 'run-1023', playbook: 'install-exporters.yml', status: 'failed', statusReason: 'Host unreachable', startedAt: '2026-08-27T01:10:00Z', finishedAt: '2026-08-27T01:12:00Z' }, requestedBy: 'admin', requestedAt: '2026-08-27T01:09:00Z', updatedAt: '2026-08-27T01:12:00Z' },
]
const clusters = [
  { id: 'cluster-a', siteId: 'site-a', name: 'production-k0s', type: 'kubernetes', integrationId: 'k8s-a', gpuStackOwner: 'gpu-operator', exporterOwner: 'k8s', sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null, memberCount: 5, matchedCount: 4 }, createdAt: '2026-08-10T00:00:00Z', updatedAt: now },
  { id: 'cluster-b', siteId: 'site-a', name: 'edge-staging', type: 'kubernetes', integrationId: null, gpuStackOwner: 'provisioning', exporterOwner: 'ansible', sync: { lastStartedAt: null, lastSucceededAt: null, lastError: null, memberCount: 0, matchedCount: 0 }, createdAt: '2026-08-20T00:00:00Z', updatedAt: now },
  { id: 'cluster-slurm', siteId: 'site-a', name: 'research-slurm', type: 'slurm', integrationId: 'slurm-a', gpuStackOwner: 'provisioning', exporterOwner: 'ansible', sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null, memberCount: 0, matchedCount: 0 }, createdAt: '2026-08-22T00:00:00Z', updatedAt: now },
]
const sites = [
  { id: 'site-a', name: 'Taipei Lab', description: 'Primary accelerator lab', createdAt: now, updatedAt: now },
  { id: 'site-b', name: 'Hsinchu Edge', description: 'Edge validation', createdAt: now, updatedAt: now },
]
const integrations = [
  { id: 'maas-a', siteId: 'site-a', kind: 'provisioner', providerKind: 'maas', name: 'MAAS Taipei', endpoint: 'https://maas.example', enabled: true, settings: {}, hasCredential: true, sync: { lastStartedAt: now, lastSucceededAt: now, lastError: null }, createdAt: now, updatedAt: now },
  { id: 'prom-a', siteId: 'site-a', kind: 'metrics', providerKind: 'prometheus', name: 'Prometheus Taipei', endpoint: 'https://prom.example', enabled: true, settings: {}, hasCredential: true, sync: { lastStartedAt: now, lastSucceededAt: now, lastError: 'Alertmanager timeout' }, createdAt: now, updatedAt: now },
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
  onMetricsRequest?: (serverIds: string[]) => void
  onMetricsActive?: (active: number) => void
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

/** Installs deterministic network fixtures; no backend or provider is contacted. */
export async function installApiFixtures(page: Page, options: FixtureOptions = {}) {
  await page.unroute('**/api/v1/**')
  const fleet = Array.from({ length: options.fleetSize ?? 4 }, (_, index) => makeServer(index))
  let metricBatchIndex = 0
  let activeMetricRequests = 0
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (path === '/api/v1/auth/me') return json(route, { id: 'admin-1', username: 'admin', role: 'admin' })
    if (path === '/api/v1/auth/login') return json(route, { accessToken: 'e2e-token' })
    if (path === '/api/v1/sites') return json(route, sites)
    if (path === '/api/v1/integrations') { const siteId = url.searchParams.get('siteId'); return json(route, integrations.filter((item) => !siteId || item.siteId === siteId)) }
    if (path === '/api/v1/overview') {
      const siteId = url.searchParams.get('siteId')
      return json(route, {
        generatedAt: now, scope: { siteId },
        inventory: { sites: siteId ? 1 : 2, servers: 4, absent: 0, deployed: 4, clustered: 4, gpuDevices: 32, health: { up: 3, down: 1, unknown: 0 } },
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
    if (/\/api\/v1\/servers\/[^/]+\/provisioner-detail$/.test(path)) return json(route, { capabilities: { ephemeralDeploy: true, power: true, hardwareValidation: true, operatorState: true, machineDetail: true, hardwareInventory: true }, sections: [{ title: 'System', fields: [{ label: 'System vendor', value: 'Supermicro' }, { label: 'Serial', value: 'SN0001' }] }], tables: [{ title: 'Network', columns: ['Interface', 'MAC', 'Link'], rows: [['eno1', '02:00:00:00:00:01', '100 Gbps']] }, { title: 'Storage', columns: ['Device', 'Size', 'Model'], rows: [['nvme0n1', '3.84 TB', 'PM1733']] }, { title: 'PCI devices', columns: ['Address', 'Device', 'Vendor'], rows: [['03:00.0', 'MI300X', 'AMD']] }] })
    const serverMatch = path.match(/^\/api\/v1\/servers\/([^/]+)$/)
    if (serverMatch) { const server = fleet.find((item) => item.id === serverMatch[1]); return server ? json(route, server) : json(route, { error: { code: 'not_found', message: 'Server not found' } }, 404) }

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

    if (path === '/api/v1/clusters/deploy' && request.method() === 'POST') return json(route, { clusterId: 'cluster-new', operationId: 'op-running' }, 202)
    if (path === '/api/v1/clusters') { const siteId = url.searchParams.get('siteId'); return json(route, clusters.filter((item) => !siteId || item.siteId === siteId)) }
    const clusterMatch = path.match(/^\/api\/v1\/clusters\/([^/]+)$/)
    if (clusterMatch) return json(route, clusters.find((cluster) => cluster.id === clusterMatch[1]) ?? null)
    if (path === '/api/v1/operations') { let items = [...operations]; const status = url.searchParams.get('status'); if (status) items = items.filter((item) => item.execution.status === status); return json(route, { items, total: items.length, page: 1, pageSize: 30 }) }
    if (path.endsWith('/events')) return json(route, { runId: 'run-1024', status: 'running', okCount: 4, changedCount: 2, failedCount: 1, events: [{ play: 'Prepare hosts', task: 'Gather facts', host: 'gpu-node-01', status: 'ok', changed: false, startedAt: now, endedAt: now }, { play: 'Install k0s', task: 'Write configuration', host: 'gpu-node-02', status: 'changed', changed: true, startedAt: now, endedAt: now }, { play: 'Install k0s', task: 'Start controller', host: 'gpu-node-04', status: 'failed', changed: false, startedAt: now, endedAt: now }] })
    if (path.endsWith('/logs')) return route.fulfill({ status: 200, contentType: 'text/plain', body: 'PLAY [Prepare hosts]\nTASK [Gather facts]\nok: [gpu-node-01]\nTASK [Write configuration]\nchanged: [gpu-node-02]\nTASK [Start controller]\nfatal: [gpu-node-04]: UNREACHABLE\n' })
    if (path.endsWith('/retry')) return json(route, { ...operations[1], id: 'op-retry' }, 201)
    const operationMatch = path.match(/^\/api\/v1\/operations\/([^/]+)$/)
    if (operationMatch) return json(route, operations.find((item) => item.id === operationMatch[1]) ?? operations[0])
    return json(route, { error: { code: 'not_found', message: `No fixture for ${path}` } }, 404)
  })
}
