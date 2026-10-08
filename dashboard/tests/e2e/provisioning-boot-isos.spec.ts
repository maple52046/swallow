import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Boot ISOs (decision 049): optional iPXE setup for Servers whose external DHCP path cannot use
// provisioner-managed network boot.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

function bootISOCard(page: Page, name: string) {
  return page.getByRole('listitem', { name })
}

test('cards keep serving, usage, links, technical facts, and guarded actions distinct', async ({ page }) => {
  const writes: string[] = []
  await installApiFixtures(page, {
    bootMediaSetting: { enabled: true, isoId: 'iso-taipei', updatedAt: '2026-10-04T00:00:00Z', lastAppliedAt: '2026-10-04T00:00:00Z', lastAppliedBy: 'preflight', bootOverride: 'Continuous', lastErrorAt: null },
    onBootISORequest: (method, path) => writes.push(`${method} ${path}`),
  })
  await page.goto('/provisioning/boot-isos')

  await expect(page.getByRole('heading', { name: 'External-network boot' })).toBeVisible()
  await expect(page.getByLabel('External-network boot path')).toContainText(
    'Server BMCSwallow Boot ISOSite DHCPMAAS rack',
  )
  const taipei = bootISOCard(page, 'taipei-rack')
  const edge = bootISOCard(page, 'edge-rack')
  await expect(taipei).toContainText('Served')
  await expect(taipei).toContainText('Used by 1 Server')
  await expect(edge).toContainText('Not in use')
  await expect(taipei).toContainText('MAAS Taipei')
  await expect(taipei).toContainText('http://10.0.0.2:5248/ipxe.cfg')
  await expect(taipei.getByRole('link', { name: 'MAAS Taipei' })).toHaveAttribute(
    'href',
    '/infrastructure/integrations?site=site-a#integration-maas-a',
  )
  await expect(taipei.getByRole('link', { name: 'Taipei Lab' })).toHaveAttribute(
    'href',
    '/infrastructure/sites?site=site-a#site-site-a',
  )
  await expect(taipei.getByRole('link', { name: 'Download taipei-rack' })).toHaveAttribute(
    'href',
    'http://192.0.2.1/boot-media/ipxe/iso-taipei/swallow-ipxe.iso',
  )

  const disclosure = taipei.getByText('Technical details', { exact: true })
  await disclosure.focus()
  await page.keyboard.press('Enter')
  await expect(taipei).toContainText('sha256-of-iso-taipei')
  await expect(taipei).toContainText('2.3 MiB')

  const taipeiMore = taipei.getByRole('button', { name: 'More actions for taipei-rack' })
  await taipeiMore.focus()
  await page.keyboard.press('Enter')
  await page.getByRole('menuitem', { name: 'View iPXE script' }).click()
  const script = page.getByRole('dialog', { name: 'iPXE script of taipei-rack' })
  await expect(script).toContainText('set maas_rack 10.0.0.2')
  await expect(script).toContainText('v2.0.0 (12798ec)')
  await script.getByRole('button', { name: 'Close' }).first().click()

  await taipeiMore.click()
  const blockedDelete = page.getByRole('menuitem', { name: /Delete Boot ISO/ })
  await expect(blockedDelete).toBeDisabled()
  await expect(blockedDelete).toContainText('Disable Boot Media')
  await page.keyboard.press('Escape')

  await edge.getByRole('button', { name: 'More actions for edge-rack' }).click()
  await page.getByRole('menuitem', { name: 'Delete Boot ISO', exact: true }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Delete Boot ISO' })
  await expect(confirm).toContainText('its URL stops working')
  await confirm.getByRole('button', { name: 'Delete edge-rack' }).click()

  await expect(page.getByText('Boot ISO edge-rack deleted')).toBeVisible()
  await expect(edge).toHaveCount(0)
  expect(writes).toEqual(['DELETE /api/v1/provisioning/boot-isos/iso-edge'])
})

test('single provisioner is summarized and its endpoint suggests the editable rack address', async ({ page }) => {
  const builds: Array<Record<string, unknown> | null> = []
  await installApiFixtures(page, {
    bootISOs: [],
    provisionerEndpoints: { 'maas-a': 'https://rack.example:5240/MAAS/api/2.0/' },
    onBootISORequest: (_method, _path, body) => builds.push(body),
  })
  await page.goto('/provisioning/boot-isos?site=site-a')
  await expect(page.getByText('No Boot ISO configured')).toBeVisible()

  await page.getByRole('button', { name: 'Build Boot ISO' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Build Boot ISO' })
  await expect(dialog.getByRole('combobox')).toHaveCount(0)
  await expect(dialog.getByLabel('Provisioner Integration')).toContainText('MAAS Taipei')
  await expect(dialog.getByLabel('Provisioner Integration')).toContainText('rack.example:5240')

  const rack = dialog.getByRole('textbox', { name: /^MAAS rack address/ })
  await expect(rack).toHaveValue('rack.example')
  await expect(dialog).toContainText('http://rack.example:5248/ipxe.cfg')

  await dialog.getByText('Advanced settings', { exact: true }).click()
  const name = dialog.getByRole('textbox', { name: /^Boot ISO name/ })
  await expect(name).toHaveValue('maas-taipei-ipxe')

  await rack.fill('10.0.0.9:5250')
  await expect(dialog).toContainText('http://10.0.0.9:5250/ipxe.cfg')
  await name.fill('taipei-rack')
  await dialog.getByRole('button', { name: 'Build Boot ISO', exact: true }).click()

  await expect(dialog).toHaveCount(0)
  await expect(page.getByText('Boot ISO taipei-rack built')).toBeVisible()
  expect(builds).toEqual([{ integrationId: 'maas-a', name: 'taipei-rack', rackAddress: '10.0.0.9:5250' }])
  await expect(bootISOCard(page, 'taipei-rack')).toContainText('http://10.0.0.9:5250/ipxe.cfg')
})

test('a failed build keeps the dialog open with the packaging error', async ({ page }) => {
  await installApiFixtures(page, {
    bootISOBuildError: { status: 500, code: 'internal_error', message: 'boot ISO build failed: genfsimg: xorriso : FAILURE : no space left on device' },
  })
  await page.goto('/provisioning/boot-isos?site=site-a')
  await page.getByRole('button', { name: 'Build Boot ISO' }).click()
  const dialog = page.getByRole('dialog', { name: 'Build Boot ISO' })
  await dialog.getByRole('button', { name: 'Build Boot ISO', exact: true }).click()

  await expect(dialog).toContainText('The Boot ISO was not built')
  await expect(dialog).toContainText('no space left on device')
})

test('an installation that cannot build says why and keeps existing ISOs', async ({ page }) => {
  await installApiFixtures(page, {
    bootISOBuilderUnavailable: 'set api.bootMedia.baseURL (SWALLOW_API_BOOT_MEDIA_BASE_URL) to the HTTP address BMCs use to reach swallow',
  })
  await page.goto('/provisioning/boot-isos?site=site-a')
  await expect(page.getByText('Boot ISO builder unavailable')).toBeVisible()
  await expect(page.getByText(/SWALLOW_API_BOOT_MEDIA_BASE_URL/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Build Boot ISO' })).toBeDisabled()
  await expect(bootISOCard(page, 'taipei-rack')).toBeVisible()
})

test('switching provisioners updates untouched suggestions and preserves edited values', async ({ page }) => {
  await installApiFixtures(page, { bootISOs: [], multipleProvisioners: true })
  await page.goto('/provisioning/boot-isos?site=site-a')
  await page.getByRole('button', { name: 'Build Boot ISO' }).first().click()

  const dialog = page.getByRole('dialog', { name: 'Build Boot ISO' })
  const integration = dialog.getByRole('combobox', { name: 'Provisioner Integration' })
  const rack = dialog.getByRole('textbox', { name: /^MAAS rack address/ })
  await integration.click()
  await page.getByRole('option', { name: 'MAAS Taipei', exact: true }).click()
  await expect(rack).toHaveValue('maas.example')

  await dialog.getByText('Advanced settings', { exact: true }).click()
  const name = dialog.getByRole('textbox', { name: /^Boot ISO name/ })
  await expect(name).toHaveValue('maas-taipei-ipxe')

  await integration.click()
  await page.getByRole('option', { name: 'MAAS Taipei Secondary', exact: true }).click()
  await expect(rack).toHaveValue('maas-edge.example')
  await expect(name).toHaveValue('maas-taipei-secondary-ipxe')

  await rack.fill('rack.external.example')
  await name.fill('external-dhcp')
  await integration.click()
  await page.getByRole('option', { name: 'MAAS Taipei', exact: true }).click()
  await expect(rack).toHaveValue('rack.external.example')
  await expect(name).toHaveValue('external-dhcp')
})

test('an invalid Integration endpoint leaves the rack address for the operator', async ({ page }) => {
  await installApiFixtures(page, {
    bootISOs: [],
    provisionerEndpoints: { 'maas-a': 'not a valid endpoint' },
  })
  await page.goto('/provisioning/boot-isos?site=site-a')
  await page.getByRole('button', { name: 'Build Boot ISO' }).first().click()

  const dialog = page.getByRole('dialog', { name: 'Build Boot ISO' })
  const rack = dialog.getByRole('textbox', { name: /^MAAS rack address/ })
  await expect(rack).toHaveValue('')
  await expect(dialog.getByRole('button', { name: 'Build Boot ISO', exact: true })).toBeDisabled()
  await rack.fill('10.23.4.5')
  await expect(dialog).toContainText('http://10.23.4.5:5248/ipxe.cfg')
  await expect(dialog.getByRole('button', { name: 'Build Boot ISO', exact: true })).toBeEnabled()
})

test('a Site without a provisioner treats the empty setup as optional and links to Integrations', async ({ page }) => {
  await installApiFixtures(page, { bootISOs: [], noProvisioners: true })
  await page.goto('/provisioning/boot-isos?site=site-a')

  await expect(page.getByText('No Boot ISO configured')).toBeVisible()
  await expect(page.getByText(/Most Sites do not need a Boot ISO/)).toBeVisible()
  await expect(page.getByRole('link', { name: 'Manage integrations' })).toHaveAttribute(
    'href',
    '/infrastructure/integrations?site=site-a',
  )
  await expect(page.getByRole('button', { name: 'Build Boot ISO' })).toBeDisabled()
})

test('five cards, all-sites identity, not-served media, and long technical values remain responsive', async ({ page }) => {
  await installApiFixtures(page, {
    bootISOs: [
      { id: 'iso-1', name: 'taipei-external-network-bootstrap-with-a-long-name', integrationId: 'maas-a', rackAddress: 'rack-controller-with-a-very-long-hostname.taipei.example' },
      { id: 'iso-2', name: 'taipei-spare', integrationId: 'maas-a', rackAddress: '10.0.0.3' },
      { id: 'iso-3', name: 'edge-primary', integrationId: 'maas-b', rackAddress: '10.9.0.2' },
      { id: 'iso-4', name: 'edge-spare', integrationId: 'maas-b', rackAddress: '10.9.0.3' },
      { id: 'iso-5', name: 'edge-not-served', integrationId: 'maas-b', rackAddress: '10.9.0.4', url: null },
    ],
  })
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('/provisioning/boot-isos')

  const list = page.getByRole('list', { name: 'Boot ISOs' })
  await expect(list.getByRole('listitem')).toHaveCount(5)
  await expect(bootISOCard(page, 'edge-primary').getByRole('link', { name: 'Hsinchu Edge' })).toBeVisible()
  const notServed = bootISOCard(page, 'edge-not-served')
  await expect(notServed).toContainText('Not served')
  await expect(notServed.getByRole('button', { name: 'Download edge-not-served unavailable' })).toBeDisabled()

  const longCard = bootISOCard(page, 'taipei-external-network-bootstrap-with-a-long-name')
  await longCard.getByText('Technical details', { exact: true }).click()
  await expect(longCard).toContainText('rack-controller-with-a-very-long-hostname.taipei.example')
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth))
    .toBe(true)

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(longCard).toBeVisible()
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth))
    .toBe(true)
})

test('a late response from the previous Site cannot replace the new Site cards', async ({ page }) => {
  await installApiFixtures(page)
  let releaseSiteA = () => {}
  const siteAGate = new Promise<void>((resolve) => {
    releaseSiteA = resolve
  })
  await page.route('**/api/v1/provisioning/boot-isos**', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() === 'GET' && url.searchParams.get('siteId') === 'site-a') {
      await siteAGate
    }
    return route.fallback()
  })

  await page.goto('/provisioning/boot-isos?site=site-a')
  await expect(page.getByRole('heading', { name: 'Boot ISOs', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Site scope: Taipei Lab' }).click()
  await page.getByRole('menuitem', { name: 'Hsinchu Edge' }).click()
  await expect(bootISOCard(page, 'edge-rack')).toBeVisible()

  const lateResponse = page.waitForResponse((response) => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/provisioning/boot-isos' && url.searchParams.get('siteId') === 'site-a'
  })
  releaseSiteA()
  await lateResponse
  await expect(bootISOCard(page, 'edge-rack')).toBeVisible()
  await expect(bootISOCard(page, 'taipei-rack')).toHaveCount(0)
})

test('manual refresh failure keeps last-good cards and shows a non-blocking warning', async ({ page }) => {
  await installApiFixtures(page)
  let failRefresh = false
  await page.route('**/api/v1/provisioning/boot-isos**', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    if (!failRefresh) return route.fallback()
    return route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({ error: { code: 'unavailable', message: 'boot media store unavailable' } }),
    })
  })
  await page.goto('/provisioning/boot-isos?site=site-a')
  await expect(bootISOCard(page, 'taipei-rack')).toBeVisible()

  const refresh = page.getByRole('button', { name: 'Refresh' })
  await expect(refresh).toBeEnabled()
  failRefresh = true
  await refresh.click()
  await expect(page.getByText('Could not refresh Boot ISOs')).toBeVisible()
  await expect(page.getByText(/boot media store unavailable/)).toBeVisible()
  await expect(bootISOCard(page, 'taipei-rack')).toBeVisible()
})
