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
    localStorage.setItem('swallow.appearance', 'light')
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
  await confirmation.getByLabel('Erase disks before release').locator('..').click()
  await confirmation.getByLabel('Use secure erase when supported').locator('..').click()
  await confirmation.getByLabel('Use quick erase if secure erase is unavailable').locator('..').click()
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
  const updating = page.getByText(/Updating servers/)
  await expect(updating).toBeVisible()
  // Releasing is a generic OS provisioning state: the row names it while the provider works,
  // then shows Ready once the Server is back in the pool.
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' })
  await expect(row.getByText('Releasing', { exact: true })).toBeVisible()
  // The in-progress cue is a spinner after the label inside the same badge.
  await expect(row.getByText('Releasing', { exact: true }).locator('.sw-progress-spinner')).toBeVisible()
  await expect(row.getByText(/^Running for \d+s$/)).toBeVisible()
  await expect(row.getByText('Ready', { exact: true })).toBeVisible({ timeout: 7_000 })
  await expect(row.getByText('Releasing', { exact: true })).toHaveCount(0)
  await expect(row.locator('.sw-progress-spinner')).toHaveCount(0)
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
    const match = path.match(/^\/api\/v1\/workflows\/([^/]+)\/cancel$/)
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
  await confirmation.getByLabel('Cancel running operations before releasing').locator('..').click()
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
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' }).first()
  await expect(row).toContainText('192.168.40.21')
  const power = row.getByRole('button', { name: 'Power actions for gpu-node-01; RAM deployment' })
  await expect(power.getByRole('img', { name: 'RAM deployment' })).toBeVisible()
  await expect(row.getByRole('img', { name: 'Ephemeral deployment' })).toHaveCount(0)
  await row.getByRole('button', { name: 'Show details for gpu-node-01' }).click()
  const details = page.getByRole('row').filter({ hasText: 'Root filesystem' })
  await expect(details).toContainText('RAM (ephemeral)')

  await page.getByLabel('Select gpu-node-01').check()
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')
  const confirmation = page.getByRole('dialog', { name: 'Release server', exact: true })
  await confirmation.getByLabel('Remove static IP bindings after release').locator('..').click()
  await confirmation.getByRole('button', { name: 'Release server', exact: true }).click()

  // Stay on the list and converge in place: the Deployment cell ends at Ready, and the provider's
  // OS qualifiers (RAM deployment, static address) disappear with the released OS.
  await expect(page).toHaveURL(/\/servers(\?|$)/)
  await expect(row.getByText('Ready', { exact: true })).toBeVisible({ timeout: 7_000 })
  await expect(row).not.toContainText('192.168.40.21', { timeout: 7_000 })
  await expect(details).not.toContainText('RAM (ephemeral)')
  await expect(row.getByRole('img', { name: 'RAM deployment' })).toHaveCount(0)
  await expect(page.getByText(/Updating servers/)).toBeHidden({ timeout: 7_000 })
})

test('Server list follows a released server that starts deployed until it converges', async ({ page }) => {
  // Model the real durable release: the Server is not flipped to "releasing" at accept time,
  // so it is not in an active provisioning axis. The list must still follow it to ready.
  await installApiFixtures(page, {
    deferReleaseProjection: true,
    releaseConvergesAfterRefreshes: 2,
  })
  await page.goto('/servers?site=site-a')
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' })
  await expect(row.getByText('Ubuntu 24.04 LTS', { exact: true })).toBeVisible()

  await page.getByLabel('Select gpu-node-01').check()
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')
  const confirmation = page.getByRole('dialog', { name: 'Release server', exact: true })
  await confirmation.getByRole('button', { name: 'Release server', exact: true }).click()

  await expect(page).toHaveURL(/\/servers(\?|$)/)
  // The Server kept its installed-image fact at accept time, so only the
  // follow-after-release polling can drive the row to its current Ready state.
  await expect(row.getByText('Ready', { exact: true })).toBeVisible({ timeout: 10_000 })
  await expect(row.getByText('Ubuntu 24.04 LTS', { exact: true })).toHaveCount(0)
})

test('Server detail follows Release until the projection becomes ready', async ({ page }) => {
  await installApiFixtures(page, { releaseConvergesAfterRefreshes: 2 })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await chooseMenuItem(page, 'Release')
  await confirmRelease(page, 1)

  // Releasing stays on the Server detail page and converges in place. The transient
  // "Updating..." flash is too brief to assert reliably when convergence is fast, so verify
  // the observable outcome: the detail page reaches ready on its own (via the follow poll).
  await expect(page).toHaveURL(/\/servers\/srv-1\/summary/)
  await expect(page.getByText('ready', { exact: true }).first()).toBeVisible({ timeout: 7_000 })
  await expect(page.getByText('Updating...', { exact: true })).toBeHidden({ timeout: 7_000 })
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
  await release.getByLabel('Remove static IP bindings after release').locator('..').click()
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
  const tasks = page.getByRole('table', { name: 'Provisioning tasks' })
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
  await page.goto(`/workflows/${operationId}?site=site-a`)
  await expect(page.getByText('partially succeeded', { exact: true })).toBeVisible()
  const results = page.getByRole('table', { name: 'Workflow steps' })
  await expect(results.getByRole('row').filter({ hasText: 'Release srv-1' })).toContainText('succeeded')
  const failed = results.getByRole('row').filter({ hasText: 'Release srv-2' })
  await expect(failed).toContainText('failed')
  await expect(failed).toContainText('Machine cannot be released while a hosted VM is running.')
  await expect(page.locator('section.sw-operation-debugger')).toContainText('provider_rejected')
  await expect(page.getByText(/e\.g\./)).toHaveCount(0)

  await page.goto('/servers/srv-2/activity?site=site-a')
  const related = page.getByRole('table', { name: 'Related Operations' })
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
  await page.goto(`/workflows/${operationId}?site=site-a`)
  await expect(page.getByText(/req-op-release-os-/)).toBeVisible()
  await expect(page.getByText('MAAS refused the request: Machine cannot be released while a hosted VM is running.').first()).toBeVisible()

  await page.goto('/servers/srv-1/activity?site=site-a')
  await expect(page.getByRole('table', { name: 'Provider events' })).toContainText('Started releasing machine.')
  const related = page.getByRole('table', { name: 'Related Operations' })
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
  await page.goto(`/workflows/${operationId}?site=site-a`)
  await expect(page.getByText('requires attention', { exact: true }).first()).toBeVisible()
  await expect(page.locator('section.sw-operation-debugger')).toContainText('provider_unavailable')
  await page.getByRole('button', { name: 'Retry Release srv-1' }).click()
  const step = page.getByRole('table', { name: 'Workflow steps' }).getByRole('row').filter({ hasText: 'Release srv-1' })
  await expect(step).toContainText('pending')
  await expect(step).toContainText('2')

  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  const confirmation = page.getByRole('dialog', { name: 'Cancel Operation' })
  await expect(confirmation).toContainText('Completed work is not rolled back')
  await confirmation.getByRole('button', { name: 'Cancel Operation', exact: true }).click()
  await expect(page.getByText('canceling', { exact: true })).toBeVisible()
})
