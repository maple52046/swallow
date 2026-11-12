import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// In-development features (alerts, OS image upload, Deployment Templates, virtual machines) are
// shown on the dev server and hidden in release builds. These journeys run on the dev server:
// switching every feature off through the dev-only setting reproduces the release view, because
// release builds bind the same gates to settings that are always off (tests/production covers the
// real build). Basic monitoring is released and behind no switch.

const RELEASE_VIEW = { alerts: false, osImageUpload: false, deploymentTemplates: false, virtualMachines: false }

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
  const alertsCard = page.locator('.sw-metric-card').filter({ hasText: 'Alerts' })
  await expect(alertsCard).toContainText('1 critical')

  await page.getByRole('button', { name: 'Account menu' }).click()
  await page.getByRole('menuitem', { name: 'Experimental features' }).click()
  const dialog = page.getByRole('dialog', { name: 'Experimental features' })
  for (const label of ['Alerts', 'OS image upload', 'Deployment Templates', 'Virtual machines']) {
    await expect(dialog.getByLabel(label, { exact: true })).toBeChecked()
  }
  for (const released of ['Monitoring', 'Validation']) {
    await expect(dialog.getByLabel(released, { exact: true })).toHaveCount(0)
  }

  await dialog.getByText('Alerts', { exact: true }).click()
  await expect(dialog.getByLabel('Alerts', { exact: true })).not.toBeChecked()
  await dialog.getByRole('button', { name: 'Done' }).click()
  // The console re-renders as soon as the switch flips, without a reload. (The page behind an
  // open modal is hidden from assistive technology, so it is checked after closing.)
  await expect(alertsCard).toContainText('Not available in this release')
  // Basic monitoring is released: switching alerts off leaves it in place.
  await expect(nav.getByRole('link', { name: 'Monitoring' })).toBeVisible()
  await expect(page.locator('.sw-metric-card').filter({ hasText: 'Fleet health' })).toContainText('3 / 5')

  // The choice is remembered per browser.
  await page.reload()
  await expect(alertsCard).toContainText('Not available in this release')

  await page.getByRole('button', { name: 'Account menu' }).click()
  await page.getByRole('menuitem', { name: 'Experimental features' }).click()
  await dialog.getByRole('button', { name: 'Reset to defaults' }).click()
  await expect(dialog.getByLabel('Alerts', { exact: true })).toBeChecked()
  await dialog.getByRole('button', { name: 'Done' }).click()
  await expect(alertsCard).toContainText('1 critical')
})

test('A stored opt-out for a switch that is no longer listed hides nothing and is dropped', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.dev.experimentalFeatures', JSON.stringify({ monitoring: false }))
  })
  await installApiFixtures(page)
  await page.goto('/?site=site-a')
  await expect(page.getByRole('navigation', { name: 'Primary navigation' }).getByRole('link', { name: 'Monitoring' })).toBeVisible()
  await expect(page.locator('.sw-metric-card').filter({ hasText: 'Fleet health' })).toContainText('3 / 5')
  await expect(page.locator('.sw-metric-card').filter({ hasText: 'Alerts' })).toContainText('1 critical')

  await page.getByRole('button', { name: 'Account menu' }).click()
  await page.getByRole('menuitem', { name: 'Experimental features' }).click()
  const dialog = page.getByRole('dialog', { name: 'Experimental features' })
  await expect(dialog.getByLabel('Alerts', { exact: true })).toBeChecked()
  await dialog.getByText('Alerts', { exact: true }).click()
  await dialog.getByRole('button', { name: 'Done' }).click()
  await expect.poll(() => page.evaluate(() => localStorage.getItem('swallow.dev.experimentalFeatures'))).toBe('{"alerts":false}')
})

test.describe('with every in-development feature hidden (the release view)', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript((choices) => {
      localStorage.setItem('swallow.dev.experimentalFeatures', JSON.stringify(choices))
    }, RELEASE_VIEW)
  })

  test('basic monitoring is shown everywhere while alerts are gone and never requested', async ({ page }) => {
    const alertRequests: string[] = []
    page.on('request', (request) => {
      const path = new URL(request.url()).pathname
      if (path.startsWith('/api/v1/monitoring/alerts')) alertRequests.push(path)
    })
    await installApiFixtures(page)

    await page.goto('/?site=site-a')
    await expect(page.getByRole('navigation', { name: 'Primary navigation' }).getByRole('link', { name: 'Monitoring' })).toBeVisible()
    await expect(page.locator('.sw-metric-card').filter({ hasText: 'Fleet health' })).toContainText('3 / 5')
    await expect(page.locator('.sw-metric-card').filter({ hasText: 'Alerts' })).toContainText('Not available in this release')
    // Firing alerts no longer raise attention items; down and unknown health still do.
    await expect(page.getByText('gpu-node-04 stopped reporting')).toHaveCount(0)
    await expect(page.getByRole('link', { name: /1 server down/ })).toBeVisible()
    await expect(page.getByRole('link', { name: /1 server with unknown health/ })).toBeVisible()

    await page.goto('/monitoring?site=site-a')
    await expect(page.getByRole('heading', { name: 'Usage charts', exact: true })).toBeVisible()
    await expect(page.getByRole('table', { name: 'Server metrics' })).toBeVisible()
    await expect(page.locator('.sw-metric-card').filter({ hasText: 'Fleet health' })).toContainText('1 down')
    await expect(page.getByRole('heading', { name: 'Alerts', exact: true })).toHaveCount(0)
    await expect(page.getByRole('list', { name: 'Monitoring alerts' })).toHaveCount(0)
    await expect(page.locator('.sw-metric-card').filter({ hasText: /Critical alerts|Warning alerts/ })).toHaveCount(0)

    await page.goto('/servers/srv-1/monitoring?site=site-a')
    await expect(page).toHaveURL('/servers/srv-1/monitoring?site=site-a')
    await expect(page.getByRole('heading', { name: 'Current metrics' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Usage history' })).toBeVisible()

    // Health is a usable filter, so a bookmarked one keeps narrowing the list.
    await page.goto('/servers?site=site-a&health=down')
    await expect(page).toHaveURL('/servers?site=site-a&health=down')
    await expect(page.getByRole('region', { name: 'Health' })).toContainText('Down')
    await page.getByRole('button', { name: /Filters/ }).click()
    await expect(page.getByRole('combobox', { name: 'Filter Server health', exact: true })).toBeEnabled()

    expect(alertRequests).toEqual([])
  })

  test('image upload and Deployment Templates are not offered anywhere', async ({ page }) => {
    const templateRequests: string[] = []
    page.on('request', (request) => {
      const path = new URL(request.url()).pathname
      if (path.startsWith('/api/v1/provisioning/templates')) templateRequests.push(path)
    })
    await installApiFixtures(page, { readyServerCount: 1 })

    await page.goto('/provisioning/images?site=site-a')
    await expect(page.getByRole('heading', { name: 'OS Images', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Upload image' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: /Create template/ })).toHaveCount(0)
    // Verifying an existing custom image is not part of upload and stays available.
    const customImage = page.getByRole('row', { name: 'Ubuntu 24.04 ROCm catalog data', exact: true })
    await customImage.getByRole('button', { name: 'More actions for Ubuntu 24.04 ROCm' }).click()
    await expect(page.getByRole('menuitem', { name: 'Test deployment' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('tab', { name: 'OS images' })).toBeVisible()
    await expect(page.getByRole('tab', { name: 'Templates' })).toHaveCount(0)

    await page.goto('/provisioning/templates?site=site-a')
    await expect(page).toHaveURL('/provisioning/images?site=site-a')
    await expect(page.getByRole('tab', { name: 'Deploy OS', exact: true })).toHaveCount(0)

    await page.goto('/platforms/deploy/kubernetes?site=site-a')
    await expect(page.getByLabel('Platform name')).toBeVisible()

    expect(templateRequests).toEqual([])
  })

  test('Validation is released: its entry, pages, and sections stay with every feature off', async ({ page }) => {
    await installApiFixtures(page)

    await page.goto('/?site=site-a')
    await expect(page.getByRole('navigation', { name: 'Primary navigation' }).getByRole('link', { name: 'Validation' })).toBeVisible()

    await page.goto('/validation/runs/run-failed?site=site-a')
    await expect(page.getByRole('heading', { name: 'Host baseline', level: 1 })).toBeVisible()

    await page.goto('/servers/srv-2/summary?site=site-a')
    await expect(page.getByRole('heading', { name: 'Validation', exact: true })).toBeVisible()
    await page.goto('/platforms/platform-a/overview?site=site-a')
    await expect(page.getByRole('heading', { name: 'Validation', exact: true })).toBeVisible()
  })

  test('libvirt virtual machines are not offered and their Boot Media is not shown', async ({ page }) => {
    const hiddenRequests: string[] = []
    page.on('request', (request) => {
      const path = new URL(request.url()).pathname
      if (path.endsWith('/virtual-machines')) hiddenRequests.push(path)
    })
    await installApiFixtures(page, {
      powerConfigurations: { 'srv-2': { driver: 'virsh', address: 'qemu+ssh://ubuntu@192.168.40.21/system', powerId: 'gpu-node-02' } },
      libvirtBootMediaServerIds: ['srv-2'],
    })

    await page.goto('/servers?site=site-a')
    await page.getByRole('button', { name: 'Add servers' }).first().click()
    const dialog = page.getByRole('dialog', { name: 'Add servers' })
    await expect(dialog.getByRole('button', { name: /Yes, keep its OS/ })).toBeVisible()
    await expect(dialog.getByRole('button', { name: /libvirt virtual machine/ })).toHaveCount(0)

    await page.goto('/servers/srv-2/summary?site=site-a')
    const card = page.getByRole('region', { name: 'Power control' })
    await expect(card.getByText('virsh (libvirt) — virtual machine')).toBeVisible()
    await expect(card).toContainText('Boot Media is not available')
    await expect(page.getByRole('heading', { name: 'Boot media' })).toHaveCount(0)

    expect(hiddenRequests).toEqual([])
  })
})
