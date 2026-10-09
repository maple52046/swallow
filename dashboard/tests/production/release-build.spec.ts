import { expect, test } from 'playwright/test'
import { installApiFixtures } from '../e2e/fixtures'

// Runs against `vite build` output (playwright.production.config.ts). Release builds bind
// experimental feature settings that are always off, so every in-development feature is hidden and
// nothing a browser stored can bring one back.

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
  await installApiFixtures(page)

  await page.goto('/?site=site-a')
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Primary navigation' }).getByRole('link', { name: 'Monitoring' })).toHaveCount(0)
  await expect(page.locator('.sw-metric-card').filter({ hasText: 'Fleet health' })).toContainText('Not available in this release')

  await page.getByRole('button', { name: 'Account menu' }).click()
  await expect(page.getByRole('menuitem', { name: 'SSH keys' })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: 'Experimental features' })).toHaveCount(0)
  await page.keyboard.press('Escape')

  await page.goto('/monitoring?site=site-a')
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible()

  await page.goto('/provisioning/images?site=site-a')
  await expect(page.getByRole('heading', { name: 'OS images', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Upload image' })).toHaveCount(0)
  await expect(page.getByRole('tab', { name: 'Templates' })).toHaveCount(0)

  await page.goto('/provisioning/templates?site=site-a')
  await expect(page).toHaveURL('/provisioning/images?site=site-a')

  // Enrolling libvirt virtual machines is CLI- and API-only in a release (decision 055).
  await page.goto('/servers?site=site-a')
  await page.getByRole('button', { name: 'Add servers' }).first().click()
  const addServers = page.getByRole('dialog', { name: 'Add servers' })
  await expect(addServers.getByRole('button', { name: /Yes, keep its OS/ })).toBeVisible()
  await expect(addServers.getByRole('button', { name: /libvirt virtual machine/ })).toHaveCount(0)
})
