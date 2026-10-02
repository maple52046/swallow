import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Session behavior (decision 042): the access token lives in memory, the refresh token in an
// HttpOnly cookie (modelled by the fixtures' signed-in flag), the app refreshes silently on a 401,
// and an ended Session returns the operator to sign-in with their place remembered.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
  })
})

const unauthorized = { error: { code: 'unauthorized', message: 'Missing or invalid token.', requestId: 'req-401' } }

async function signIn(page: Page) {
  await page.getByLabel('Username').fill('admin')
  await page.getByLabel('Password').fill('correct-pass')
  await page.getByRole('button', { name: 'Sign in' }).click()
}

test('A signed-out browser signs in and returns to the page it asked for', async ({ page }) => {
  await installApiFixtures(page, { signedIn: false })
  await page.goto('/servers?site=site-a')

  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
  await expect(page.getByText('Your session ended')).toHaveCount(0)
  await signIn(page)
  await expect(page).toHaveURL('/servers?site=site-a')
  await expect(page.getByRole('heading', { name: 'Servers', exact: true })).toBeVisible()
})

test('An expired access token is renewed silently and the request is retried', async ({ page }) => {
  // Development builds run effects twice (StrictMode), so the order of events is asserted rather
  // than exact request counts.
  const events: string[] = []
  await installApiFixtures(page, { onSessionRefresh: () => { events.push('refresh') } })
  let rejected = false
  await page.route('**/api/v1/overview**', async (route) => {
    // The first call meets an access token the server no longer accepts.
    if (!rejected) {
      rejected = true
      events.push('overview:401')
      return route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify(unauthorized) })
    }
    events.push('overview')
    return route.fallback()
  })

  await page.goto('/?site=site-a')
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible()
  const rejectedAt = events.indexOf('overview:401')
  expect(rejectedAt).toBeGreaterThanOrEqual(0)
  expect(events.lastIndexOf('refresh')).toBeGreaterThan(rejectedAt)
  expect(events.lastIndexOf('overview')).toBeGreaterThan(events.lastIndexOf('refresh'))
  await expect(page.getByRole('heading', { name: 'Sign in' })).toHaveCount(0)
})

test('A Session that ends mid-work returns to sign-in and then back to the same page', async ({ page }) => {
  await installApiFixtures(page)
  let refreshCalls = 0
  await page.route('**/api/v1/auth/refresh', async (route) => {
    refreshCalls += 1
    // The startup refresh resumes the Session; the next one finds it expired.
    if (refreshCalls === 1) return route.fallback()
    return route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify(unauthorized) })
  })
  let overviewCalls = 0
  await page.route('**/api/v1/overview**', async (route) => {
    overviewCalls += 1
    if (overviewCalls === 1) return route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify(unauthorized) })
    return route.fallback()
  })

  await page.goto('/?site=site-a')
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
  await expect(page.getByText('Your session ended')).toBeVisible()

  await signIn(page)
  await expect(page).toHaveURL('/?site=site-a')
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible()
})

test('Signing out ends the Session on the server, so a reload stays signed out', async ({ page }) => {
  await installApiFixtures(page)
  const logouts: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/v1/auth/logout') logouts.push(request.method())
  })

  await page.goto('/?site=site-a')
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Account menu' }).click()
  await page.getByRole('menuitem', { name: 'Sign out' }).click()

  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
  await expect.poll(() => logouts).toEqual(['POST'])
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
})

test('The access token stays in memory and an old stored token is discarded', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'legacy-token-from-an-older-release')
  })
  await installApiFixtures(page)
  const bearers = new Set<string>()
  page.on('request', (request) => {
    const header = request.headers()['authorization']
    if (header) bearers.add(header)
  })

  await page.goto('/?site=site-a')
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible()
  expect(await page.evaluate(() => localStorage.getItem('access_token'))).toBeNull()
  expect([...bearers]).toEqual(['Bearer e2e-token'])
})
