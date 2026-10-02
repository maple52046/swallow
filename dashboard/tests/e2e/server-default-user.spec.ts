import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// The Server Default User (decision 045): shown on the Summary Connection card and set there with an
// optional one-time password that installs the Deployment Key, or the shown manual command.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

test('the default user is set with a one-time password and becomes the login user', async ({ page }) => {
  const writes: Array<{ method: string; body: Record<string, unknown> | null }> = []
  await installApiFixtures(page, { onDefaultUserRequest: (method, body) => writes.push({ method, body }) })
  await page.goto('/servers/srv-1/summary?site=site-a')

  const connection = page.getByRole('region', { name: 'Connection' })
  await expect(connection).toContainText('ubuntu(from the OS image)')
  await connection.getByRole('button', { name: 'Change default user' }).click()

  const dialog = page.getByRole('dialog', { name: 'Default user' })
  await expect(dialog).toContainText('Currently ubuntu, from the OS image.')
  // The manual alternative carries the Deployment Key's public key, never a private key.
  await expect(dialog.getByText(/echo 'ssh-ed25519 AAAADEPLOY swallow-deployment' >> ~\/\.ssh\/authorized_keys/)).toBeVisible()

  await dialog.getByRole('textbox', { name: /Account/ }).fill('amd')
  await dialog.getByLabel('Password (optional)').fill('s3cret')
  await dialog.getByRole('button', { name: 'Save', exact: true }).click()

  await expect(dialog).toHaveCount(0)
  await expect(page.getByText('Default user set to amd')).toBeVisible()
  await expect(page.getByText(/The Deployment Key was added to amd's authorized_keys/)).toBeVisible()
  expect(writes).toEqual([{ method: 'PUT', body: { user: 'amd', password: 's3cret' } }])
  await expect(connection).toContainText('amd(set on this Server)')
  await expect(connection).toContainText('ssh amd@192.168.40.21')
  await expect(page.getByText('s3cret')).toHaveCount(0)
})

test('an account that needs a sudo password is saved with a warning', async ({ page }) => {
  const writes: Array<{ method: string; body: Record<string, unknown> | null }> = []
  await installApiFixtures(page, { defaultUserSudo: 'password_required', onDefaultUserRequest: (method, body) => writes.push({ method, body }) })
  await page.goto('/servers/srv-1/summary?site=site-a')

  await page.getByRole('button', { name: 'Change default user' }).click()
  const dialog = page.getByRole('dialog', { name: 'Default user' })
  await dialog.getByRole('textbox', { name: /Account/ }).fill('amd')
  await dialog.getByRole('button', { name: 'Save', exact: true }).click()

  await expect(page.getByText(/amd needs a password for sudo\. Automation uses the Site's become password/)).toBeVisible()
  // Without a password nothing but the account is sent: the key must already be authorized.
  expect(writes).toEqual([{ method: 'PUT', body: { user: 'amd' } }])
})

test('a rejected Deployment Key keeps the dialog open with the API reason', async ({ page }) => {
  await installApiFixtures(page, {
    defaultUserError: {
      status: 409,
      code: 'conflict',
      message: 'Server "gpu-node-01" rejected the Deployment Key for amd. Give the account\'s password once so swallow installs the key, or add the key to amd\'s authorized_keys yourself.',
    },
  })
  await page.goto('/servers/srv-1/summary?site=site-a')

  await page.getByRole('button', { name: 'Change default user' }).click()
  const dialog = page.getByRole('dialog', { name: 'Default user' })
  await dialog.getByRole('textbox', { name: /Account/ }).fill('amd')
  await dialog.getByRole('button', { name: 'Save', exact: true }).click()

  await expect(dialog).toContainText('The default user was not changed')
  await expect(dialog).toContainText('rejected the Deployment Key for amd')
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(page.getByRole('region', { name: 'Connection' })).toContainText('ubuntu(from the OS image)')
})

test('a value set on the Server can be reset to the OS image default', async ({ page }) => {
  const writes: string[] = []
  await installApiFixtures(page, { onDefaultUserRequest: (method) => writes.push(method) })
  await page.goto('/servers/srv-1/summary?site=site-a')
  const connection = page.getByRole('region', { name: 'Connection' })

  await connection.getByRole('button', { name: 'Change default user' }).click()
  let dialog = page.getByRole('dialog', { name: 'Default user' })
  // No reset while the value already comes from the image.
  await expect(dialog.getByRole('button', { name: 'Use the OS image default' })).toHaveCount(0)
  await dialog.getByRole('textbox', { name: /Account/ }).fill('amd')
  await dialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(connection).toContainText('amd(set on this Server)')

  await connection.getByRole('button', { name: 'Change default user' }).click()
  dialog = page.getByRole('dialog', { name: 'Default user' })
  await dialog.getByRole('button', { name: 'Use the OS image default' }).click()
  await expect(dialog).toHaveCount(0)
  await expect(connection).toContainText('ubuntu(from the OS image)')
  expect(writes).toEqual(['PUT', 'DELETE'])
})

test('an invalid account name or a locked Server cannot be saved', async ({ page }) => {
  await installApiFixtures(page, { lockedServerIds: ['srv-1'] })
  await page.goto('/servers/srv-1/summary?site=site-a')

  await page.getByRole('button', { name: 'Change default user' }).click()
  const dialog = page.getByRole('dialog', { name: 'Default user' })
  await expect(dialog).toContainText('Read-only while locked')
  await dialog.getByRole('textbox', { name: /Account/ }).fill('Bad User')
  await expect(dialog).toContainText('Use lowercase letters, digits, _ or -')
  await expect(dialog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await dialog.getByRole('textbox', { name: /Account/ }).fill('amd')
  await expect(dialog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
})
