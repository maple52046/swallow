import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// swallow's built-in usage charts (server-metrics.md "Usage History"): a Server's own history on
// its Monitoring tab, aggregated history for a selection, Pool, Zone, or Platform on the
// Monitoring console, the "no metrics backend" guidance, and Platform Monitoring Links. The charts
// are basic monitoring only; GPU metrics are a Platform Grafana's. Also the Overview's alert-source
// states: Alertmanager is optional, so only a failing one warns.

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
  // swallow charts basic monitoring only: srv-1 has GPUs, but its GPU metrics belong to a
  // Platform's Grafana, so neither a GPU chart nor a GPU tile appears and none is requested.
  await expect(page.getByRole('group', { name: /GPU/ })).toHaveCount(0)
  await expect(page.getByText(/GPU (utilization|temperature|power|memory)/)).toHaveCount(0)
  // A metric the backend has no history for reads "No data", never a flat zero line.
  const disk = page.getByRole('group', { name: 'Disk used (/)' })
  await expect(disk.getByText('No data')).toBeVisible()
  expect(requests[0]?.get('serverIds')).toBe('srv-1')
  expect(requests[0]?.get('window')).toBe('1h')
  expect(requests[0]?.get('metrics')).toBe('cpuUsagePercent,memoryUsedPercent,diskUsedPercent,networkReceiveBytesPerSecond,networkTransmitBytesPerSecond')

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

test('Overview counts four health states and raises Down and Unknown, not N/A, as attention', async ({ page }) => {
  // The default aggregate has one down, one unknown, and one N/A Server beside three up ones.
  await installApiFixtures(page)
  await page.goto('/?site=site-a')

  const fleetHealth = page.locator('.sw-metric-card').filter({ hasText: 'Fleet health' })
  // Up of the Servers swallow monitors; N/A ones are named in the caption, not in the denominator.
  await expect(fleetHealth).toContainText('3 / 5')
  await expect(fleetHealth).toContainText('1 down · 1 unknown · 1 N/A')
  await expect(fleetHealth).toHaveAttribute('data-tone', 'critical')

  const down = page.getByRole('link', { name: /1 server down/ })
  const unknown = page.getByRole('link', { name: /1 server with unknown health/ })
  // Both open the Servers list filtered to that state, in the current Site scope.
  await expect(down).toHaveAttribute('href', '/servers?health=down&site=site-a')
  await expect(unknown).toHaveAttribute('href', '/servers?health=unknown&site=site-a')
  await expect(unknown).toContainText('Swallow expects to monitor them but has no answer from their Node Exporter')
  // Down is critical and Unknown a warning: same kind label, different tone.
  const [downColor, unknownColor] = await Promise.all([down, unknown].map((item) => item.locator('.chakra-badge').evaluate((badge) => getComputedStyle(badge).color)))
  expect(downColor).not.toBe(unknownColor)
  await expect(page.locator('.sw-attention-row').filter({ hasText: /N\/A|not monitor/ })).toHaveCount(0)

  await unknown.click()
  await expect(page).toHaveURL('/servers?health=unknown&site=site-a')
  await page.getByRole('button', { name: /Filters/ }).click()
  await expect(page.getByRole('combobox', { name: 'Filter Server health', exact: true })).toContainText('Unknown')

  // Without a down Server, Unknown alone gives the card the warning tone and no down item.
  await page.unroute('**/api/v1/**')
  await installApiFixtures(page, { overviewHealth: { up: 4, down: 0, unknown: 2, notApplicable: 0 } })
  await page.goto('/?site=site-a')
  await expect(fleetHealth).toContainText('4 / 6')
  await expect(fleetHealth).toHaveAttribute('data-tone', 'warning')
  await expect(page.getByRole('link', { name: /2 servers with unknown health/ })).toBeVisible()
  await expect(page.getByRole('link', { name: /\d+ servers? down/ })).toHaveCount(0)
})

test('Monitoring counts the fleet by health and labels each Server with the shared badge', async ({ page }) => {
  await installApiFixtures(page, { serverHealth: { 'srv-2': 'unknown', 'srv-3': 'not_applicable' } })
  await page.goto('/monitoring?site=site-a')

  const fleetHealth = page.locator('.sw-metric-card').filter({ hasText: 'Fleet health' })
  await expect(fleetHealth).toContainText('1 / 3')
  await expect(fleetHealth).toContainText('1 down · 1 unknown · 1 N/A')
  await expect(fleetHealth).toHaveAttribute('data-tone', 'critical')
  const table = page.getByRole('table', { name: 'Server metrics' })
  for (const [name, label] of [['gpu-node-01', 'Up'], ['gpu-node-02', 'Unknown'], ['gpu-node-03', 'N/A'], ['gpu-node-04', 'Down']]) {
    await expect(table.getByRole('row').filter({ hasText: name }).getByText(`Health ${label}`, { exact: true })).toBeVisible()
  }
})

test('Platform detail links to its own monitoring UI and to its usage charts', async ({ page }) => {
  const requests: URLSearchParams[] = []
  await installApiFixtures(page, { onMetricRangeRequest: (params) => requests.push(params) })
  await page.goto('/platforms/platform-slurm?site=site-a')

  const grafana = page.getByRole('link', { name: 'Platform Grafana' })
  await expect(grafana).toHaveAttribute('href', 'http://10.0.0.11:3000/d/edqnz7pce7togc-rms/global-rms')
  await expect(grafana).toHaveAttribute('target', '_blank')

  await page.getByRole('link', { name: 'View usage charts' }).click()
  await expect(page).toHaveURL(/\/monitoring\?.*chartScope=platform/)
  await expect(page.getByRole('combobox', { name: 'Chart platform', exact: true })).toContainText('research-slurm')
  await expect.poll(() => requests.at(-1)?.get('platformId')).toBe('platform-slurm')
})

test('A Kubernetes Platform links the Grafana it runs in its cluster', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/platforms/platform-a?site=site-a')

  const grafana = page.getByRole('link', { name: 'Platform Grafana' })
  await expect(grafana).toHaveAttribute('href', 'http://192.168.40.21:30300/d/swallow-node-exporter/servers')
  await expect(grafana).toHaveAttribute('target', '_blank')
})
