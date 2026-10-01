import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// In-development features (monitoring, OS image upload, Deployment Templates) are shown on the dev
// server and hidden in release builds. These journeys run on the dev server: switching every feature
// off through the dev-only setting reproduces the release view, because release builds bind the same
// gates to settings that are always off (tests/production covers the real build).

const RELEASE_VIEW = { monitoring: false, osImageUpload: false, deploymentTemplates: false }

async function chooseSelectOption(page: Page, fieldLabel: string, optionLabel: string) {
  await page.getByRole('combobox', { name: fieldLabel, exact: true }).click()
  await page.getByRole('option', { name: optionLabel, exact: true }).click()
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

test('Experimental features are on in development and switch live from the account menu', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/?site=site-a')
  const nav = page.getByRole('navigation', { name: 'Primary navigation' })
  await expect(nav.getByRole('link', { name: 'Monitoring' })).toBeVisible()

  await page.getByRole('button', { name: 'Account menu' }).click()
  await page.getByRole('menuitem', { name: 'Experimental features' }).click()
  const dialog = page.getByRole('dialog', { name: 'Experimental features' })
  for (const label of ['Monitoring', 'OS image upload', 'Deployment Templates']) {
    await expect(dialog.getByLabel(label, { exact: true })).toBeChecked()
  }

  await dialog.getByText('Monitoring', { exact: true }).click()
  await expect(dialog.getByLabel('Monitoring', { exact: true })).not.toBeChecked()
  await dialog.getByRole('button', { name: 'Done' }).click()
  // The console re-renders as soon as the switch flips, without a reload. (The page behind an
  // open modal is hidden from assistive technology, so it is checked after closing.)
  await expect(nav.getByRole('link', { name: 'Monitoring' })).toHaveCount(0)
  await expect(page.locator('.sw-metric-card').filter({ hasText: 'Fleet health' })).toContainText('Not available in this release')

  // The choice is remembered per browser.
  await page.reload()
  await expect(nav.getByRole('link', { name: 'Monitoring' })).toHaveCount(0)

  await page.getByRole('button', { name: 'Account menu' }).click()
  await page.getByRole('menuitem', { name: 'Experimental features' }).click()
  await dialog.getByRole('button', { name: 'Reset to defaults' }).click()
  await expect(dialog.getByLabel('Monitoring', { exact: true })).toBeChecked()
  await dialog.getByRole('button', { name: 'Done' }).click()
  await expect(nav.getByRole('link', { name: 'Monitoring' })).toBeVisible()
})

test.describe('with every in-development feature hidden (the release view)', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript((choices) => {
      localStorage.setItem('swallow.dev.experimentalFeatures', JSON.stringify(choices))
    }, RELEASE_VIEW)
  })

  test('monitoring surfaces are gone and shared health keeps its place with a note', async ({ page }) => {
    const hiddenRequests: string[] = []
    page.on('request', (request) => {
      const path = new URL(request.url()).pathname
      if (path.startsWith('/api/v1/monitoring')) hiddenRequests.push(path)
    })
    await installApiFixtures(page)

    await page.goto('/?site=site-a')
    await expect(page.getByRole('navigation', { name: 'Primary navigation' }).getByRole('link', { name: 'Monitoring' })).toHaveCount(0)
    for (const card of ['Fleet health', 'Alerts']) {
      await expect(page.locator('.sw-metric-card').filter({ hasText: card })).toContainText('Not available in this release')
    }
    // Firing alerts and down Servers are monitoring facts; they no longer raise attention items.
    await expect(page.getByText('gpu-node-04 stopped reporting')).toHaveCount(0)

    await page.goto('/monitoring?site=site-a')
    await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible()

    await page.goto('/servers/srv-1/monitoring?site=site-a')
    await expect(page).toHaveURL('/servers/srv-1/summary?site=site-a')
    await expect(page.getByRole('tab', { name: 'Summary' })).toBeVisible()
    await expect(page.getByRole('tab', { name: 'Monitoring' })).toHaveCount(0)
    await expect(page.getByText('unavailable: health is not available in this release').first()).toBeAttached()

    // A bookmarked health filter cannot narrow the list through a control nobody can use.
    await page.goto('/servers?site=site-a&health=down')
    await expect(page).toHaveURL('/servers?site=site-a')
    await expect(page.getByRole('region', { name: 'Health' })).toContainText('Not available in this release')
    await page.getByRole('button', { name: /Filters/ }).click()
    await expect(page.getByRole('combobox', { name: 'Filter Server health', exact: true })).toBeDisabled()

    expect(hiddenRequests).toEqual([])
  })

  test('image upload and Deployment Templates are not offered anywhere', async ({ page }) => {
    const templateRequests: string[] = []
    page.on('request', (request) => {
      const path = new URL(request.url()).pathname
      if (path.startsWith('/api/v1/provisioning/templates')) templateRequests.push(path)
    })
    await installApiFixtures(page, { readyServerCount: 1 })

    await page.goto('/provisioning/images?site=site-a')
    await expect(page.getByRole('heading', { name: 'OS images', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Upload image' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: /Create template/ })).toHaveCount(0)
    // Verifying an existing custom image is not part of upload and stays available.
    await expect(page.getByRole('button', { name: 'Verify Ubuntu 24.04 ROCm' }).first()).toBeVisible()
    await expect(page.getByRole('tab', { name: 'OS images' })).toBeVisible()
    await expect(page.getByRole('tab', { name: 'Templates' })).toHaveCount(0)

    await page.goto('/provisioning/templates?site=site-a')
    await expect(page).toHaveURL('/provisioning/deploy?site=site-a')

    await page.goto('/provisioning/deploy?site=site-a&serverId=srv-1&templateId=template-1')
    await page.getByRole('button', { name: 'Next' }).click()
    await expect(page.getByRole('heading', { name: 'Operating system configuration' })).toBeVisible()
    await expect(page.getByLabel('Configuration source')).toHaveCount(0)
    await chooseSelectOption(page, 'OS image', 'Ubuntu 22.04 LTS (amd64)')
    await page.getByRole('button', { name: 'Next' }).click()
    await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await expect(page.getByText('Save as a deployment template')).toHaveCount(0)

    await page.goto('/platforms/deploy?site=site-a')
    await expect(page.getByLabel('Platform name')).toBeVisible()

    expect(templateRequests).toEqual([])
  })
})
