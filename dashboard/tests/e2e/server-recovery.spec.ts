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

  // Provider lifecycle remains an internal action-policy input; the list shows current OS deployment.
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' })
  await expect(row.getByText('Failed', { exact: true })).toHaveCount(0)
  await expect(row.getByText('Not deployed', { exact: true })).toBeVisible()

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

test('a broken Server keeps recovery actions without exposing provider state', async ({ page }) => {
  const recovered: string[] = []
  await installApiFixtures(page, {
    brokenServerIds: ['srv-1'],
    onServerRecoverRequest: (serverId) => recovered.push(serverId),
  })
  await page.goto('/servers?site=site-a')
  const row = page.getByRole('row').filter({ hasText: 'gpu-node-01' })
  await expect(row.getByText('Broken', { exact: true })).toHaveCount(0)
  await expect(row.getByText('Not deployed', { exact: true })).toBeVisible()

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
