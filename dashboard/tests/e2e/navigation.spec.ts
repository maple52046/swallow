import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// The console's own navigation: the sidebar's sections in every layout (expanded rail, icon-only
// rail, mobile drawer), and the Activity area's tabs. Every request is served by the deterministic
// fixtures.

test.beforeEach(async ({ page }) => {
  await installApiFixtures(page)
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
  })
})

function desktopNavigation(page: Page) {
  return page.getByTestId('desktop-navigation').getByRole('navigation', { name: 'Primary navigation' })
}

test('the sidebar lists its sections in one flat list with OS Images above Infrastructure', async ({ page }) => {
  await page.goto('/provisioning/images?site=site-a')
  const nav = desktopNavigation(page)

  await expect(nav.getByRole('link')).toHaveText(['Overview', 'Servers', 'Platforms', 'Software', 'Monitoring', 'Activity', 'Validation', 'OS Images', 'Deployment Templates', 'Infrastructure', 'Pools', 'Users'])
  // The rules between the four sections are drawn but hidden from assistive technology, so the
  // list counts only destinations; there is no section label.
  await expect(nav.locator('[role="separator"]')).toHaveCount(3)
  await expect(nav.getByRole('separator')).toHaveCount(0)
  await expect(nav.getByRole('listitem')).toHaveCount(12)
  await expect(page.getByTestId('desktop-navigation').getByText('Workspace', { exact: true })).toHaveCount(0)
  for (const retired of ['Provisioning', 'Workflows', 'Change Plans']) {
    await expect(nav.getByRole('link', { name: retired })).toHaveCount(0)
  }

  // OS Images is an entry of the flat list, not nested in another item, and the only current entry.
  await expect(nav.getByRole('list')).toHaveCount(1)
  const osImages = nav.getByRole('link', { name: 'OS Images', exact: true })
  await expect(osImages).toHaveAttribute('aria-current', 'page')
  await expect(osImages).toHaveAttribute('href', '/provisioning/images?site=site-a')
  await expect(nav.getByRole('link', { name: 'Servers', exact: true })).not.toHaveAttribute('aria-current', 'page')
})

test('each sidebar entry is current on its own routes only', async ({ page }) => {
  const nav = desktopNavigation(page)
  const cases = [
    { path: '/?site=site-a', current: 'Overview' },
    { path: '/servers?site=site-a', current: 'Servers' },
    { path: '/servers/srv-1/summary?site=site-a', current: 'Servers' },
    { path: '/provisioning/images?site=site-a', current: 'OS Images' },
    { path: '/provisioning/templates?site=site-a', current: 'Deployment Templates' },
    { path: '/workflows?site=site-a', current: 'Activity' },
    { path: '/change-plans?site=site-a', current: 'Activity' },
    { path: '/validation/runs/run-failed?site=site-a', current: 'Validation' },
    { path: '/infrastructure/boot-isos?site=site-a', current: 'Infrastructure' },
  ]
  for (const entry of cases) {
    await page.goto(entry.path)
    await expect(nav.locator('[aria-current="page"]')).toHaveCount(1)
    await expect(nav.getByRole('link', { name: entry.current, exact: true })).toHaveAttribute('aria-current', 'page')
  }
})

test('the collapsed rail shows every entry as an icon whose tooltip names it', async ({ page }) => {
  await page.goto('/servers?site=site-a')
  const rail = page.getByTestId('desktop-navigation')
  await page.getByRole('button', { name: 'Toggle navigation' }).click()
  await expect.poll(() => rail.evaluate((element) => Math.round(element.getBoundingClientRect().width))).toBe(64)

  const nav = desktopNavigation(page)
  await expect(nav.getByRole('link')).toHaveText(['', '', '', '', '', '', '', '', '', '', '', ''])
  const osImages = nav.getByRole('link', { name: 'OS Images', exact: true })
  await osImages.hover()
  await expect(page.getByRole('tooltip', { name: 'OS Images', exact: true })).toBeVisible()

  // Keyboard users reach it in list order and it navigates like any entry.
  await nav.getByRole('link', { name: 'Validation', exact: true }).focus()
  await page.keyboard.press('Tab')
  await expect(osImages).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL('/provisioning/images?site=site-a')
  await expect(osImages).toHaveAttribute('aria-current', 'page')
})

test('the mobile drawer shows the same sections and closes after choosing an entry', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/?site=site-a')
  await page.getByRole('button', { name: 'Open navigation' }).click()
  const nav = page.getByRole('dialog').getByRole('navigation', { name: 'Primary navigation' })
  await expect(nav.getByRole('link')).toHaveText(['Overview', 'Servers', 'Platforms', 'Software', 'Monitoring', 'Activity', 'Validation', 'OS Images', 'Deployment Templates', 'Infrastructure', 'Pools', 'Users'])
  await expect(nav.locator('[role="separator"]')).toHaveCount(3)

  await nav.getByRole('link', { name: 'OS Images' }).click()
  await expect(page).toHaveURL('/provisioning/images?site=site-a')
  await expect(nav).toBeHidden()
  await expect(page.getByRole('heading', { name: 'OS Images', level: 1 })).toBeVisible()
})

test('Activity switches between Workflows and Change Plans with tabs that follow the URL', async ({ page }) => {
  await page.goto('/?site=site-a')
  await desktopNavigation(page).getByRole('link', { name: 'Activity' }).click()
  await expect(page).toHaveURL('/workflows?site=site-a')
  await expect(page.getByRole('heading', { name: 'Activity', level: 1 })).toBeVisible()
  const tabs = page.getByRole('tablist', { name: 'Activity navigation' })
  await expect(tabs.getByRole('tab')).toHaveText(['Workflows', 'Change Plans'])
  await expect(tabs.getByRole('tab', { name: 'Workflows' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('heading', { name: 'Workflow activity' })).toBeVisible()

  await tabs.getByRole('tab', { name: 'Change Plans' }).click()
  await expect(page).toHaveURL('/change-plans?site=site-a')
  await expect(tabs.getByRole('tab', { name: 'Change Plans' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('heading', { name: 'Approval of confirmed changes' })).toBeVisible()

  // Arrow keys switch tabs too; focus stays on the tabs although each tab is its own page, and
  // history moves the selection with the URL.
  await expect(tabs.getByRole('tab', { name: 'Change Plans' })).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(page).toHaveURL('/workflows?site=site-a')
  await expect(tabs.getByRole('tab', { name: 'Workflows' })).toBeFocused()
  await page.goBack()
  await expect(page).toHaveURL('/change-plans?site=site-a')
  await expect(tabs.getByRole('tab', { name: 'Change Plans' })).toHaveAttribute('aria-selected', 'true')
})

test('a Workflow detail page leads back to Activity and its Workflows tab', async ({ page }) => {
  await page.goto('/workflows?site=site-a')
  const first = page.getByRole('table', { name: 'Workflows' }).getByRole('link').first()
  await first.click()
  await expect(page).toHaveURL(/\/workflows\/[^/?]+\?site=site-a$/)
  await expect(desktopNavigation(page).getByRole('link', { name: 'Activity' })).toHaveAttribute('aria-current', 'page')
  const trail = page.getByRole('navigation', { name: 'breadcrumb' })
  await expect(trail.getByRole('link')).toHaveText(['Activity', 'Workflows', /^op-/])
  await expect(trail.getByRole('link', { name: 'Activity' })).toHaveAttribute('href', '/workflows?site=site-a')
  await trail.getByRole('link', { name: 'Workflows' }).click()
  await expect(page).toHaveURL('/workflows?site=site-a')
  await expect(page.getByRole('tab', { name: 'Workflows', selected: true })).toBeVisible()
})
