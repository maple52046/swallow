import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// User journeys for Change Plans (decision 076). Every request is served by deterministic
// fixtures: one plan to decide, one with a blocker, one already applied, and an expired one whose
// Platform and Server no longer exist.

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

  const uninstall = page.getByRole('article', { name: 'Change Plan for Platform research-slurm' })
  await expect(uninstall).toContainText('Waiting for approval')
  const actions = uninstall.getByRole('table', { name: 'Planned actions for Platform research-slurm' })
  await expect(actions.getByRole('row', { name: /Server gpu-node-03/ })).toContainText('Destructive')
  await expect(uninstall).toContainText('srv-3 stops running Slurm jobs')

  // A plan with blockers names them and says why it cannot be approved.
  const blocked = page.getByRole('article', { name: 'Change Plan for Platform production-k0s' }).first()
  await expect(blocked.getByRole('group', { name: 'Blockers' })).toContainText('srv-1 is locked in the provisioner')
  await expect(blocked.getByRole('button', { name: 'Approve plan' })).toBeDisabled()
  await expect(blocked).toContainText('a plan with blockers cannot be approved')

  // An applied plan links the Workflow it produced and names who approved it.
  await expect(page.getByText('Recent plans')).toBeVisible()
  await expect(page.getByRole('link', { name: 'op-remove-member' })).toBeVisible()
  await expect(page.getByText('Approved by admin')).toBeVisible()
})

test('Plans name their resources and targets, keep the ids, and fall back to an id with no name', async ({ page }) => {
  const serverReads: string[] = []
  page.on('request', (request) => {
    const match = new URL(request.url()).pathname.match(/^\/api\/v1\/servers\/([^/]+)$/)
    if (match && request.method() === 'GET') serverReads.push(decodeURIComponent(match[1]))
  })
  await installApiFixtures(page)
  await page.goto('/change-plans?site=site-a')

  // Names replace the ids, which stay readable beside them; the plan id can be copied.
  const uninstall = page.getByRole('article', { name: 'Change Plan for Platform research-slurm' })
  await expect(uninstall).toContainText('Platform ID platform-slurm')
  await expect(uninstall).toContainText('Plan ID plan-uninstall')
  await expect(uninstall.getByRole('button', { name: 'Copy Change Plan ID' })).toBeVisible()
  await expect(uninstall.getByRole('button', { name: 'Copy platform ID' })).toBeVisible()
  const target = uninstall.getByRole('table', { name: 'Planned actions for Platform research-slurm' }).getByRole('row', { name: /Server gpu-node-03/ })
  await expect(target).toContainText('srv-3')

  // A deleted Platform and Server have no name to show, so their ids stand in and are not repeated.
  const retired = page.getByRole('article', { name: 'Change Plan for Platform platform-retired' })
  await expect(retired).toContainText('Plan ID plan-retired')
  await expect(retired).not.toContainText('Platform ID')
  await expect(retired.getByRole('table', { name: 'Planned actions for Platform platform-retired' }).getByRole('row', { name: /Server srv-retired/ })).toBeVisible()

  // Every Server the plans name is read once for the whole page, never once per row or render.
  await expect.poll(() => [...serverReads].sort()).toEqual(['srv-1', 'srv-2', 'srv-3', 'srv-retired'])
})

test('Approving a plan confirms the consequence first and follows it until it is applied', async ({ page }) => {
  let approved = false
  let listReads = 0
  const serverReads: string[] = []
  await installApiFixtures(page, { applyApprovedChangePlans: true })
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (request.method() === 'POST' && path === '/api/v1/change-plans/plan-uninstall/approve') approved = true
    if (request.method() === 'GET' && path === '/api/v1/change-plans') listReads += 1
    if (request.method() === 'GET' && /^\/api\/v1\/servers\/[^/]+$/.test(path)) serverReads.push(path)
  })
  await page.goto('/change-plans')
  const plan = page.getByRole('article', { name: 'Change Plan for Platform research-slurm' })
  await plan.getByRole('button', { name: 'Approve plan' }).click()

  const dialog = page.getByRole('alertdialog', { name: 'Approve Change Plan' })
  // The dialog opens on the safe choice, so Enter alone never approves.
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await expect(dialog).toContainText('Swallow will carry out every action of this plan for Platform research-slurm, including 2 destructive actions.')
  await expect(dialog).toContainText('cannot be undone')
  expect(approved).toBe(false)
  await dialog.getByRole('button', { name: 'Approve plan' }).click()

  await expect(page.getByText('Change Plan approved')).toBeVisible()
  expect(approved).toBe(true)
  await expect(page.getByRole('heading', { name: 'Approved', exact: true })).toBeVisible()
  await expect(plan).toContainText('Approved by admin')

  // While it is approved the page re-reads the list in place, without a reload or a loading state:
  // a card that stays where it is keeps its element, and the plan reaches applied on its own.
  const blocked = await page.getByRole('article', { name: 'Change Plan for Platform production-k0s' }).first().elementHandle()
  const readsBefore = listReads
  const serverReadsBefore = serverReads.length
  await expect(plan).toContainText('Applied as Workflow op-plan-uninstall', { timeout: 15_000 })
  await expect(page.getByRole('heading', { name: 'Approved', exact: true })).toHaveCount(0)
  expect(listReads).toBeGreaterThan(readsBefore)
  expect(await blocked?.evaluate((node) => node.isConnected)).toBe(true)
  // The names resolved on the first read are kept: polling reads no Server again.
  expect(serverReads).toHaveLength(serverReadsBefore)
})

test('The approval statement counts destructive actions in words and omits a zero count', async ({ page }) => {
  await installApiFixtures(page)
  const plan = (id: string, destructive: boolean[]) => ({
    id, resource: { kind: 'platform', id: 'platform-b' }, generation: 2, observedAt: '2026-08-27T02:00:00Z',
    actions: destructive.map((isDestructive, index) => ({
      target: { kind: 'server', id: `srv-${index + 1}` }, action: isDestructive ? 'remove-member' : 'join', from: '', to: '',
      destructive: isDestructive, reason: 'fixture',
    })),
    effects: [], blockers: [], state: 'pending_approval', stateReason: '', decidedBy: '', decidedAt: null, workflowId: '',
    expiresAt: '2026-08-28T02:00:00Z', createdAt: '2026-08-27T02:00:00Z', updatedAt: '2026-08-27T02:00:00Z',
  })
  let items = [plan('plan-one', [true, false])]
  await page.route('**/api/v1/change-plans', (route) => (
    route.request().method() === 'GET'
      ? route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items }) })
      : route.fallback()
  ))
  await page.goto('/change-plans')
  const card = page.getByRole('article', { name: 'Change Plan for Platform edge-staging' })
  await card.getByRole('button', { name: 'Approve plan' }).click()
  const dialog = page.getByRole('alertdialog', { name: 'Approve Change Plan' })
  await expect(dialog).toContainText('for Platform edge-staging, including 1 destructive action.')
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toBeHidden()

  items = [plan('plan-none', [false])]
  await page.reload()
  await page.getByRole('article', { name: 'Change Plan for Platform edge-staging' }).getByRole('button', { name: 'Approve plan' }).click()
  await expect(dialog).toContainText('Swallow will carry out every action of this plan for Platform edge-staging.')
  await expect(dialog).not.toContainText('including')
})

test('A waiting plan says why it waits', async ({ page }) => {
  await installApiFixtures(page, { requireSeparateApproval: true })
  await page.goto('/platforms/platform-a?site=site-a')
  await page.getByRole('button', { name: 'Uninstall platform', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Uninstall platform' })
  await dialog.getByLabel('Platform name confirmation').fill('production-k0s')
  await dialog.getByLabel('Cancel these workflows, then uninstall').locator('..').click()
  await dialog.getByRole('button', { name: 'Uninstall', exact: true }).click()

  await expect(page).toHaveURL('/change-plans?site=site-a')
  const plan = page.getByRole('article', { name: 'Change Plan for Platform production-k0s' }).first()
  await expect(plan).toContainText('Waiting for approval')
  await expect(plan).toContainText('Why it waits: the Change Plan policy requires a separate approval.')
  await expect(plan.getByRole('button', { name: 'Approve plan' })).toBeEnabled()
})

test('The safety switch reads the policy, turns on and off, and names who changed it', async ({ page }) => {
  const writes: unknown[] = []
  page.on('request', (request) => {
    if (request.method() === 'PUT' && new URL(request.url()).pathname === '/api/v1/change-plan-policy') writes.push(request.postDataJSON())
  })
  await installApiFixtures(page)
  await page.goto('/change-plans?site=site-a')

  const card = page.locator('section', { has: page.getByRole('heading', { name: 'Approval of confirmed changes' }) })
  const toggle = card.getByRole('switch', { name: 'Require a separate approval for every Change Plan' })
  await expect(toggle).not.toBeChecked()
  await expect(card).toContainText('Off: a change confirmed in the dashboard')
  await expect(card).toContainText('Never changed; the default lets confirmed changes start at once.')

  await toggle.locator('..').click()
  await expect(toggle).toBeChecked()
  await expect(page.getByText('Separate approval required')).toBeVisible()
  await expect(card).toContainText('On: every destructive change waits here until an admin approves it')
  await expect(card).toContainText('Last changed by admin at')

  await toggle.locator('..').click()
  await expect(toggle).not.toBeChecked()
  await expect(page.getByText('Confirmed changes start at once')).toBeVisible()
  expect(writes).toEqual([{ approveOnSubmit: false }, { approveOnSubmit: true }])
})

test('Rejecting a plan moves it to the recent plans', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/change-plans')
  await page.getByRole('article', { name: 'Change Plan for Platform research-slurm' }).getByRole('button', { name: 'Reject plan' }).click()
  const dialog = page.getByRole('alertdialog', { name: 'Reject Change Plan' })
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await expect(dialog).toContainText('Nothing in this plan for Platform research-slurm')
  await dialog.getByRole('button', { name: 'Reject plan' }).click()
  await expect(page.getByText('Change Plan rejected')).toBeVisible()
  await expect(page.getByRole('article', { name: 'Change Plan for Platform research-slurm' })).toContainText('Rejected by admin')
})
