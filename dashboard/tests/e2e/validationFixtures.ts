import type { Request, Route } from 'playwright/test'
import type {
  ValidationResult,
  ValidationRun,
  ValidationSchedule,
  ValidationSuite,
  ValidationSuiteRevision,
} from '@/domain/validation/types'
import type { FixtureIdentity } from './accessFixtures'

// Deterministic Validation fixtures in the validation contract's shapes (validation.md): module
// suites for Servers and for a Kubernetes Platform; three script suites (alice's member-level
// "Data mounts" at revision 3 whose revision 1 was never kept, bob's owner-level sudo suite, and one
// whose author was deleted); a completed failed run with a result per check and Server, a
// completed gate run, running runs of the admin and of bob, a finished run of alice, and two
// schedules; the `error` verdict only on request (`withErrors`). State is per page, so one test's
// writes never leak into another. The signed-in identity is the access fixtures' (admin, alice the
// owner of pool-research, bob its member).

const now = '2026-08-27T03:00:00Z'

/** The fixture accounts by identity, matching accessFixtures' ids, usernames, and uid numbers. */
const CALLERS: Readonly<Record<FixtureIdentity, { id: string; username: string; uidNumber: number }>> = {
  admin: { id: 'admin-1', username: 'admin', uidNumber: 10000 },
  owner: { id: 'user-alice', username: 'alice', uidNumber: 10001 },
  member: { id: 'user-bob', username: 'bob', uidNumber: 10002 },
}

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
  script: null, sudo: null, starterLevel: null, timeoutSeconds: null, revision: null, scriptSha256: null, experimental: false, retired: false,
  createdAt: null, updatedAt: null, createdBy: null, createdById: null,
}

// The one experimental suite: hidden by the dashboard until the User shows experimental suites.
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

const scriptCheck = [{ id: 'script', title: 'Script exit code', description: 'The script exits 0.', gating: false }]

// alice's member-level suite at revision 3. Revision 2 had another script, timeout, and starter
// level; revision 1 predates kept revisions, so reading it is a 404.
const mounts: ValidationSuite = {
  ...hostBaseline, id: 'user-mounts', source: 'user', module: null, name: 'Data mounts', description: 'Checks /data is mounted.',
  impact: 'workload', parameters: null, budgetSeconds: 900, checks: scriptCheck,
  script: '#!/bin/sh\nmountpoint -q /data\nmountpoint -q /scratch\n', sudo: false, starterLevel: 'member', timeoutSeconds: 600, revision: 3,
  scriptSha256: 'ab13', createdAt: '2026-08-20T00:00:00Z', updatedAt: '2026-08-22T00:00:00Z', createdBy: 'alice', createdById: 'user-alice',
}

// bob's owner-level suite that runs through the starter's sudo.
const kernelModules: ValidationSuite = {
  ...mounts, id: 'user-kernel-modules', name: 'Kernel modules', description: 'Checks the amdgpu module is loaded.', impact: 'observe',
  script: '#!/bin/bash\nlsmod | grep -q amdgpu\n', sudo: true, starterLevel: 'owner', timeoutSeconds: 120, revision: 1, scriptSha256: 'cd01',
  createdBy: 'bob', createdById: 'user-bob',
}

// A suite whose author could not be identified: nobody may edit it, and an admin may retire it.
const legacyDisk: ValidationSuite = {
  ...mounts, id: 'user-legacy-disk', name: 'Legacy disk check', description: 'Checks the root disk has space.', starterLevel: 'owner',
  script: '#!/bin/sh\ntest "$(df --output=pcent / | tail -1 | tr -d " %")" -lt 90\n', revision: 1, scriptSha256: 'ef01',
  createdBy: 'carol', createdById: null,
}

function revisionOf(suite: ValidationSuite, createdAt: string): ValidationSuiteRevision {
  return {
    suiteId: suite.id, revision: suite.revision ?? 1, script: suite.script ?? '', scriptSha256: suite.scriptSha256 ?? '',
    sudo: suite.sudo === true, starterLevel: suite.starterLevel ?? 'owner', timeoutSeconds: suite.timeoutSeconds ?? 600,
    impact: suite.impact, createdAt,
  }
}

function initialRevisions(): ValidationSuiteRevision[] {
  return [
    { ...revisionOf(mounts, '2026-08-21T00:00:00Z'), revision: 2, script: '#!/bin/sh\nmountpoint -q /data\n', scriptSha256: 'ab12', timeoutSeconds: 300, starterLevel: 'owner' },
    revisionOf(mounts, '2026-08-22T00:00:00Z'),
    revisionOf(kernelModules, '2026-08-20T00:00:00Z'),
    revisionOf(legacyDisk, '2026-08-20T00:00:00Z'),
  ]
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
    requestedBy: 'admin', requestedById: 'admin-1', runAs: null,
    requestedAt: '2026-08-27T02:30:00Z', startedAt: '2026-08-27T02:31:00Z', finishedAt: '2026-08-27T02:41:00Z',
    ...overrides,
  }
}

const mountsRun = (revision: number) => ({ id: 'user-mounts', source: 'user' as const, name: 'Data mounts', version: null, revision, scriptSha256: `ab1${revision}` })

function initialRuns(): ValidationRun[] {
  return [
    run('run-running', {
      suite: mountsRun(2), checks: mounts.checks, target: { kind: 'servers', serverIds: ['srv-3'], platformId: null },
      runAs: { username: 'admin', uidNumber: 10000, sudo: false },
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
      counts: { passed: 2, failed: 0, error: 0, skipped: 0 }, requestedBy: 'platform-controller', requestedById: null,
      requestedAt: '2026-08-26T09:00:00Z', startedAt: '2026-08-26T09:05:00Z', finishedAt: '2026-08-26T09:07:00Z',
      results: [
        { ...result('nodes-ready', 'srv-1', 'passed', 'Ready'), taskId: 'gate-kubernetes-smoke' },
        { ...result('cluster-dns', 'platform-a', 'passed', 'a pod resolves kubernetes.default through cluster DNS'), target: { kind: 'platform', id: 'platform-a' } },
      ],
    }),
    // The runs of alice and bob are in the other Site, so the site-a inventory keeps its three runs.
    run('run-alice-mounts', {
      siteId: 'site-b', suite: mountsRun(2), checks: mounts.checks, target: { kind: 'servers', serverIds: ['srv-3'], platformId: null },
      requestedBy: 'alice', requestedById: 'user-alice', runAs: { username: 'alice', uidNumber: 10001, sudo: false },
      counts: { passed: 1, failed: 0, error: 0, skipped: 0 }, requestedAt: '2026-08-21T02:30:00Z',
    }),
    run('run-bob-mounts', {
      siteId: 'site-b', suite: mountsRun(1), checks: mounts.checks, target: { kind: 'servers', serverIds: ['srv-4'], platformId: null },
      requestedBy: 'bob', requestedById: 'user-bob', runAs: { username: 'bob', uidNumber: 10002, sudo: false },
      status: 'running', verdict: null, finishedAt: null, requestedAt: '2026-08-27T02:50:00Z', startedAt: '2026-08-27T02:51:00Z',
    }),
  ]
}

// The `error` verdict, opted into with `withErrors`: run-failed also holds a check that could not
// judge its target (its verdict stays failed, as any failed result fails a run), and a site-a run
// whose verdict is error sits beside it, so one page shows Failed and Error side by side.
function withErrorRuns(runs: ValidationRun[]): ValidationRun[] {
  const errored = runs.map((item) => (item.id !== 'run-failed' ? item : {
    ...item,
    counts: { passed: 2, failed: 1, error: 1, skipped: 0 },
    results: item.results.map((entry) => (entry.check === 'time-sync' && entry.target.id === 'srv-2'
      ? { ...entry, verdict: 'error' as const, summary: 'chronyc tracking failed: 506 Cannot talk to daemon' }
      : entry)),
  }))
  const erroredRun = run('run-error', {
    target: { kind: 'servers', serverIds: ['srv-1'], platformId: null },
    verdict: 'error', counts: { passed: 1, failed: 0, error: 1, skipped: 0 },
    requestedAt: '2026-08-27T01:30:00Z', startedAt: '2026-08-27T01:31:00Z', finishedAt: '2026-08-27T01:40:00Z',
    results: [
      result('time-sync', 'srv-1', 'passed', 'synchronized, 0.5 ms from 10.0.0.1'),
      result('cpu-count', 'srv-1', 'error', 'no result was recorded', { taskId: null }),
    ],
  })
  return [...errored, erroredRun]
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
    // Written before schedules refused user suites; the controller no longer starts it.
    {
      id: 'schedule-mounts', siteId: 'site-a', generation: 1,
      spec: { name: 'Hourly mounts', suiteId: 'user-mounts', serverIds: ['srv-3'], platformId: null, parameters: {}, trigger: { kind: 'cron', cron: '0 * * * *', timezone: 'UTC' }, enabled: true },
      status: { observedGeneration: 1, nextRunAt: null, lastRunId: null, lastRunAt: null, lastSkipReason: null, conditions: realized('false', 'user_suite_unsupported', 'suite user-mounts is a user suite, which a schedule cannot start') },
      createdAt: '2026-08-21T00:00:00Z', updatedAt: '2026-08-21T00:00:00Z',
    },
  ]
}

/** The body of a user suite write (validation.md, Suites); omitted fields take the API's defaults. */
interface SuiteBody {
  name: string
  description?: string
  script: string
  sudo?: boolean
  starterLevel?: 'owner' | 'member'
  timeoutSeconds?: number
  impact?: ValidationSuite['impact']
  revision?: number
}

/**
 * Per-page validation API state and its route handler, written as `identity` (the admin by
 * default). `handle` answers every `/api/v1/validation` request the way the contract does:
 *
 * - a user suite is created with the caller as author; only its author replaces it and its author
 *   or an admin retires it (`403`); a replace with a stale `revision` is a `409`, and a replace that
 *   changes anything but the name and description keeps a new revision;
 * - a run start with `suiteRevision` for a module suite is a `400`, for a user suite at another
 *   revision a `409`; a member starting a module or owner-level suite is a `403`; an accepted run
 *   records the caller as `requestedBy`/`requestedById` and, for a user suite, `runAs`;
 * - `requestedBy=me` keeps the caller's runs; a cancel by other than the starter, an owner, or an
 *   admin is a `403`;
 * - a disruptive start without `confirm` is a `400`; a stale schedule `generation`, retiring a
 *   suite a schedule uses, and a schedule naming a user suite are refused.
 *
 * `concurrentSave`, a user suite id, makes another author save a new revision of that suite just
 * before the page's next write to it, so that write meets a `409`. `withErrors` adds the `error`
 * verdict to the runs (see `withErrorRuns`); without it, no run has an error.
 */
export function createValidationFixtures(
  identity: FixtureIdentity = 'admin',
  concurrentSave?: string,
  withErrors = false,
): { handle: (route: Route, request: Request, url: URL) => Promise<void> } {
  const caller = CALLERS[identity]
  const suites = [hostBaseline, kubernetesSmoke, failover, mounts, kernelModules, legacyDisk]
  const revisions = initialRevisions()
  let runs = withErrors ? withErrorRuns(initialRuns()) : initialRuns()
  let schedules = initialSchedules()
  let pendingConcurrentSave = concurrentSave

  // Another author's save lands: a new revision with one more line of script.
  const applyConcurrentSave = (index: number) => {
    const suite = suites[index]
    if (pendingConcurrentSave !== suite.id) return
    pendingConcurrentSave = undefined
    const revision = (suite.revision ?? 1) + 1
    suites[index] = { ...suite, script: `${suite.script ?? ''}echo checked by revision ${revision}\n`, revision, scriptSha256: `concurrent-${revision}`, updatedAt: now }
    revisions.push(revisionOf(suites[index], now))
  }

  const handle = async (route: Route, request: Request, url: URL) => {
    const path = url.pathname
    const method = request.method()
    if (path === '/api/v1/validation/suites' && method === 'GET') {
      const includeRetired = url.searchParams.get('includeRetired') === 'true'
      return json(route, { items: suites.filter((suite) => includeRetired || !suite.retired) })
    }
    if (path === '/api/v1/validation/suites' && method === 'POST') {
      const body = request.postDataJSON() as SuiteBody
      const created: ValidationSuite = {
        ...mounts, id: `user-${suites.length + 1}`, name: body.name, description: body.description ?? '', script: body.script,
        sudo: body.sudo ?? false, starterLevel: body.starterLevel ?? 'owner', timeoutSeconds: body.timeoutSeconds ?? 600,
        impact: body.impact ?? 'workload', revision: 1, scriptSha256: 'new01', createdAt: now, updatedAt: now,
        createdBy: caller.username, createdById: caller.id,
      }
      suites.push(created)
      revisions.push(revisionOf(created, now))
      return json(route, created, 201)
    }
    const revisionMatch = path.match(/^\/api\/v1\/validation\/suites\/([^/]+)\/revisions\/([^/]+)$/)
    if (revisionMatch && method === 'GET') {
      const found = revisions.find((item) => item.suiteId === decodeURIComponent(revisionMatch[1]) && String(item.revision) === revisionMatch[2])
      return found ? json(route, found) : failure(route, 404, 'not_found', 'validation suite revision not found')
    }
    const suiteMatch = path.match(/^\/api\/v1\/validation\/suites\/([^/]+)$/)
    if (suiteMatch) {
      const index = suites.findIndex((suite) => suite.id === decodeURIComponent(suiteMatch[1]))
      if (index < 0) return failure(route, 404, 'not_found', 'validation suite not found')
      if (method === 'GET') return json(route, suites[index])
      if (suites[index].source === 'module') return failure(route, 409, 'conflict', `suite ${suites[index].id} is declared by a module and cannot be changed`)
      if (method === 'DELETE') {
        const suite = suites[index]
        if (identity !== 'admin' && suite.createdById !== caller.id) return failure(route, 403, 'forbidden', `only the author of suite ${suite.id} or an admin may retire it`)
        if (schedules.some((schedule) => schedule.spec.suiteId === suite.id)) return failure(route, 409, 'conflict', 'a Validation Schedule uses the suite')
        suites[index] = { ...suite, retired: true }
        return route.fulfill({ status: 204 })
      }
      if (suites[index].createdById !== caller.id) return failure(route, 403, 'forbidden', `not the author of the validation suite: only the author of suite ${suites[index].id} may change it`)
      applyConcurrentSave(index)
      const suite = suites[index]
      const body = request.postDataJSON() as SuiteBody
      if (body.revision !== undefined && body.revision !== suite.revision) {
        return failure(route, 409, 'conflict', `the validation suite changed since it was read: suite ${suite.id} is at revision ${suite.revision}, not ${body.revision}`)
      }
      const next: ValidationSuite = {
        ...suite, name: body.name, description: body.description ?? '', script: body.script, sudo: body.sudo ?? false,
        starterLevel: body.starterLevel ?? 'owner', timeoutSeconds: body.timeoutSeconds ?? 600, impact: body.impact ?? 'workload', updatedAt: now,
      }
      const bumps = next.script !== suite.script || next.sudo !== suite.sudo || next.starterLevel !== suite.starterLevel
        || next.timeoutSeconds !== suite.timeoutSeconds || next.impact !== suite.impact
      suites[index] = bumps ? { ...next, revision: (suite.revision ?? 1) + 1 } : next
      if (bumps) revisions.push(revisionOf(suites[index], now))
      return json(route, suites[index])
    }
    if (path === '/api/v1/validation/runs' && method === 'GET') {
      const requestedBy = url.searchParams.get('requestedBy')
      if (requestedBy !== null && requestedBy !== 'me') return failure(route, 400, 'validation_error', 'requestedBy accepts only me')
      const filtered = runs.filter((item) =>
        (!url.searchParams.get('siteId') || item.siteId === url.searchParams.get('siteId'))
        && (!url.searchParams.get('suiteId') || item.suite.id === url.searchParams.get('suiteId'))
        && (!url.searchParams.get('serverId') || item.target.serverIds.includes(url.searchParams.get('serverId') ?? '') || item.results.some((r) => r.target.id === url.searchParams.get('serverId')))
        && (!url.searchParams.get('platformId') || item.target.platformId === url.searchParams.get('platformId'))
        && (!url.searchParams.get('status') || item.status === url.searchParams.get('status'))
        && (!url.searchParams.get('verdict') || item.verdict === url.searchParams.get('verdict'))
        && (!url.searchParams.get('scheduleId') || item.trigger.scheduleId === url.searchParams.get('scheduleId'))
        && (!url.searchParams.get('trigger') || item.trigger.kind === url.searchParams.get('trigger'))
        && (requestedBy !== 'me' || item.requestedById === caller.id)
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
      const body = request.postDataJSON() as { suiteId: string; serverIds?: string[]; platformId?: string; parameters?: Record<string, unknown>; confirm?: boolean; suiteRevision?: number }
      const index = suites.findIndex((item) => item.id === body.suiteId)
      if (index < 0 || suites[index].retired) return failure(route, 404, 'not_found', 'validation suite not found')
      if (body.suiteRevision !== undefined) applyConcurrentSave(index)
      const suite = suites[index]
      if (body.suiteRevision !== undefined && suite.source === 'module') return failure(route, 400, 'validation_error', 'suiteRevision is only for a user suite')
      if (body.suiteRevision !== undefined && body.suiteRevision !== suite.revision) {
        return failure(route, 409, 'conflict', `the validation suite changed since it was read: suite ${suite.id} is at revision ${suite.revision}, not ${body.suiteRevision}; review the new revision and start it again`)
      }
      const memberLevel = suite.source === 'user' && suite.starterLevel === 'member'
      if (identity === 'member' && !memberLevel) return failure(route, 403, 'forbidden', `not allowed for this validation run: suite ${suite.id} is started only by an owner of every target's Pool`)
      if (suite.impact === 'disruptive' && !body.confirm) return failure(route, 400, 'validation_error', `suite ${suite.id} is disruptive; confirm it`)
      const created = run(`run-${runs.length + 1}`, {
        suite: { id: suite.id, source: suite.source, name: suite.name, version: suite.module?.version ?? null, revision: suite.revision, scriptSha256: suite.scriptSha256 },
        checks: suite.checks, parameters: body.parameters ?? {},
        target: { kind: suite.target, serverIds: body.serverIds ?? ['srv-1'], platformId: body.platformId ?? null },
        requestedBy: caller.username, requestedById: caller.id,
        runAs: suite.source === 'user' ? { username: caller.username, uidNumber: caller.uidNumber, sudo: suite.sudo === true } : null,
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
      // The owner identity owns pool-research, where the fixture runs' targets are placed.
      if (identity === 'member' && found.requestedById !== caller.id) return failure(route, 403, 'forbidden', 'only the run\'s starter, an owner of its targets\' Pools, or an admin may cancel it')
      if (found.mode === 'gate' || (found.status !== 'pending' && found.status !== 'running')) return failure(route, 409, 'conflict', 'the run cannot be canceled')
      runs = runs.map((item) => (item.id === found.id ? { ...item, status: 'canceled', statusReason: `Canceled by ${caller.username}.`, finishedAt: now } : item))
      return json(route, found, 202)
    }
    if (path === '/api/v1/validation/schedules' && method === 'GET') {
      const siteId = url.searchParams.get('siteId')
      return json(route, { items: schedules.filter((schedule) => !siteId || schedule.siteId === siteId) })
    }
    if (path === '/api/v1/validation/schedules' && method === 'POST') {
      const body = request.postDataJSON() as { name: string; suiteId: string; serverIds?: string[]; platformId?: string; trigger: { kind: 'cron' | 'after-deployment'; cron?: string; timezone?: string }; enabled: boolean }
      if (body.trigger.kind === 'cron' && (body.trigger.cron ?? '').split(' ').length !== 5) return failure(route, 400, 'validation_error', 'cron needs five fields')
      if (suites.find((suite) => suite.id === body.suiteId)?.source === 'user') return failure(route, 400, 'validation_error', 'a schedule cannot start a user suite')
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
      const body = request.postDataJSON() as { name: string; suiteId: string; enabled: boolean; generation?: number }
      if (body.generation && body.generation !== found.generation) return failure(route, 409, 'conflict', 'the validation schedule changed since it was read')
      if (suites.find((suite) => suite.id === body.suiteId)?.source === 'user') return failure(route, 400, 'validation_error', 'a schedule cannot start a user suite')
      const replaced = { ...found, generation: found.generation + 1, spec: { ...found.spec, name: body.name, enabled: body.enabled }, updatedAt: now }
      schedules = schedules.map((item) => (item.id === found.id ? replaced : item))
      return json(route, replaced)
    }
    return failure(route, 404, 'not_found', `no validation fixture for ${method} ${path}`)
  }
  return { handle }
}
