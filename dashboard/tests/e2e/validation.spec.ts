import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
  })
})

test('Runs is a Site-scoped assurance inventory with quick outcome lenses', async ({ page }) => {
  const listRequests: URL[] = []
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (url.pathname === '/api/v1/validation/runs' && request.method() === 'GET') listRequests.push(url)
  })
  await installApiFixtures(page)
  await page.goto('/?site=site-a')
  await page.getByRole('navigation', { name: 'Primary navigation' }).getByRole('link', { name: 'Validation' }).click()
  await expect(page).toHaveURL('/validation/runs?site=site-a')
  await expect(page.getByRole('heading', { name: 'Validation activity' })).toBeVisible()
  await expect(page.getByText('Showing 1–3 of 3 runs')).toBeVisible()
  await expect.poll(() => listRequests.some((url) => url.searchParams.get('siteId') === 'site-a')).toBe(true)

  const runs = page.getByRole('table', { name: 'Validation Runs' })
  await expect(runs.getByRole('row', { name: /Data mounts/ })).toContainText('Running')
  const failed = runs.getByRole('row', { name: /Host baseline/ })
  await expect(failed).toContainText('3 passed, 1 failed')
  await expect(failed.getByRole('link', { name: 'Review run' })).toBeVisible()

  await page.getByRole('button', { name: 'Failed' }).click()
  await expect(page).toHaveURL('/validation/runs?site=site-a&verdict=failed')
  await expect(runs.getByRole('row')).toHaveCount(2)
  await page.getByRole('button', { name: 'Active' }).click()
  await expect(page).toHaveURL('/validation/runs?site=site-a&active=true')
  await expect(runs).toContainText('Data mounts')
  await expect(runs).not.toContainText('Host baseline')
})

test('Run detail presents issues first and keeps a complete inline-inspector matrix', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/validation/runs/run-failed?site=site-a')
  await expect(page.getByRole('heading', { name: 'Host baseline', level: 1 })).toBeVisible()
  await expect(page.getByRole('link', { name: 'View workflow' })).toHaveAttribute('href', '/workflows/wf-run-failed?site=site-a')
  await expect(page.getByRole('heading', { name: 'Issues found' })).toBeVisible()
  await expect(page.getByText(/Failed means a check completed/)).toBeVisible()

  const issue = page.getByRole('region', { name: 'CPU count on gpu-node-02' })
  await expect(issue).toContainText('the OS shows 64 CPUs, expected at least 128')
  await expect(issue.getByRole('table', { name: 'Measurements' })).toContainText('>= 128')
  await issue.getByText('Evidence').click()
  await expect(issue).toContainText('On-line CPU(s) list: 0-63')

  const matrix = page.getByRole('table', { name: 'Results by check and target' })
  await matrix.getByRole('button', { name: 'CPU count on gpu-node-02: Failed. Show details' }).click()
  await expect(matrix.getByRole('region', { name: 'CPU count on gpu-node-02' })).toContainText('CPU(s): 64')
})

test('Suite catalog groups capabilities and Suite detail keeps secondary history visible', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/validation/suites?site=site-a')
  await expect(page.getByRole('heading', { name: 'Server validation' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Custom script suites' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Platform validation' })).toHaveCount(0)
  await page.getByRole('checkbox', { name: 'Show experimental suites (1)' }).locator('..').click()
  await expect(page.getByRole('heading', { name: 'Platform validation' })).toBeVisible()
  await expect(page.getByRole('link', { name: /Kubernetes smoke test/ })).toBeVisible()

  await page.getByRole('link', { name: /Data mounts/ }).click()
  await expect(page).toHaveURL('/validation/suites/user-mounts?site=site-a')
  await expect(page.getByRole('heading', { name: 'Data mounts', level: 1 })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Checks' })).toBeVisible()
  await expect(page.getByText('Revision').first()).toBeVisible()
  await expect(page.getByRole('table', { name: 'Recent runs of this suite' })).toContainText('Data mounts')

  await page.goto('/validation/suites/kubernetes-smoke?site=site-a')
  await expect(page.getByRole('heading', { name: 'Kubernetes smoke test', level: 1 })).toBeVisible()
  await expect(page.getByText('Experimental').first()).toBeVisible()
})

test('Run a suite uses Suite & targets then Configuration & review', async ({ page }) => {
  const started: unknown[] = []
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/validation/runs') {
      started.push(request.postDataJSON())
    }
  })
  await installApiFixtures(page)
  await page.goto('/validation/runs?site=site-a')
  await page.getByRole('button', { name: 'Run a suite' }).click()
  const dialog = page.getByRole('dialog', { name: 'Run a suite' })
  await dialog.getByRole('radio', { name: /Host baseline/ }).click()
  await dialog.getByRole('checkbox', { name: 'gpu-node-03' }).locator('..').click()
  await dialog.getByRole('button', { name: 'Continue' }).click()
  await expect(dialog.getByText('Configuration & review').first()).toBeVisible()
  await dialog.getByLabel('Fewest CPUs').fill('64')
  await dialog.getByRole('button', { name: 'Start run' }).click()

  await expect(page).toHaveURL(/\/validation\/runs\/run-4\?site=site-a$/)
  expect(started).toEqual([{ suiteId: 'host-baseline', serverIds: ['srv-3'], parameters: { minCpus: 64 }, confirm: false }])
})

test('Disruptive Suite detail requires confirmation in the review step', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/validation/suites/failover-drill?site=site-a')
  await page.getByRole('button', { name: 'Run suite' }).click()
  const dialog = page.getByRole('dialog', { name: 'Run a suite' })
  await dialog.getByRole('checkbox', { name: 'gpu-node-01' }).locator('..').click()
  await dialog.getByRole('button', { name: 'Continue' }).click()
  await expect(dialog.getByRole('button', { name: 'Start run' })).toBeDisabled()
  await dialog.getByRole('checkbox', { name: /may interrupt service/ }).locator('..').click()
  await expect(dialog.getByRole('button', { name: 'Start run' })).toBeEnabled()
})

test('Schedules use friendly presets and generation-safe inline enablement', async ({ page }) => {
  const created: unknown[] = []
  const replaced: unknown[] = []
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (url.pathname === '/api/v1/validation/schedules' && request.method() === 'POST') created.push(request.postDataJSON())
    if (url.pathname === '/api/v1/validation/schedules/schedule-nightly' && request.method() === 'PUT') replaced.push(request.postDataJSON())
  })
  await installApiFixtures(page)
  await page.goto('/validation/schedules?site=site-a')
  const table = page.getByRole('table', { name: 'Validation Schedules' })
  const nightly = table.getByRole('row', { name: /Nightly baseline/ })
  await expect(nightly).toContainText('Active')
  await nightly.getByRole('checkbox', { name: 'Enabled' }).locator('..').click()
  await expect.poll(() => replaced.length).toBe(1)
  expect(replaced[0]).toMatchObject({ enabled: false, generation: 1, name: 'Nightly baseline' })

  await page.getByRole('button', { name: 'Create schedule' }).click()
  const editor = page.getByRole('dialog', { name: 'Create schedule' })
  await editor.getByLabel('Name').fill('Weekly mounts')
  await editor.getByRole('combobox', { name: 'Suite' }).click()
  await page.getByRole('option', { name: 'Data mounts' }).click()
  await editor.getByRole('checkbox', { name: 'gpu-node-02' }).locator('..').click()
  await editor.getByRole('combobox', { name: 'When' }).click()
  await page.getByRole('option', { name: 'Weekly' }).click()
  await editor.getByLabel('Time', { exact: true }).fill('03:00')
  await editor.getByLabel('Time zone').fill('Asia/Taipei')
  await editor.getByRole('button', { name: 'Create schedule' }).click()

  await expect(table.getByRole('row', { name: /Weekly mounts/ })).toContainText('Applying')
  expect(created).toEqual([{
    name: 'Weekly mounts',
    suiteId: 'user-mounts',
    serverIds: ['srv-2'],
    enabled: true,
    trigger: { kind: 'cron', cron: '0 3 * * 1', timezone: 'Asia/Taipei' },
  }])
})

test('An active report run can be canceled, while its evidence stays durable', async ({ page }) => {
  let canceled = false
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/validation/runs/run-running/cancel') canceled = true
  })
  await installApiFixtures(page)
  await page.goto('/validation/runs/run-running?site=site-a')
  await page.getByRole('button', { name: 'Cancel run' }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Cancel Validation Run' })
  await expect(confirm).toContainText('Results recorded so far remain available')
  await confirm.getByRole('button', { name: 'Cancel run' }).click()
  await expect.poll(() => canceled).toBe(true)
})
