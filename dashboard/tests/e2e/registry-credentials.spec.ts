import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Docker CE Registry Credentials (decision 044): managed in Docker CE settings,
// write-only passwords, and used by the Pull image dialog for references whose registry has one.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

test('credentials are added, replaced, and deleted without ever showing a password', async ({ page }) => {
  const writes: Array<{ method: string; body: Record<string, unknown> | null }> = []
  await installApiFixtures(page, {
    registryCredentials: [{ registry: 'docker.io', username: 'hub-bot' }],
    onRegistryCredentialRequest: (method, _path, body) => writes.push({ method, body }),
  })
  // Credentials are owned by Docker CE: the catalog has no global settings action.
  await page.goto('/software?site=site-a')
  await expect(page.getByRole('heading', { name: 'Software', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Registry credentials' })).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'Settings', exact: true })).toHaveCount(0)
  await page.getByRole('link', { name: 'Open Docker CE' }).click()
  await page.getByRole('link', { name: 'Settings', exact: true }).click()
  await expect(page).toHaveURL('/software/docker-ce/settings?site=site-a')
  await expect(page.getByRole('heading', { name: 'Docker CE settings', exact: true })).toBeVisible()

  const list = page.getByRole('table', { name: 'Registry credentials' })
  await expect(list.getByRole('row').filter({ hasText: 'docker.io' })).toContainText('Docker Hub')

  await page.getByRole('button', { name: 'Add credential' }).click()
  const add = page.getByRole('dialog', { name: 'Add registry credential' })
  // The dialog previews the stored registry, so a Docker Hub name typed as the website is visibly docker.io.
  await add.getByRole('textbox', { name: /Registry/ }).fill('hub.docker.com')
  await expect(add).toContainText('Saved as docker.io (Docker Hub): used for images without a registry host')
  await add.getByRole('textbox', { name: /Registry/ }).fill('https://Harbor.Lab.Local/')
  await expect(add).toContainText('Saved as harbor.lab.local: used only for images named harbor.lab.local/…')
  await add.getByRole('textbox', { name: /Username/ }).fill('robot$ci')
  await add.getByLabel(/Password or token/).fill('s3cret-token')
  await add.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(add).toHaveCount(0)
  const harbor = list.getByRole('row').filter({ hasText: 'harbor.lab.local' })
  await expect(harbor).toContainText('robot$ci')
  expect(writes).toContainEqual({ method: 'POST', body: { registry: 'https://Harbor.Lab.Local/', username: 'robot$ci', password: 's3cret-token' } })
  await expect(page.getByText('s3cret-token')).toHaveCount(0)

  await harbor.getByRole('button', { name: 'Replace credential for harbor.lab.local' }).click()
  const replace = page.getByRole('dialog', { name: 'Replace credential for harbor.lab.local' })
  await expect(replace.getByRole('textbox', { name: /Registry/ })).toHaveValue('harbor.lab.local')
  await expect(replace.getByRole('button', { name: 'Replace', exact: true })).toBeDisabled()
  await replace.getByLabel(/New password or token/).fill('rotated')
  await replace.getByRole('button', { name: 'Replace', exact: true }).click()
  await expect(replace).toHaveCount(0)
  expect(writes).toContainEqual({ method: 'PUT', body: { username: 'robot$ci', password: 'rotated' } })

  await harbor.getByRole('button', { name: 'Delete credential for harbor.lab.local' }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Delete registry credential' })
  await expect(confirm).toContainText('private images there can no longer be pulled')
  await confirm.getByRole('button', { name: 'Delete credential' }).click()
  await expect(list.getByRole('row').filter({ hasText: 'harbor.lab.local' })).toHaveCount(0)
})

test('the pull dialog says which saved credential a reference will use', async ({ page }) => {
  await installApiFixtures(page, {
    dockerAssignments: { 'srv-1': 'enabled' },
    registryCredentials: [{ registry: 'harbor.lab.local', username: 'robot' }],
  })
  await page.goto('/servers/srv-1/containers?site=site-a')
  await page.getByRole('tab', { name: 'Images', exact: true }).click()
  await page.getByRole('button', { name: 'Pull image' }).click()
  const pull = page.getByRole('dialog', { name: 'Pull image' })
  const reference = pull.getByRole('textbox', { name: /Image reference/ })

  await reference.fill('nginx:1.27')
  await expect(pull.getByRole('status')).toContainText('No saved credential for docker.io, so the pull is anonymous.')
  await expect(pull.getByRole('link', { name: 'registry credential' })).toHaveAttribute('href', '/software/docker-ce/settings?site=site-a')

  await reference.fill('harbor.lab.local/team/app:1.4')
  await expect(pull.getByRole('status')).toContainText('Signs in to harbor.lab.local as robot')
  await pull.getByRole('button', { name: 'Pull', exact: true }).click()
  await expect(page.getByText('(signed in to harbor.lab.local)')).toBeVisible()
  await expect(page.getByRole('table', { name: 'Docker images' }).getByText('harbor.lab.local/team/app:1.4')).toBeVisible()
})
