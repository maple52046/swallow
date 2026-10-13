import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Provisioner HTTP Proxy (decision 056): the proxy MAAS hands to the machines it deploys, read live
// and applied from the HTTP proxy tab of the Integration settings page by its own button — never by
// the General or Connection Save.

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

test('an External proxy is applied to MAAS from the Integration and read back', async ({ page }) => {
  const writes: Array<{ integrationId: string; body: Record<string, unknown> }> = []
  await installApiFixtures(page, { onHttpProxyRequest: (integrationId, body) => writes.push({ integrationId, body }) })
  await page.goto('/infrastructure/integrations?site=site-a')

  await openSettingsTab(page, 'MAAS Taipei', 'HTTP proxy')
  await expect(page).toHaveURL('/infrastructure/integrations/maas-a/http-proxy?site=site-a')
  const proxy = page.getByRole('region', { name: 'HTTP proxy' })
  await expect(proxy.getByRole('status')).toHaveText('MAAS uses: MAAS built-in.')

  await proxy.getByRole('combobox', { name: 'Proxy mode' }).click()
  await page.getByRole('option', { name: 'External', exact: true }).click()
  const apply = proxy.getByRole('button', { name: 'Apply proxy' })
  await expect(apply).toBeDisabled()

  // A URL without a scheme is flagged before anything reaches MAAS.
  const urlField = proxy.getByRole('textbox', { name: /^Proxy URL/ })
  await urlField.fill('10.170.168.20:18080')
  await expect(proxy.getByText('Use http://host:port or https://host:port.')).toBeVisible()
  await expect(apply).toBeDisabled()

  // Enter in the URL applies the proxy; the read-back drops the trailing slash.
  await urlField.fill('http://10.170.168.20:18080/')
  await expect(proxy.getByRole('status')).toHaveText('Not applied yet.')
  await urlField.press('Enter')
  await expect(page.getByText('HTTP proxy applied')).toBeVisible()
  await expect(proxy.getByRole('status')).toHaveText('MAAS uses: External (http://10.170.168.20:18080).')
  await expect(urlField).toHaveValue('http://10.170.168.20:18080')
  expect(writes).toEqual([{ integrationId: 'maas-a', body: { mode: 'external', httpProxy: 'http://10.170.168.20:18080/' } }])

  // Reopening the tab reads MAAS again rather than remembering the form.
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

test('turning the proxy off sends no URL and shows why MAAS refused it', async ({ page }) => {
  const writes: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    httpProxies: { 'maas-a': { mode: 'peer', httpProxy: 'http://squid.example:3128' } },
    httpProxyWriteForbidden: true,
    onHttpProxyRequest: (_integrationId, body) => writes.push(body),
  })
  await page.goto('/infrastructure/integrations/maas-a/http-proxy?site=site-a')

  const proxy = page.getByRole('region', { name: 'HTTP proxy' })
  await expect(proxy.getByRole('combobox', { name: 'Proxy mode' })).toHaveText('Peer')
  await expect(proxy.getByRole('status')).toHaveText('MAAS uses: Peer (http://squid.example:3128).')

  await proxy.getByRole('combobox', { name: 'Proxy mode' }).click()
  await page.getByRole('option', { name: "Don't use a proxy", exact: true }).click()
  await expect(proxy.getByRole('textbox', { name: /^Proxy URL/ })).toHaveCount(0)
  await proxy.getByRole('button', { name: 'Apply proxy' }).click()

  await expect(proxy.getByText('HTTP proxy could not be applied')).toBeVisible()
  await expect(proxy.getByText(/its MAAS account must be an administrator/)).toBeVisible()
  await expect(proxy.getByRole('status')).toHaveText('Not applied yet.')
  expect(writes).toEqual([{ mode: 'none', httpProxy: '' }])
})
