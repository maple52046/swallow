import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Provisioner HTTP Proxy (decision 056): the proxy MAAS hands to the machines it deploys, read live
// and applied from the Edit integration dialog by its own button — never by the dialog's Save.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

async function openEditDialog(page: Page, integrationName: string) {
  await page.getByRole('row', { name: new RegExp(integrationName) }).getByRole('button', { name: 'Edit' }).click()
  return page.getByRole('dialog', { name: 'Edit integration' })
}

test('an External proxy is applied to MAAS from the Integration and read back', async ({ page }) => {
  const writes: Array<{ integrationId: string; body: Record<string, unknown> }> = []
  await installApiFixtures(page, { onHttpProxyRequest: (integrationId, body) => writes.push({ integrationId, body }) })
  await page.goto('/infrastructure/integrations?site=site-a')

  let dialog = await openEditDialog(page, 'MAAS Taipei')
  const proxy = dialog.getByRole('region', { name: 'HTTP proxy' })
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

  // Enter in the URL applies the proxy instead of saving the Integration; the read-back drops the
  // trailing slash.
  await urlField.fill('http://10.170.168.20:18080/')
  await expect(proxy.getByRole('status')).toHaveText('Not applied yet. Save changes does not apply the proxy.')
  await urlField.press('Enter')
  await expect(page.getByText('HTTP proxy applied')).toBeVisible()
  await expect(proxy.getByRole('status')).toHaveText('MAAS uses: External (http://10.170.168.20:18080).')
  await expect(urlField).toHaveValue('http://10.170.168.20:18080')
  await expect(dialog).toBeVisible()
  expect(writes).toEqual([{ integrationId: 'maas-a', body: { mode: 'external', httpProxy: 'http://10.170.168.20:18080/' } }])

  // Reopening reads MAAS again rather than remembering the form.
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  dialog = await openEditDialog(page, 'MAAS Taipei')
  await expect(dialog.getByRole('combobox', { name: 'Proxy mode' })).toHaveText('External')
  await expect(dialog.getByRole('textbox', { name: /^Proxy URL/ })).toHaveValue('http://10.170.168.20:18080')
  await dialog.getByRole('button', { name: 'Cancel' }).click()

  // Only MAAS provisioners have the section.
  dialog = await openEditDialog(page, 'Prometheus Taipei')
  await expect(dialog.getByRole('heading', { name: 'HTTP proxy' })).toHaveCount(0)
})

test('turning the proxy off sends no URL and shows why MAAS refused it', async ({ page }) => {
  const writes: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    httpProxies: { 'maas-a': { mode: 'peer', httpProxy: 'http://squid.example:3128' } },
    httpProxyWriteForbidden: true,
    onHttpProxyRequest: (_integrationId, body) => writes.push(body),
  })
  await page.goto('/infrastructure/integrations?site=site-a')

  const dialog = await openEditDialog(page, 'MAAS Taipei')
  const proxy = dialog.getByRole('region', { name: 'HTTP proxy' })
  await expect(proxy.getByRole('combobox', { name: 'Proxy mode' })).toHaveText('Peer')
  await expect(proxy.getByRole('status')).toHaveText('MAAS uses: Peer (http://squid.example:3128).')

  await proxy.getByRole('combobox', { name: 'Proxy mode' }).click()
  await page.getByRole('option', { name: "Don't use a proxy", exact: true }).click()
  await expect(proxy.getByRole('textbox', { name: /^Proxy URL/ })).toHaveCount(0)
  await proxy.getByRole('button', { name: 'Apply proxy' }).click()

  await expect(proxy.getByText('HTTP proxy could not be applied')).toBeVisible()
  await expect(proxy.getByText(/its MAAS account must be an administrator/)).toBeVisible()
  await expect(proxy.getByRole('status')).toHaveText('Not applied yet. Save changes does not apply the proxy.')
  expect(writes).toEqual([{ mode: 'none', httpProxy: '' }])
})
