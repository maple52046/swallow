import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Platform Claim (decision 067): a Server a Platform's deployment targets is released through that
// Platform's uninstall, so its direct Release and its Delete are disabled. The menu says so once,
// as the label of the group holding those two, the same refusals the API answers with 409.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

/** Asserts the claim note groups a disabled Release and Delete only that carry no reason of their own. */
async function expectClaimBlocked(page: Page, note: string) {
  const group = page.getByRole('group', { name: note })
  await expect(group).toBeVisible()
  for (const label of ['Release', 'Delete only']) {
    const item = group.getByRole('menuitem', { name: label, exact: true })
    await expect(item).toBeDisabled()
    await expect(item).toHaveText(label)
  }
}

test('Release and Delete name the claiming Platform once on list rows, in bulk, and on detail', async ({ page }) => {
  const releases: string[] = []
  const deletes: string[] = []
  // A fifth Server no Platform targets stays releasable beside the claimed ones.
  await installApiFixtures(page, {
    fleetSize: 5,
    onServerReleaseRequest: (serverId) => releases.push(serverId),
    onServerDeleteRequest: (serverId) => deletes.push(serverId),
  })
  await page.goto('/servers?site=site-a')

  await page.getByRole('button', { name: 'Actions for gpu-node-01', exact: true }).click()
  await expectClaimBlocked(page, 'This server belongs to Platform production-k0s and cannot be released or deleted.')
  await page.keyboard.press('Escape')

  await page.getByLabel('Select gpu-node-01').check()
  await page.getByLabel('Select gpu-node-02').check()
  await page.getByRole('button', { name: 'Take action' }).click()
  await expectClaimBlocked(page, '2 selected servers belong to Platform production-k0s and cannot be released or deleted.')
  await page.keyboard.press('Escape')

  // One claimed Server keeps the whole selection from releasing, as the API refuses the batch.
  await page.getByLabel('Select gpu-node-02').uncheck()
  await page.getByLabel('Select compute-node-005').check()
  await page.getByRole('button', { name: 'Take action' }).click()
  await expectClaimBlocked(page, 'gpu-node-01 belongs to Platform production-k0s and cannot be released or deleted.')
  await page.keyboard.press('Escape')

  // Without a claim there is no note, and Release goes ahead.
  await page.getByLabel('Select gpu-node-01').uncheck()
  await page.getByRole('button', { name: 'Take action' }).click()
  await expect(page.getByRole('group', { name: /cannot be released or deleted/ })).toHaveCount(0)
  await page.getByRole('menuitem', { name: 'Release', exact: true }).click()
  const confirmation = page.getByRole('dialog', { name: 'Release server', exact: true })
  await confirmation.getByRole('button', { name: 'Release server', exact: true }).click()
  await expect.poll(() => releases).toEqual(['srv-5'])

  // A failed deployment still claims its target until the Platform is uninstalled or deleted.
  await page.goto('/servers/srv-4/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await expectClaimBlocked(page, 'This server belongs to Platform edge-staging and cannot be released or deleted.')
  expect(deletes).toEqual([])
})

test('a Platform\'s failed Server can be recovered but not released', async ({ page }) => {
  const recovered: string[] = []
  await installApiFixtures(page, {
    failedServerIds: ['srv-1'],
    onServerRecoverRequest: (serverId) => recovered.push(serverId),
  })
  await page.goto('/servers?site=site-a')
  await page.getByLabel('Select gpu-node-01').check()
  await page.getByRole('button', { name: 'Take action' }).click()

  await expectClaimBlocked(page, 'This server belongs to Platform production-k0s and cannot be released or deleted.')
  // Recover is refused only for a deployed claimed Server, which it would release.
  await page.getByRole('menuitem', { name: /^Recover/ }).click()
  await expect.poll(() => recovered).toEqual(['srv-1'])
})

test('deleting a Platform record frees its Servers for Release and Delete', async ({ page }) => {
  const deletes: string[] = []
  await installApiFixtures(page, { onServerDeleteRequest: (serverId) => deletes.push(serverId) })
  await page.goto('/platforms/platform-b?site=site-a')
  await page.getByRole('button', { name: 'More removal options' }).click()
  await page.getByRole('menuitem', { name: 'Delete only…' }).click()
  const dialog = page.getByRole('dialog', { name: 'Delete platform record' })
  await dialog.getByLabel('Platform name confirmation').fill('edge-staging')
  await dialog.getByRole('button', { name: 'Delete record' }).click()
  await expect(page.getByText('Platform record deleted')).toBeVisible()

  await page.goto('/servers/srv-4/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await expect(page.getByRole('group', { name: /cannot be released or deleted/ })).toHaveCount(0)
  await expect(page.getByRole('menuitem', { name: 'Release', exact: true })).toBeEnabled()
  await page.getByRole('menuitem', { name: 'Delete only', exact: true }).click()
  const confirmation = page.getByRole('alertdialog', { name: 'Delete server' })
  await confirmation.getByLabel('Server name confirmation').fill('gpu-node-04')
  await confirmation.getByRole('button', { name: 'Delete server' }).click()
  await expect.poll(() => deletes).toEqual(['srv-4'])
  await expect(page).toHaveURL('/servers?site=site-a')
})
