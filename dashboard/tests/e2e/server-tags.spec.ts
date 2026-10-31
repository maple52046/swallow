import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Operator journeys for swallow-owned Server tags (decision 071): the edit writes the spec, the
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
