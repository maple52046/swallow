import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', JSON.stringify('light'))
    localStorage.setItem('access_token', 'e2e-token')
  })
})

test('locked Servers are visible, filterable, and mixed Lock converges with skipped targets', async ({ page }) => {
  await installApiFixtures(page, {
    lockedServerIds: ['srv-1'],
    freePlatformCandidates: true,
  })
  await page.goto('/servers?site=site-a')

  const first = page.getByRole('row').filter({ hasText: 'gpu-node-01' })
  const second = page.getByRole('row').filter({ hasText: 'gpu-node-02' })
  await expect(first).toContainText('Locked')

  await page.getByRole('button', { name: /Filters/ }).click()
  await page.getByLabel('Filter Server lock').selectOption('locked')
  await expect(first).toBeVisible()
  await expect(second).toHaveCount(0)
  await page.getByLabel('Filter Server lock').selectOption('any')

  await page.getByLabel('Select gpu-node-01').check()
  await page.getByLabel('Select gpu-node-02').check()
  await expect(page.getByRole('button', { name: 'Deploy OS' })).toBeDisabled()
  await expect(page.getByText('Unlock every selected Server before deployment.')).toBeVisible()

  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: /^Lock/ }).click()
  const confirmation = page.getByRole('dialog', { name: 'Lock Servers' })
  await expect(confirmation).toContainText('1 Server will be locked.')
  await expect(confirmation).toContainText('1 Server already locked will be skipped.')
  await expect(confirmation).toContainText('Monitoring and diagnostics remain available')
  await confirmation.getByRole('button', { name: 'Lock', exact: true }).click()

  await expect(second).toContainText('Locked')
})

test('Unlock is the only mutation offered for a protected Server', async ({ page }) => {
  await installApiFixtures(page, { lockedServerIds: ['srv-1'] })
  await page.goto('/servers/srv-1/summary?site=site-a')

  await expect(page.getByText('Locked', { exact: true }).first()).toBeVisible()
  await page.getByRole('button', { name: 'Take action' }).click()
  await expect(page.getByRole('menuitem', { name: /^Power on/ })).toBeDisabled()
  await expect(page.getByRole('menuitem', { name: /^Release/ })).toBeDisabled()
  await page.getByRole('menuitem', { name: /^Unlock/ }).click()

  const confirmation = page.getByRole('dialog', { name: 'Unlock Servers' })
  await expect(confirmation).toContainText('It does not resume, retry, or create any work.')
  await confirmation.getByRole('button', { name: 'Unlock', exact: true }).click()
  await expect(page.getByText('Locked', { exact: true })).toHaveCount(0)
})

test('locked candidates remain visible but cannot enter OS or Platform deployment', async ({ page }) => {
  await installApiFixtures(page, {
    lockedServerIds: ['srv-1'],
    freePlatformCandidates: true,
    readyServerCount: 4,
  })
  await page.goto('/provisioning/deploy?site=site-a&serverId=srv-1&serverId=srv-2')

  await expect(page.getByText('Locked targets removed')).toBeVisible()
  await expect(page).not.toHaveURL(/serverId=srv-1/)
  await expect(page.getByLabel('Select gpu-node-01')).toBeDisabled()
  await expect(page.getByRole('grid', { name: 'Deployment targets' }).getByText('Locked')).toBeVisible()

  await installApiFixtures(page, {
    lockedServerIds: ['srv-1'],
    freePlatformCandidates: true,
  })
  await page.goto('/platforms/deploy?site=site-a')
  await page.getByLabel('Site', { exact: true }).click()
  await page.getByRole('option', { name: 'Taipei Lab', exact: true }).click()
  await page.getByLabel('Platform name').fill('locked-candidate-check')
  await page.getByRole('button', { name: 'Next' }).click()
  const lockedRow = page.getByRole('row').filter({ hasText: 'gpu-node-01' })
  await expect(lockedRow).toContainText('Locked')
  await expect(lockedRow.locator('td').nth(2)).toHaveText('-')
  await expect(page.getByLabel('Role for gpu-node-01')).toBeDisabled()
  await expect(page.getByLabel('Role for gpu-node-02')).toBeEnabled()
})
test('locked targets disable Platform repair, uninstall, and Operation retry', async ({ page }) => {
  await installApiFixtures(page, { lockedServerIds: ['srv-4'] })

  await page.goto('/platforms/platform-b?site=site-a')
  const repair = page.getByRole('button', {
    name: /Repair deployment: gpu-node-04 is locked/,
  })
  await expect(repair).toBeDisabled()

  await page.getByRole('button', { name: 'Platform actions' }).click()
  const uninstall = page.getByRole('menuitem', { name: /Uninstall platform/ })
  await expect(uninstall).toBeDisabled()
  await expect(uninstall).toContainText('Unlock it before changing this Platform.')
  await expect(page.getByRole('menuitem', { name: 'Delete platform' })).toBeEnabled()

  await page.goto('/operations/op-deploy-failed?site=site-a')
  await expect(page.getByRole('button', {
    name: /Retry: gpu-node-04 is locked/,
  })).toBeDisabled()
})
