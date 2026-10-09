import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Power Configuration (decision 054): the provisioner-owned power driver of a Server, read live and
// written through from the Server Summary. Its driver family decides whether the Server has a BMC
// (and so Boot Media), and an inspection that stopped for a missing driver points here.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

test('a VM without a power driver gets a virsh Power Configuration and no Boot Media', async ({ page }) => {
  const writes: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    powerConfigurations: { 'srv-2': { driver: '' } },
    onPowerConfigurationRequest: (_serverId, body) => writes.push(body),
  })
  await page.goto('/servers/srv-2/summary?site=site-a')

  const card = page.getByRole('region', { name: 'Power control' })
  await expect(card).toBeVisible()
  await expect(card.getByText('No power control', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Boot media' })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Management controller' })).toHaveCount(0)

  await card.getByRole('button', { name: 'Set power configuration' }).click()
  const dialog = page.getByRole('dialog', { name: 'Power configuration' })
  await dialog.getByRole('combobox', { name: 'Driver' }).click()
  await page.getByRole('option', { name: 'virsh (libvirt) — virtual machine', exact: true }).click()

  // A URI with SSH options in a query is refused before anything is sent.
  await dialog.getByRole('textbox', { name: /^Hypervisor URI/ }).fill('qemu+ssh://maas@tainan-ci/system?keyfile=/k')
  await dialog.getByRole('textbox', { name: /^Domain/ }).fill('simple-pig')
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog.getByText('Use qemu+ssh://[user@]host[:port]/system, without a password, query, or fragment.')).toBeVisible()
  expect(writes).toEqual([])

  await dialog.getByRole('textbox', { name: /^Hypervisor URI/ }).fill('qemu+ssh://maas@tainan-ci/system')
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog).toHaveCount(0)
  expect(writes).toEqual([{ driver: 'virsh', address: 'qemu+ssh://maas@tainan-ci/system', powerId: 'simple-pig' }])

  await expect(card.getByText('virsh (libvirt) — virtual machine')).toBeVisible()
  await expect(card.getByText('Automatic', { exact: true })).toBeVisible()
  await expect(card.getByText('simple-pig', { exact: true })).toBeVisible()
  await expect(card.getByRole('button', { name: 'Edit power configuration' })).toBeVisible()
})

test('a Server with a BMC keeps its management controller, Power Configuration, and Boot Media', async ({ page }) => {
  const writes: Array<Record<string, unknown>> = []
  await installApiFixtures(page, { onPowerConfigurationRequest: (_serverId, body) => writes.push(body) })
  await page.goto('/servers/srv-1/summary?site=site-a')

  const card = page.getByRole('region', { name: 'Management controller' })
  await expect(card).toBeVisible()
  await expect(card.getByText('IPMI — BMC')).toBeVisible()
  await expect(card.getByRole('heading', { name: 'Boot media' })).toBeVisible()

  // Leaving the password empty keeps the stored one: the request carries no password key.
  await card.getByRole('button', { name: 'Edit power configuration' }).click()
  const dialog = page.getByRole('dialog', { name: 'Power configuration' })
  await expect(dialog.getByText('Leave empty to keep the stored password.', { exact: false })).toBeVisible()
  await dialog.getByRole('textbox', { name: /^BMC address/ }).fill('192.0.2.21')
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog).toHaveCount(0)
  expect(writes).toEqual([{ driver: 'ipmi', address: '192.0.2.21', username: 'bmc-admin' }])
})

test('a VM-host member is read-only and says where its power is managed', async ({ page }) => {
  await installApiFixtures(page, {
    powerConfigurations: { 'srv-2': { driver: 'virsh', address: 'qemu+ssh://kvm-3/system', powerId: 'vm-2', managedBy: 'kvm-3' } },
  })
  await page.goto('/servers/srv-2/summary?site=site-a')

  const card = page.getByRole('region', { name: 'Power control' })
  await expect(card.getByText('Managed by the provisioner')).toBeVisible()
  await expect(card.getByRole('button', { name: 'Edit power configuration' })).toBeDisabled()
})

test('an inspection stopped for a missing power driver points to the Power Configuration', async ({ page }) => {
  await installApiFixtures(page, { powerConfigurations: { 'srv-2': { driver: '' } }, powerAttentionServerIds: ['srv-2'] })
  await page.goto('/servers?site=site-a')

  await expect(page.getByText('Hardware inspection needs attention')).toBeVisible()
  await expect(page.getByText(/no power driver it can read/)).toBeVisible()
  await expect(page.getByText("If the server can't PXE boot, enable its Boot Media, then retry.")).toHaveCount(0)
  await page.getByRole('button', { name: 'Set power configuration' }).click()
  await expect(page).toHaveURL(/\/servers\/srv-2\/summary/)
  await expect(page.getByRole('region', { name: 'Power control' })).toBeVisible()
})
