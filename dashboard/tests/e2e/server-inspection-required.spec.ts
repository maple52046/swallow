import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

// lab-afde-mi308-3: a UEFI Server whose layout was built while it booted BIOS failed every disk
// deployment, and retrying redeployed onto the same layout. The detail page now names the cause and
// offers the inspection that rebuilds the layout, and no longer promises that a retry redeploys it.
test('a Server whose layout needs a new inspection explains it and offers Inspect', async ({ page }) => {
  const actions: Array<{ serverId: string; action: string }> = []
  await installApiFixtures(page, {
    failedServerIds: ['srv-1'],
    inspectionRequiredServerIds: ['srv-1'],
    onServerActionRequest: (serverId, action) => actions.push({ serverId, action }),
  })
  await page.goto('/servers/srv-1/summary?site=site-a')

  await expect(page.getByText('Inspect this Server before a disk deployment')).toBeVisible()
  await expect(page.getByText(/its storage layout has no EFI system partition/)).toBeVisible()
  await expect(page.getByText(/A RAM deployment does not use the disk and is not affected/)).toBeVisible()
  await expect(page.getByText(/retrying the deployment that failed here ends its Step with that reason instead of redeploying/)).toBeVisible()
  await expect(page.getByText(/Swallow returns this Server to Ready and redeploys it/)).toHaveCount(0)

  await page.getByRole('button', { name: 'Inspect hardware' }).click()
  await expect.poll(() => actions).toContainEqual({ serverId: 'srv-1', action: 'inspect' })
  await expect(page.getByText('An inspection is running; this warning clears when it finishes.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Inspect hardware' })).toHaveCount(0)
})

test('Inspect from the layout warning keeps the lock gate', async ({ page }) => {
  await installApiFixtures(page, {
    failedServerIds: ['srv-1'],
    inspectionRequiredServerIds: ['srv-1'],
    lockedServerIds: ['srv-1'],
  })
  await page.goto('/servers/srv-1/summary?site=site-a')

  await expect(page.getByRole('button', { name: 'Inspect hardware' })).toBeDisabled()
  await expect(page.getByText('gpu-node-01 is locked. Unlock it before starting this action.')).toBeVisible()
})

test('a Server without the flag shows no layout warning', async ({ page }) => {
  await installApiFixtures(page, { failedServerIds: ['srv-1'] })
  await page.goto('/servers/srv-1/summary?site=site-a')

  await expect(page.getByText('Provider lifecycle failed')).toBeVisible()
  await expect(page.getByText('Inspect this Server before a disk deployment')).toHaveCount(0)
})
