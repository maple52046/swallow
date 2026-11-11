import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// User journeys for swallow-owned Server tags (decision 071): the edit writes the spec, the
// provisioner follows asynchronously, and the Server's conditions say whether it has. Every request
// is served by deterministic fixtures; no backend or provisioner is contacted.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

test('Server summary says whether the provisioner matches the tags swallow owns', async ({ page }) => {
  await installApiFixtures(page, { driftedServerIds: ['srv-2'] })

  await page.goto('/servers/srv-1/summary?site=site-a')
  await expect(page.getByText('Provisioner sync', { exact: true })).toBeVisible()
  await expect(page.getByText('In sync', { exact: true })).toBeVisible()

  await page.goto('/servers/srv-2/summary?site=site-a')
  await expect(page.getByText('Drift detected', { exact: true })).toBeVisible()
  await expect(page.getByText(/unexpected hand-made\)\. Swallow corrects it at its next periodic check\./)).toBeVisible()
})

test('Tag editor edits the manual-tag spec and keeps automatic tags read-only', async ({ page }) => {
  await installApiFixtures(page, { automaticTags: { 'srv-1': ['amd-gpu'] } })
  const edits: unknown[] = []
  await page.route('**/api/v1/provisioning/tags**', async (route) => {
    if (route.request().method() === 'GET') {
      // The catalog deliberately omits amd-gpu: the Server's own automatic tags must still lock it.
      await route.fulfill({ json: { tags: [{ name: 'gpu', editable: true }, { name: 'production', editable: true }] } })
      return
    }
    edits.push(route.request().postDataJSON())
    await route.fulfill({ json: { servers: [{ serverId: 'srv-1', generation: 2, tags: ['gpu'] }] } })
  })

  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Edit tags' }).click()
  const editor = page.getByRole('dialog', { name: 'Edit tags' })
  await expect(editor.getByRole('checkbox', { name: 'gpu', exact: true })).toBeChecked()
  await expect(editor.getByRole('checkbox', { name: 'amd-gpu' })).toBeDisabled()
  await expect(editor.getByText('Automatic', { exact: true })).toBeVisible()

  await editor.getByRole('checkbox', { name: 'production' }).locator('..').click()
  await editor.getByRole('button', { name: 'Save tags' }).click()

  await expect.poll(() => edits).toEqual([{ serverIds: ['srv-1'], add: [], remove: ['production'] }])
  await expect(page.getByText('Tags saved', { exact: true })).toBeVisible()
  await expect(page.getByText(/The provisioner applies them shortly\./)).toBeVisible()
})

test('Tag editor can remove an automatic tag the provisioner maintains but not one it computes from a rule', async ({ page }) => {
  await installApiFixtures(page, {
    automaticTags: { 'srv-1': ['virtual', 'amd-gpu'] },
    serverTagCatalog: [{ name: 'amd-gpu', editable: false, removable: false }],
  })
  const edits: unknown[] = []
  await page.route('**/api/v1/provisioning/tags', async (route) => {
    if (route.request().method() !== 'POST') return route.fallback()
    edits.push(route.request().postDataJSON())
    await route.fulfill({ json: { servers: [{ serverId: 'srv-1', generation: 1, tags: ['gpu', 'production'] }] } })
  })

  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Edit tags' }).click()
  const editor = page.getByRole('dialog', { name: 'Edit tags' })
  await expect(editor.getByRole('checkbox', { name: 'amd-gpu' })).toBeDisabled()
  const virtual = editor.getByRole('checkbox', { name: 'virtual' })
  await expect(virtual).toBeEnabled()
  await expect(virtual).toBeChecked()

  await virtual.locator('..').click()
  await expect(virtual).not.toBeChecked()
  await editor.getByRole('button', { name: 'Save tags' }).click()

  await expect.poll(() => edits).toEqual([{ serverIds: ['srv-1'], add: [], remove: ['virtual'] }])
})

test('Tag manager deletes tags after a confirmation that states the consequence', async ({ page }) => {
  const changes: Array<[string, string, unknown]> = []
  await installApiFixtures(page, {
    automaticTags: { 'srv-2': ['virtual'] },
    serverTagCatalog: [{ name: 'legacy', editable: true, removable: true }],
    onServerTagChange: (method, name, body) => changes.push([method, name, body]),
  })

  await page.goto('/servers?site=site-a')
  await page.getByRole('button', { name: 'Manage tags' }).click()
  const manager = page.getByRole('dialog', { name: 'Manage tags' })
  const list = manager.getByRole('list', { name: 'Tags of Taipei Lab' })
  await expect(list.getByRole('listitem').filter({ hasText: 'legacy' })).toContainText('Unused')
  await expect(list.getByRole('listitem').filter({ hasText: 'production' })).toContainText('3 servers')
  const virtual = list.getByRole('listitem').filter({ hasText: 'virtual' })
  await expect(virtual).toContainText('Automatic')
  await expect(virtual.getByRole('button', { name: 'Rename tag virtual' })).toHaveCount(0)

  await manager.getByRole('button', { name: 'Delete tag production' }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Delete tag production?' })
  await expect(confirm).toContainText('production is removed from 3 servers and deleted from the provisioner.')
  await expect(confirm.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await confirm.getByRole('button', { name: 'Delete tag' }).click()

  await expect(page.getByText('Tag deleted', { exact: true })).toBeVisible()
  await expect(list.getByRole('listitem').filter({ hasText: 'production' })).toHaveCount(0)

  await manager.getByRole('button', { name: 'Delete tag virtual' }).click()
  await expect(page.getByRole('alertdialog', { name: 'Delete tag virtual?' })).toContainText('the provisioner may add it again')
  await page.getByRole('alertdialog', { name: 'Delete tag virtual?' }).getByRole('button', { name: 'Delete tag' }).click()
  await expect(list.getByRole('listitem').filter({ hasText: 'virtual' })).toHaveCount(0)

  expect(changes).toEqual([['DELETE', 'production', null], ['DELETE', 'virtual', null]])
})

test('Tag manager deletes selected tags one at a time and keeps the ones that failed selected', async ({ page }) => {
  const changes: Array<[string, string, unknown]> = []
  await installApiFixtures(page, {
    automaticTags: { 'srv-2': ['virtual'] },
    serverTagCatalog: [{ name: 'legacy', editable: true, removable: true }],
    onServerTagChange: (method, name, body) => changes.push([method, name, body]),
  })
  // Each deletion is held briefly so overlapping requests would show; production is refused, the
  // way the API refuses a tag a Server not adopted yet carries.
  let inFlight = 0
  let maxInFlight = 0
  await page.route('**/api/v1/provisioning/tags/*', async (route) => {
    if (route.request().method() !== 'DELETE') return route.fallback()
    inFlight += 1
    maxInFlight = Math.max(maxInFlight, inFlight)
    await new Promise((resolve) => setTimeout(resolve, 100))
    inFlight -= 1
    if (route.request().url().includes('/tags/production?')) {
      return route.fulfill({
        status: 409,
        json: { error: { code: 'conflict', message: 'A Server not adopted yet carries this tag; retry.' } },
      })
    }
    return route.fallback()
  })

  await page.goto('/servers?site=site-a')
  await page.getByRole('button', { name: 'Manage tags' }).click()
  const manager = page.getByRole('dialog', { name: 'Manage tags' })
  const list = manager.getByRole('list', { name: 'Tags of Taipei Lab' })
  await expect(manager.getByRole('button', { name: 'Delete selected' })).toBeDisabled()

  await manager.getByRole('checkbox', { name: 'Select all tags' }).locator('..').click()
  await expect(manager.getByRole('button', { name: 'Delete 4 tags' })).toBeEnabled()
  await list.getByRole('checkbox', { name: 'gpu', exact: true }).locator('..').click()
  await expect(manager.getByRole('checkbox', { name: 'Select all tags' })).toBeChecked({ indeterminate: true })
  await manager.getByRole('button', { name: 'Delete 3 tags' }).click()

  const confirm = page.getByRole('alertdialog', { name: 'Delete 3 tags?' })
  const listed = confirm.getByRole('list', { name: 'Tags to delete' })
  await expect(listed.getByRole('listitem')).toHaveText([
    'legacy: unused',
    'production: removed from 3 servers',
    'virtual: automatic; taken off 1 server now, the provisioner may add it again',
  ])
  await confirm.getByRole('button', { name: 'Delete 3 tags' }).click()

  await expect(manager.getByText('1 tag was not deleted')).toBeVisible()
  await expect(manager.getByText('production: A Server not adopted yet carries this tag; retry.')).toBeVisible()
  await expect(list.getByRole('listitem').filter({ hasText: 'legacy' })).toHaveCount(0)
  await expect(list.getByRole('listitem').filter({ hasText: 'virtual' })).toHaveCount(0)
  await expect(list.getByRole('checkbox', { name: 'production' })).toBeChecked()
  await expect(list.getByRole('checkbox', { name: 'gpu', exact: true })).not.toBeChecked()
  await expect(manager.getByRole('button', { name: 'Delete 1 tag' })).toBeEnabled()

  expect(maxInFlight).toBe(1)
  expect(changes).toEqual([['DELETE', 'legacy', null], ['DELETE', 'virtual', null]])
})

test('Tag manager renames a manual tag at once and confirms a merge into an existing one', async ({ page }) => {
  const changes: Array<[string, string, unknown]> = []
  await installApiFixtures(page, {
    serverTagCatalog: [{ name: 'legacy', editable: true, removable: true }],
    onServerTagChange: (method, name, body) => changes.push([method, name, body]),
  })

  await page.goto('/servers?site=site-a')
  await page.getByRole('button', { name: 'Manage tags' }).click()
  const manager = page.getByRole('dialog', { name: 'Manage tags' })
  const list = manager.getByRole('list', { name: 'Tags of Taipei Lab' })

  await manager.getByRole('button', { name: 'Rename tag gpu' }).click()
  const rename = manager.getByRole('textbox', { name: 'New name for gpu' })
  await expect(rename).toBeFocused()
  await rename.fill('has space')
  await rename.press('Enter')
  await expect(manager.getByText('Use letters, digits, and _-. only.')).toBeVisible()
  await rename.fill('accelerator')
  await rename.press('Enter')
  await expect(page.getByText('Tag renamed', { exact: true })).toBeVisible()
  await expect(list.getByRole('listitem').filter({ hasText: 'accelerator' })).toContainText('3 servers')

  await manager.getByRole('button', { name: 'Rename tag legacy' }).click()
  await manager.getByRole('textbox', { name: 'New name for legacy' }).fill('production')
  await manager.getByRole('button', { name: 'Save' }).click()
  const merge = page.getByRole('alertdialog', { name: 'Merge legacy into production?' })
  await expect(merge).toContainText('production already exists on 3 servers.')
  await merge.getByRole('button', { name: 'Merge tags' }).click()
  await expect(list.getByRole('listitem').filter({ hasText: 'legacy' })).toHaveCount(0)

  expect(changes).toEqual([
    ['PATCH', 'gpu', { name: 'accelerator' }],
    ['PATCH', 'legacy', { name: 'production' }],
  ])
})
