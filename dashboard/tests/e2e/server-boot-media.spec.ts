import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Boot Media (decision 047): shown inside the Summary's Management controller card, enabled there
// through a preflight the API runs against the BMC, and disabled with a best-effort BMC reset.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

test('Boot Media is enabled through the preflight and reported with its persistence', async ({ page }) => {
  const writes: Array<{ method: string; path: string; body: Record<string, unknown> | null }> = []
  await installApiFixtures(page, { onBootMediaRequest: (method, path, body) => writes.push({ method, path, body }) })
  await page.goto('/servers/srv-1/summary?site=site-a')

  const management = page.getByRole('region', { name: 'Management controller' })
  const bootMedia = management.getByRole('region', { name: 'Boot media' })
  await expect(bootMedia).toContainText('Redfish Boot Media supported')
  await expect(bootMedia).toContainText('AMI · firmware 13.06.10')
  await expect(bootMedia).toContainText('Disabled')
  await expect(bootMedia).toContainText('http://192.0.2.1/boot-media/ipxe/swallow-ipxe.iso')

  await bootMedia.getByRole('button', { name: 'Enable Boot Media' }).click()
  const dialog = page.getByRole('dialog', { name: 'Enable Boot Media' })
  await expect(dialog).toContainText('Mount http://192.0.2.1/boot-media/ipxe/swallow-ipxe.iso as a virtual CD.')
  await dialog.getByRole('button', { name: 'Enable', exact: true }).click()

  await expect(dialog).toHaveCount(0)
  await expect(page.getByText('Boot Media enabled on gpu-node-01')).toBeVisible()
  expect(writes).toEqual([{ method: 'PUT', path: '/api/v1/servers/srv-1/boot-media', body: { enabled: true } }])
  await expect(bootMedia).toContainText('Enabled')
  await expect(bootMedia).toContainText('Boots the ISO first on every boot')
  await expect(bootMedia).toContainText('when it was enabled')
})

test('a refused preflight keeps the dialog open with the BMC reason and saves nothing', async ({ page }) => {
  await installApiFixtures(page, {
    bootMediaError: {
      status: 409,
      code: 'conflict',
      message: 'The BMC of Server "gpu-node-01" refused the Boot Media. The BMC rejected the ISO URL\'s format; many BMCs accept only http:// on port 80.',
    },
  })
  await page.goto('/servers/srv-1/summary?site=site-a')

  const bootMedia = page.getByRole('region', { name: 'Boot media' })
  await bootMedia.getByRole('button', { name: 'Enable Boot Media' }).click()
  const dialog = page.getByRole('dialog', { name: 'Enable Boot Media' })
  await dialog.getByRole('button', { name: 'Enable', exact: true }).click()

  await expect(dialog).toContainText('Boot Media was not enabled')
  await expect(dialog).toContainText('many BMCs accept only http:// on port 80')
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(bootMedia).toContainText('Disabled')
})

test('disabling still saves when the BMC cannot be reset, and says so', async ({ page }) => {
  const writes: Array<Record<string, unknown> | null> = []
  await installApiFixtures(page, {
    bootMediaRevertError: 'The BMC of Server "gpu-node-01" could not be reached over Redfish.',
    onBootMediaRequest: (_method, _path, body) => writes.push(body),
  })
  await page.goto('/servers/srv-1/summary?site=site-a')
  const bootMedia = page.getByRole('region', { name: 'Boot media' })
  await bootMedia.getByRole('button', { name: 'Enable Boot Media' }).click()
  await page.getByRole('dialog', { name: 'Enable Boot Media' }).getByRole('button', { name: 'Enable', exact: true }).click()
  await expect(bootMedia).toContainText('Enabled')

  await bootMedia.getByRole('button', { name: 'Disable' }).click()
  await page.getByRole('dialog', { name: 'Disable Boot Media' }).getByRole('button', { name: 'Disable', exact: true }).click()

  await expect(page.getByText(/OS deployments no longer apply it, but the BMC was not reset/)).toBeVisible()
  await expect(bootMedia).toContainText('Disabled')
  expect(writes).toEqual([{ enabled: true }, { enabled: false }])
})

test('the BMC is read live only on request, and Redfish can be re-detected', async ({ page }) => {
  const writes: string[] = []
  await installApiFixtures(page, { onBootMediaRequest: (method, path) => writes.push(`${method} ${path}`) })
  await page.goto('/servers/srv-1/summary?site=site-a')
  const bootMedia = page.getByRole('region', { name: 'Boot media' })
  await expect(bootMedia).not.toContainText('BMC now')

  await bootMedia.getByRole('button', { name: 'Check BMC' }).click()
  await expect(bootMedia).toContainText('BMC now')
  await expect(bootMedia).toContainText('Next boot does not start from the ISO')
  await expect(bootMedia).toContainText('ISO not mounted')

  await bootMedia.getByRole('button', { name: 'Re-detect Redfish' }).click()
  await expect(page.getByText('Redfish Boot Media supported').last()).toBeVisible()
  expect(writes).toEqual(['POST /api/v1/servers/srv-1/redfish/probe'])
})
