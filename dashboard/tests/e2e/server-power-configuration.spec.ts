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
  await dialog.getByRole('textbox', { name: /^Hypervisor URI/ }).fill('qemu+ssh://maas@hv-01/system?keyfile=/k')
  await dialog.getByRole('textbox', { name: /^Domain/ }).fill('simple-pig')
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog.getByText('Use qemu+ssh://[user@]host[:port]/system, without a password, query, or fragment.')).toBeVisible()
  expect(writes).toEqual([])

  await dialog.getByRole('textbox', { name: /^Hypervisor URI/ }).fill('qemu+ssh://maas@hv-01/system')
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog).toHaveCount(0)
  expect(writes).toEqual([{ driver: 'virsh', address: 'qemu+ssh://maas@hv-01/system', powerId: 'simple-pig' }])

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

test('the Server list power button sets up a missing power driver in one checked save', async ({ page }) => {
  const writes: Array<Record<string, unknown>> = []
  const refreshes: string[] = []
  await installApiFixtures(page, {
    powerStates: { 'srv-1': 'unknown' },
    powerConfigurations: { 'srv-1': { driver: '' } },
    onPowerConfigurationRequest: (_serverId, body) => writes.push(body),
    onServerRefreshRequest: (serverId) => refreshes.push(serverId),
  })
  // Hold the live read so the test sees what the dialog shows before it answers.
  let releaseRead = () => {}
  const readHeld = new Promise<void>((resolve) => { releaseRead = resolve })
  await page.route('**/api/v1/servers/srv-1/power-configuration', async (route) => {
    if (route.request().method() === 'GET') await readHeld
    await route.fallback()
  })
  await page.goto('/servers?site=site-a')
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' }).first()
  // The projection's power control says why the state is unknown, on the button itself.
  const powerButton = row.getByRole('button', { name: 'Power actions for gpu-node-01; no power driver' })
  await expect(powerButton).toHaveText('NO DRIVER')
  await powerButton.click()

  // An unknown power state without a driver needs the configuration first, not power actions, and
  // the form opens before the live read answers; only the driver choice waits for it.
  const dialog = page.getByRole('dialog', { name: 'Power configuration' })
  await expect(dialog).toContainText('gpu-node-01 has no power driver the provisioner can use')
  await expect(dialog.getByRole('combobox', { name: 'Driver' })).toBeDisabled()
  await expect(dialog.getByRole('button', { name: 'Save' })).toBeDisabled()
  releaseRead()
  await expect(dialog.getByRole('combobox', { name: 'Driver' })).toBeEnabled()
  await expect(dialog.getByRole('button', { name: 'Back' })).toHaveCount(0)
  await dialog.getByRole('combobox', { name: 'Driver' }).click()
  await page.getByRole('option', { name: 'IPMI — BMC', exact: true }).click()
  await dialog.getByRole('textbox', { name: /^BMC address/ }).fill('192.0.2.30')
  await dialog.getByRole('textbox', { name: 'Account' }).fill('maas')
  const password = dialog.getByLabel('Password', { exact: true })
  await password.fill('bmc-secret')
  // The write-only password can be revealed to check it before saving, and hidden again.
  await expect(password).toHaveAttribute('type', 'password')
  await dialog.getByRole('button', { name: 'Show password' }).click()
  await expect(password).toHaveAttribute('type', 'text')
  await expect(password).toHaveValue('bmc-secret')
  await dialog.getByRole('button', { name: 'Hide password' }).click()
  await expect(password).toHaveAttribute('type', 'password')
  await dialog.getByRole('button', { name: 'Save' }).click()

  // The API kept it only after the provisioner read the power through it: the dialog closes with
  // that reading, and the projection is refreshed so the list's button follows at once.
  await expect(dialog).toHaveCount(0)
  await expect(page.getByRole('dialog', { name: 'Power actions' })).toHaveCount(0)
  await expect(page.getByText(/through IPMI and reads its power as powered on/)).toBeVisible()
  expect(writes).toEqual([{ driver: 'ipmi', address: '192.0.2.30', username: 'maas', password: 'bmc-secret' }])
  await expect.poll(() => refreshes).toEqual(['srv-1'])
})

test('a Power Configuration the provisioner cannot read the power through is not saved', async ({ page }) => {
  const reason = 'The provisioner cannot read the power through this configuration (Failed to query power: password invalid); the previous configuration was kept'
  await installApiFixtures(page, {
    powerStates: { 'srv-1': 'unknown' },
    powerConfigurations: { 'srv-1': { driver: '' } },
    powerConfigurationError: { status: 400, code: 'validation_error', message: reason },
  })
  // Hold the check so the test sees the dialog while the provisioner is still reading the power.
  let releaseWrite = () => {}
  const writeHeld = new Promise<void>((resolve) => { releaseWrite = resolve })
  await page.route('**/api/v1/servers/srv-1/power-configuration', async (route) => {
    if (route.request().method() === 'PUT') await writeHeld
    await route.fallback()
  })
  await page.goto('/servers?site=site-a')
  await page.getByRole('row').filter({ hasText: 'gpu-node-01' }).first()
    .getByRole('button', { name: /^Power actions for gpu-node-01/ }).click()

  const dialog = page.getByRole('dialog', { name: 'Power configuration' })
  await dialog.getByRole('combobox', { name: 'Driver' }).click()
  await page.getByRole('option', { name: 'IPMI — BMC', exact: true }).click()
  await dialog.getByRole('textbox', { name: /^BMC address/ }).fill('192.0.2.30')
  await dialog.getByLabel('Password', { exact: true }).fill('wrong')
  await dialog.getByRole('button', { name: 'Save' }).click()

  // One step: the same dialog checks, then says why nothing was saved and keeps what was typed.
  await expect(dialog.getByRole('button', { name: /Checking/ })).toBeVisible()
  releaseWrite()
  await expect(dialog.getByText('The power configuration was not changed')).toBeVisible()
  await expect(dialog).toContainText('password invalid')
  await expect(dialog.getByRole('textbox', { name: /^BMC address/ })).toHaveValue('192.0.2.30')
  await expect(dialog.getByLabel('Password', { exact: true })).toHaveValue('wrong')
  await expect(dialog.getByRole('button', { name: 'Save' })).toBeEnabled()
})

test('the Server list power dialog reads the configuration first when the projection has not observed it', async ({ page }) => {
  await installApiFixtures(page, {
    powerStates: { 'srv-1': 'unknown' },
    powerConfigurations: { 'srv-1': { driver: '' } },
    powerControlUnobservedServerIds: ['srv-1'],
  })
  let releaseRead = () => {}
  const readHeld = new Promise<void>((resolve) => { releaseRead = resolve })
  await page.route('**/api/v1/servers/srv-1/power-configuration', async (route) => {
    if (route.request().method() === 'GET') await readHeld
    await route.fallback()
  })
  await page.goto('/servers?site=site-a')
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' }).first()
  const powerButton = row.getByRole('button', { name: 'Power actions for gpu-node-01' })
  await expect(powerButton).toHaveText('UNKNOWN')
  await powerButton.click()

  await expect(page.getByRole('dialog', { name: 'Power actions' })).toContainText('Reading its power configuration…')
  releaseRead()
  await expect(page.getByRole('dialog', { name: 'Power configuration' })).toContainText('has no power driver the provisioner can use')
})

test('the Server list power dialog flags a driver the provisioner cannot read', async ({ page }) => {
  await installApiFixtures(page, { powerStates: { 'srv-1': 'error' } })
  await page.goto('/servers?site=site-a')
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' }).first()
  await row.getByRole('button', { name: /^Power actions for gpu-node-01/ }).click()

  const actions = page.getByRole('dialog', { name: 'Power actions' })
  await expect(actions).toContainText('Current power state: Power state error.')
  await expect(actions.getByText('The provisioner cannot read its power')).toBeVisible()
  await expect(actions.getByRole('button', { name: 'Power on' })).toBeVisible()
  await actions.getByRole('button', { name: 'Edit power configuration' }).click()

  const form = page.getByRole('dialog', { name: 'Power configuration' })
  await expect(form.getByRole('textbox', { name: /^BMC address/ })).toHaveValue('192.0.2.20')
  await form.getByRole('button', { name: 'Back' }).click()
  await expect(page.getByRole('dialog', { name: 'Power actions' }).getByRole('button', { name: 'Power off' })).toBeVisible()
})

test('the Server list power dialog keeps its actions when the configuration cannot be read', async ({ page }) => {
  await installApiFixtures(page)
  await page.route('**/api/v1/servers/srv-1/power-configuration', (route) => (
    route.request().method() === 'GET'
      ? route.fulfill({ status: 503, json: { error: { code: 'provider_unavailable', message: 'The provisioner could not be reached.', requestId: 'req-power' } } })
      : route.fallback()
  ))
  await page.goto('/servers?site=site-a')
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' }).first()
  await row.getByRole('button', { name: /^Power actions for gpu-node-01/ }).click()

  const actions = page.getByRole('dialog', { name: 'Power actions' })
  await expect(actions.getByText('Power configuration unavailable')).toBeVisible()
  await expect(actions).toContainText('The provisioner could not be reached.')
  await expect(actions).toContainText('Current power state: Powered on.')
  await expect(actions.getByRole('button', { name: 'Power off' })).toBeVisible()
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
