import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

const visualCases = [
  { name: 'login', path: '/login', heading: 'Sign in', authenticated: false },
  { name: 'overview', path: '/?site=site-a', heading: 'Overview', authenticated: true },
  { name: 'servers', path: '/servers?site=site-a', heading: 'Servers', authenticated: true },
  { name: 'platform-list', path: '/platforms?site=site-a', heading: 'Platforms', authenticated: true },
  { name: 'workflow-list', path: '/workflows?site=site-a', heading: 'Workflows', authenticated: true },
  { name: 'monitoring', path: '/monitoring?site=site-a', heading: 'Monitoring', authenticated: true },
  { name: 'server-detail', path: '/servers/srv-1/summary?site=site-a', heading: 'gpu-node-01', authenticated: true },
  { name: 'platform-detail', path: '/platforms/platform-a?site=site-a', heading: 'production-k0s', authenticated: true },
  { name: 'deploy-wizard', path: '/provisioning/deploy?site=site-a&serverId=srv-1', heading: 'Deploy OS', authenticated: true },
  { name: 'platform-wizard', path: '/platforms/deploy?site=site-a', heading: 'Deploy platform', authenticated: true },
  { name: 'infrastructure', path: '/infrastructure/sites?site=site-a', heading: 'Infrastructure', authenticated: true },
] as const

const viewports = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'mobile', width: 390, height: 844 },
] as const

const appearances = ['light', 'dark'] as const

/**
 * Deterministic light/dark baselines for the core operator journeys.
 *
 * API fixtures, clock, viewport, and persisted appearance are fixed so diffs
 * represent presentation changes rather than provider data or animation timing.
 */
test.describe('dashboard visual regression', () => {
  for (const visualCase of visualCases) {
    for (const viewport of viewports) {
      for (const appearance of appearances) {
        test(`${visualCase.name} · ${viewport.name} · ${appearance}`, async ({ page }) => {
          await installApiFixtures(page)
          await page.setViewportSize({ width: viewport.width, height: viewport.height })
          await page.addInitScript(({ authenticated, appearance }) => {
            Date.now = () => Date.parse('2026-08-27T03:05:00Z')
            localStorage.setItem('swallow.appearance', appearance)
            localStorage.removeItem('swallow.navigation.collapsed')
            if (authenticated) localStorage.setItem('access_token', 'e2e-token')
            else localStorage.removeItem('access_token')
          }, { authenticated: visualCase.authenticated, appearance })

          await page.goto(visualCase.path)
          await expect(page.getByRole('heading', { name: visualCase.heading, exact: true }).first()).toBeVisible()
          await page.waitForLoadState('networkidle')
          await page.evaluate(() => document.fonts.ready.then(() => true))

          if (appearance === 'dark') await expect(page.locator('html')).toHaveClass(/dark/)
          else await expect(page.locator('html')).not.toHaveClass(/dark/)

          await expect(page).toHaveScreenshot(
            `${visualCase.name}-${viewport.name}-${appearance}.png`,
            { animations: 'disabled', caret: 'hide', fullPage: true },
          )
        })
      }
    }
  }
})
