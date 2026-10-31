import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Subnet DNS (decisions 058 and 071): per provisioner subnet, the DNS servers MAAS gives Automatic
// and Static deployments, saved as swallow's spec from the Subnet DNS tab of the Integration
// settings page by each subnet's own button and applied to MAAS by the backend.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

test('DNS servers are saved for one subnet and settle once MAAS holds them', async ({ page }) => {
  const writes: Array<{ integrationId: string; subnetId: string; body: Record<string, unknown> }> = []
  await installApiFixtures(page, {
    onSubnetDnsRequest: (integrationId, subnetId, body) => writes.push({ integrationId, subnetId, body }),
  })
  await page.goto('/infrastructure/integrations?site=site-a')
  await page.getByRole('row', { name: /MAAS Taipei/ }).getByRole('button', { name: 'Edit' }).click()
  await page.getByRole('tab', { name: 'Subnet DNS' }).click()
  await expect(page).toHaveURL('/infrastructure/integrations/maas-a/subnet-dns?site=site-a')

  const section = page.getByRole('region', { name: 'Subnet DNS' })
  const subnets = section.getByRole('list', { name: 'Subnets' }).getByRole('listitem')
  await expect(subnets).toHaveCount(2)
  // A host-internal bridge MAAS recorded is listed apart, collapsed but still editable.
  const other = section.getByRole('list', { name: 'Other subnets' })
  await expect(other).toBeHidden()
  await section.getByText('Other subnets (1)').click()
  await expect(other.getByRole('textbox', { name: 'DNS servers for 172.17.0.0/16' })).toBeVisible()
  const site = subnets.filter({ hasText: '10.170.168.0/23' })
  // Who runs DHCP is stated in words, because it decides whether the setting matters.
  await expect(site.getByText('DHCP: not MAAS (site DHCP, if any) · Gateway 10.170.169.254')).toBeVisible()
  await expect(subnets.filter({ hasText: '192.168.40.0/24' }).getByText(/^DHCP: MAAS/)).toBeVisible()
  await expect(site.getByRole('status')).toContainText('MAAS reports: its own DNS (none set).')

  // A hostname is flagged before anything reaches MAAS.
  const field = site.getByRole('textbox', { name: 'DNS servers for 10.170.168.0/23' })
  const apply = site.getByRole('button', { name: 'Save DNS' })
  await field.fill('dns.example.com')
  await expect(site.getByText('dns.example.com is not an IP address.')).toBeVisible()
  await expect(apply).toBeDisabled()

  // Enter saves this subnet's DNS; the row shows it being applied, then settles once MAAS holds it.
  await field.fill('165.204.124.158 165.204.219.249')
  await expect(site.getByRole('status')).toContainText('Not saved yet.')
  await field.press('Enter')
  await expect(page.getByText('DNS saved for 10.170.168.0/23')).toBeVisible()
  await expect(site.getByRole('status')).toContainText('Applying')
  await expect(field).toHaveValue('165.204.124.158, 165.204.219.249')
  await expect(site.getByRole('status')).toContainText('MAAS reports: 165.204.124.158, 165.204.219.249.', { timeout: 10_000 })
  await expect(site.getByRole('status')).toContainText('In sync')
  expect(writes).toEqual([{ integrationId: 'maas-a', subnetId: '7', body: { dnsServers: ['165.204.124.158', '165.204.219.249'] } }])

  // Reopening reads the stored setting; clearing the field sends [] and MAAS falls back to its own DNS.
  await page.reload()
  const reopened = page.getByRole('region', { name: 'Subnet DNS' }).getByRole('listitem').filter({ hasText: '10.170.168.0/23' })
  const reopenedField = reopened.getByRole('textbox', { name: 'DNS servers for 10.170.168.0/23' })
  await expect(reopenedField).toHaveValue('165.204.124.158, 165.204.219.249')
  await reopenedField.fill('')
  await reopened.getByRole('button', { name: 'Save DNS' }).click()
  await expect(reopened.getByRole('status')).toContainText('MAAS reports: its own DNS (none set).', { timeout: 10_000 })
  expect(writes.at(-1)).toEqual({ integrationId: 'maas-a', subnetId: '7', body: { dnsServers: [] } })
})
