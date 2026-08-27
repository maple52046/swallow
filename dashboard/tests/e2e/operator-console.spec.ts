import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

const visibleGlobalNavigation = (page: import('playwright/test').Page) => page.locator('button[aria-label="Global navigation"]:visible')
const visibleAppearance = (page: import('playwright/test').Page) => page.locator('button[aria-label^="Appearance:"]:visible')
const visibleSiteScope = (page: import('playwright/test').Page) => page.locator('button[aria-label^="Site scope:"]:visible')

async function chooseMenuItem(page: import('playwright/test').Page, name: string) {
  await page.getByRole('menuitem', { name, exact: true }).click()
}

test.beforeEach(async ({ page }, testInfo) => {
  await installApiFixtures(page)
  const appearance = 'light'
  const authenticated = !testInfo.title.includes('login')
  await page.addInitScript(({ appearance, authenticated }) => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', JSON.stringify(appearance))
    if (authenticated) localStorage.setItem('access_token', 'e2e-token')
    else localStorage.removeItem('access_token')
  }, { appearance, authenticated })
})

test.describe('operator interactions', () => {
  test('site scope is URL-owned and detail switching returns to the list', async ({ page }) => {
    await page.goto('/')
    await visibleSiteScope(page).click()
    await chooseMenuItem(page, 'Taipei Lab')
    await expect(page).toHaveURL(/\?site=site-a$/)
    await page.getByText('1 failed operations in 24h').click()
    await expect(page).toHaveURL('/operations?status=failed&site=site-a')

    await page.goto('/servers/srv-1/summary?site=site-a')
    await visibleSiteScope(page).click()
    await chooseMenuItem(page, 'Hsinchu Edge')
    await expect(page).toHaveURL('/servers?site=site-b')

    await page.goto('/servers?site=missing')
    await expect(page).toHaveURL('/servers')
    await expect(page.getByText('Unknown Site')).toBeVisible()
  })

  test('PatternFly theme resolves system, manual modes, surface color, and persistence', async ({ page }) => {
    await page.goto('/')
    const lightSurface = await page.evaluate(() => getComputedStyle(document.body).backgroundColor)
    await expect(page.locator('html')).not.toHaveClass(/pf-v6-theme-dark/)

    await visibleAppearance(page).click(); await chooseMenuItem(page, 'Dark')
    await expect(page.locator('html')).toHaveClass(/pf-v6-theme-dark/)
    const darkSurface = await page.evaluate(() => getComputedStyle(document.body).backgroundColor)
    expect(darkSurface).not.toBe(lightSurface)
    await page.reload()
    await expect(page.locator('html')).toHaveClass(/pf-v6-theme-dark/)
    await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('swallow.appearance') ?? 'null'))).toBe('dark')

    await visibleAppearance(page).click(); await chooseMenuItem(page, 'System')
    await page.emulateMedia({ colorScheme: 'light' })
    await expect(page.locator('html')).not.toHaveClass(/pf-v6-theme-dark/)
    await page.emulateMedia({ colorScheme: 'dark' })
    await expect(page.locator('html')).toHaveClass(/pf-v6-theme-dark/)
    await visibleAppearance(page).click(); await chooseMenuItem(page, 'Light')
    await expect(page.locator('html')).not.toHaveClass(/pf-v6-theme-dark/)
  })

  test('desktop dock collapses to icons with tooltip and persists', async ({ page }) => {
    await page.goto('/')
    await visibleGlobalNavigation(page).click()
    const dock = page.locator('.pf-v6-c-page__dock')
    await expect(dock).not.toHaveClass(/pf-m-text-expanded/)
    await expect.poll(() => dock.evaluate((element) => Math.round(element.getBoundingClientRect().width))).toBe(64)
    await expect.poll(() => page.evaluate(() => localStorage.getItem('swallow.shell.sidebar-collapsed'))).toBe('true')
    await dock.getByRole('link', { name: 'Servers', exact: true }).hover()
    await expect(page.getByRole('tooltip', { name: 'Servers' })).toBeVisible()
    await page.reload()
    await expect(dock).not.toHaveClass(/pf-m-text-expanded/)
  })

  test('mobile drawer traps focus, closes with Escape, and restores the toggle', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/')
    const toggle = visibleGlobalNavigation(page)
    await toggle.click()
    const dock = page.locator('.pf-v6-c-page__dock')
    await expect(dock).toBeVisible()
    await expect.poll(() => dock.evaluate((element) => element.contains(document.activeElement))).toBe(true)
    await page.keyboard.press('Escape')
    await expect(dock).toBeHidden()
    await expect(toggle).toBeFocused()
  })

  test('NetBox views, columns, selection, and MAAS actions work together', async ({ page }) => {
    await page.goto('/servers?site=site-a')
    await page.getByLabel('Select all on this page').click()
    await expect(page.getByText('4 selected')).toBeVisible()
    await page.getByRole('button', { name: 'Take action' }).click()
    await expect(page.getByRole('menuitem', { name: 'Power on' })).toBeVisible()
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: 'Clear', exact: true }).click()

    await page.getByLabel('Configure columns').click()
    await page.getByLabel('GPUs').uncheck()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('columnheader', { name: 'GPUs' })).toHaveCount(0)

    await page.getByRole('button', { name: /Saved views/ }).click()
    await chooseMenuItem(page, 'Save current view')
    await page.getByRole('textbox', { name: 'Name' }).fill('Compute view')
    await page.getByRole('button', { name: 'Save view' }).click()
    await page.getByRole('button', { name: /Saved views/ }).click()
    await expect(page.getByText('Compute view', { exact: true })).toBeVisible()
    await page.getByLabel('Rename Compute view').click()
    await page.getByRole('textbox', { name: 'Name' }).fill('Operators')
    await page.getByRole('button', { name: 'Rename' }).click()
    await page.getByRole('button', { name: /Saved views/ }).click()
    await page.getByLabel('Delete Operators').click()
    await page.getByRole('button', { name: /Saved views/ }).click()
    await expect(page.getByText('Operators', { exact: true })).toHaveCount(0)
  })

  test('Cockpit machine detail retains tabs and real action controls', async ({ page }) => {
    await page.goto('/servers/srv-1/summary?site=site-a')
    await expect(page.getByText('Power and provisioning')).toBeVisible()
    await expect(page.getByText('Hardware inventory')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Take action' })).toBeVisible()
    await page.getByRole('tab', { name: 'Network' }).click()
    await expect(page).toHaveURL('/servers/srv-1/network?site=site-a')
    await expect(page.getByText('eno1')).toBeVisible()
    await page.getByRole('tab', { name: 'PCI devices' }).click()
    await expect(page.getByText('03:00.0')).toBeVisible()
  })

  test('PatternFly wizard validates, preserves input, reviews, and redirects', async ({ page }) => {
    await page.goto('/clusters/deploy?site=site-a')
    const next = page.getByRole('button', { name: 'Next' })
    await expect(next).toBeDisabled()
    await page.getByLabel('Cluster name').fill('compute-k0s')
    await next.click()
    await page.getByLabel('API virtual IP').fill('192.168.40.200')
    await next.click()
    for (const server of ['gpu-node-01', 'gpu-node-02', 'gpu-node-03']) await page.getByLabel(`Role for ${server}`).selectOption('control-plane')
    await page.getByLabel('Role for gpu-node-04').selectOption('worker')
    await next.click()
    await expect(page.getByText('3 control-plane, 1 workers')).toBeVisible()
    await page.getByRole('button', { name: 'Back' }).click()
    await page.getByRole('button', { name: 'Back' }).click()
    await expect(page.getByLabel('API virtual IP')).toHaveValue('192.168.40.200')
    await next.click(); await next.click()
    await page.getByRole('button', { name: 'Deploy cluster' }).click()
    await expect(page).toHaveURL('/operations/op-running?site=site-a')
  })

  test('Headlamp language is type-aware and Slurm stays neutral', async ({ page }) => {
    await page.goto('/clusters/cluster-a?site=site-a')
    await expect(page.locator('.sw-stat-strip').getByText('Control-plane', { exact: true })).toBeVisible()
    await expect(page.getByText(/Kubernetes membership/)).toBeVisible()
    await page.goto('/clusters/cluster-slurm?site=site-a')
    await expect(page.getByText('Managers')).toBeVisible()
    await expect(page.getByText('Compute members')).toBeVisible()
    await expect(page.getByText(/Kubernetes/)).toHaveCount(0)
  })

  test('AWX stdout is first and supports search, navigation, copy, download, events, and retry', async ({ page }) => {
    await page.goto('/operations?site=site-a')
    await page.getByLabel('Filter by status').selectOption('failed')
    await expect(page).toHaveURL(/status=failed/)
    await page.goto('/operations/op-running?site=site-a')
    await expect(page.getByRole('tab', { name: 'Stdout' })).toHaveAttribute('aria-selected', 'true')
    await page.getByPlaceholder('Search stdout').fill('TASK')
    await expect(page.getByText('1 / 3')).toBeVisible()
    await page.getByLabel('Next').click()
    await expect(page.getByText('2 / 3')).toBeVisible()
    await page.getByLabel('Previous').click()
    await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: async (text: string) => localStorage.setItem('e2e.clipboard', text) },
    }))
    await page.getByLabel('Copy stdout').click()
    await expect.poll(() => page.evaluate(() => localStorage.getItem('e2e.clipboard'))).toContain('PLAY [Prepare hosts]')
    await expect(page.getByRole('heading', { name: /Stdout copied/ })).toBeVisible()
    const download = page.waitForEvent('download')
    await page.getByLabel('Download stdout').click()
    expect((await download).suggestedFilename()).toBe('swallow-operation-op-running.log')
    await page.getByRole('tab', { name: 'Events' }).click()
    await page.getByLabel('Filter events by status').selectOption('failed')
    await expect(page.getByRole('gridcell', { name: 'Start controller' })).toBeVisible()
    await page.goto('/operations/op-failed?site=site-a')
    await page.getByRole('button', { name: 'Retry' }).click()
    await expect(page).toHaveURL('/operations/op-retry?site=site-a')
  })

  test('Monitoring filters, acknowledges, and exposes the provider Grafana link', async ({ page }) => {
    await page.goto('/monitoring?site=site-a')
    const grafana = page.getByRole('link', { name: 'Open Grafana' })
    await expect(grafana).toHaveAttribute('href', 'https://grafana.example')
    await expect(grafana).toHaveAttribute('target', '_blank')
    await page.getByLabel('Filter alert severity').selectOption('critical')
    await expect(page).toHaveURL(/severity=critical/)
    await expect(page.getByText('NodeDown')).toBeVisible()
    await page.getByRole('row', { name: /NodeDown/ }).getByRole('button', { name: 'Acknowledge' }).click()
    await page.getByLabel('Comment').fill('Investigating host power')
    await page.getByRole('dialog').getByRole('button', { name: 'Acknowledge' }).click()
    await expect(page.getByText('Alert acknowledged')).toBeVisible()
  })

  test('Monitoring uses 200-ID batches, at most two workers, and keeps partial results', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    const batches: number[] = []
    let maxActive = 0
    await installApiFixtures(page, { fleetSize: 205, metricsDelayMs: 50, failMetricsBatchIndex: 1, onMetricsRequest: (ids) => batches.push(ids.length), onMetricsActive: (active) => { maxActive = Math.max(maxActive, active) } })
    await page.goto('/monitoring?site=site-a')
    await expect.poll(() => [...batches].sort((left, right) => right - left)).toEqual([200, 5])
    await expect(page.getByText('Some metric batches are unavailable')).toBeVisible()
    expect(maxActive).toBeLessThanOrEqual(2)
    expect(maxActive).toBe(2)
    await expect(page.getByRole('gridcell', { name: '30.0%' }).first()).toBeVisible()
    await expect(page.getByText('No data').first()).toBeVisible()
  })

  test('Monitoring surfaces acknowledge provider errors without losing alerts', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, { acknowledgeFails: true })
    await page.goto('/monitoring?site=site-a')
    await page.getByRole('button', { name: 'Acknowledge' }).first().click()
    await page.getByRole('dialog').getByRole('button', { name: 'Acknowledge' }).click()
    await expect(page.getByText('Alertmanager is unavailable')).toBeVisible()
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click()
    await expect(page.getByRole('row', { name: /NodeDown/ })).toBeVisible()
  })

  test('keyboard can traverse primary navigation', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: 'Servers' }).focus()
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL('/servers')
  })
})
