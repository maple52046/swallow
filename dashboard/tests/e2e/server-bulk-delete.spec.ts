import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

async function openBulkDelete(page: Page) {
  await page.goto('/servers?site=site-a')
  await page.getByLabel('Select gpu-node-01').check()
  await page.getByLabel('Select gpu-node-04').check()
  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: 'Delete only', exact: true }).click()
  return page.getByRole('alertdialog', { name: 'Delete 2 servers' })
}

// Servers could only be deleted one at a time. A selection is now deleted together: the dialog
// lists the targets, warns about Platform members and Servers held by running Workflows without
// excluding them, and is confirmed by typing how many Servers will go. These journeys use Servers
// no Platform claims; a claimed Server cannot be deleted (server-platform-claim.spec.ts).
test('a selection of Servers is deleted after typing the count', async ({ page }) => {
  const deleted: string[] = []
  await installApiFixtures(page, { withoutPlatformClaims: true, onServerDeleteRequest: (serverId) => deleted.push(serverId) })
  const dialog = await openBulkDelete(page)

  await expect(dialog.getByRole('list', { name: 'Servers to delete' })).toContainText('gpu-node-01')
  await expect(dialog.getByRole('list', { name: 'Servers to delete' })).toContainText('gpu-node-04')
  // gpu-node-01 is a Platform member; both are held by unfinished Workflows in the fixture.
  await expect(dialog).toContainText('1 of these servers belongs to a Platform')
  await expect(dialog).toContainText('2 of these servers have running operations')
  const submit = dialog.getByRole('button', { name: 'Delete 2 servers' })
  await expect(submit).toBeDisabled()
  await dialog.getByLabel('Server count confirmation').fill('3')
  await expect(submit).toBeDisabled()
  await dialog.getByLabel('Server count confirmation').fill('2')
  await submit.click()

  await expect(dialog).toBeHidden()
  expect(deleted.sort()).toEqual(['srv-1', 'srv-4'])
  await expect(page.getByText('2 servers deleted', { exact: true })).toBeVisible()
  const table = page.getByRole('table', { name: 'Servers' })
  await expect(table.getByText('gpu-node-01', { exact: true })).toHaveCount(0)
  await expect(table.getByText('gpu-node-04', { exact: true })).toHaveCount(0)
  await expect(table.getByText('gpu-node-02', { exact: true })).toBeVisible()
})

test('a partly refused bulk delete reports each refusal', async ({ page }) => {
  await installApiFixtures(page, { withoutPlatformClaims: true, serverDeleteFailureIds: ['srv-4'] })
  const dialog = await openBulkDelete(page)
  await dialog.getByLabel('Server count confirmation').fill('2')
  await dialog.getByRole('button', { name: 'Delete 2 servers' }).click()

  await expect(dialog).toBeHidden()
  const results = page.getByRole('dialog').filter({ hasText: 'gpu-node-04' })
  await expect(results).toContainText('MAAS refused to delete the machine')
  await expect(page.getByText('Some servers were not deleted', { exact: true })).toBeVisible()
})

test('a bulk delete that every Server refused stays open with the reason', async ({ page }) => {
  await installApiFixtures(page, { withoutPlatformClaims: true, serverDeleteFailureIds: ['srv-1', 'srv-4'] })
  const dialog = await openBulkDelete(page)
  await dialog.getByLabel('Server count confirmation').fill('2')
  await dialog.getByRole('button', { name: 'Delete 2 servers' }).click()

  await expect(dialog).toContainText('None of the 2 servers could be deleted')
  await expect(dialog).toContainText('MAAS refused to delete the machine')
})
