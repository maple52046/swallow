import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Operator journeys for API keys (decision 042). Every request is served by deterministic fixtures;
// the "secret" is a fixture string.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
  })
})

test('Account menu opens API keys with prefixes, expiry, and last use spelled out', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/?site=site-a')
  await page.getByRole('button', { name: 'Account menu' }).click()
  await page.getByRole('menuitem', { name: 'API keys' }).click()

  await expect(page).toHaveURL(/\/account\/api-keys/)
  await expect(page.getByRole('heading', { name: 'API keys', exact: true })).toBeVisible()
  const table = page.getByRole('table', { name: 'API keys' })
  const laptop = table.getByRole('row', { name: /laptop-cli/ })
  await expect(laptop).toContainText('swk_Lap1op00…')
  await expect(laptop).toContainText('Never')
  // An expired key says so in words, not only by colour.
  const old = table.getByRole('row', { name: /old-ci/ })
  await expect(old).toContainText('expired')
  await expect(old).toContainText('Never used')
})

test('Creating an API key shows its secret once and blocks dismissal until it is saved', async ({ page }) => {
  let created: Record<string, unknown> | undefined
  await installApiFixtures(page)
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/api-keys') created = request.postDataJSON() as Record<string, unknown>
  })
  await page.goto('/account/api-keys')
  await page.getByRole('button', { name: 'Create API key' }).first().click()
  await page.locator('#api-key-create-name').fill('ci-runner')
  await page.getByRole('button', { name: 'Create', exact: true }).click()

  await expect(page.getByRole('heading', { name: 'Save your API key' })).toBeVisible()
  await expect(page.locator('#api-key-created-secret')).toHaveValue('swk_N3wK3y00FIXTURE-SECRET')
  // The default expiry is 90 days from now.
  expect(created?.name).toBe('ci-runner')
  expect(Date.parse(String(created?.expiresAt)) - Date.parse('2026-08-27T03:05:00Z')).toBe(90 * 24 * 60 * 60 * 1000)

  const done = page.getByRole('button', { name: 'Done' })
  await expect(done).toBeDisabled()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('heading', { name: 'Save your API key' })).toBeVisible()

  await page.getByText('I have saved the API key').click()
  await done.click()
  await expect(page.getByText('API key created')).toBeVisible()
  await expect(page.getByRole('table', { name: 'API keys' })).toContainText('ci-runner')
})

test('A key that never expires sends no expiry, and a duplicate name is explained', async ({ page }) => {
  let created: Record<string, unknown> | undefined
  await installApiFixtures(page)
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/api-keys') created = request.postDataJSON() as Record<string, unknown>
  })
  await page.goto('/account/api-keys')
  await page.getByRole('button', { name: 'Create API key' }).first().click()

  await page.locator('#api-key-create-name').fill('laptop-cli')
  await page.getByRole('button', { name: 'Create', exact: true }).click()
  await expect(page.getByText('You already have an API key with this name.')).toBeVisible()

  await page.locator('#api-key-create-name').fill('forever')
  await page.getByRole('combobox', { name: 'Expires', exact: true }).click()
  await page.getByRole('option', { name: 'Never expires', exact: true }).click()
  await page.getByRole('button', { name: 'Create', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Save your API key' })).toBeVisible()
  expect(created).toEqual({ name: 'forever' })
})

test('Deleting an API key states the consequence and removes it', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/account/api-keys')
  await page.getByRole('button', { name: 'Delete old-ci' }).click()

  const dialog = page.getByRole('alertdialog', { name: 'Delete old-ci' })
  await expect(dialog).toContainText('stop working immediately')
  await dialog.getByRole('button', { name: 'Delete key' }).click()
  await expect(page.getByText('API key deleted')).toBeVisible()
  await expect(page.getByRole('table', { name: 'API keys' })).not.toContainText('old-ci')
})
