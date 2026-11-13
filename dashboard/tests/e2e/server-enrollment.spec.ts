import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Server Enrollment (decision 053): Add servers guides a user one question at a time to the
// one action that adds a Server, and the list says when hardware inspection needs attention.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

test('PXE with external DHCP shows the Boot ISO and Redfish commands to mount it', async ({ page }) => {
  await installApiFixtures(page, { fleetSize: 0 })
  await page.goto('/servers?site=site-a')

  await expect(page.getByText('No servers yet')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Review integrations' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Add servers' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Add servers' })

  await dialog.getByRole('button', { name: /No, boot it from PXE/ }).click()
  await dialog.getByRole('button', { name: /External DHCP/ }).click()
  // Only the current Site provisioner's Boot ISO is offered.
  const isoURL = 'http://192.0.2.1/boot-media/ipxe/iso-taipei/swallow-ipxe.iso'
  await expect(dialog.getByRole('textbox', { name: 'Boot ISO URL' })).toHaveValue(isoURL)

  await dialog.getByRole('tab', { name: 'Redfish' }).click()
  await dialog.getByRole('textbox', { name: 'BMC address' }).fill('10.0.0.50')
  await dialog.getByRole('textbox', { name: 'BMC user' }).fill('root')
  const commands = dialog.getByLabel('Redfish commands', { exact: true })
  await expect(commands).toContainText("BMC='https://10.0.0.50'")
  await expect(commands).toContainText(`"Image":"${isoURL}"`)
  await expect(commands).toContainText("curl -sk -u 'root' -X PATCH")
  await expect(dialog.getByText('Waiting for the server to appear…')).toBeVisible()
})

test('keeping the OS shows one command that downloads swallow from this console', async ({ page }) => {
  const swallowUrls: string[] = []
  await installApiFixtures(page, { fleetSize: 0, onEnrollmentRequest: (_method, _path, swallowUrl) => swallowUrls.push(swallowUrl) })
  await page.goto('/servers?site=site-a')
  await page.getByRole('button', { name: 'Add servers' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Add servers' })

  await dialog.getByRole('button', { name: /Yes, keep its OS/ }).click()
  // The Site's only provisioner is resolved without another choice.
  const origin = new URL(page.url()).origin
  const bundleCommand = `curl -fsSL '${origin}/downloads/swallow-enroll.sh' | sudo sh -s -- --provisioner=maas --endpoint 'https://maas.example/MAAS' --token 'consumer-maas-a:token:secret'`
  const command = dialog.getByLabel('Enrollment command', { exact: true })
  await expect(command).toHaveText(bundleCommand)
  await expect(dialog.getByText("Contains the provisioner's API key.")).toBeVisible()
  expect(swallowUrls).toEqual([origin])

  // Registration leaves the server without a power driver; the opt-in BMC setup is one more flag.
  const bmc = dialog.getByRole('checkbox', { name: 'Also set up its BMC over IPMI' })
  await expect(bmc).not.toBeChecked()
  await expect(dialog.getByText('Without it, the server has no power driver')).toBeVisible()
  await dialog.getByText('Also set up its BMC over IPMI').click()
  await expect(bmc).toBeChecked()
  await expect(command).toHaveText(`${bundleCommand} --configure-bmc`)
  await expect(dialog.getByText(/creates or reuses an IPMI user on its BMC/)).toBeVisible()
  expect(swallowUrls).toEqual([origin])
})

test('libvirt virtual machines are enrolled by name from their hypervisor (experimental)', async ({ page }) => {
  const requests: Record<string, unknown>[] = []
  await installApiFixtures(page, { onVirtualMachineEnrollmentRequest: (body) => requests.push(body) })
  await page.goto('/servers?site=site-a')
  await page.getByRole('button', { name: 'Add servers' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Add servers' })

  await dialog.getByRole('button', { name: /It is a libvirt virtual machine/ }).click()
  await dialog.getByRole('combobox', { name: 'Hypervisor' }).click()
  await dialog.getByRole('option', { name: 'gpu-node-01 (192.168.40.21)' }).click()
  await dialog.getByRole('button', { name: 'List virtual machines' }).click()

  const machines = dialog.getByRole('group', { name: 'Virtual machines' })
  await expect(machines).toContainText('Read as ubuntu.')
  await expect(machines).toContainText('52:54:00:af:de:01')
  // A domain that already is a Server links to it and cannot be chosen again.
  await expect(machines.getByRole('link', { name: 'Already gpu-node-03' })).toBeVisible()
  await expect(machines.getByRole('checkbox', { name: 'lab-vm-3' })).toBeDisabled()

  await machines.getByText('lab-vm-1', { exact: true }).click()
  await machines.getByText('lab-vm-2', { exact: true }).click()
  await dialog.getByRole('combobox', { name: 'Boot ISO' }).click()
  await dialog.getByRole('option', { name: /taipei-rack/ }).click()
  await dialog.getByText('Stop a running virtual machine').click()
  await dialog.getByRole('button', { name: 'Enroll 2 virtual machines' }).click()

  await expect(dialog.getByText('Enrolling 2 virtual machines')).toBeVisible()
  expect(requests).toEqual([{
    integrationId: 'maas-a', hypervisorServerId: 'srv-1', domains: ['lab-vm-1', 'lab-vm-2'], bootIsoId: 'iso-taipei', powerOffRunning: true,
  }])
  await dialog.getByRole('link', { name: 'View workflow' }).click()
  await expect(page).toHaveURL(/\/workflows\/op-vm-enroll/)
})

test('a Site without a provisioner is sent to connect one', async ({ page }) => {
  await installApiFixtures(page, { fleetSize: 0, noProvisioners: true })
  await page.goto('/servers?site=site-a')

  await expect(page.getByText('No provisioner connected')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add servers' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Connect a provisioner' }).click()
  await expect(page).toHaveURL(/\/infrastructure\/integrations\?site=site-a/)
})

test('a user who is not an admin and sees no Servers is not sent to connect a provisioner', async ({ page }) => {
  const integrationReads: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/v1/integrations') integrationReads.push(request.url())
  })
  await installApiFixtures(page, { fleetSize: 0, noProvisioners: true, identity: 'member' })
  await page.goto('/servers?site=site-a')

  await expect(page.getByText('No servers available')).toBeVisible()
  await expect(page.getByText('Servers appear here when an admin places them in a Pool you belong to.')).toBeVisible()
  await expect(page.getByText('No provisioner connected')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Connect a provisioner' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Add servers' })).toHaveCount(0)
  expect(integrationReads).toEqual([])
})

test('hardware inspection waiting for attention points to Boot Media and the Workflow', async ({ page }) => {
  await installApiFixtures(page, { inspectionAttentionServerIds: ['srv-2'] })
  await page.goto('/servers?site=site-a')

  await expect(page.getByText('Hardware inspection needs attention')).toBeVisible()
  await expect(page.getByText("If the server can't PXE boot, enable its Boot Media, then retry.")).toBeVisible()
  await page.getByRole('button', { name: 'View workflow' }).click()
  await expect(page).toHaveURL(/\/workflows\/op-inspect-srv-2/)
})
