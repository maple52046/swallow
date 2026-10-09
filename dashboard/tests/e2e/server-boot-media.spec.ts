import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Boot Media (decisions 047 and 049): shown inside the Summary's Management controller card,
// enabled there with a Boot ISO of the Server's own provisioner through a preflight the API runs
// against the BMC, switched to another Boot ISO, and disabled with a best-effort BMC reset.

const TAIPEI_ISO_URL = 'http://192.0.2.1/boot-media/ipxe/iso-taipei/swallow-ipxe.iso'

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

test('Boot Media is enabled with a Boot ISO through the preflight and reported with its persistence', async ({ page }) => {
  const writes: Array<{ method: string; path: string; body: Record<string, unknown> | null }> = []
  await installApiFixtures(page, { onBootMediaRequest: (method, path, body) => writes.push({ method, path, body }) })
  await page.goto('/servers/srv-1/summary?site=site-a')

  const management = page.getByRole('region', { name: 'Management controller' })
  const bootMedia = management.getByRole('region', { name: 'Boot media' })
  await expect(bootMedia).toContainText('Redfish Boot Media supported')
  await expect(bootMedia).toContainText('AMI · firmware 13.06.10')
  await expect(bootMedia).toContainText('Disabled')
  await expect(bootMedia).toContainText('None chosen')

  await bootMedia.getByRole('button', { name: 'Enable Boot Media' }).click()
  const dialog = page.getByRole('dialog', { name: 'Enable Boot Media' })
  // The provisioner's only Boot ISO is preselected; another provisioner's is never offered.
  await expect(dialog.getByRole('combobox', { name: 'Boot ISO', exact: true })).toContainText('taipei-rack')
  await expect(dialog).toContainText(`Mount ${TAIPEI_ISO_URL} as a virtual CD.`)
  await dialog.getByRole('button', { name: 'Enable', exact: true }).click()

  await expect(dialog).toHaveCount(0)
  await expect(page.getByText('Boot Media enabled on gpu-node-01')).toBeVisible()
  expect(writes).toEqual([{ method: 'PUT', path: '/api/v1/servers/srv-1/boot-media', body: { enabled: true, isoId: 'iso-taipei' } }])
  await expect(bootMedia).toContainText('Enabled')
  await expect(bootMedia).toContainText('Boots the ISO first on every boot')
  await expect(bootMedia).toContainText('when it was enabled')
  await expect(bootMedia).toContainText('taipei-rack')
  await expect(bootMedia).toContainText(TAIPEI_ISO_URL)
})

test('a running preflight shows its real progress, and keeps showing it after the dialog is closed', async ({ page }) => {
  let release = () => {}
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  await installApiFixtures(page, { bootMediaApplyGate: gate })
  await page.goto('/servers/srv-1/summary?site=site-a')
  const bootMedia = page.getByRole('region', { name: 'Boot media' })
  await bootMedia.getByRole('button', { name: 'Enable Boot Media' }).click()
  const dialog = page.getByRole('dialog', { name: 'Enable Boot Media' })
  await dialog.getByRole('button', { name: 'Enable', exact: true }).click()

  // The API reports the settle wait; the bar and the steps follow it, with the time left.
  const bar = dialog.getByRole('progressbar', { name: 'Boot Media progress' })
  await expect(bar).toHaveAttribute('aria-valuetext', /Step 3 of 5: Let the BMC settle the mount/)
  await expect(dialog.getByRole('listitem').filter({ hasText: 'Mount the Boot ISO as a virtual CD (done)' })).toBeVisible()
  await expect(dialog).toContainText('left in this step')
  await expect(dialog.getByRole('button', { name: 'Continue in background' })).toBeEnabled()

  // Closing leaves the preflight running; the Boot media block shows it and waits with its actions.
  await dialog.getByRole('button', { name: 'Continue in background' }).click()
  await expect(dialog).toHaveCount(0)
  const running = bootMedia.getByRole('group', { name: 'Boot Media being applied' })
  await expect(running.getByRole('progressbar', { name: 'Boot Media progress' })).toBeVisible()
  await expect(bootMedia.getByRole('button', { name: 'Check BMC' })).toBeDisabled()

  release()
  await expect(page.getByText('Boot Media enabled on gpu-node-01')).toBeVisible()
  await expect(running).toHaveCount(0)
  await expect(bootMedia).toContainText('Enabled')
  await expect(bootMedia.getByRole('button', { name: 'Check BMC' })).toBeEnabled()
})

test('Change ISO offers only the provisioner’s Boot ISOs and switches after ejecting the current one', async ({ page }) => {
  const writes: Array<Record<string, unknown> | null> = []
  await installApiFixtures(page, {
    bootISOs: [
      { id: 'iso-taipei', name: 'taipei-rack', integrationId: 'maas-a', rackAddress: '10.0.0.2' },
      { id: 'iso-spare', name: 'taipei-spare', integrationId: 'maas-a', rackAddress: '10.0.0.3' },
      { id: 'iso-edge', name: 'edge-rack', integrationId: 'maas-b', rackAddress: '10.9.0.2' },
    ],
    bootMediaSetting: { enabled: true, isoId: 'iso-taipei', updatedAt: '2026-10-04T00:00:00Z', lastAppliedAt: '2026-10-04T00:00:00Z', lastAppliedBy: 'preflight', bootOverride: 'Continuous', lastErrorAt: null },
    onBootMediaRequest: (_method, _path, body) => writes.push(body),
  })
  await page.goto('/servers/srv-1/summary?site=site-a')
  const bootMedia = page.getByRole('region', { name: 'Boot media' })
  await expect(bootMedia).toContainText('taipei-rack')

  await bootMedia.getByRole('button', { name: 'Change ISO' }).click()
  const dialog = page.getByRole('dialog', { name: 'Change Boot ISO' })
  const choice = dialog.getByRole('combobox', { name: 'Boot ISO', exact: true })
  await expect(choice).toContainText('taipei-rack')
  await expect(dialog.getByRole('button', { name: 'Switch ISO', exact: true })).toBeDisabled()
  await choice.click()
  await expect(page.getByRole('option', { name: /taipei-rack.*\(current\)/ })).toBeVisible()
  await expect(page.getByRole('option', { name: /edge-rack/ })).toHaveCount(0)
  await page.getByRole('option', { name: /taipei-spare/ }).click()
  await expect(dialog).toContainText('Eject the current Boot ISO.')
  await dialog.getByRole('button', { name: 'Switch ISO', exact: true }).click()

  await expect(page.getByText('gpu-node-01 now boots taipei-spare')).toBeVisible()
  expect(writes).toEqual([{ enabled: true, isoId: 'iso-spare' }])
  await expect(bootMedia).toContainText('taipei-spare')
})

test('without a Boot ISO for the provisioner, enabling points to the Boot ISOs tab', async ({ page }) => {
  await installApiFixtures(page, {
    bootISOs: [{ id: 'iso-edge', name: 'edge-rack', integrationId: 'maas-b', rackAddress: '10.9.0.2' }],
  })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('region', { name: 'Boot media' }).getByRole('button', { name: 'Enable Boot Media' }).click()

  const dialog = page.getByRole('dialog', { name: 'Enable Boot Media' })
  await expect(dialog).toContainText("No Boot ISO for this Server's provisioner")
  await expect(dialog.getByRole('button', { name: 'Enable', exact: true })).toBeDisabled()
  await dialog.getByRole('link', { name: 'Boot ISOs tab' }).click()
  await expect(page).toHaveURL(/\/provisioning\/boot-isos\?site=site-a$/)
  await expect(page.getByRole('heading', { name: 'Boot ISOs', level: 1 })).toBeVisible()
})

test('a setting enabled before Boot ISOs asks for one to be chosen', async ({ page }) => {
  await installApiFixtures(page, {
    bootMediaSetting: { enabled: true, isoId: null, updatedAt: '2026-10-03T00:00:00Z', lastAppliedAt: '2026-10-03T00:00:00Z', lastAppliedBy: 'preflight', bootOverride: 'Continuous', lastErrorAt: null },
  })
  await page.goto('/servers/srv-1/summary?site=site-a')
  const bootMedia = page.getByRole('region', { name: 'Boot media' })
  await expect(bootMedia).toContainText('Choose a Boot ISO')
  await expect(bootMedia).toContainText('OS deployments of this Server stop at the Boot Media step')

  // Re-apply cannot reuse a missing ISO, so it asks for one like Change ISO does.
  await bootMedia.getByRole('button', { name: 'Re-apply' }).click()
  const dialog = page.getByRole('dialog', { name: 'Re-apply Boot Media' })
  await expect(dialog.getByRole('combobox', { name: 'Boot ISO', exact: true })).toContainText('taipei-rack')
  await dialog.getByRole('button', { name: 'Re-apply', exact: true }).click()
  await expect(bootMedia).not.toContainText('Choose a Boot ISO')
  await expect(bootMedia).toContainText('taipei-rack')
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

  await expect(dialog).toContainText('Boot Media was not applied')
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
  // The chosen Boot ISO is kept, so re-enabling offers it again.
  await expect(bootMedia).toContainText('taipei-rack')
  expect(writes).toEqual([{ enabled: true, isoId: 'iso-taipei' }, { enabled: false }])
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

// libvirt Boot Media (decision 055, experimental in the Dashboard): a virsh virtual machine whose
// hypervisor is a swallow Server gets the same block, through the hypervisor instead of a BMC.
test('a libvirt virtual machine gets Boot Media through its hypervisor (experimental)', async ({ page }) => {
  const writes: Array<{ method: string; path: string; body: Record<string, unknown> | null }> = []
  await installApiFixtures(page, {
    powerConfigurations: { 'srv-2': { driver: 'virsh', address: 'qemu+ssh://ubuntu@192.168.40.21/system', powerId: 'gpu-node-02' } },
    libvirtBootMediaServerIds: ['srv-2'],
    bootISOs: [{ id: 'iso-taipei', name: 'taipei-rack', integrationId: 'maas-a', rackAddress: '10.0.0.2', url: '' }],
    onBootMediaRequest: (method, path, body) => writes.push({ method, path, body }),
  })
  await page.goto('/servers/srv-2/summary?site=site-a')

  const card = page.getByRole('region', { name: 'Power control' })
  await expect(card).toContainText('Boot Media goes through its hypervisor')
  const bootMedia = card.getByRole('region', { name: 'Boot media' })
  await expect(bootMedia).toContainText('Hypervisor ready for Boot Media')
  await expect(bootMedia).toContainText('domain gpu-node-02 · as ubuntu · pool default · has a CD-ROM')

  await bootMedia.getByRole('button', { name: 'Enable Boot Media' }).click()
  const dialog = page.getByRole('dialog', { name: 'Enable Boot Media' })
  await expect(dialog).toContainText('from its CD-ROM on the hypervisor')
  // Without a Boot Media base URL the ISO is still selectable: libvirt uploads the file.
  await expect(dialog.getByRole('combobox', { name: 'Boot ISO', exact: true })).toContainText('taipei-rack')
  await expect(dialog).not.toContainText('(not served)')
  await dialog.getByRole('button', { name: 'Enable', exact: true }).click()
  await expect(page.getByText('Boot Media enabled on gpu-node-02')).toBeVisible()
  expect(writes).toEqual([{ method: 'PUT', path: '/api/v1/servers/srv-2/boot-media', body: { enabled: true, isoId: 'iso-taipei' } }])
  await expect(bootMedia).toContainText('Uploaded to the hypervisor’s storage pool')

  await bootMedia.getByRole('button', { name: 'Check hypervisor' }).click()
  await expect(bootMedia).toContainText('Hypervisor now')
  await expect(bootMedia).toContainText('CD-ROM holds the Boot ISO · boots first')
  await bootMedia.getByRole('button', { name: 'Re-detect hypervisor' }).click()
  await expect(page.getByText('Hypervisor ready for Boot Media').last()).toBeVisible()
})
