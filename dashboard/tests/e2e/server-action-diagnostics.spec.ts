import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

async function chooseMenuItem(page: import('playwright/test').Page, name: string) {
  await page.getByRole('menuitem', { name, exact: true }).click()
}

async function confirmRelease(page: import('playwright/test').Page, targetCount: number) {
  const title = targetCount > 1 ? `Release ${targetCount} servers` : 'Release server'
  const confirmation = page.getByRole('dialog', { name: title, exact: true })
  await expect(confirmation).toBeVisible()
  await confirmation.getByRole('button', { name: title, exact: true }).click()
}

test.beforeEach(async ({ page }) => {
  await installApiFixtures(page, { serverActionFailureIds: ['srv-2'] })
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', JSON.stringify('light'))
    localStorage.setItem('access_token', 'e2e-token')
  })
})

test('Release requires confirmation and submits MAAS disk-erasure options', async ({ page }) => {
  const requests: Array<{ serverId: string; body: Record<string, unknown> | null }> = []
  const refreshes: string[] = []
  await installApiFixtures(page, {
    onServerReleaseRequest: (serverId, body) => requests.push({ serverId, body }),
    onServerRefreshRequest: (serverId) => refreshes.push(serverId),
  })
  await page.goto('/servers?site=site-a')
  await page.getByLabel('Select gpu-node-01').check()
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')

  let confirmation = page.getByRole('dialog', { name: 'Release server', exact: true })
  await expect(confirmation).toContainText('Disk contents will be retained')
  await expect.poll(() => requests.length).toBe(0)
  await confirmation.getByRole('button', { name: 'Cancel' }).click()
  await expect(confirmation).toBeHidden()
  await expect.poll(() => requests.length).toBe(0)

  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')
  confirmation = page.getByRole('dialog', { name: 'Release server', exact: true })
  await confirmation.getByLabel('Erase disks before release').check()
  await confirmation.getByLabel('Use secure erase when supported').check()
  await confirmation.getByLabel('Use quick erase if secure erase is unavailable').check()
  await confirmation.getByLabel('Provider event comment (optional)').fill('retire from test pool')
  await expect(confirmation).toContainText('Secure erase is preferred')
  await confirmation.getByRole('button', { name: 'Release server', exact: true }).click()

  await expect.poll(() => requests.length).toBe(1)
  expect(requests[0]).toEqual({
    serverId: 'srv-1',
    body: {
      erase: true,
      secureErase: true,
      quickErase: true,
      comment: 'retire from test pool',
      unbindStaticIPs: false,
    },
  })
  const updating = page.getByText('Updating released Servers...', { exact: true })
  await expect(updating).toBeVisible()
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' })
  await expect(row).toContainText(/releasing/i)
  await expect(row).toContainText(/ready/i, { timeout: 7_000 })
  await expect.poll(() => refreshes.filter((serverId) => serverId === 'srv-1').length).toBeGreaterThanOrEqual(2)
  await expect(updating).toBeHidden({ timeout: 7_000 })
})

test('Server list waits for release cleanup and refreshes addresses and Ephemeral state', async ({ page }) => {
  await installApiFixtures(page, {
    staticNetworkServerIds: ['srv-1'],
    ephemeralServerIds: ['srv-1'],
    releaseConvergesAfterRefreshes: 1,
  })
  await page.goto('/servers?site=site-a')
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' })
  await expect(row).toContainText('192.168.40.21')
  await expect(row).toContainText('Ephemeral')

  await page.getByLabel('Select gpu-node-01').check()
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')
  const confirmation = page.getByRole('dialog', { name: 'Release server', exact: true })
  await confirmation.getByLabel('Remove static IP bindings after release').check()
  await confirmation.getByRole('button', { name: 'Release server', exact: true }).click()

  const updating = page.getByText('Updating released Servers...', { exact: true })
  await expect(updating).toBeVisible()
  await expect(row).toContainText('ready', { timeout: 7_000 })
  await expect(row).not.toContainText('192.168.40.21', { timeout: 7_000 })
  await expect(row).not.toContainText('Ephemeral')
  await expect(row).not.toContainText('in memory')
  await expect(updating).toBeHidden({ timeout: 7_000 })
})

test('Server detail follows Release until the projection becomes ready', async ({ page }) => {
  await installApiFixtures(page, { releaseConvergesAfterRefreshes: 2 })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')
  await confirmRelease(page, 1)

  await expect(page.getByText('Updating...', { exact: true })).toBeVisible()
  await expect(page.getByText('ready', { exact: true }).first()).toBeVisible({ timeout: 7_000 })
  await expect(page.getByText('Updating...', { exact: true })).toBeHidden()
})

test('Release static cleanup is opt-in, durable, and retries cleanup without another Release', async ({ page }) => {
  const releases: Array<Record<string, unknown> | null> = []
  await installApiFixtures(page, {
    releaseCleanupFails: true,
    onServerReleaseRequest: (_serverId, body) => releases.push(body),
  })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')

  const release = page.getByRole('dialog', { name: 'Release server', exact: true })
  await expect(release.getByLabel('Remove static IP bindings after release')).not.toBeChecked()
  await release.getByLabel('Remove static IP bindings after release').check()
  await expect(release).toContainText('DHCP, provider-managed, Link only, and later changes are preserved.')
  await release.getByRole('button', { name: 'Release server', exact: true }).click()
  expect(releases).toEqual([{
    erase: false,
    secureErase: false,
    quickErase: false,
    unbindStaticIPs: true,
  }])

  await page.getByRole('tab', { name: 'Activity' }).click()
  const tasks = page.getByRole('grid', { name: 'Provisioning tasks' })
  await expect(tasks).toContainText('MAAS refused to unlink the captured Static address.')
  await tasks.getByRole('button', { name: 'Retry cleanup' }).click()
  await expect(tasks).toContainText('pending')
  expect(releases).toHaveLength(1)
})

test('Server list keeps complete partial action diagnostics after the dialog closes', async ({ page }) => {
  await page.goto('/servers?site=site-a')
  await page.getByLabel('Select gpu-node-01').check()
  await page.getByLabel('Select gpu-node-02').check()
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')
  await confirmRelease(page, 2)

  const dialog = page.getByRole('dialog', { name: 'Release result' })
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('1 accepted, 1 failed across 2 targets.')
  const results = dialog.getByRole('grid', { name: 'Release target results' })
  await expect(results.getByRole('row').filter({ hasText: 'gpu-node-01' })).toContainText('Accepted')
  const failed = results.getByRole('row').filter({ hasText: 'gpu-node-02' })
  await expect(failed).toContainText('Failed')
  await expect(failed).toContainText('validation_error')
  await expect(failed).toContainText('400')
  await expect(failed).toContainText('req-release-srv-2')
  await expect(failed).toContainText('Machine cannot be released while a hosted VM is running.')
  await expect(page.getByText(/e\.g\./)).toHaveCount(0)

  await dialog.getByRole('button', { name: 'Done' }).click()
  const summary = page.locator('#swallow-main-content').getByRole('heading', { name: /Release partially accepted/ })
  await expect(summary).toBeVisible()
  await page.getByRole('button', { name: 'View details' }).click()
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: 'Done' }).click()

  await page.goto('/servers/srv-2/activity?site=site-a')
  const sessionActions = page.getByRole('grid', { name: 'Current browser session Server actions' })
  await expect(sessionActions).toContainText('req-release-srv-2')
  await expect(sessionActions).toContainText('Machine cannot be released while a hosted VM is running.')
})

test('Server detail failure exposes and reopens its correlated provider error', async ({ page }) => {
  await installApiFixtures(page, { serverActionFailureIds: ['srv-1'] })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')
  await confirmRelease(page, 1)

  const dialog = page.getByRole('dialog', { name: 'Release result' })
  await expect(dialog).toContainText('0 accepted, 1 failed across 1 target.')
  await expect(dialog).toContainText('req-release-srv-1')
  await expect(dialog).toContainText('MAAS refused the request: Machine cannot be released while a hosted VM is running.')

  await dialog.getByRole('button', { name: 'Done' }).click()
  await page.getByRole('tab', { name: 'Activity' }).click()

  const sessionActions = page.getByRole('grid', { name: 'Current browser session Server actions' })
  await expect(sessionActions).toContainText('req-release-srv-1')
  await sessionActions.getByRole('button', { name: 'View details' }).click()
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: 'Done' }).click()

  await expect(page.getByRole('grid', { name: 'Provider events' })).toContainText('Started releasing machine.')
  const related = page.getByRole('grid', { name: 'Related Operations' })
  await expect(related).toContainText('Deploy production k0s cluster')
  await expect(related).not.toContainText('Install GPU exporters')
})
