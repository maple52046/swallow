import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

const now = '2026-08-27T03:00:00Z'

function task(id: string, name: string, job?: string) {
  return {
    id, kind: 'noop', name, ...(job ? { job } : {}), executor: 'internal', dependsOn: [],
    targets: [{ kind: 'server', id: 'srv-1' }], status: 'succeeded', attempt: 1, progress: 100, error: null,
    externalExecution: null, artifacts: [], startedAt: now, finishedAt: now,
  }
}

// A Workflow's Job groups are titled with the labels it declares; a Job it does not list reads as
// its name title-cased, and a Step outside any Job is listed without a group.
test('Workflow detail titles Job groups with the declared labels', async ({ page }) => {
  await installApiFixtures(page)
  const operation = {
    id: 'op-jobs', schemaVersion: 3, kind: 'deploy-kubernetes', intent: 'Deploy lab k0s', intentSnapshot: {},
    definition: 'platform/kubernetes@0.1.0', definitionVersion: 1, status: 'succeeded', statusReason: null,
    startState: 'started', temporal: { workflowId: 'swallow-operation/op-jobs' }, siteId: 'site-a', platformId: 'platform-a',
    targetResources: [{ kind: 'server', id: 'srv-1' }], targetServerIds: ['srv-1'], retryOfOperationId: null,
    steps: [
      task('provision-srv-1', 'Provision OS on srv-1', 'ensure-os'),
      task('install-platform', 'Install k0s Platform', 'configure-k0s'),
      task('post-check', 'Check the cluster answers', 'post-checks'),
      task('note', 'Record the deployment'),
    ],
    jobs: [
      { name: 'ensure-os', label: 'Provision OS', retry: 'job' },
      { name: 'configure-k0s', label: 'Configure Kubernetes', retry: 'job' },
    ],
    leases: [], requestCorrelation: null,
    execution: { runId: '', playbook: '', status: 'succeeded', statusReason: null, startedAt: now, finishedAt: now },
    requestedBy: 'admin', requestedAt: now, startedAt: now, finishedAt: now, updatedAt: now,
  }
  await page.route('**/api/v1/workflows/op-jobs', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(operation) }))
  await page.goto('/workflows/op-jobs?site=site-a')

  for (const label of ['Provision OS', 'Configure Kubernetes', 'Post Checks']) {
    await expect(page.getByRole('button', { name: new RegExp(`^(Expand|Collapse) ${label}$`) })).toBeVisible()
  }
  await expect(page.getByRole('button', { name: /^(Expand|Collapse) (Ensure Os|Configure K0s)$/ })).toHaveCount(0)
  await expect(page.getByText('Record the deployment')).toBeVisible()
})
