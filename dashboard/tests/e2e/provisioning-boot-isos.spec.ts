import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Boot ISOs (decision 049): the Provisioning tab where swallow builds an iPXE boot ISO per
// provisioner from its fixed template, shows the rendered script, and deletes unused ISOs.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

test('Boot ISOs are listed per provisioner with their script, and only unused ones can be deleted', async ({ page }) => {
  const writes: string[] = []
  await installApiFixtures(page, {
    bootMediaSetting: { enabled: true, isoId: 'iso-taipei', updatedAt: '2026-10-04T00:00:00Z', lastAppliedAt: '2026-10-04T00:00:00Z', lastAppliedBy: 'preflight', bootOverride: 'Continuous', lastErrorAt: null },
    onBootISORequest: (method, path) => writes.push(`${method} ${path}`),
  })
  await page.goto('/provisioning/boot-isos')

  const table = page.getByRole('table', { name: 'Boot ISOs' })
  const taipei = table.getByRole('row', { name: /taipei-rack/ })
  const edge = table.getByRole('row', { name: /edge-rack/ })
  await expect(taipei).toContainText('MAAS Taipei')
  await expect(taipei).toContainText('http://10.0.0.2:5248/ipxe.cfg')
  await expect(taipei).toContainText('1 Server')
  await expect(edge).toContainText('Not used')
  await expect(taipei.getByRole('link', { name: 'Download taipei-rack' })).toHaveAttribute('href', 'http://192.0.2.1/boot-media/ipxe/iso-taipei/swallow-ipxe.iso')

  await taipei.getByRole('button', { name: 'View script' }).click()
  const script = page.getByRole('dialog', { name: 'iPXE script of taipei-rack' })
  await expect(script).toContainText('set maas_rack 10.0.0.2')
  await expect(script).toContainText('v2.0.0 (12798ec)')
  await script.getByRole('button', { name: 'Close' }).first().click()

  // In use by srv-1's Boot Media: the API would refuse, so the action is unavailable up front.
  await expect(taipei.getByRole('button', { name: /Delete taipei-rack/ })).toBeDisabled()
  await edge.getByRole('button', { name: 'Delete edge-rack' }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Delete Boot ISO' })
  await expect(confirm).toContainText('its URL stops working')
  await confirm.getByRole('button', { name: 'Delete edge-rack' }).click()

  await expect(page.getByText('Boot ISO edge-rack deleted')).toBeVisible()
  await expect(table.getByRole('row', { name: /edge-rack/ })).toHaveCount(0)
  expect(writes).toEqual(['DELETE /api/v1/provisioning/boot-isos/iso-edge'])
})

test('Build ISO pre-fills the rack from the provisioner endpoint and previews the chain URL', async ({ page }) => {
  const builds: Array<Record<string, unknown> | null> = []
  await installApiFixtures(page, { bootISOs: [], onBootISORequest: (_method, _path, body) => builds.push(body) })
  await page.goto('/provisioning/boot-isos?site=site-a')
  await expect(page.getByText('No Boot ISOs')).toBeVisible()

  await page.getByRole('button', { name: 'Build ISO' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Build Boot ISO' })
  await expect(dialog.getByRole('combobox', { name: 'Provisioner', exact: true })).toContainText('MAAS Taipei')

  const name = dialog.getByRole('textbox', { name: /^Name/ })
  const rack = dialog.getByRole('textbox', { name: /^MAAS rack address/ })
  await expect(name).toHaveValue('maas-taipei-ipxe')
  await expect(rack).toHaveValue('maas.example')
  await expect(dialog).toContainText('chains to http://maas.example:5248/ipxe.cfg')

  await rack.fill('10.0.0.9:5250')
  await expect(dialog).toContainText('chains to http://10.0.0.9:5250/ipxe.cfg')
  await name.fill('taipei-rack')
  await dialog.getByRole('button', { name: 'Build ISO', exact: true }).click()

  await expect(dialog).toHaveCount(0)
  await expect(page.getByText('Boot ISO taipei-rack built')).toBeVisible()
  expect(builds).toEqual([{ integrationId: 'maas-a', name: 'taipei-rack', rackAddress: '10.0.0.9:5250' }])
  await expect(page.getByRole('table', { name: 'Boot ISOs' }).getByRole('row', { name: /taipei-rack/ })).toContainText('http://10.0.0.9:5250/ipxe.cfg')
})

test('a failed build keeps the dialog open with the packaging error', async ({ page }) => {
  await installApiFixtures(page, {
    bootISOBuildError: { status: 500, code: 'internal_error', message: 'boot ISO build failed: genfsimg: xorriso : FAILURE : no space left on device' },
  })
  await page.goto('/provisioning/boot-isos?site=site-a')
  await page.getByRole('button', { name: 'Build ISO' }).click()
  const dialog = page.getByRole('dialog', { name: 'Build Boot ISO' })
  await dialog.getByRole('combobox', { name: 'Provisioner', exact: true }).click()
  await page.getByRole('option', { name: 'MAAS Taipei', exact: true }).click()
  await dialog.getByRole('button', { name: 'Build ISO', exact: true }).click()

  await expect(dialog).toContainText('The Boot ISO was not built')
  await expect(dialog).toContainText('no space left on device')
})

test('an installation that cannot build says why and keeps existing ISOs', async ({ page }) => {
  await installApiFixtures(page, {
    bootISOBuilderUnavailable: 'set api.bootMedia.baseURL (SWALLOW_API_BOOT_MEDIA_BASE_URL) to the HTTP address BMCs use to reach swallow',
  })
  await page.goto('/provisioning/boot-isos?site=site-a')
  await expect(page.getByText('Boot ISOs cannot be built here')).toBeVisible()
  await expect(page.getByText(/SWALLOW_API_BOOT_MEDIA_BASE_URL/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Build ISO' })).toBeDisabled()
  await expect(page.getByRole('table', { name: 'Boot ISOs' }).getByRole('row', { name: /taipei-rack/ })).toBeVisible()
})
