import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Managed Software installs (decision 038): the shared install-target rule keeps Servers with a
// running or unsuccessful OS deployment out of the choice, and a Server's detail page installs on
// that Server directly.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

test('the install list leaves out Servers whose OS deployment did not succeed', async ({ page }) => {
  // gpu-node-04 (srv-4) has a failed deployment; gpu-node-02 (srv-2) was installed outside swallow.
  await installApiFixtures(page, { existingServerIds: ['srv-2'] })
  await page.goto('/software?site=site-a')
  await page.getByRole('button', { name: 'Install software', exact: true }).first().click()

  const dialog = page.getByRole('dialog', { name: 'Install software' })
  const targets = dialog.getByRole('group', { name: /Target Servers/ })
  await expect(targets).toContainText('running, failed, or needs attention')
  await expect(targets.getByRole('checkbox', { name: 'gpu-node-01' })).toBeVisible()
  await expect(targets.getByRole('checkbox', { name: 'gpu-node-02' })).toBeVisible()
  await expect(targets.getByRole('checkbox', { name: 'gpu-node-03' })).toBeVisible()
  await expect(targets.getByRole('checkbox', { name: 'gpu-node-04' })).toHaveCount(0)
})

test('a Server detail page installs software on that Server only', async ({ page }) => {
  const installs: Array<Record<string, unknown>> = []
  await installApiFixtures(page, { onSoftwareInstallRequest: (body) => installs.push(body) })
  await page.goto('/servers/srv-1/summary?site=site-a')

  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: /Install software/ }).click()

  // The Server is the fixed target: named in the description, with no target list to pick from.
  const dialog = page.getByRole('dialog', { name: 'Install software' })
  await expect(dialog).toContainText('Install a single piece of host software on gpu-node-01.')
  await expect(dialog.getByRole('group', { name: /Target Servers/ })).toHaveCount(0)
  await expect(dialog.getByRole('checkbox', { name: /gpu-node-/ })).toHaveCount(0)

  await dialog.getByRole('button', { name: 'Install', exact: true }).click()
  await expect(page).toHaveURL(/\/workflows\/op-docker-reapply/)
  expect(installs).toEqual([{ kind: 'docker-ce', assignments: [{ serverId: 'srv-1', roles: [] }], spec: { enableApi: true } }])
})

test('a kind with roles still asks for the fixed Server role', async ({ page }) => {
  const installs: Array<Record<string, unknown>> = []
  await installApiFixtures(page, { onSoftwareInstallRequest: (body) => installs.push(body) })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: /Install software/ }).click()

  const dialog = page.getByRole('dialog', { name: 'Install software' })
  await dialog.getByRole('combobox', { name: 'Software', exact: true }).click()
  await page.getByRole('option', { name: 'NFS', exact: true }).click()
  const install = dialog.getByRole('button', { name: 'Install', exact: true })
  await expect(install).toBeDisabled()

  const roles = dialog.getByRole('group', { name: /Roles/ })
  await roles.getByText('client', { exact: true }).click()
  await expect(roles.getByRole('checkbox', { name: 'client' })).toBeChecked()
  await dialog.getByRole('textbox', { name: 'Source (client)' }).fill('10.0.0.9:/export/data')
  await dialog.getByRole('textbox', { name: 'Mount path (client)' }).fill('/shared')
  await install.click()
  await expect(page).toHaveURL(/\/workflows\/op-docker-reapply/)
  expect(installs).toEqual([
    { kind: 'nfs', assignments: [{ serverId: 'srv-1', roles: ['client'] }], spec: { source: '10.0.0.9:/export/data', mountPath: '/shared' } },
  ])
})

test('Install software is disabled with the reason on a Server that cannot take it', async ({ page }) => {
  await installApiFixtures(page, { lockedServerIds: ['srv-3'] })

  // srv-4's last OS deployment failed.
  await page.goto('/servers/srv-4/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  let item = page.getByRole('menuitem', { name: /Install software/ })
  await expect(item).toHaveAttribute('aria-disabled', 'true')
  await expect(item).toContainText('The last OS deployment did not succeed.')
  await page.keyboard.press('Escape')

  // srv-3 is eligible but locked, which the install API refuses.
  await page.goto('/servers/srv-3/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  item = page.getByRole('menuitem', { name: /Install software/ })
  await expect(item).toHaveAttribute('aria-disabled', 'true')
  await expect(item).toContainText('Unlock the Server before installing software')
})
