import { expect, test } from 'playwright/test'
import { installApiFixtures } from '../e2e/fixtures'

const visualCases = [
  { name: 'login', path: '/login', heading: 'Sign in', authenticated: false, representative: true },
  { name: 'overview', path: '/?site=site-a', heading: 'Overview', authenticated: true, representative: true },
  { name: 'servers', path: '/servers?site=site-a', heading: 'Servers', authenticated: true, representative: true },
  { name: 'platform-list', path: '/platforms?site=site-a', heading: 'Platforms', authenticated: true, representative: false },
  { name: 'workflow-list', path: '/workflows?site=site-a', heading: 'Workflows', authenticated: true, representative: false },
  { name: 'os-image-list', path: '/provisioning/images?site=site-a', heading: 'OS images', authenticated: true, representative: false },
  { name: 'monitoring', path: '/monitoring?site=site-a', heading: 'Monitoring', authenticated: true, representative: false },
  { name: 'server-detail', path: '/servers/srv-1/summary?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: true },
  { name: 'platform-detail', path: '/platforms/platform-a?site=site-a', heading: 'production-k0s', authenticated: true, representative: false },
  { name: 'deploy-wizard', path: '/provisioning/deploy?site=site-a&serverId=srv-1', heading: 'Deploy OS', authenticated: true, representative: true },
  { name: 'platform-wizard', path: '/platforms/deploy?site=site-a', heading: 'Deploy platform', authenticated: true, representative: false },
  { name: 'infrastructure', path: '/infrastructure/sites?site=site-a', heading: 'Infrastructure', authenticated: true, representative: false },
] as const

/**
 * Captures deterministic rendered pages for direct agent or developer inspection.
 *
 * These files are intentionally disposable: they live under the ignored Playwright output tree and
 * never become expected images. Test titles expose route names plus `@representative`, allowing the
 * caller to limit a review to the changed route or to the shared-layout sample with `--grep`.
 */
test.describe('dashboard visual review', () => {
  for (const visualCase of visualCases) {
    const title = `${visualCase.name}${visualCase.representative ? ' @representative' : ''}`

    test(title, async ({ page }, testInfo) => {
      const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'

      await installApiFixtures(page, {
        signedIn: visualCase.authenticated,
        ...(visualCase.name === 'servers'
          ? { ephemeralServerIds: ['srv-1'], lockedServerIds: ['srv-1'] }
          : {}),
      })
      await page.addInitScript(({ appearance }) => {
        Date.now = () => Date.parse('2026-08-27T03:05:00Z')
        localStorage.setItem('swallow.appearance', appearance)
        localStorage.removeItem('swallow.navigation.collapsed')
        localStorage.removeItem('access_token')
      }, { appearance })

      await page.goto(visualCase.path)
      await expect(page.getByRole('heading', { name: visualCase.heading, exact: true }).first()).toBeVisible()
      await page.waitForLoadState('networkidle')
      await page.evaluate(() => document.fonts.ready.then(() => true))

      if (appearance === 'dark') await expect(page.locator('html')).toHaveClass(/dark/)
      else await expect(page.locator('html')).not.toHaveClass(/dark/)

      await page.screenshot({
        path: testInfo.outputPath(`${visualCase.name}-${testInfo.project.name}.png`),
        animations: 'disabled',
        caret: 'hide',
        fullPage: true,
      })
    })
  }
})
