import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Task Diagnostics on the Workflow detail page (workflows contract, "Task errors and diagnostics"):
// a failed Task reads its user layer first and its admin evidence behind a disclosure, a running
// Task shows what it waits for against its budget, and the timeline keeps an earlier attempt's
// failure. Each test serves one Workflow, and its timeline, over the shared fixtures.

const now = '2026-11-05T03:00:00Z'
const observedAt = '2026-11-05T02:59:30Z'

const diagnosticFailure = {
  code: 'deployment_stalled',
  message: 'The OS deployment of srv-1 did not finish within its budget.\nThe provider still reports Deploying.',
  retryable: true,
  stage: 'deploying',
  diagnostic: {
    failureClass: 'transient',
    target: { kind: 'server', id: 'srv-1' },
    endpoint: 'https://provisioner.lab:5240/api/2.0/machines/abc123/',
    operation: 'read machine status',
    cause: 'GET https://provisioner.lab:5240/api/2.0/machines/abc123/: still Deploying after 12 polls\nAuthorization: [REDACTED]',
    remediation: 'Open the Server console to see where the installer stopped, then retry the Task.',
    observation: 'srv-1 is Deploying (installing the OS)',
    observedAt,
    attempts: 12,
    elapsedSeconds: 1800,
    budgetSeconds: 1800,
    providerEvents: [
      { type: 'Deploying', description: 'Started deploying the machine', at: '2026-11-05T02:30:00Z' },
      { type: 'Node changed status', description: 'From Allocated to Deploying' },
    ],
  },
}

function step(overrides: Record<string, unknown>) {
  return {
    id: 'provision-srv-1', kind: 'provision-os', name: 'Provision OS on srv-1', executor: 'maas', dependsOn: [],
    targets: [{ kind: 'server', id: 'srv-1' }], status: 'failed', attempt: 1, progress: 0,
    error: null, externalExecution: null, waiting: null, artifacts: [],
    startedAt: '2026-11-05T02:29:00Z', finishedAt: now,
    ...overrides,
  }
}

function workflow(id: string, status: string, steps: Array<Record<string, unknown>>) {
  return {
    id, schemaVersion: 3, kind: 'deploy-os', intent: 'Deploy Ubuntu on srv-1', intentSnapshot: {},
    definition: 'deploy-os', definitionVersion: 1, status, statusReason: null,
    startState: 'started', temporal: { workflowId: `swallow-operation/${id}` }, siteId: 'site-a', platformId: null,
    targetResources: [{ kind: 'server', id: 'srv-1' }], targetServerIds: ['srv-1'], retryOfOperationId: null,
    steps, jobs: [], leases: [], requestCorrelation: 'req-diagnostics',
    execution: { runId: '', playbook: 'provision-os', status, statusReason: null, startedAt: '2026-11-05T02:29:00Z', finishedAt: null },
    requestedBy: 'admin', requestedAt: '2026-11-05T02:28:00Z', startedAt: '2026-11-05T02:29:00Z', finishedAt: null, updatedAt: now,
  }
}

/** Serves `operation` and `timeline` for its id; every other request falls back to the shared fixtures. */
async function serveWorkflow(page: Page, operation: ReturnType<typeof workflow>, timeline: Array<Record<string, unknown>> = []) {
  await page.route(`**/api/v1/workflows/${operation.id}**`, async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === `/api/v1/workflows/${operation.id}` && route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(operation) })
    }
    if (path === `/api/v1/workflows/${operation.id}/timeline`) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(timeline) })
    }
    return route.fallback()
  })
}

test.beforeEach(async ({ page }) => {
  await installApiFixtures(page)
})

test('a failed Task reads what happened, who is affected, and the next step before its evidence', async ({ page }) => {
  await serveWorkflow(page, workflow('op-diagnostic', 'failed', [step({ error: diagnosticFailure })]))
  await page.goto('/workflows/op-diagnostic?site=site-a')

  const debuggerSection = page.locator('section.sw-operation-debugger')
  await expect(debuggerSection).toContainText('The OS deployment of srv-1 did not finish within its budget.')
  await expect(debuggerSection).toContainText('Affected: Server srv-1')
  await expect(debuggerSection.getByRole('link', { name: 'Server srv-1' })).toHaveAttribute('href', '/servers/srv-1?site=site-a')
  await expect(debuggerSection).toContainText('Next step: Open the Server console to see where the installer stopped, then retry the Task.')
  await expect(debuggerSection).toContainText('deployment_stalled')
  await expect(debuggerSection).toContainText('stage deploying')

  // The evidence starts behind a closed disclosure, so the user layer is what is read first.
  const disclosure = debuggerSection.getByRole('button', { name: 'Show diagnostic details' })
  await expect(disclosure).toHaveAttribute('aria-expanded', 'false')
  const details = page.getByRole('region', { name: 'Failure diagnostic details' })
  await expect(details).toBeHidden()

  await disclosure.click()
  const hide = debuggerSection.getByRole('button', { name: 'Hide diagnostic details' })
  await expect(hide).toHaveAttribute('aria-expanded', 'true')
  await expect(details).toBeVisible()
  await expect(details).toContainText('Transient — Expected to clear by itself')
  await expect(details).toContainText('read machine status')
  await expect(details).toContainText('https://provisioner.lab:5240/api/2.0/machines/abc123/')
  await expect(details).toContainText('Attempts12')
  await expect(details).toContainText('30m 0s of a 30m 0s budget (budget used up)')
  await expect(details).toContainText('srv-1 is Deploying (installing the OS)')
  await expect(details).toContainText('Authorization: [REDACTED]')
  const events = details.getByRole('table', { name: 'Provider events around the failure' })
  await expect(events.getByRole('row')).toHaveCount(3)
  await expect(events).toContainText('Started deploying the machine')
  await expect(events).toContainText('From Allocated to Deploying')

  // The disclosure works from the keyboard and closes again.
  await hide.focus()
  await page.keyboard.press('Enter')
  await expect(debuggerSection.getByRole('button', { name: 'Show diagnostic details' })).toHaveAttribute('aria-expanded', 'false')
  await expect(details).toBeHidden()
})

test('an error without a diagnostic keeps its message and code and offers no disclosure', async ({ page }) => {
  await serveWorkflow(page, workflow('op-plain-error', 'failed', [step({
    error: { code: 'deployment_failed', message: 'The provider reported Failed deployment.', retryable: false, diagnostic: null },
  })]))
  await page.goto('/workflows/op-plain-error?site=site-a')

  const debuggerSection = page.locator('section.sw-operation-debugger')
  await expect(debuggerSection).toContainText('The provider reported Failed deployment.')
  await expect(debuggerSection).toContainText('deployment_failed')
  await expect(debuggerSection).not.toContainText('Affected:')
  await expect(debuggerSection).not.toContainText('Next step:')
  await expect(debuggerSection.getByRole('button', { name: /diagnostic details/ })).toHaveCount(0)
})

test('a running Task shows what it waits for and its elapsed time against the budget', async ({ page }) => {
  await serveWorkflow(page, workflow('op-waiting', 'waiting_external', [step({
    status: 'running', finishedAt: null,
    waiting: { observation: 'srv-1 is Deploying (installing the OS)', observedAt, attempts: 4, elapsedSeconds: 125, budgetSeconds: 1800 },
  })]))
  await page.goto('/workflows/op-waiting?site=site-a')

  const debuggerSection = page.locator('section.sw-operation-debugger')
  await expect(debuggerSection).toContainText('Waiting: srv-1 is Deploying (installing the OS)')
  await expect(debuggerSection).toContainText('Waited 2m 5s of a 30m 0s budget (27m 55s left) · 4 observations')
  const row = page.getByRole('table', { name: 'Workflow steps' }).getByRole('row').filter({ hasText: 'Provision OS on srv-1' })
  await expect(row).toContainText('srv-1 is Deploying (installing the OS)')
})

test('a wait without a budget reports only its elapsed time', async ({ page }) => {
  await serveWorkflow(page, workflow('op-unbounded', 'waiting_external', [step({
    status: 'running', finishedAt: null,
    waiting: { observation: 'Waiting for the Server to enroll', observedAt, attempts: 1, elapsedSeconds: 30 },
  })]))
  await page.goto('/workflows/op-unbounded?site=site-a')

  const debuggerSection = page.locator('section.sw-operation-debugger')
  await expect(debuggerSection).toContainText('Waiting: Waiting for the Server to enroll')
  await expect(debuggerSection).toContainText('Waited 30s · 1 observation')
  await expect(debuggerSection).not.toContainText('budget')
})

test('the timeline keeps an earlier attempt\'s failure after a retry', async ({ page }) => {
  const operation = workflow('op-retried', 'waiting_external', [step({ status: 'running', attempt: 2, finishedAt: null })])
  await serveWorkflow(page, operation, [
    {
      id: 'op-retried/step/provision-srv-1/failed/1', operationId: 'op-retried', stepId: 'provision-srv-1', type: 'step_status',
      message: 'Provision OS on srv-1 changed to failed.', createdAt: '2026-11-05T02:40:00Z',
      details: {
        status: 'failed', attempt: 1, code: 'provider_unreachable', message: 'The provisioner API did not answer.\nGET timed out.',
        stage: '', retryable: true, failureClass: 'transient', endpoint: 'https://provisioner.lab:5240/api/2.0/',
      },
    },
    {
      id: 'op-retried/step/provision-srv-1/running/2', operationId: 'op-retried', stepId: 'provision-srv-1', type: 'step_status',
      message: 'Provision OS on srv-1 changed to running.', createdAt: '2026-11-05T02:45:00Z',
      details: { status: 'running', attempt: 2 },
    },
  ])
  await page.goto('/workflows/op-retried?site=site-a')

  const timeline = page.locator('ol.sw-timeline')
  const failed = timeline.locator('li').filter({ hasText: 'changed to failed.' })
  await expect(failed).toContainText('Attempt 1 · provider_unreachable · Transient failure · https://provisioner.lab:5240/api/2.0/')
  await expect(failed).toContainText('The provisioner API did not answer.')
  await expect(failed).not.toContainText('GET timed out.')
  const running = timeline.locator('li').filter({ hasText: 'changed to running.' })
  await expect(running).not.toContainText('Attempt 2')
})
