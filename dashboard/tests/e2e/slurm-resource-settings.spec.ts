import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

async function chooseSingleSelectOption(
  page: import('playwright/test').Page,
  fieldLabel: string,
  optionLabel: string,
) {
  await page.getByRole('combobox', { name: fieldLabel, exact: true }).click()
  await page.getByRole('option', { name: optionLabel, exact: true }).click()
}

test.beforeEach(async ({ page }) => {
  await installApiFixtures(page, { freePlatformCandidates: true, minimumResourceCandidates: true })
  await page.addInitScript(() => localStorage.setItem('access_token', 'e2e-token'))
})

test('Slurm settings enforce 24 GiB eligibility and disabling restores selection', async ({ page }) => {
  await page.goto('/platforms/settings?site=site-a')
  await expect(page.getByRole('heading', { name: 'Platform settings' })).toBeVisible()
  await page.getByRole('checkbox', { name: 'Enforce a minimum for Slurm deployment nodes' }).locator('..').click()

  const save = page.getByRole('button', { name: 'Save settings' })
  await page.getByLabel('CPU cores').fill('4.5')
  await expect(save).toBeDisabled()
  await page.getByLabel('CPU cores').fill('4')
  await page.getByLabel('Memory GiB').fill('0')
  await expect(save).toBeDisabled()
  await page.getByLabel('Memory GiB').fill('24')
  await page.getByLabel('Storage GB').fill('0')
  await expect(save).toBeDisabled()
  await page.getByLabel('Storage GB').fill('80')
  await save.click()
  await expect(page.getByText('Current policy: 4 cores / 24 GiB / 80 GB')).toBeVisible()

  await page.goto('/platforms/deploy?site=site-a')
  await chooseSingleSelectOption(page, 'Platform type', 'Slurm')
  await page.getByLabel('Platform name').fill('minimum-resource-slurm')
  await page.getByRole('button', { name: 'Next' }).click()
  const candidates = page.getByRole('table', { name: 'Deployable Servers' })
  const eligible = candidates.getByRole('row', { name: /gpu-node-01/ })
  const ineligible = candidates.getByRole('row', { name: /gpu-node-02/ })
  await expect(eligible).toContainText('Eligible')
  await expect(eligible.getByRole('checkbox', { name: 'Run slurmctld on gpu-node-01' })).toBeEnabled()
  await expect(ineligible).toContainText('Memory 16 GiB; minimum 24 GiB')
  await expect(ineligible.getByRole('checkbox', { name: /slurmd on gpu-node-02, unavailable/ })).toBeDisabled()

  await page.goto('/platforms/settings?site=site-a')
  await page.getByRole('checkbox', { name: 'Enforce a minimum for Slurm deployment nodes' }).locator('..').click()
  await page.getByRole('button', { name: 'Save settings' }).click()
  await expect(page.getByText('Current policy: Disabled')).toBeVisible()

  await page.goto('/platforms/deploy?site=site-a')
  await chooseSingleSelectOption(page, 'Platform type', 'Slurm')
  await page.getByLabel('Platform name').fill('policy-disabled-slurm')
  await page.getByRole('button', { name: 'Next' }).click()
  await expect(page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-02' })).toBeEnabled()
})
