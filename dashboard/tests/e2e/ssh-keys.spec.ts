import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Operator journeys for SSH key management and the OS Image default user (decision 039). Every
// request is served by deterministic fixtures; no backend or provisioner is contacted.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

test('Account menu opens SSH keys with the deployment key and per-provisioner status', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/?site=site-a')
  await page.getByRole('button', { name: 'Account menu' }).click()
  await page.getByRole('menuitem', { name: 'SSH keys' }).click()

  await expect(page).toHaveURL(/\/account\/ssh-keys/)
  await expect(page.getByRole('heading', { name: 'SSH keys', exact: true })).toBeVisible()
  await expect(page.getByText('ssh-ed25519 AAAADEPLOY swallow-deployment')).toBeVisible()
  const provisioners = page.getByRole('list', { name: 'Provisioner sync status' })
  await expect(provisioners).toContainText('MAAS Taipei')
  await expect(provisioners).toContainText('Synced')
  // A failure is spelled out with its reason, never conveyed by colour alone.
  await expect(provisioners).toContainText('Failed')
  await expect(provisioners).toContainText('Could not reach MAAS.')
  await expect(page.getByRole('table', { name: 'Access keys' })).toContainText('work-laptop')
})

test('Generating an access key shows the private key once and blocks dismissal until saved', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/account/ssh-keys')
  await page.getByRole('button', { name: 'Generate key pair' }).click()
  await page.locator('#ssh-key-generate-name').fill('ops-jumpbox')
  await page.getByRole('button', { name: 'Generate', exact: true }).click()

  await expect(page.getByRole('heading', { name: 'Save your private key' })).toBeVisible()
  await expect(page.locator('#ssh-key-generated-private')).toHaveValue(/BEGIN OPENSSH PRIVATE KEY/)
  const done = page.getByRole('button', { name: 'Done' })
  await expect(done).toBeDisabled()
  // Escape must not discard a private key the operator has not saved yet.
  await page.keyboard.press('Escape')
  await expect(page.getByRole('heading', { name: 'Save your private key' })).toBeVisible()

  await page.getByText('I have saved the private key').click()
  await done.click()
  await expect(page.getByText('SSH key pair generated')).toBeVisible()
  await expect(page.getByRole('table', { name: 'Access keys' })).toContainText('ops-jumpbox')
})

// Operators often reach the dashboard over plain HTTP on a LAN, where the async Clipboard API is
// unavailable and copying falls back to a temporary textarea. Inside a modal that fallback must still
// put the real text on the clipboard, not just show "Copied".
test('Copying the generated private key inside the dialog works on an insecure origin', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.addInitScript(() => {
    Object.defineProperty(window, 'isSecureContext', { configurable: true, get: () => false })
  })
  await installApiFixtures(page)
  await page.goto('/account/ssh-keys')
  await page.evaluate(() => navigator.clipboard.writeText('stale clipboard'))
  // Outside any dialog the fallback must keep working as before.
  await page.getByRole('button', { name: 'Copy public key', exact: true }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('ssh-ed25519 AAAADEPLOY swallow-deployment')

  await page.getByRole('button', { name: 'Generate key pair' }).click()
  await page.locator('#ssh-key-generate-name').fill('ops-jumpbox')
  await page.getByRole('button', { name: 'Generate', exact: true }).click()

  await page.getByRole('button', { name: 'Copy private key' }).click()
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toBe('-----BEGIN OPENSSH PRIVATE KEY-----\nFIXTURE\n-----END OPENSSH PRIVATE KEY-----\n')
})

test('Importing a key surfaces the backend refusal inline and accepts a valid key', async ({ page }) => {
  const requests: Array<{ method: string; path: string }> = []
  await installApiFixtures(page, { onSSHKeyRequest: (method, path) => requests.push({ method, path }) })
  await page.goto('/account/ssh-keys')
  await page.getByRole('button', { name: 'Import key' }).first().click()
  await page.locator('#ssh-key-import-name').fill('desk')
  await page.locator('#ssh-key-import-public-key').fill('not a key')
  await page.getByRole('button', { name: 'Import', exact: true }).click()
  await expect(page.getByText('invalid ssh key: not a public key')).toBeVisible()

  await page.locator('#ssh-key-import-public-key').fill('ssh-ed25519 AAAADESK bob@desk')
  await page.getByRole('button', { name: 'Import', exact: true }).click()
  await expect(page.getByText('SSH key imported')).toBeVisible()
  await expect(page.getByRole('table', { name: 'Access keys' })).toContainText('desk')
  expect(requests.filter((request) => request.method === 'POST' && request.path === '/api/v1/ssh-keys')).toHaveLength(2)
})

test('A pending key settles in place by re-reading only that key, not the list', async ({ page }) => {
  const requests: string[] = []
  await installApiFixtures(page, { onSSHKeyRequest: (method, path) => requests.push(`${method} ${path}`) })
  await page.goto('/account/ssh-keys')
  await page.getByRole('button', { name: 'Import key' }).first().click()
  await page.locator('#ssh-key-import-name').fill('desk')
  await page.locator('#ssh-key-import-public-key').fill('ssh-ed25519 AAAADESK bob@desk')
  await page.getByRole('button', { name: 'Import', exact: true }).click()

  const row = page.getByRole('table', { name: 'Access keys' }).getByRole('row', { name: /desk/ })
  await expect(row).toContainText('Pending')
  // No manual reload: the pending status cell follows the key until it settles.
  await expect(row).toContainText('Synced · 2/2', { timeout: 10_000 })

  const listReads = requests.filter((request) => request === 'GET /api/v1/ssh-keys')
  const keyReads = requests.filter((request) => /^GET \/api\/v1\/ssh-keys\/key-\d+$/.test(request))
  // One list read on open and one after the import; the settling is done by single-key reads.
  expect(listReads).toHaveLength(2)
  expect(keyReads.length).toBeGreaterThanOrEqual(1)
  expect(requests.some((request) => request === 'GET /api/v1/ssh-keys/key-laptop')).toBe(false)
})

test('Regenerating the deployment key warns about deployed Servers before sending', async ({ page }) => {
  const requests: string[] = []
  await installApiFixtures(page, { onSSHKeyRequest: (method, path) => requests.push(`${method} ${path}`) })
  await page.goto('/account/ssh-keys')
  await page.getByRole('button', { name: 'Regenerate' }).click()
  await expect(page.getByText('Servers deployed earlier keep the old key')).toBeVisible()
  await page.getByRole('button', { name: 'Regenerate key' }).click()

  await expect(page.getByText('Deployment key regenerated')).toBeVisible()
  expect(requests).toContain('POST /api/v1/ssh-keys/deployment/regenerate')
  await expect(page.getByText('ssh-ed25519 AAAAREGENERATED swallow-deployment')).toBeVisible()
})

test('OS image default user is shown, validated, and saved with the rest of the overlay', async ({ page }) => {
  const overlays: Array<Record<string, unknown>> = []
  await installApiFixtures(page, { onOSImageOverlayRequest: (body) => overlays.push(body) })
  await page.goto('/provisioning/images?site=site-a')
  await expect(page.getByRole('columnheader', { name: 'Tags' })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: 'Default user' })).toBeVisible()
  const builtInTitle = page.getByRole('row', { name: 'Ubuntu 22.04 LTS image', exact: true }).first()
  const builtInData = page.getByRole('row', { name: 'Ubuntu 22.04 LTS catalog data', exact: true }).first()
  await expect(builtInData.locator('.sw-os-image-col--default-user')).toHaveText('ubuntu')
  await builtInTitle.getByRole('button', { name: 'Show details for Ubuntu 22.04 LTS' }).click()
  const details = page.locator('.sw-os-image-detail-row').filter({ hasText: 'Ubuntu 22.04 LTS' })
  await expect(details).toContainText('Default user')
  await expect(details).toContainText('ubuntu')

  const custom = page.getByRole('row', { name: 'Ubuntu 24.04 ROCm catalog data', exact: true }).first()
  await custom.getByRole('button', { name: 'More actions for Ubuntu 24.04 ROCm' }).click()
  await page.getByRole('menuitem', { name: 'Edit image settings' }).click()
  const field = page.locator('#os-image-default-user')
  await field.fill('Cloud User')
  await expect(page.getByText('Use a login name')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

  await field.fill('cloud-user')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('OS image updated')).toBeVisible()
  // The PATCH replaces the whole overlay, so every field is sent, including the default user.
  expect(overlays).toEqual([{ name: '', osSystem: '', release: '', tags: [], defaultUser: 'cloud-user' }])
})

test('Server summary shows how to connect with the deployed image login user', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/servers/srv-1/summary?site=site-a')

  const connection = page.getByRole('region', { name: 'Connection' })
  await expect(connection).toContainText('Login user')
  await expect(connection).toContainText('ubuntu')
  await expect(connection).toContainText('192.168.40.21')
  await expect(connection).toContainText('ssh ubuntu@192.168.40.21')
  await expect(connection.getByRole('button', { name: 'Copy SSH command' })).toBeVisible()
})

test('OS image upload sends the optional default user', async ({ page }) => {
  const defaultUsers: string[] = []
  await installApiFixtures(page, { onOSImageUploadDefaultUser: (value) => defaultUsers.push(value) })
  await page.goto('/provisioning/images?site=site-a')
  await page.getByRole('button', { name: 'Upload image' }).click()
  await page.getByRole('combobox', { name: 'Provisioner integration', exact: true }).click()
  await page.getByRole('option', { name: 'MAAS Taipei', exact: true }).click()
  await page.locator('#upload-image-name').fill('rocky-10')
  await page.locator('#upload-image-default-user').fill('cloud-user')
  await page.locator('#upload-image-file').setInputFiles({
    name: 'image.tar.gz',
    mimeType: 'application/gzip',
    buffer: Buffer.from('fake-image-bytes'),
  })
  await page.getByRole('button', { name: 'Upload', exact: true }).click()

  await expect(page.getByText('OS image uploaded')).toBeVisible()
  await expect.poll(() => defaultUsers).toEqual(['cloud-user'])
})
