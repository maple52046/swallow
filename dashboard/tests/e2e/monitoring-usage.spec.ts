import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// swallow's built-in usage charts (server-metrics.md "Usage History"): a Server's own history on
// its Monitoring tab, aggregated history for a selection, Pool, Zone, or Platform on the
// Monitoring console, the "no metrics backend" guidance, and Platform Monitoring Links. Also the
// Overview's alert-source states: Alertmanager is optional, so only a failing one warns.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

async function chooseSingleSelectOption(page: Page, fieldLabel: string, optionLabel: string) {
  await page.getByRole('combobox', { name: fieldLabel, exact: true }).click()
  await page.getByRole('option', { name: optionLabel, exact: true }).click()
}

test('Server detail charts its own usage and keeps the window in the URL', async ({ page }) => {
  const requests: URLSearchParams[] = []
  await installApiFixtures(page, { onMetricRangeRequest: (params) => requests.push(params) })
  await page.goto('/servers/srv-1/monitoring?site=site-a')

  await expect(page.getByRole('heading', { name: 'Usage history' })).toBeVisible()
  await expect(page.getByRole('slider', { name: 'CPU usage' })).toBeVisible()
  await expect(page.getByRole('slider', { name: 'Network received' })).toBeVisible()
  // srv-1 is a GPU server, so its GPU history is charted too.
  await expect(page.getByRole('slider', { name: 'GPU utilization' })).toBeVisible()
  // A metric the backend has no history for reads "No data", never a flat zero line.
  const disk = page.getByRole('group', { name: 'Disk used (/)' })
  await expect(disk.getByText('No data')).toBeVisible()
  expect(requests[0]?.get('serverIds')).toBe('srv-1')
  expect(requests[0]?.get('window')).toBe('1h')

  await page.getByRole('button', { name: '6h (last 6 hours)' }).click()
  await expect(page).toHaveURL(/window=6h/)
  await expect(page.getByRole('button', { name: '6h (last 6 hours)' })).toHaveAttribute('aria-pressed', 'true')
  await expect.poll(() => requests.at(-1)?.get('window')).toBe('6h')
})

test('Chart scrubber reads values from the keyboard', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/servers/srv-1/monitoring?site=site-a')

  const cpu = page.getByRole('slider', { name: 'CPU usage' })
  await cpu.focus()
  await page.keyboard.press('End')
  await expect(cpu).toHaveAttribute('aria-valuetext', /\d+\.\d%$/)
  await page.keyboard.press('Home')
  await expect(cpu).toHaveAttribute('aria-valuenow', '0')
})

test('Monitoring charts ticked Servers, then a Pool, aggregated by the backend', async ({ page }) => {
  const requests: URLSearchParams[] = []
  await installApiFixtures(page, { fleetSize: 6, onMetricRangeRequest: (params) => requests.push(params) })
  await page.goto('/monitoring?site=site-a')

  await expect(page.getByText('Nothing to chart yet')).toBeVisible()
  // Chakra paints the box over its hidden native input, as other specs account for with force.
  await page.getByRole('checkbox', { name: 'Chart gpu-node-01' }).check({ force: true })
  await page.getByRole('checkbox', { name: 'Chart gpu-node-02' }).check({ force: true })
  await expect(page.getByText('2 selected')).toBeVisible()
  await expect(page.getByRole('slider', { name: 'CPU usage' })).toBeVisible()
  await expect.poll(() => requests.at(-1)?.get('serverIds')).toBe('srv-1,srv-2')
  expect(new URL(page.url()).searchParams.get('chartServers')).toBe('srv-1,srv-2')

  await chooseSingleSelectOption(page, 'Chart scope', 'Pool')
  await expect(page.getByText('Choose a pool to chart its current members.')).toBeVisible()
  await chooseSingleSelectOption(page, 'Chart pool', 'research')
  await expect(page).toHaveURL(/chartScope=pool/)
  await expect.poll(() => requests.at(-1)?.get('poolId')).toBe('pool-research')
  await expect(page.getByRole('slider', { name: 'Memory used' })).toBeVisible()
})

test('Missing metrics backend explains what to register instead of failing', async ({ page }) => {
  await installApiFixtures(page, { metricsBackendMissing: true })
  await page.goto('/servers/srv-1/monitoring?site=site-a')

  await expect(page.getByText('No metrics backend for this Site')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Open Integrations' })).toHaveAttribute('href', '/infrastructure/integrations?site=site-a')
  // The current-value panel does not repeat the warning; its tiles simply read "No data".
  await expect(page.getByText('Metrics are unavailable')).toHaveCount(0)
})

test('Overview treats a Site without Alertmanager as not configured, not as a failure', async ({ page }) => {
  await installApiFixtures(page, { overviewAlertSource: 'not-configured' })
  await page.goto('/?site=site-a')

  const alertsCard = page.locator('.sw-metric-card').filter({ hasText: 'Alerts' })
  await expect(alertsCard).toContainText('Not configured')
  await expect(alertsCard).toContainText('No Alertmanager for this scope')
  await expect(page.getByText('Alerts unavailable')).toHaveCount(0)
  await expect(page.getByText('Monitoring unavailable')).toHaveCount(0)
})

test('Overview warns only when a configured Alertmanager cannot be read', async ({ page }) => {
  await installApiFixtures(page, { overviewAlertSource: 'unavailable' })
  await page.goto('/?site=site-a')

  await expect(page.getByText('Alerts unavailable', { exact: true })).toBeVisible()
  await expect(page.getByText('Swallow could not read alerts from Alertmanager.')).toBeVisible()
  // Zero counts from an unread source are never shown as "0 alerts".
  await expect(page.locator('.sw-metric-card').filter({ hasText: 'Alerts' })).toContainText('Unavailable')
})

test('Platform detail links to its own monitoring UI and to its usage charts', async ({ page }) => {
  const requests: URLSearchParams[] = []
  await installApiFixtures(page, { onMetricRangeRequest: (params) => requests.push(params) })
  await page.goto('/platforms/platform-slurm?site=site-a')

  const omnistat = page.getByRole('link', { name: 'Omnistat Grafana' })
  await expect(omnistat).toHaveAttribute('href', 'https://omnistat.example/grafana')
  await expect(omnistat).toHaveAttribute('target', '_blank')

  await page.getByRole('link', { name: 'View usage charts' }).click()
  await expect(page).toHaveURL(/\/monitoring\?.*chartScope=platform/)
  await expect(page.getByRole('combobox', { name: 'Chart platform', exact: true })).toContainText('research-slurm')
  await expect.poll(() => requests.at(-1)?.get('platformId')).toBe('platform-slurm')
})
