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
  // The release must not leave the Server list; it converges in place instead of
  // navigating to the Operation.
  await expect(page).toHaveURL(/\/servers(\?|$)/)
  const updating = page.getByText('Updating active Servers...', { exact: true })
  await expect(updating).toBeVisible()
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' })
  await expect(row).toContainText(/releasing/i)
  await expect(row).toContainText(/ready/i, { timeout: 7_000 })
  await expect.poll(() => refreshes.filter((serverId) => serverId === 'srv-1').length).toBeGreaterThanOrEqual(2)
  await expect(updating).toBeHidden({ timeout: 7_000 })
})

test('Release detects running operations and cancels them before releasing', async ({ page }) => {
  const releases: Array<{ serverId: string; body: Record<string, unknown> | null }> = []
  const cancels: string[] = []
  await installApiFixtures(page, {
    cancelMarksTerminal: true,
    onServerReleaseRequest: (serverId, body) => releases.push({ serverId, body }),
  })
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    const match = path.match(/^\/api\/v1\/operations\/([^/]+)\/cancel$/)
    if (match && request.method() === 'POST') cancels.push(match[1])
  })

  await page.goto('/servers?site=site-a')
  await page.getByLabel('Select gpu-node-01').check()
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')

  const confirmation = page.getByRole('dialog', { name: 'Release server', exact: true })
  await expect(confirmation).toContainText('This server has a running operation')
  await expect(confirmation).toContainText('Deploy production k0s platform')

  // Leaving the box unchecked would release straight away; opting in must cancel the
  // blocking Operation first and only then submit the release.
  await confirmation.getByLabel('Cancel running operations before releasing').check()
  await confirmation.getByRole('button', { name: 'Release server', exact: true }).click()

  await expect.poll(() => releases.map((entry) => entry.serverId)).toContain('srv-1')
  expect(cancels).toContain('op-running')
  // Releasing stays on the Server list rather than navigating to the Operation.
  await expect(page).toHaveURL(/\/servers(\?|$)/)
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

  // Stay on the list and converge in place. With a single-refresh convergence the transient
  // "Updating active Servers..." flash is too brief to assert reliably, so verify the
  // observable outcome: the row refreshes to ready and drops the released address/Ephemeral.
  await expect(page).toHaveURL(/\/servers(\?|$)/)
  await expect(row).toContainText('ready', { timeout: 7_000 })
  await expect(row).not.toContainText('192.168.40.21', { timeout: 7_000 })
  await expect(row).not.toContainText('Ephemeral')
  await expect(row).not.toContainText('in memory')
  await expect(page.getByText('Updating active Servers...', { exact: true })).toBeHidden({ timeout: 7_000 })
})

test('Server detail follows Release until the projection becomes ready', async ({ page }) => {
  await installApiFixtures(page, { releaseConvergesAfterRefreshes: 2 })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')
  await confirmRelease(page, 1)

  // Releasing stays on the Server detail page and converges in place.
  await expect(page).toHaveURL(/\/servers\/srv-1\/summary/)
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
  await expect(page).toHaveURL(/\/servers\/srv-1\/summary/)
  await expect.poll(() => releases).toEqual([{
    erase: false,
    secureErase: false,
    quickErase: false,
    unbindStaticIPs: true,
  }])

  await page.goto('/servers/srv-1/activity?site=site-a')
  await page.getByRole('tab', { name: 'Activity' }).click()
  const tasks = page.getByRole('grid', { name: 'Provisioning tasks' })
  await expect(tasks).toContainText('MAAS refused to unlink the captured Static address.')
  await tasks.getByRole('button', { name: 'Retry cleanup' }).click()
  await expect(tasks).toContainText('pending')
  expect(releases).toHaveLength(1)
})

test('Release Operation keeps complete per-Step partial diagnostics', async ({ page }) => {
  await page.goto('/servers?site=site-a')
  await page.getByLabel('Select gpu-node-01').check()
  await page.getByLabel('Select gpu-node-02').check()
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')
  const releaseResponse = page.waitForResponse(
    (response) =>
      response.url().includes('/provisioning/release-operations') &&
      response.request().method() === 'POST',
  )
  await confirmRelease(page, 2)
  const { operationId } = (await (await releaseResponse).json()) as { operationId: string }

  // Releasing stays on the Server list; open the created Operation explicitly to inspect
  // its per-Step diagnostics.
  await expect(page).toHaveURL(/\/servers(\?|$)/)
  await page.goto(`/operations/${operationId}?site=site-a`)
  await expect(page.getByText('partially succeeded', { exact: true })).toBeVisible()
  const results = page.getByRole('grid', { name: 'Operation Steps' })
  await expect(results.getByRole('row').filter({ hasText: 'Release srv-1' })).toContainText('succeeded')
  const failed = results.getByRole('row').filter({ hasText: 'Release srv-2' })
  await expect(failed).toContainText('failed')
  await expect(failed).toContainText('Machine cannot be released while a hosted VM is running.')
  await expect(page.locator('section.sw-operation-debugger')).toContainText('provider_rejected')
  await expect(page.getByText(/e\.g\./)).toHaveCount(0)

  await page.goto('/servers/srv-2/activity?site=site-a')
  const related = page.getByRole('grid', { name: 'Related Operations' })
  await expect(related).toContainText('Release 2 Server(s)')
  await expect(related).toContainText('partially succeeded')
})

test('Server detail failure exposes and reopens its correlated provider error', async ({ page }) => {
  await installApiFixtures(page, { serverActionFailureIds: ['srv-1'] })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')
  const releaseResponse = page.waitForResponse(
    (response) =>
      response.url().includes('/provisioning/release-operations') &&
      response.request().method() === 'POST',
  )
  await confirmRelease(page, 1)
  const { operationId } = (await (await releaseResponse).json()) as { operationId: string }

  // Releasing stays on the Server detail page; open the created Operation to inspect its
  // correlated provider error.
  await expect(page).toHaveURL(/\/servers\/srv-1\/summary/)
  await page.goto(`/operations/${operationId}?site=site-a`)
  await expect(page.getByText(/req-op-release-os-/)).toBeVisible()
  await expect(page.getByText('MAAS refused the request: Machine cannot be released while a hosted VM is running.').first()).toBeVisible()

  await page.goto('/servers/srv-1/activity?site=site-a')
  await expect(page.getByRole('grid', { name: 'Provider events' })).toContainText('Started releasing machine.')
  const related = page.getByRole('grid', { name: 'Related Operations' })
  await expect(related).toContainText('Release 1 Server(s)')
  await expect(related).toContainText('failed')
})

test('Durable Operation retries one safe Step and accepts cancellation', async ({ page }) => {
  await installApiFixtures(page, {
    serverActionFailureIds: ['srv-1'],
    providerFailureRetryable: true,
  })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')
  const releaseResponse = page.waitForResponse(
    (response) =>
      response.url().includes('/provisioning/release-operations') &&
      response.request().method() === 'POST',
  )
  await confirmRelease(page, 1)
  const { operationId } = (await (await releaseResponse).json()) as { operationId: string }

  // Releasing stays on the Server detail page; open the created Operation to retry and
  // cancel it.
  await expect(page).toHaveURL(/\/servers\/srv-1\/summary/)
  await page.goto(`/operations/${operationId}?site=site-a`)
  await expect(page.getByText('requires attention', { exact: true }).first()).toBeVisible()
  await expect(page.locator('section.sw-operation-debugger')).toContainText('provider_unavailable')
  await page.getByRole('button', { name: 'Retry Release srv-1' }).click()
  const step = page.getByRole('grid', { name: 'Operation Steps' }).getByRole('row').filter({ hasText: 'Release srv-1' })
  await expect(step).toContainText('pending')
  await expect(step).toContainText('2')

  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  const confirmation = page.getByRole('dialog', { name: 'Cancel Operation' })
  await expect(confirmation).toContainText('Completed side effects are preserved.')
  await confirmation.getByRole('button', { name: 'Cancel Operation', exact: true }).click()
  await expect(page.getByText('canceling', { exact: true })).toBeVisible()
})
