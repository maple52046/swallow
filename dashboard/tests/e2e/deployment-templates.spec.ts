import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-11-19T05:00:00Z')
    localStorage.setItem('swallow.appearance', 'light')
  })
})

test('recipe cards group by Pool and URL search preserves Site scope', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/provisioning/templates?site=site-a')

  await expect(page.locator('.sw-template-group__heading h3')).toHaveText(['research', 'No Pool'])
  await expect(page.getByText('Showing 2 of 2 templates across 1 Pool')).toBeVisible()
  const gpu = page.getByRole('article').filter({ hasText: 'GPU compute baseline' })
  await expect(gpu).toContainText('Ubuntu 22.04 LTS')
  await expect(gpu).toContainText('Used by 1 node group')
  await gpu.getByText('Technical details').click()
  await expect(gpu.getByRole('link', { name: 'lab-slurm-ha' })).toHaveAttribute(
    'href',
    '/platforms/platform-slurm-ha/overview?site=site-a',
  )

  const search = page.getByRole('textbox', { name: 'Search deployment templates' })
  await search.fill('CPU login')
  await expect(page).toHaveURL('/provisioning/templates?site=site-a&q=CPU+login')
  await expect(page.getByRole('article')).toHaveCount(1)
  await expect(page.getByRole('article')).toContainText('CPU login')
  await expect(page.getByRole('heading', { name: 'GPU compute baseline' })).toHaveCount(0)

  await search.fill('no matching recipe')
  await expect(page.getByRole('heading', { name: 'No matching templates' })).toBeVisible()
  await page.getByRole('button', { name: 'Clear search' }).last().click()
  await expect(page).toHaveURL('/provisioning/templates?site=site-a')
  await expect(page.getByRole('article')).toHaveCount(2)

  await page.goto('/provisioning/templates')
  await expect(page.getByRole('article').filter({ hasText: 'GPU compute baseline' })).toContainText('Taipei Lab')
})

test('cards distinguish missing images, catalog failures, and multi-architecture metadata', async ({ page }) => {
  await installApiFixtures(page, { templateCatalogScenario: 'missing-image' })
  await page.goto('/provisioning/templates?site=site-a')
  await expect(page.getByRole('article').filter({ hasText: 'GPU compute baseline' })).toContainText('OS image unavailable')

  await page.unroute('**/api/v1/**')
  await installApiFixtures(page, { failImageIntegrationIds: ['maas-a'] })
  await page.reload()
  await expect(page.getByText('1 image catalog is unavailable')).toBeVisible()
  await expect(page.getByRole('article').filter({ hasText: 'GPU compute baseline' })).toContainText('Image catalog unavailable')

  await page.unroute('**/api/v1/**')
  await installApiFixtures(page, { templateCatalogScenario: 'multi-architecture' })
  await page.reload()
  const multi = page.getByRole('article').filter({ hasText: 'GPU compute baseline' })
  await expect(multi).toContainText('ubuntu/jammy')
  await multi.getByText('Technical details').click()
  await expect(multi).toContainText('amd64, arm64')
  await expect(multi).toContainText('Ubuntu Jammy ARM variant')
})

test('deep-link create carries image intent and cloud-init stays write-only', async ({ page }) => {
  const writes: Array<{ method: string; body: Record<string, unknown> }> = []
  await installApiFixtures(page, { onTemplateRequest: (method, body) => writes.push({ method, body }) })
  await page.goto('/provisioning/templates?site=site-a&create=1&integrationId=maas-a&imageId=ubuntu%2Fnoble')

  const dialog = page.getByRole('dialog', { name: 'Create deployment template' })
  await expect(dialog.getByRole('combobox', { name: 'Provisioner Integration' })).toContainText('MAAS Taipei')
  await expect(dialog.getByRole('radio', { name: 'Ubuntu 24.04 LTS' })).toBeChecked()
  await dialog.getByLabel('Name').fill('Login recipe')
  await dialog.getByRole('button', { name: 'Continue' }).click()
  await dialog.getByLabel('Include cloud-init').focus()
  await page.keyboard.press('Space')
  await dialog.getByRole('textbox', { name: 'Cloud-init', exact: true }).fill('#cloud-config\nusers: []')
  await dialog.getByRole('button', { name: 'Create template' }).click()

  expect(writes).toHaveLength(1)
  expect(writes[0]?.method).toBe('POST')
  expect(writes[0]?.body).toMatchObject({
    integrationId: 'maas-a',
    poolId: 'pool-research',
    imageId: 'ubuntu/noble',
    name: 'Login recipe',
    userData: '#cloud-config\nusers: []',
  })
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain('#cloud-config')
  await expect(page).toHaveURL('/provisioning/templates?site=site-a')
})

test('editing metadata sends a sparse PATCH and mobile cards do not overflow', async ({ page }) => {
  const writes: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    onTemplateRequest: (method, body) => {
      if (method === 'PATCH') writes.push(body)
    },
  })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/provisioning/templates?site=site-a')
  const card = page.getByRole('article').filter({ hasText: 'GPU compute baseline' })
  await card.getByRole('button', { name: 'Edit template' }).click()
  const dialog = page.getByRole('dialog', { name: 'Edit GPU compute baseline' })
  await dialog.getByLabel('Name').fill('GPU accelerator baseline')
  await dialog.getByRole('button', { name: 'Continue' }).click()
  await dialog.getByRole('button', { name: 'Save changes' }).click()
  expect(writes).toEqual([{ name: 'GPU accelerator baseline' }])
  await expect(page.getByRole('heading', { name: 'GPU accelerator baseline' })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})

test('a genuinely empty catalog explains that the first recipe can be created', async ({ page }) => {
  await installApiFixtures(page, { templateCatalogScenario: 'empty' })
  await page.goto('/provisioning/templates?site=site-a')
  await expect(page.getByRole('heading', { name: 'No deployment templates' })).toBeVisible()
  await expect(page.getByText('Create the first reusable OS deployment recipe for one of your Pools.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Create template' })).toHaveCount(2)
})
