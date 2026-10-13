import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// The Integration settings page replaces the Edit integration dialog: a tab per route, drafts that
// survive switching tabs, and a Save per tab that sends only that tab's changes.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

/** Records the JSON body of every Integration PATCH, in order. */
function recordIntegrationPatches(page: Page): Array<Record<string, unknown>> {
  const patches: Array<Record<string, unknown>> = []
  page.on('request', (request) => {
    if (request.method() === 'PATCH' && /\/api\/v1\/integrations\/[^/]+$/.test(new URL(request.url()).pathname)) {
      patches.push(request.postDataJSON() as Record<string, unknown>)
    }
  })
  return patches
}

test('a MAAS provisioner opens on General with its five tabs, and each tab has its own URL', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/infrastructure/integrations?site=site-a')
  await page.getByRole('row', { name: /MAAS Taipei/ }).getByRole('link', { name: 'MAAS Taipei' }).click()

  await expect(page).toHaveURL('/infrastructure/integrations/maas-a/general?site=site-a')
  await expect(page.getByRole('heading', { level: 1, name: 'MAAS Taipei' })).toBeVisible()
  const tabs = page.getByRole('tablist', { name: 'Integration settings' }).getByRole('tab')
  await expect(tabs).toHaveText(['General', 'Connection', 'Credential', 'HTTP proxy', 'Subnet DNS'])
  await expect(page.getByRole('tab', { name: 'General' })).toHaveAttribute('aria-selected', 'true')
  // Identity fixed at registration is shown, not offered as a field.
  const general = page.getByRole('region', { name: 'General' })
  await expect(general.getByText('Taipei Lab')).toBeVisible()
  await expect(general.getByRole('combobox')).toHaveCount(0)

  await page.getByRole('tab', { name: 'Connection' }).click()
  await expect(page).toHaveURL('/infrastructure/integrations/maas-a/connection?site=site-a')
  await expect(page.getByText('Inspect newly enrolled Servers automatically')).toBeVisible()

  // The breadcrumb returns to the list with the Site scope kept.
  await page.getByRole('link', { name: 'Integrations' }).click()
  await expect(page).toHaveURL('/infrastructure/integrations?site=site-a')
})

test('drafts survive switching tabs, and each Save sends only its own tab', async ({ page }) => {
  await installApiFixtures(page)
  const patches = recordIntegrationPatches(page)
  await page.goto('/infrastructure/integrations/maas-a/general?site=site-a')

  const name = page.getByRole('textbox', { name: /^Name/ })
  await name.fill('MAAS Taipei Primary')
  await expect(page.getByRole('region', { name: 'General' }).getByRole('status')).toHaveText(/^Unsaved changes/)

  // Saving Connection keeps the saved name: the General draft is not part of that write.
  await page.getByRole('tab', { name: 'Connection' }).click()
  await page.getByRole('textbox', { name: 'Request timeout' }).fill('45s')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('Connection settings saved')).toBeVisible()
  expect(patches).toHaveLength(1)
  expect(patches[0]).toMatchObject({ name: 'MAAS Taipei', settings: { timeout: '45s' } })
  await expect(page.getByRole('region', { name: 'Connection' }).getByRole('status')).toHaveText('All changes saved.')

  // Back on General the draft is still there; saving it keeps the new connection settings.
  await page.getByRole('tab', { name: 'General' }).click()
  await expect(name).toHaveValue('MAAS Taipei Primary')
  await page.getByRole('checkbox', { name: 'Enabled' }).locator('..').click()
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('Integration updated')).toBeVisible()
  expect(patches[1]).toMatchObject({ name: 'MAAS Taipei Primary', enabled: false, settings: { timeout: '45s' } })
  await expect(page.getByRole('heading', { level: 1, name: 'MAAS Taipei Primary' })).toBeVisible()
  await expect(page.locator('.sw-page-header').getByText('Paused')).toBeVisible()
})

test('the Credential tab replaces the write-only key without keeping it in the browser', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/infrastructure/integrations?site=site-a')
  await page.getByRole('row', { name: /MAAS Taipei/ }).getByRole('button', { name: /credential for MAAS Taipei/ }).click()
  await expect(page).toHaveURL('/infrastructure/integrations/maas-a/credential?site=site-a')

  const credential = page.getByRole('region', { name: 'Credential' })
  await credential.getByLabel('New MAAS API key').fill('replacement-secret')
  await credential.getByRole('button', { name: 'Replace credential' }).click()
  await expect(page.getByText('Credential replaced')).toBeVisible()
  await expect(credential.getByLabel('New MAAS API key')).toHaveValue('')
  expect(await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }))).not.toContain('replacement-secret')
})

test('other Integrations have no MAAS tabs, and an unknown id says so', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/infrastructure/integrations?site=site-a')
  await page.getByRole('row', { name: /Prometheus Taipei/ }).getByRole('button', { name: 'Edit' }).click()
  await expect(page.getByRole('tablist', { name: 'Integration settings' }).getByRole('tab')).toHaveText(['General', 'Connection', 'Credential'])
  await page.getByRole('tab', { name: 'Connection' }).click()
  await expect(page.getByRole('textbox', { name: 'Alertmanager URL' })).toBeVisible()
  await expect(page.getByText('Inspect newly enrolled Servers automatically')).toHaveCount(0)

  await page.goto('/infrastructure/integrations/missing/general?site=site-a')
  await expect(page.getByText('Integration not found')).toBeVisible()
  await page.getByRole('button', { name: 'Back to Integrations' }).click()
  await expect(page).toHaveURL('/infrastructure/integrations?site=site-a')
})
