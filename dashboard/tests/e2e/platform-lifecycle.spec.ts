import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

// A deploying Platform showed no running cue on its detail page or mobile card, and the list's own
// spinner froze under reduced motion. Every lifecycle badge now carries the shared spinner that
// keeps rotating there.
test('a deploying Platform shows the running cue wherever its lifecycle appears', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await installApiFixtures(page, { platformLifecycleStates: { 'platform-a': 'deploying' } })

  await page.goto('/platforms?site=site-a')
  const row = page.getByRole('row').filter({ hasText: 'production-k0s' })
  await expect(row.getByText('Deploying', { exact: true })).toBeVisible()
  const spinner = row.locator('.sw-progress-spinner')
  await expect(spinner).toBeVisible()
  expect(await spinner.evaluate((element) => getComputedStyle(element).animationName)).not.toBe('none')
  const failedRow = page.getByRole('row').filter({ hasText: 'edge-staging' })
  await expect(failedRow.locator('.sw-progress-spinner')).toHaveCount(0)

  await page.setViewportSize({ width: 390, height: 844 })
  const card = page.locator('.sw-platform-runtime-card').filter({ hasText: 'production-k0s' })
  await expect(card.getByText('Deploying', { exact: true })).toBeVisible()
  await expect(card.locator('.sw-progress-spinner')).toBeVisible()

  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/platforms/platform-a?site=site-a')
  const header = page.locator('.sw-platform-lifecycle-badge')
  await expect(header.getByText('Deploying', { exact: true })).toBeVisible()
  await expect(header.locator('.sw-progress-spinner')).toBeVisible()
})

test('a settled Platform shows no running cue', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/platforms/platform-a?site=site-a')
  const header = page.locator('.sw-platform-lifecycle-badge')
  await expect(header.getByText('Active', { exact: true })).toBeVisible()
  await expect(header.locator('.sw-progress-spinner')).toHaveCount(0)
})
