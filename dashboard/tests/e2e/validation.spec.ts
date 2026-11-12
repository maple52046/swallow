import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// User journeys of the Validation area (decision 081) on deterministic fixtures
// (validationFixtures.ts): a running script run, a completed failed host-baseline run, a gate run of a
// Kubernetes deployment, and two schedules.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
  })
})

test('Runs lists every run with its outcome in words and narrows by verdict', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/?site=site-a')
  await page.getByRole('navigation', { name: 'Primary navigation' }).getByRole('link', { name: 'Validation' }).click()
  await expect(page).toHaveURL('/validation/runs?site=site-a')
  await expect(page.getByRole('heading', { name: 'Validation', level: 1 })).toBeVisible()

  const runs = page.getByRole('table', { name: 'Validation Runs' })
  await expect(runs.getByRole('row', { name: /Data mounts/ })).toContainText('Running')
  const failed = runs.getByRole('row', { name: /Host baseline/ })
  await expect(failed).toContainText('Failed')
  await expect(failed).toContainText('3 passed, 1 failed')
  await expect(failed).toContainText('gpu-node-01, gpu-node-02')
  await expect(runs.getByRole('row', { name: /Kubernetes smoke test/ })).toContainText('Deployment gate')
  await expect(runs.getByRole('row', { name: /Kubernetes smoke test/ })).toContainText('Platform production-k0s')

  await page.getByRole('combobox', { name: 'Verdict' }).click()
  await page.getByRole('option', { name: 'Failed' }).click()
  await expect(page).toHaveURL('/validation/runs?site=site-a&verdict=failed')
  await expect(runs.getByRole('row')).toHaveCount(2)
})

test('A run shows each check’s verdict on each target and what a failed check found', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/validation/runs/run-failed?site=site-a')
  await expect(page.getByRole('heading', { name: 'Host baseline', level: 1 })).toBeVisible()
  await expect(page.getByText('Validation Run of gpu-node-01, gpu-node-02')).toBeVisible()
  await expect(page.getByRole('link', { name: 'View Workflow' })).toHaveAttribute('href', '/workflows/wf-run-failed?site=site-a')
  // A completed run cannot be canceled.
  await expect(page.getByRole('button', { name: 'Cancel run' })).toHaveCount(0)

  const matrix = page.getByRole('table', { name: 'Results by check and target' })
  await expect(matrix.getByRole('columnheader', { name: 'gpu-node-02' })).toBeVisible()
  await expect(matrix.getByRole('row', { name: /CPU count/ })).toContainText('Gating')
  await matrix.getByRole('button', { name: 'CPU count on gpu-node-02: Failed. Show details' }).click()
  const details = page.getByRole('region', { name: 'CPU count on gpu-node-02' })
  await expect(details).toContainText('the OS shows 64 CPUs, expected at least 128')
  await expect(details.getByRole('table', { name: 'Measurements' })).toContainText('>= 128')
  await expect(details.getByLabel('Evidence')).toContainText('On-line CPU(s) list: 0-63')
})

test('Running a suite chooses its Servers and parameters and opens the accepted run', async ({ page }) => {
  const started: unknown[] = []
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/validation/runs') started.push(request.postDataJSON())
  })
  await installApiFixtures(page)
  await page.goto('/validation/runs?site=site-a')
  await page.getByRole('button', { name: 'Run a suite' }).click()
  const dialog = page.getByRole('dialog', { name: 'Run a Validation Suite' })
  await expect(dialog.getByRole('button', { name: 'Start run' })).toBeDisabled()

  await dialog.getByRole('combobox', { name: 'Suite' }).click()
  await dialog.getByRole('option', { name: 'Host baseline' }).click()
  await dialog.getByRole('checkbox', { name: 'gpu-node-03' }).locator('..').click()
  await expect(dialog.getByRole('list', { name: 'Chosen Servers' })).toContainText('gpu-node-03')
  await dialog.getByLabel('Fewest CPUs').fill('64')
  await dialog.getByRole('button', { name: 'Start run' }).click()

  await expect(page).toHaveURL(/\/validation\/runs\/run-4\?site=site-a$/)
  await expect(page.getByRole('heading', { name: 'Host baseline', level: 1 })).toBeVisible()
  expect(started).toEqual([{ suiteId: 'host-baseline', serverIds: ['srv-3'], parameters: { minCpus: 64 }, confirm: false }])
})

test('A disruptive suite runs only after its impact is confirmed', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/validation/suites?site=site-a')
  await page.getByRole('button', { name: 'Run Failover drill' }).click()
  const dialog = page.getByRole('dialog', { name: 'Run a Validation Suite' })
  await dialog.getByRole('checkbox', { name: 'gpu-node-01' }).locator('..').click()
  await expect(dialog.getByRole('button', { name: 'Start run' })).toBeDisabled()
  await dialog.getByRole('checkbox', { name: /may interrupt service/ }).locator('..').click()
  await expect(dialog.getByRole('button', { name: 'Start run' })).toBeEnabled()
})

test('An unfinished run is canceled after a confirmation that names its consequence', async ({ page }) => {
  let canceled = false
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/validation/runs/run-running/cancel') canceled = true
  })
  await installApiFixtures(page)
  await page.goto('/validation/runs/run-running?site=site-a')
  await page.getByRole('button', { name: 'Cancel run' }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Cancel Validation Run' })
  await expect(confirm).toContainText('The results recorded so far are kept')
  await confirm.getByRole('button', { name: 'Cancel run' }).click()
  await expect.poll(() => canceled).toBe(true)
  await expect(page.getByText('Cancel requested')).toBeVisible()
})

test('Suites lists module and script suites, and a script suite is written and retired', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/validation/suites?site=site-a')
  const suites = page.getByRole('table', { name: 'Validation Suites' })
  await expect(suites.getByRole('row', { name: /Host baseline/ })).toContainText('host (validation module) 0.1.0')
  await expect(suites.getByRole('row', { name: /Data mounts/ })).toContainText('Script, revision 2')
  // Module suites are read-only.
  await expect(page.getByRole('button', { name: 'Edit Host baseline' })).toHaveCount(0)

  await page.getByRole('button', { name: 'New script suite' }).click()
  const editor = page.getByRole('dialog', { name: 'New script suite' })
  await expect(editor).toContainText('Do not put secrets in the script')
  await editor.getByLabel('Name').fill('Scratch space')
  await editor.getByRole('textbox', { name: /^Script/ }).fill('#!/bin/sh\ntest -d /scratch\n')
  await editor.getByRole('button', { name: 'Create suite' }).click()
  await expect(suites.getByRole('row', { name: /Scratch space/ })).toContainText('Script, revision 1')

  await page.getByRole('button', { name: 'Retire Scratch space' }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Retire suite' })
  await expect(confirm).toContainText('Its past runs stay readable')
  await confirm.getByRole('button', { name: 'Retire suite' }).click()
  await expect(suites.getByRole('row', { name: /Scratch space/ })).toHaveCount(0)
})

test('Experimental suites are offered only once shown, and the choice is remembered', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/validation/suites?site=site-a')
  const suites = page.getByRole('table', { name: 'Validation Suites' })
  await expect(suites.getByRole('row', { name: /Host baseline/ })).toBeVisible()
  await expect(suites.getByRole('row', { name: /Kubernetes smoke test/ })).toHaveCount(0)

  // The run dialog says where the hidden suite went.
  await page.getByRole('button', { name: 'Run Host baseline' }).click()
  const dialog = page.getByRole('dialog', { name: 'Run a Validation Suite' })
  await expect(dialog).toContainText('1 experimental suite is hidden.')
  await dialog.getByRole('button', { name: 'Cancel' }).click()

  const show = page.getByRole('checkbox', { name: 'Show experimental suites (1)' })
  await expect(show).not.toBeChecked()
  await show.locator('..').click()
  const smoke = suites.getByRole('row', { name: /Kubernetes smoke test/ })
  await expect(smoke).toContainText('Experimental')
  await expect(smoke).toContainText('A kubernetes Platform')

  await page.reload()
  await expect(page.getByRole('checkbox', { name: 'Show experimental suites (1)' })).toBeChecked()
  await page.goto('/validation/runs?site=site-a')
  await page.getByRole('button', { name: 'Run a suite' }).click()
  const offered = page.getByRole('dialog', { name: 'Run a Validation Suite' })
  await expect(offered).not.toContainText('experimental suite is hidden')
  await offered.getByRole('combobox', { name: 'Suite' }).click()
  await expect(offered.getByRole('option', { name: 'Kubernetes smoke test' })).toBeVisible()
})

test('Schedules show when they run, whether they can, and why an occurrence was skipped', async ({ page }) => {
  const written: unknown[] = []
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/validation/schedules') written.push(request.postDataJSON())
  })
  await installApiFixtures(page)
  await page.goto('/validation/schedules?site=site-a')
  const schedules = page.getByRole('table', { name: 'Validation Schedules' })
  const nightly = schedules.getByRole('row', { name: /Nightly baseline/ })
  await expect(nightly).toContainText('Cron 0 2 * * * (Asia/Taipei)')
  await expect(nightly).toContainText('Active')
  const after = schedules.getByRole('row', { name: /After k0s deploys/ })
  await expect(after).toContainText('After each deployment')
  await expect(after).toContainText('Disabled')
  await expect(after).toContainText('Skipped: a target is already in an unfinished operation')

  await page.getByRole('button', { name: 'New schedule' }).click()
  const editor = page.getByRole('dialog', { name: 'New schedule' })
  await editor.getByLabel('Name').fill('Weekly mounts')
  await editor.getByRole('combobox', { name: 'Suite' }).click()
  // A disruptive suite cannot be scheduled.
  await expect(page.getByRole('option', { name: 'Failover drill' })).toHaveCount(0)
  await page.getByRole('option', { name: 'Data mounts' }).click()
  await editor.getByRole('checkbox', { name: 'gpu-node-02' }).locator('..').click()
  await editor.getByLabel('Cron expression').fill('0 3 * * 1')
  await editor.getByLabel('Time zone').fill('Asia/Taipei')
  await editor.getByRole('button', { name: 'Create schedule' }).click()
  await expect(schedules.getByRole('row', { name: /Weekly mounts/ })).toContainText('Not observed yet')
  expect(written).toEqual([{
    name: 'Weekly mounts', suiteId: 'user-mounts', serverIds: ['srv-2'], enabled: true,
    trigger: { kind: 'cron', cron: '0 3 * * 1', timezone: 'Asia/Taipei' },
  }])
})

test('A Server and a Platform list their recent runs and lead to the filtered list', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/servers/srv-2/summary?site=site-a')
  await expect(page.getByRole('heading', { name: 'Validation', exact: true })).toBeVisible()
  await expect(page.getByRole('table', { name: 'Validation Runs of this Server' })).toContainText('Host baseline')
  await page.getByRole('link', { name: 'All runs' }).click()
  await expect(page).toHaveURL('/validation/runs?serverId=srv-2&site=site-a')
  await expect(page.getByText('Runs of Server gpu-node-02.')).toBeVisible()

  await page.goto('/platforms/platform-a/overview?site=site-a')
  await expect(page.getByRole('table', { name: 'Validation Runs of this Platform' })).toContainText('Kubernetes smoke test')
})
