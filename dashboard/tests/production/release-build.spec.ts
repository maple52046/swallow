import { expect, test } from 'playwright/test'
import { installApiFixtures } from '../e2e/fixtures'

// Runs against `vite build` output (playwright.production.config.ts). Release builds bind
// experimental feature settings that are always off, so every in-development feature is hidden and
// nothing a browser stored can bring one back. Basic monitoring is released and shown.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
    // A development opt-out list left in the browser must not matter either way.
    localStorage.setItem('swallow.dev.experimentalFeatures', '{}')
  })
})

test('Release builds hide in-development features and offer no switch', async ({ page }) => {
  const alertRequests: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path.startsWith('/api/v1/monitoring/alerts')) alertRequests.push(path)
  })
  await installApiFixtures(page)

  await page.goto('/?site=site-a')
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Primary navigation' }).getByRole('link', { name: 'Monitoring' })).toBeVisible()
  await expect(page.locator('.sw-metric-card').filter({ hasText: 'Fleet health' })).toContainText('3 / 5')
  await expect(page.locator('.sw-metric-card').filter({ hasText: 'Alerts' })).toContainText('Not available in this release')
  await expect(page.getByText('gpu-node-04 stopped reporting')).toHaveCount(0)

  await page.getByRole('button', { name: 'Account menu' }).click()
  await expect(page.getByRole('menuitem', { name: 'SSH keys' })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: 'Experimental features' })).toHaveCount(0)
  await page.keyboard.press('Escape')

  // The Monitoring console shows basic monitoring; its alert list is in development.
  await page.goto('/monitoring?site=site-a')
  await expect(page.getByRole('heading', { name: 'Usage charts', exact: true })).toBeVisible()
  await expect(page.getByRole('table', { name: 'Server metrics' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Alerts', exact: true })).toHaveCount(0)

  await page.goto('/servers/srv-1/monitoring?site=site-a')
  await expect(page.getByRole('heading', { name: 'Current metrics' })).toBeVisible()

  // Monitoring Links are Platform data, and the usage-chart shortcut leads to the released console.
  await page.goto('/platforms/platform-slurm/monitoring?site=site-a')
  await expect(page.getByRole('link', { name: 'Open Platform Grafana' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Open monitoring' })).toBeVisible()

  await page.goto('/provisioning/images?site=site-a')
  await expect(page.getByRole('heading', { name: 'OS Images', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Upload image' })).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'Deployment Templates' })).toHaveCount(0)

  await page.goto('/provisioning/templates?site=site-a')
  await expect(page).toHaveURL('/provisioning/images?site=site-a')

  // Enrolling libvirt virtual machines is CLI- and API-only in a release (decision 055).
  await page.goto('/servers?site=site-a')
  await page.getByRole('button', { name: 'Add servers' }).first().click()
  const addServers = page.getByRole('dialog', { name: 'Add servers' })
  await expect(addServers.getByRole('button', { name: /Yes, keep its OS/ })).toBeVisible()
  await expect(addServers.getByRole('button', { name: /libvirt virtual machine/ })).toHaveCount(0)

  // Validation is released: its pages and its entry are in every build.
  await page.goto('/validation/runs?site=site-a')
  await expect(page.getByRole('table', { name: 'Validation Runs' })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Primary navigation' }).getByRole('link', { name: 'Validation' })).toBeVisible()

  expect(alertRequests).toEqual([])
})
