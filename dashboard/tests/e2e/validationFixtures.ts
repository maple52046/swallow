import type { Request, Route } from 'playwright/test'
import type {
  ValidationResult,
  ValidationRun,
  ValidationSchedule,
  ValidationSuite,
} from '@/domain/validation/types'

// Deterministic Validation fixtures in the validation contract's shapes (validation.md): module
// suites for Servers and for a Kubernetes Platform, one script suite, a completed failed run with a
// result per check and Server, a completed gate run, a running run, and two schedules. State is per
// page, so one test's writes never leak into another.

const now = '2026-08-27T03:00:00Z'

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

function failure(route: Route, status: number, code: string, message: string) {
  return json(route, { error: { code, message, requestId: `req-validation-${code}` } }, status)
}

const hostBaseline: ValidationSuite = {
  id: 'host-baseline', source: 'module', module: { kind: 'validation', id: 'host', version: '0.1.0' },
  name: 'Host baseline', description: 'Checks clocks, CPUs, memory, and the root filesystem.', target: 'servers',
  platformType: null, impact: 'observe', budgetSeconds: 900,
  checks: [
    { id: 'time-sync', title: 'Time synchronization', description: 'The clock is synchronized.', gating: true },
    { id: 'cpu-count', title: 'CPU count', description: 'The OS shows the expected CPUs.', gating: true },
  ],
  parameters: {
    type: 'object', additionalProperties: false,
    properties: { minCpus: { type: 'integer', title: 'Fewest CPUs', minimum: 1 } },
  },
  script: null, runAs: null, timeoutSeconds: null, revision: null, scriptSha256: null, experimental: false, retired: false,
  createdAt: null, updatedAt: null, createdBy: null,
}

// The one experimental suite: hidden by the dashboard until the admin shows experimental suites.
const kubernetesSmoke: ValidationSuite = {
  ...hostBaseline, id: 'kubernetes-smoke', module: { kind: 'platform', id: 'kubernetes', version: '0.1.0' },
  name: 'Kubernetes smoke test', description: 'Runs short pods on every node.', target: 'platform', platformType: 'kubernetes',
  experimental: true,
  checks: [
    { id: 'nodes-ready', title: 'Nodes Ready', description: 'Every node is Ready.', gating: true },
    { id: 'cluster-dns', title: 'Cluster DNS', description: 'A pod resolves cluster DNS.', gating: true },
  ],
  parameters: null,
}

const failover: ValidationSuite = {
  ...hostBaseline, id: 'failover-drill', module: { kind: 'validation', id: 'host', version: '0.1.0' },
  name: 'Failover drill', description: 'Reboots one Server.', impact: 'disruptive', parameters: null,
  checks: [{ id: 'reboot', title: 'Reboot', description: 'The Server comes back.', gating: true }],
}

const mounts: ValidationSuite = {
  ...hostBaseline, id: 'user-mounts', source: 'user', module: null, name: 'Data mounts', description: 'Checks /data is mounted.',
  impact: 'workload', parameters: null, budgetSeconds: 900,
  checks: [{ id: 'script', title: 'Script exit code', description: 'The script exits 0.', gating: false }],
  script: '#!/bin/sh\nmountpoint -q /data\n', runAs: 'automation-user', timeoutSeconds: 600, revision: 2,
  scriptSha256: 'ab12', createdAt: '2026-08-20T00:00:00Z', updatedAt: '2026-08-21T00:00:00Z', createdBy: 'admin',
}

function result(check: string, id: string, verdict: ValidationResult['verdict'], summary: string, extra: Partial<ValidationResult> = {}): ValidationResult {
  return { check, target: { kind: 'server', id }, verdict, summary, measurements: [], evidence: '', observedAt: '2026-08-27T02:40:00Z', taskId: 'host-baseline', ...extra }
}

function run(id: string, overrides: Partial<ValidationRun>): ValidationRun {
  return {
    id, siteId: 'site-a',
    suite: { id: 'host-baseline', source: 'module', name: 'Host baseline', version: '0.1.0', revision: null, scriptSha256: null },
    checks: hostBaseline.checks, target: { kind: 'servers', serverIds: ['srv-1', 'srv-2'], platformId: null },
    parameters: {}, mode: 'report', trigger: { kind: 'manual', scheduleId: null, workflowId: null },
    workflowId: `wf-${id}`, status: 'completed', statusReason: null, verdict: 'passed',
    counts: { passed: 0, failed: 0, error: 0, skipped: 0 }, results: [],
    requestedBy: 'admin', requestedAt: '2026-08-27T02:30:00Z', startedAt: '2026-08-27T02:31:00Z', finishedAt: '2026-08-27T02:41:00Z',
    ...overrides,
  }
}

function initialRuns(): ValidationRun[] {
  return [
    run('run-running', {
      suite: { id: 'user-mounts', source: 'user', name: 'Data mounts', version: null, revision: 2, scriptSha256: 'ab12' },
      checks: mounts.checks, target: { kind: 'servers', serverIds: ['srv-3'], platformId: null },
      status: 'running', verdict: null, finishedAt: null, requestedAt: '2026-08-27T02:58:00Z', startedAt: '2026-08-27T02:59:00Z',
    }),
    run('run-failed', {
      parameters: { minCpus: 128 },
      verdict: 'failed', counts: { passed: 3, failed: 1, error: 0, skipped: 0 },
      results: [
        result('time-sync', 'srv-1', 'passed', 'synchronized, 0.4 ms from 10.0.0.1'),
        result('time-sync', 'srv-2', 'passed', 'synchronized, 0.7 ms from 10.0.0.1'),
        result('cpu-count', 'srv-1', 'passed', 'the OS shows 128 CPUs, expected at least 128'),
        result('cpu-count', 'srv-2', 'failed', 'the OS shows 64 CPUs, expected at least 128', {
          measurements: [{ name: 'cpus', value: 64, unit: 'count', expected: '>= 128' }],
          evidence: 'CPU(s): 64\nOn-line CPU(s) list: 0-63',
        }),
      ],
    }),
    run('run-gate', {
      suite: { id: 'kubernetes-smoke', source: 'module', name: 'Kubernetes smoke test', version: '0.1.0', revision: null, scriptSha256: null },
      checks: kubernetesSmoke.checks, target: { kind: 'platform', serverIds: ['srv-1'], platformId: 'platform-a' },
      mode: 'gate', trigger: { kind: 'deployment', scheduleId: null, workflowId: 'op-running' }, workflowId: 'op-running',
      counts: { passed: 2, failed: 0, error: 0, skipped: 0 }, requestedBy: 'platform-controller',
      requestedAt: '2026-08-26T09:00:00Z', startedAt: '2026-08-26T09:05:00Z', finishedAt: '2026-08-26T09:07:00Z',
      results: [
        { ...result('nodes-ready', 'srv-1', 'passed', 'Ready'), taskId: 'gate-kubernetes-smoke' },
        { ...result('cluster-dns', 'platform-a', 'passed', 'a pod resolves kubernetes.default through cluster DNS'), target: { kind: 'platform', id: 'platform-a' } },
      ],
    }),
  ]
}

function initialSchedules(): ValidationSchedule[] {
  const realized = (status: 'true' | 'false', reason: string, message = '') => [{ type: 'realized', status, reason, message, lastTransitionTime: now, observedGeneration: 1 }]
  return [
    {
      id: 'schedule-nightly', siteId: 'site-a', generation: 1,
      spec: { name: 'Nightly baseline', suiteId: 'host-baseline', serverIds: ['srv-1', 'srv-2'], platformId: null, parameters: {}, trigger: { kind: 'cron', cron: '0 2 * * *', timezone: 'Asia/Taipei' }, enabled: true },
      status: { observedGeneration: 1, nextRunAt: '2026-08-27T18:00:00Z', lastRunId: 'run-failed', lastRunAt: '2026-08-27T02:30:00Z', lastSkipReason: null, conditions: realized('true', 'converged') },
      createdAt: '2026-08-20T00:00:00Z', updatedAt: '2026-08-20T00:00:00Z',
    },
    {
      id: 'schedule-after', siteId: 'site-a', generation: 1,
      spec: { name: 'After k0s deploys', suiteId: 'kubernetes-smoke', serverIds: [], platformId: 'platform-a', parameters: {}, trigger: { kind: 'after-deployment', cron: null, timezone: null }, enabled: false },
      status: { observedGeneration: 1, nextRunAt: null, lastRunId: null, lastRunAt: null, lastSkipReason: 'a target is already in an unfinished operation', conditions: realized('false', 'disabled', 'The schedule is disabled.') },
      createdAt: '2026-08-21T00:00:00Z', updatedAt: '2026-08-21T00:00:00Z',
    },
  ]
}

/**
 * Per-page validation API state and its route handler. `handle` answers every `/api/v1/validation`
 * request the way the contract does: run starts are accepted as `pending` and refused for a
 * disruptive suite without `confirm`; a stale schedule `generation` is a `409`; retiring a suite a
 * schedule uses is a `409`.
 */
export function createValidationFixtures(): { handle: (route: Route, request: Request, url: URL) => Promise<void> } {
  const suites = [hostBaseline, kubernetesSmoke, failover, mounts]
  let runs = initialRuns()
  let schedules = initialSchedules()

  const handle = async (route: Route, request: Request, url: URL) => {
    const path = url.pathname
    const method = request.method()
    if (path === '/api/v1/validation/suites' && method === 'GET') {
      const includeRetired = url.searchParams.get('includeRetired') === 'true'
      return json(route, { items: suites.filter((suite) => includeRetired || !suite.retired) })
    }
    if (path === '/api/v1/validation/suites' && method === 'POST') {
      const body = request.postDataJSON() as { name: string; script: string; description?: string }
      const created: ValidationSuite = { ...mounts, id: `user-${suites.length + 1}`, name: body.name, description: body.description ?? '', script: body.script, revision: 1, createdAt: now, updatedAt: now }
      suites.push(created)
      return json(route, created, 201)
    }
    const suiteMatch = path.match(/^\/api\/v1\/validation\/suites\/([^/]+)$/)
    if (suiteMatch) {
      const index = suites.findIndex((suite) => suite.id === decodeURIComponent(suiteMatch[1]))
      if (index < 0) return failure(route, 404, 'not_found', 'validation suite not found')
      const suite = suites[index]
      if (method === 'GET') return json(route, suite)
      if (suite.source === 'module') return failure(route, 409, 'conflict', `suite ${suite.id} is declared by a module and cannot be changed`)
      if (method === 'DELETE') {
        if (schedules.some((schedule) => schedule.spec.suiteId === suite.id)) return failure(route, 409, 'conflict', 'a Validation Schedule uses the suite')
        suites[index] = { ...suite, retired: true }
        return route.fulfill({ status: 204 })
      }
      const body = request.postDataJSON() as { name: string; script: string }
      suites[index] = { ...suite, name: body.name, script: body.script, revision: (suite.revision ?? 1) + 1, updatedAt: now }
      return json(route, suites[index])
    }
    if (path === '/api/v1/validation/runs' && method === 'GET') {
      const filtered = runs.filter((item) =>
        (!url.searchParams.get('siteId') || item.siteId === url.searchParams.get('siteId'))
        && (!url.searchParams.get('suiteId') || item.suite.id === url.searchParams.get('suiteId'))
        && (!url.searchParams.get('serverId') || item.target.serverIds.includes(url.searchParams.get('serverId') ?? '') || item.results.some((r) => r.target.id === url.searchParams.get('serverId')))
        && (!url.searchParams.get('platformId') || item.target.platformId === url.searchParams.get('platformId'))
        && (!url.searchParams.get('status') || item.status === url.searchParams.get('status'))
        && (!url.searchParams.get('verdict') || item.verdict === url.searchParams.get('verdict'))
        && (!url.searchParams.get('scheduleId') || item.trigger.scheduleId === url.searchParams.get('scheduleId'))
        && (!url.searchParams.get('trigger') || item.trigger.kind === url.searchParams.get('trigger'))
        && (url.searchParams.get('active') !== 'true' || item.status === 'pending' || item.status === 'running'))
      const pageSize = Number(url.searchParams.get('pageSize') ?? 20)
      const page = Number(url.searchParams.get('page') ?? 1)
      // The list answers summaries: every run field but its results.
      const items = filtered.slice((page - 1) * pageSize, page * pageSize).map((item) => {
        const summary: Partial<ValidationRun> = { ...item }
        delete summary.results
        return summary
      })
      return json(route, { items, total: filtered.length, page, pageSize })
    }
    if (path === '/api/v1/validation/runs' && method === 'POST') {
      const body = request.postDataJSON() as { suiteId: string; serverIds?: string[]; platformId?: string; parameters?: Record<string, unknown>; confirm?: boolean }
      const suite = suites.find((item) => item.id === body.suiteId)
      if (!suite) return failure(route, 404, 'not_found', 'validation suite not found')
      if (suite.impact === 'disruptive' && !body.confirm) return failure(route, 400, 'validation_error', `suite ${suite.id} is disruptive; confirm it`)
      const created = run(`run-${runs.length + 1}`, {
        suite: { id: suite.id, source: suite.source, name: suite.name, version: suite.module?.version ?? null, revision: suite.revision, scriptSha256: suite.scriptSha256 },
        checks: suite.checks, parameters: body.parameters ?? {},
        target: { kind: suite.target, serverIds: body.serverIds ?? ['srv-1'], platformId: body.platformId ?? null },
        status: 'pending', verdict: null, startedAt: null, finishedAt: null, requestedAt: now,
      })
      runs = [created, ...runs]
      return json(route, created, 202)
    }
    const runMatch = path.match(/^\/api\/v1\/validation\/runs\/([^/]+)(\/cancel)?$/)
    if (runMatch) {
      const found = runs.find((item) => item.id === decodeURIComponent(runMatch[1]))
      if (!found) return failure(route, 404, 'not_found', 'validation run not found')
      if (!runMatch[2]) return json(route, found)
      if (found.mode === 'gate' || (found.status !== 'pending' && found.status !== 'running')) return failure(route, 409, 'conflict', 'the run cannot be canceled')
      runs = runs.map((item) => (item.id === found.id ? { ...item, status: 'canceled', statusReason: 'Canceled by admin.', finishedAt: now } : item))
      return json(route, found, 202)
    }
    if (path === '/api/v1/validation/schedules' && method === 'GET') {
      const siteId = url.searchParams.get('siteId')
      return json(route, { items: schedules.filter((schedule) => !siteId || schedule.siteId === siteId) })
    }
    if (path === '/api/v1/validation/schedules' && method === 'POST') {
      const body = request.postDataJSON() as { name: string; suiteId: string; serverIds?: string[]; platformId?: string; trigger: { kind: 'cron' | 'after-deployment'; cron?: string; timezone?: string }; enabled: boolean }
      if (body.trigger.kind === 'cron' && (body.trigger.cron ?? '').split(' ').length !== 5) return failure(route, 400, 'validation_error', 'cron needs five fields')
      const created: ValidationSchedule = {
        id: `schedule-${schedules.length + 1}`, siteId: 'site-a', generation: 1,
        spec: { name: body.name, suiteId: body.suiteId, serverIds: body.serverIds ?? [], platformId: body.platformId ?? null, parameters: {}, trigger: { kind: body.trigger.kind, cron: body.trigger.cron ?? null, timezone: body.trigger.timezone || (body.trigger.kind === 'cron' ? 'UTC' : null) }, enabled: body.enabled },
        status: { observedGeneration: 0, nextRunAt: null, lastRunId: null, lastRunAt: null, lastSkipReason: null, conditions: [] },
        createdAt: now, updatedAt: now,
      }
      schedules = [...schedules, created]
      return json(route, created, 201)
    }
    const scheduleMatch = path.match(/^\/api\/v1\/validation\/schedules\/([^/]+)$/)
    if (scheduleMatch) {
      const found = schedules.find((item) => item.id === decodeURIComponent(scheduleMatch[1]))
      if (!found) return failure(route, 404, 'not_found', 'validation schedule not found')
      if (method === 'GET') return json(route, found)
      if (method === 'DELETE') {
        schedules = schedules.filter((item) => item.id !== found.id)
        return route.fulfill({ status: 204 })
      }
      const body = request.postDataJSON() as { name: string; enabled: boolean; generation?: number }
      if (body.generation && body.generation !== found.generation) return failure(route, 409, 'conflict', 'the validation schedule changed since it was read')
      const replaced = { ...found, generation: found.generation + 1, spec: { ...found.spec, name: body.name, enabled: body.enabled }, updatedAt: now }
      schedules = schedules.map((item) => (item.id === found.id ? replaced : item))
      return json(route, replaced)
    }
    return failure(route, 404, 'not_found', `no validation fixture for ${method} ${path}`)
  }
  return { handle }
}
