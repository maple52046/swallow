import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Provisioner HTTP Proxy (decisions 056 and 071): the proxy MAAS hands to the machines it deploys,
// saved as swallow's spec from the HTTP proxy tab of the Integration settings page by its own button
// — never by the General or Connection Save — and applied to MAAS by the backend.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

/** Opens an Integration's settings from its list row and switches to `tab`. */
async function openSettingsTab(page: Page, integrationName: string, tab: string) {
  await page.getByRole('row', { name: new RegExp(integrationName) }).getByRole('button', { name: 'Edit' }).click()
  await expect(page.getByRole('heading', { level: 1, name: integrationName })).toBeVisible()
  await page.getByRole('tab', { name: tab }).click()
}

test('an External proxy is saved from the Integration and settles once MAAS holds it', async ({ page }) => {
  const writes: Array<{ integrationId: string; body: Record<string, unknown> }> = []
  await installApiFixtures(page, { onHttpProxyRequest: (integrationId, body) => writes.push({ integrationId, body }) })
  await page.goto('/infrastructure/integrations?site=site-a')

  await openSettingsTab(page, 'MAAS Taipei', 'HTTP proxy')
  await expect(page).toHaveURL('/infrastructure/integrations/maas-a/http-proxy?site=site-a')
  const proxy = page.getByRole('region', { name: 'HTTP proxy' })
  await expect(proxy.getByRole('status')).toContainText('In sync')
  await expect(proxy.getByRole('status')).toContainText('MAAS reports: MAAS built-in.')

  await proxy.getByRole('combobox', { name: 'Proxy mode' }).click()
  await page.getByRole('option', { name: 'External', exact: true }).click()
  const apply = proxy.getByRole('button', { name: 'Save proxy' })
  await expect(apply).toBeDisabled()

  // A URL without a scheme is flagged before anything reaches MAAS.
  const urlField = proxy.getByRole('textbox', { name: /^Proxy URL/ })
  await urlField.fill('10.170.168.20:18080')
  await expect(proxy.getByText('Use http://host:port or https://host:port.')).toBeVisible()
  await expect(apply).toBeDisabled()

  // Enter in the URL saves the proxy; the stored spec drops the trailing slash. The tab shows it
  // being applied, then settles once MAAS holds it.
  await urlField.fill('http://10.170.168.20:18080/')
  await expect(proxy.getByRole('status')).toContainText('Not saved yet.')
  await urlField.press('Enter')
  await expect(page.getByText('HTTP proxy saved')).toBeVisible()
  await expect(proxy.getByRole('status')).toContainText('Applying')
  await expect(urlField).toHaveValue('http://10.170.168.20:18080')
  await expect(proxy.getByRole('status')).toContainText('In sync', { timeout: 10_000 })
  await expect(proxy.getByRole('status')).toContainText('MAAS reports: External (http://10.170.168.20:18080).')
  expect(writes).toEqual([{ integrationId: 'maas-a', body: { mode: 'external', httpProxy: 'http://10.170.168.20:18080/' } }])

  // Reopening the tab reads the stored setting rather than remembering the form.
  await page.reload()
  await expect(page.getByRole('combobox', { name: 'Proxy mode' })).toHaveText('External')
  await expect(page.getByRole('textbox', { name: /^Proxy URL/ })).toHaveValue('http://10.170.168.20:18080')

  // Only MAAS provisioners have the tab; a deep link to it from another Integration lands on General.
  await page.goto('/infrastructure/integrations?site=site-a')
  await page.getByRole('row', { name: /Prometheus Taipei/ }).getByRole('button', { name: 'Edit' }).click()
  await expect(page.getByRole('tab', { name: 'General' })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'HTTP proxy' })).toHaveCount(0)
  const prometheusUrl = new URL(page.url())
  await page.goto(prometheusUrl.pathname.replace(/\/general$/, '/http-proxy') + prometheusUrl.search)
  await expect(page).toHaveURL(/\/general\?site=site-a$/)
})

test('turning the proxy off sends no URL and shows why MAAS refused to apply it', async ({ page }) => {
  const writes: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    httpProxies: { 'maas-a': { mode: 'peer', httpProxy: 'http://squid.example:3128' } },
    httpProxyWriteForbidden: true,
    onHttpProxyRequest: (_integrationId, body) => writes.push(body),
  })
  await page.goto('/infrastructure/integrations/maas-a/http-proxy?site=site-a')

  const proxy = page.getByRole('region', { name: 'HTTP proxy' })
  await expect(proxy.getByRole('combobox', { name: 'Proxy mode' })).toHaveText('Peer')
  await expect(proxy.getByRole('status')).toContainText('MAAS reports: Peer (http://squid.example:3128).')

  await proxy.getByRole('combobox', { name: 'Proxy mode' }).click()
  await page.getByRole('option', { name: "Don't use a proxy", exact: true }).click()
  await expect(proxy.getByRole('textbox', { name: /^Proxy URL/ })).toHaveCount(0)
  await proxy.getByRole('button', { name: 'Save proxy' }).click()

  // The spec is saved; MAAS refuses to apply it, which the status reports once the tab re-reads it.
  await expect(page.getByText('HTTP proxy saved')).toBeVisible()
  await expect(proxy.getByRole('status')).toContainText('Needs attention', { timeout: 10_000 })
  await expect(proxy.getByText(/its MAAS account must be an administrator/)).toBeVisible()
  await expect(proxy.getByRole('status')).toContainText('MAAS reports: Peer (http://squid.example:3128).')
  expect(writes).toEqual([{ mode: 'none', httpProxy: '' }])
})
