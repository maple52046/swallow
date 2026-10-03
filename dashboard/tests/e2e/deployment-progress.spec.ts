import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Operator journey for OS deployment progress (decision 046): while a Server deploys, the detail
// page shows the provisioner's current installation stage instead of an unchanging generic
// message. Every request is served by deterministic fixtures; no backend or provisioner is
// contacted.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

test('A deploying Server shows the provider stage it is in', async ({ page }) => {
  await installApiFixtures(page, {
    changingServerIds: ['srv-1'],
    deployingStage: 'configuring_os',
    deployingStatusReason: 'Provider stage: Configuring OS (since 2026-08-27T02:40:00Z).',
  })
  await page.goto('/servers/srv-1/summary?site=site-a')

  await expect(page.getByText('Deploying an operating system')).toBeVisible()
  await expect(page.getByText('Swallow asks for attention if the provider stops reporting progress.')).toBeVisible()
  await expect(page.getByText('Provider stage: Configuring OS (since 2026-08-27T02:40:00Z).')).toBeVisible()
})
