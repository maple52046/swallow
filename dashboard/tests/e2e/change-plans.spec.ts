import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// User journeys for Change Plans (decision 076). Every request is served by deterministic
// fixtures: one plan to decide, one with a blocker, and one already applied.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
  })
})

test('Change Plans lists each plan with destructive actions, effects, and blockers spelled out', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/?site=site-a')
  await page.getByRole('link', { name: 'Change Plans' }).click()
  await expect(page).toHaveURL(/\/change-plans/)
  await expect(page.getByRole('heading', { name: 'Change Plans', exact: true })).toBeVisible()

  const uninstall = page.getByRole('article', { name: 'Change Plan for Platform platform-slurm' })
  await expect(uninstall).toContainText('Waiting for approval')
  const actions = uninstall.getByRole('table', { name: 'Planned actions for Platform platform-slurm' })
  await expect(actions.getByRole('row', { name: /Server srv-3/ })).toContainText('Destructive')
  await expect(uninstall).toContainText('srv-3 stops running Slurm jobs')

  // A plan with blockers names them and says why it cannot be approved.
  const blocked = page.getByRole('article', { name: 'Change Plan for Platform platform-a' }).first()
  await expect(blocked.getByRole('group', { name: 'Blockers' })).toContainText('srv-1 is locked in the provisioner')
  await expect(blocked.getByRole('button', { name: 'Approve plan' })).toBeDisabled()
  await expect(blocked).toContainText('a plan with blockers cannot be approved')

  // An applied plan links the Workflow it produced and names who approved it.
  await expect(page.getByText('Recent plans')).toBeVisible()
  await expect(page.getByRole('link', { name: 'op-remove-member' })).toBeVisible()
  await expect(page.getByText('Approved by admin')).toBeVisible()
})

test('Approving a plan confirms the consequence first and then shows it approved', async ({ page }) => {
  let approved = false
  await installApiFixtures(page)
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/change-plans/plan-uninstall/approve') approved = true
  })
  await page.goto('/change-plans')
  const plan = page.getByRole('article', { name: 'Change Plan for Platform platform-slurm' })
  await plan.getByRole('button', { name: 'Approve plan' }).click()

  const dialog = page.getByRole('alertdialog', { name: 'Approve Change Plan' })
  await expect(dialog).toContainText('including 2 destructive action(s)')
  await expect(dialog).toContainText('cannot be undone')
  expect(approved).toBe(false)
  await dialog.getByRole('button', { name: 'Approve plan' }).click()

  await expect(page.getByText('Change Plan approved')).toBeVisible()
  expect(approved).toBe(true)
  await expect(page.getByRole('heading', { name: 'Approved', exact: true })).toBeVisible()
  await expect(page.getByRole('article', { name: 'Change Plan for Platform platform-slurm' })).toContainText('Approved by admin')
})

test('Rejecting a plan moves it to the recent plans', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/change-plans')
  await page.getByRole('article', { name: 'Change Plan for Platform platform-slurm' }).getByRole('button', { name: 'Reject plan' }).click()
  const dialog = page.getByRole('alertdialog', { name: 'Reject Change Plan' })
  await expect(dialog).toContainText('Nothing in this plan')
  await dialog.getByRole('button', { name: 'Reject plan' }).click()
  await expect(page.getByText('Change Plan rejected')).toBeVisible()
  await expect(page.getByRole('article', { name: 'Change Plan for Platform platform-slurm' })).toContainText('Rejected by admin')
})
