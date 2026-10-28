import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
  await installApiFixtures(page, { freePlatformCandidates: true })
})

test('Platform catalog uses keyboard-accessible runtime links and preserves Site scope', async ({ page }) => {
  await page.goto('/platforms/deploy?site=site-a')

  const kubernetes = page.getByRole('link', { name: 'Configure Kubernetes' })
  const slurm = page.getByRole('link', { name: 'Configure Slurm' })
  await expect(kubernetes).toHaveAttribute('href', '/platforms/deploy/kubernetes?site=site-a')
  await expect(slurm).toHaveAttribute('href', '/platforms/deploy/slurm?site=site-a')
  const [kubernetesHeight, slurmHeight] = await Promise.all([
    kubernetes.evaluate((element) => element.getBoundingClientRect().height),
    slurm.evaluate((element) => element.getBoundingClientRect().height),
  ])
  expect(kubernetesHeight).toBe(slurmHeight)

  await kubernetes.focus()
  await expect(kubernetes).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL('/platforms/deploy/kubernetes?site=site-a')
  await expect(page.getByRole('heading', { name: 'Deploy Kubernetes Platform' })).toBeVisible()
  await expect(page.getByLabel('k0s version')).toBeVisible()
  await expect(page.getByLabel('Cluster name')).toHaveCount(0)
})

test('fixed Slurm route survives refresh and does not expose Kubernetes settings', async ({ page }) => {
  await page.goto('/platforms/deploy/slurm?site=site-a')
  await expect(page.getByRole('heading', { name: 'Deploy Slurm Platform' })).toBeVisible()
  await expect(page.locator('[data-scope="steps"][data-part="root"]')).toHaveAttribute('data-orientation', 'horizontal')
  await expect(page.getByLabel('k0s version')).toHaveCount(0)
  await page.reload()
  await expect(page).toHaveURL('/platforms/deploy/slurm?site=site-a')
  await expect(page.getByRole('heading', { name: 'Deploy Slurm Platform' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Change platform' })).toHaveAttribute('href', '/platforms/deploy?site=site-a')
})

test('feature enablement expands settings only after its Switch is enabled', async ({ page }) => {
  await page.goto('/platforms/deploy/kubernetes?site=site-a')
  await page.getByLabel('Platform name').fill('feature-switches')
  await page.getByRole('button', { name: 'Next' }).click()
  for (const server of ['gpu-node-01', 'gpu-node-03', 'gpu-node-04']) {
    await page.getByRole('combobox', { name: `Role for ${server}` }).click()
    await page.getByRole('option', { name: 'Control-plane' }).click()
  }
  await page.getByRole('combobox', { name: 'Role for gpu-node-02' }).click()
  await page.getByRole('option', { name: 'Worker' }).click()
  await expect(page.getByRole('button', { name: 'Next' })).toBeEnabled()
  await expect(page.getByText('1 workload-capable', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Next' }).click()

  const feature = page.getByRole('switch', { name: 'Enable AMD GPU Operator' })
  await expect(feature).not.toBeChecked()
  await expect(page.getByRole('combobox', { name: 'GPU allocation mode' })).toHaveCount(0)
  await feature.locator('xpath=following-sibling::*[1]').click()
  await expect(feature).toBeChecked()
  await expect(page.getByRole('combobox', { name: 'GPU allocation mode' })).toBeVisible()
  await expect(page.getByRole('switch', { name: 'Node Labeller' })).toBeChecked()
})
test('Server picker becomes information-equivalent cards on a narrow viewport without page overflow', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/platforms/deploy/kubernetes?site=site-a')
  await page.getByLabel('Platform name').fill('mobile-k0s')
  await page.getByRole('button', { name: 'Next' }).click()

  const card = page.getByRole('article').filter({ hasText: 'gpu-node-01' })
  await expect(page.locator('.sw-wizard__stepbar')).toBeVisible()
  await expect(page.locator('[data-scope="steps"][data-part="root"]')).toHaveAttribute('data-orientation', 'horizontal')
  await expect.poll(() => page.locator('.sw-wizard__stepbar').evaluate((element) => {
    const rect = element.getBoundingClientRect()
    return rect.bottom > 0 && rect.top < window.innerHeight
  })).toBe(true)
  await expect(card).toBeVisible()
  await expect(page.getByRole('table', { name: 'Deployable Servers' })).toBeHidden()
  await expect(card.getByRole('combobox', { name: 'Role for gpu-node-01' })).toBeVisible()
  await expect.poll(() => page.evaluate(() => (
    document.documentElement.scrollWidth <= document.documentElement.clientWidth
  ))).toBe(true)
})

test('Slurm Review groups keep feature ownership and return directly to editing', async ({ page }) => {
  await page.goto('/platforms/deploy/slurm?site=site-a')
  await page.getByLabel('Platform name').fill('review-slurm')
  const next = page.getByRole('button', { name: 'Next' })
  await next.click()
  await page.getByRole('checkbox', { name: 'Run slurmctld on gpu-node-01' }).locator('..').click()
  await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-02' }).locator('..').click()
  await next.click()
  await next.click()

  await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Nodes and roles' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Shared workload storage' })).toBeVisible()
  const runtimeGroup = page.locator('.sw-platform-review-card').filter({
    has: page.getByRole('heading', { name: 'Container runtime' }),
  })
  await runtimeGroup.getByRole('button', { name: 'Edit' }).click()
  await expect(page.getByRole('heading', { name: 'Slurm features' })).toBeVisible()
})

test('changing Site confirms before clearing Site-bound choices and restarts the flow', async ({ page }) => {
  await page.goto('/platforms/deploy/kubernetes?site=site-a')
  await page.getByLabel('Platform name').fill('site-reset-k0s')
  const next = page.getByRole('button', { name: 'Next' })
  await next.click()
  await page.getByRole('radio', { name: /Standalone/ }).locator('xpath=following-sibling::*[1]').click()
  await page.getByRole('combobox', { name: 'Role for gpu-node-01' }).click()
  await page.getByRole('option', { name: 'Standalone node' }).click()
  await page.getByRole('button', { name: 'Back' }).click()

  page.once('dialog', (dialog) => dialog.dismiss())
  await page.getByRole('combobox', { name: 'Site' }).click()
  await page.getByRole('option', { name: 'Hsinchu Edge' }).click()
  await expect(page.getByRole('combobox', { name: 'Site' })).toContainText('Taipei Lab')

  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('combobox', { name: 'Site' }).click()
  await page.getByRole('option', { name: 'Hsinchu Edge' }).click()
  await expect(page.getByRole('combobox', { name: 'Site' })).toContainText('Hsinchu Edge')
  await next.click()
  await expect(page.getByRole('heading', { name: 'Topology and machines' })).toBeVisible()
  await expect(page.getByText('No deployable Servers')).toBeVisible()
})

test('Slurm with shared storage switched off omits the workload-storage intent', async ({ page }) => {
  let submitted: Record<string, unknown> | undefined
  await page.route('**/api/v1/platforms/deploy', async (route) => {
    submitted = route.request().postDataJSON() as Record<string, unknown>
    await route.fallback()
  })
  await page.goto('/platforms/deploy/slurm?site=site-a')
  await page.getByLabel('Platform name').fill('no-workload-nfs')
  const next = page.getByRole('button', { name: 'Next' })
  await next.click()
  await page.getByRole('checkbox', { name: 'Run slurmctld on gpu-node-01' }).locator('..').click()
  await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-02' }).locator('..').click()
  await next.click()
  await expect(page.getByRole('switch', { name: 'Enable Shared workload storage' })).not.toBeChecked()
  await next.click()
  await page.getByRole('button', { name: 'Deploy Slurm Platform' }).click()

  await expect(page).toHaveURL('/platforms/platform-new?site=site-a')
  const slurm = submitted?.slurm as Record<string, unknown> | undefined
  expect(slurm?.workloadStorage).toBeUndefined()
})

// The time-sync check runs on every Platform deployment (decision 065); the Features switch only
// chooses whether an unsynchronized node fails it (enforce, the default) or is reported (warn).
test('the clock check is required by default and can be relaxed to a warning', async ({ page }) => {
  const submitted: Array<Record<string, unknown>> = []
  await page.route('**/api/v1/platforms/deploy', async (route) => {
    submitted.push(route.request().postDataJSON() as Record<string, unknown>)
    await route.fallback()
  })
  // Each deployment claims its Servers, so the second one uses the other two.
  const deploySlurm = async (relax: boolean, controller: string, compute: string) => {
    await page.goto('/platforms/deploy/slurm?site=site-a')
    await page.getByLabel('Platform name').fill(relax ? 'clock-warn' : 'clock-enforce')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()
    await page.getByRole('checkbox', { name: `Run slurmctld on ${controller}` }).locator('..').click()
    await page.getByRole('checkbox', { name: `Run slurmd on ${compute}` }).locator('..').click()
    await next.click()
    const clocks = page.getByRole('switch', { name: 'Require synchronized clocks' })
    await expect(clocks).toBeChecked()
    if (relax) {
      await clocks.locator('xpath=following-sibling::*[1]').click()
      await expect(clocks).not.toBeChecked()
      await expect(page.getByText(/only reported as a warning/)).toBeVisible()
    }
    await next.click()
    await expect(page.getByText(relax ? /^Warning only/ : /^Required: an unsynchronized node/)).toBeVisible()
    await page.getByRole('button', { name: 'Deploy Slurm Platform' }).click()
    await expect(page).toHaveURL('/platforms/platform-new?site=site-a')
  }
  await deploySlurm(false, 'gpu-node-01', 'gpu-node-02')
  await deploySlurm(true, 'gpu-node-03', 'gpu-node-04')
  expect(submitted.map((body) => body.timeSyncCheck)).toEqual(['enforce', 'warn'])
})
