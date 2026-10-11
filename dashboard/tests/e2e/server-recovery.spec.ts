import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

test('a failed Server offers Recover and Release, but Mark fixed is gated to Broken', async ({ page }) => {
  const recovered: Array<{ serverId: string; body: Record<string, unknown> | null }> = []
  await installApiFixtures(page, {
    failedServerIds: ['srv-1'],
    onServerRecoverRequest: (serverId, body) => recovered.push({ serverId, body }),
  })
  await page.goto('/servers?site=site-a')

  // `failed` is a generic OS provisioning state, so the list shows it instead of a stale result.
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' })
  await expect(row.getByText('Failed', { exact: true })).toBeVisible()

  await page.getByLabel('Select gpu-node-01').check()
  await page.getByRole('button', { name: 'Take action' }).click()
  // Recover and Release are the primary recovery verbs and are enabled for a failed Server.
  await expect(page.getByRole('menuitem', { name: /^Recover/ })).toBeEnabled()
  await expect(page.getByRole('menuitem', { name: /^Release/ })).toBeEnabled()
  // Mark fixed lives under State & recovery and is disabled with a Swallow reason.
  await page.getByRole('menuitem', { name: 'State & recovery', exact: true }).hover()
  await expect(page.getByRole('menuitem', { name: /^Mark fixed/ })).toBeDisabled()

  await page.getByRole('menuitem', { name: /^Recover/ }).click()
  await expect.poll(() => recovered.map((entry) => entry.serverId)).toContain('srv-1')
  await expect(page).toHaveURL(/\/servers(\?|$)/)
})

test('the detail page explains recovery for failed and rescue Servers', async ({ page }) => {
  await installApiFixtures(page, { failedServerIds: ['srv-1'] })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await expect(page.getByText('Provider lifecycle failed')).toBeVisible()
  await expect(page.getByText(/Mark fixed does not apply to a failed Server/)).toBeVisible()

  await installApiFixtures(page, { rescueServerIds: ['srv-1'] })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await expect(page.getByText('Server is in rescue mode')).toBeVisible()
  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: 'State & recovery', exact: true }).hover()
  // Rescue exit carries an always-on explanation that it does not reach Ready.
  await expect(page.getByText(/Restores the pre-rescue state/)).toBeVisible()
})

test('a Releasing Server links its detail page to the Workflow following the release', async ({ page }) => {
  await installApiFixtures(page, {
    providerWorkServerIds: { 'srv-1': 'releasing' },
    releaseWorkflows: { 'srv-1': 'waiting_external' },
  })
  await page.goto('/servers/srv-1/summary?site=site-a')

  const banner = page.getByRole('status').filter({ hasText: 'Releasing the Server' })
  await expect(banner).toContainText('Release 1 Server(s) is following the provider')
  await expect(banner).toContainText('Swallow cannot change the state of a Server the provider is releasing')
  await expect(banner.getByRole('link', { name: 'View workflow' })).toHaveAttribute('href', '/workflows/op-release-srv-1?site=site-a')
  await expect(banner.getByRole('link', { name: 'View Related Operations' }))
    .toHaveAttribute('href', '/servers/srv-1/activity?site=site-a#activity-related-operations')

  // The disabled lifecycle actions name the running provider work, not their source states.
  await page.getByRole('button', { name: 'Take action' }).click()
  const release = page.getByRole('menuitem', { name: /^Release/ })
  await expect(release).toBeDisabled()
  await expect(release).toContainText('gpu-node-01 is releasing. Wait for the provider to finish before starting this action.')
})

test('a Releasing Server whose Workflow stopped following it points to provider events', async ({ page }) => {
  await installApiFixtures(page, {
    providerWorkServerIds: { 'srv-1': 'releasing' },
    releaseWorkflows: { 'srv-1': 'requires_attention' },
  })
  await page.goto('/servers/srv-1/summary?site=site-a')
  const attention = page.getByRole('status').filter({ hasText: 'Release needs attention' })
  await expect(attention).toContainText('Timed out waiting for the Server to return to Ready.')
  await expect(attention.getByRole('link', { name: 'View workflow' })).toHaveAttribute('href', '/workflows/op-release-srv-1?site=site-a')
  await expect(attention.getByRole('link', { name: 'View provider events' }))
    .toHaveAttribute('href', '/servers/srv-1/activity?site=site-a#activity-provider-events')

  await installApiFixtures(page, {
    providerWorkServerIds: { 'srv-1': 'releasing' },
    releaseWorkflows: { 'srv-1': 'canceled' },
  })
  await page.goto('/servers/srv-1/summary?site=site-a')
  const ended = page.getByRole('status').filter({ hasText: 'No Workflow is following this release' })
  await expect(ended).toContainText('ended (canceled) while the provider still reports Releasing')
  await expect(ended.getByRole('link', { name: 'View provider events' })).toBeVisible()
})

test('a release with no Swallow Workflow says so on the banner and in Activity', async ({ page }) => {
  await installApiFixtures(page, {
    providerWorkServerIds: { 'srv-1': 'releasing' },
    releaseWorkflows: { 'srv-1': null },
  })
  await page.goto('/servers/srv-1/summary?site=site-a')
  const banner = page.getByRole('status').filter({ hasText: 'The provider is releasing this Server' })
  await expect(banner).toContainText('No Swallow Workflow covers this release')
  await expect(banner.getByRole('link', { name: 'View workflow' })).toHaveCount(0)

  await banner.getByRole('link', { name: 'View provider events' }).click()
  await expect(page).toHaveURL('/servers/srv-1/activity?site=site-a#activity-provider-events')
  await expect(page.locator('#activity-provisioning-tasks'))
    .toContainText('This section only tracks the static IP cleanup a Release can opt into')
  await expect(page.locator('#activity-related-operations'))
    .toContainText('No Swallow Workflow covers the current release')
})

test('a broken Server shows Broken and keeps recovery actions', async ({ page }) => {
  const recovered: string[] = []
  await installApiFixtures(page, {
    brokenServerIds: ['srv-1'],
    onServerRecoverRequest: (serverId) => recovered.push(serverId),
  })
  await page.goto('/servers?site=site-a')
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' })
  await expect(row.getByText('Broken', { exact: true })).toBeVisible()

  await page.getByLabel('Select gpu-node-01').check()
  await page.getByRole('button', { name: 'Take action' }).click()
  // Mark fixed is valid for a Broken Server, and so is Recover.
  await page.getByRole('menuitem', { name: 'State & recovery', exact: true }).hover()
  await expect(page.getByRole('menuitem', { name: /^Mark fixed/ })).toBeEnabled()
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: /^Recover/ }).click()
  await expect.poll(() => recovered).toContain('srv-1')
})
