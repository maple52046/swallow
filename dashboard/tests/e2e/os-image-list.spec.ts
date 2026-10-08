import { expect, test, type Route } from 'playwright/test'
import { installApiFixtures } from './fixtures'

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

test.beforeEach(async ({ page }) => {
  await installApiFixtures(page)
})

test('catalog overview stays scope-wide while discovery search resets selection', async ({ page }) => {
  await page.goto('/provisioning/images?site=site-a')

  const overview = page.getByRole('region', { name: 'Image catalog' })
  await expect(overview.getByText('Images loaded').locator('..')).toContainText('3')
  await expect(overview.getByText('Disk supported').locator('..')).toContainText('2')
  await expect(overview.getByText('RAM supported').locator('..')).toContainText('3')
  await expect(overview.getByText('Failed tests').locator('..')).toContainText('1')

  const jammyTitle = page.getByRole('row', { name: 'Ubuntu 22.04 LTS image', exact: true }).first()
  const jammyData = page.getByRole('row', { name: 'Ubuntu 22.04 LTS catalog data', exact: true }).first()
  const osFamily = jammyTitle.locator('td[colspan="6"] .sw-os-image-os-family')
  await expect(osFamily).toHaveText('ubuntu')
  await expect(osFamily).toHaveCSS('border-radius', '0px')
  await expect(osFamily).toHaveCSS('height', '18px')
  await expect(jammyTitle.locator('td[colspan="6"] strong')).toHaveText('Ubuntu 22.04 LTS')
  await expect(jammyData.getByText('Supported', { exact: true })).toHaveCount(2)
  await expect(jammyData.getByText('ubuntu/jammy', { exact: true })).toBeVisible()
  await expect(jammyData).toContainText('amd64 · 4 GiB')
  await expect(jammyData.getByRole('button', { name: 'Deploy Ubuntu 22.04 LTS' })).toBeVisible()
  await jammyTitle.getByRole('checkbox', { name: 'Select Ubuntu 22.04 LTS' }).check({ force: true })
  await expect(page.getByRole('region', { name: 'Selection actions' })).toBeVisible()

  await page.getByRole('textbox', { name: 'Search OS images' }).fill('ROCm')
  await expect.poll(() => new URL(page.url()).searchParams.get('q')).toBe('ROCm')
  await expect(page.getByRole('region', { name: 'Selection actions' })).toHaveCount(0)
  await expect(page.getByRole('table', { name: 'OS images' }).getByRole('row', { name: 'Ubuntu 24.04 ROCm catalog data', exact: true })).toHaveCount(1)
  await expect(overview.getByText('Images loaded').locator('..')).toContainText('3')
  await expect(page.getByRole('region', { name: 'Deployment images' })).toContainText('Showing 1–1 of 1 images')
})

test('manual refresh keeps last-good images when the integration read fails', async ({ page }) => {
  let failRefresh = false
  await page.route('**/api/v1/integrations?*', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET' || url.searchParams.get('kind') !== 'provisioner') return route.fallback()
    if (failRefresh) return json(route, { error: { code: 'unavailable', message: 'Integration registry unavailable.' } }, 503)
    return route.fallback()
  })

  await page.goto('/provisioning/images?site=site-a')
  const catalogRow = page.getByRole('row', { name: 'Ubuntu 22.04 LTS image', exact: true }).first()
  await expect(catalogRow).toBeVisible()
  failRefresh = true
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect(page.getByText('Catalog refresh failed')).toBeVisible()
  await expect(catalogRow).toBeVisible()
})

test('deploy-mode filters, display state, and legacy focus remain URL-owned', async ({ page }) => {
  await page.goto('/provisioning/images?site=site-a&view=verification_failed&target=disk&readiness=failed&group=os&sort=name&dir=desc')
  await expect(page.getByRole('button', { name: 'Failed tests' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('row', { name: 'Ubuntu 24.04 ROCm catalog data', exact: true })).toHaveCount(1)
  await expect(page.getByRole('row', { name: 'Ubuntu 22.04 LTS catalog data', exact: true })).toHaveCount(0)
  await expect.poll(() => new URL(page.url()).searchParams.get('site')).toBe('site-a')

  await page.goto('/provisioning/images?site=site-a&readiness=failed')
  await expect.poll(() => new URL(page.url()).searchParams.has('readiness')).toBe(false)
  await expect(page.getByRole('row', { name: 'Ubuntu 22.04 LTS catalog data', exact: true })).toHaveCount(1)

  await page.goto('/provisioning/images?site=site-a&view=overridden')
  await expect.poll(() => new URL(page.url()).searchParams.has('view')).toBe(false)
  await expect(page.getByRole('button', { name: 'All', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect.poll(() => new URL(page.url()).searchParams.get('site')).toBe('site-a')

  await page.goto('/provisioning/images?site=site-a&integrationId=maas-a&imageName=Ubuntu%2024.04%20LTS&architecture=amd64&osSystem=ubuntu&release=noble')
  await expect(page.getByRole('row', { name: 'Ubuntu 24.04 LTS catalog data', exact: true })).toHaveCount(1)
  await expect(page.getByRole('row', { name: 'Ubuntu 22.04 LTS catalog data', exact: true })).toHaveCount(0)
  await page.getByRole('textbox', { name: 'Search OS images' }).fill('jammy')
  await expect.poll(() => new URL(page.url()).searchParams.get('q')).toBe('jammy')
  await expect.poll(() => new URL(page.url()).searchParams.has('imageName')).toBe(false)
})

test('deployment-test activity drives View, Review, Test, and Deploy actions without provider collisions', async ({ page }) => {
  await page.setViewportSize({ width: 1600, height: 900 })
  const image = (id: string, name: string, providerOsSystem: string, verified: string[] = [], failed: string[] = []) => ({
    id,
    name,
    providerName: name,
    osSystem: providerOsSystem,
    providerOsSystem,
    release: id.split('/').at(-1) ?? id,
    providerRelease: id.split('/').at(-1) ?? id,
    tags: [],
    architecture: 'amd64',
    sizeBytes: 1024,
    verifiedDeployTargets: verified,
    failedDeployTargets: failed,
  })
  const workflow = (id: string, imageId: string, target: 'disk' | 'ram', status: 'running' | 'requires_attention') => ({
    id,
    schemaVersion: 3,
    kind: 'verify-os-image',
    intent: `Verify ${imageId}`,
    intentSnapshot: { request: { integrationId: 'maas-a', imageId, architecture: 'amd64', deployTarget: target } },
    status,
    statusReason: status === 'requires_attention' ? 'Provider access must be reviewed.' : null,
    siteId: 'site-a',
    platformId: null,
    targetServerIds: ['srv-1'],
    steps: [],
    retryOfOperationId: null,
    execution: { runId: `run-${id}`, playbook: '', status, statusReason: null, startedAt: '2026-08-27T02:00:00Z', finishedAt: null },
    requestedBy: 'admin',
    requestedAt: '2026-08-27T02:00:00Z',
    updatedAt: '2026-08-27T02:05:00Z',
  })

  await page.route('**/api/v1/provisioning/images?*', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET') return route.fallback()
    if (url.searchParams.get('integrationId') !== 'maas-a') {
      return json(route, [image('custom/running', 'Running image test', 'custom')])
    }
    return json(route, [
      image('ubuntu/jammy', 'Ubuntu 22.04 LTS', 'ubuntu'),
      { ...image('ubuntu/golden', 'Ubuntu Golden', 'ubuntu'), name: 'GPU baseline', customName: 'GPU baseline', tags: ['gpu', 'production', 'stable'], defaultUser: 'cloud-user', customDefaultUser: 'cloud-user' },
      image('custom/running', 'Running image test', 'custom'),
      image('custom/attention', 'Attention image test', 'custom'),
      image('custom/untested', 'Untested image', 'custom'),
    ])
  })
  await page.route('**/api/v1/workflows?*', async (route) => {
    const url = new URL(route.request().url())
    if (url.searchParams.get('kind') !== 'verify-os-image') return route.fallback()
    const items = [
      workflow('verify-running', 'custom/running', 'disk', 'running'),
      workflow('verify-attention', 'custom/attention', 'ram', 'requires_attention'),
    ]
    return json(route, { items, total: items.length, page: 1, pageSize: 100 })
  })

  await page.route('**/api/v1/workflows/verify-legacy', async (route) => {
    return json(route, {
      ...workflow('verify-legacy', 'legacy/image', 'disk', 'running'),
      intent: 'Verify legacy image',
      intentSnapshot: undefined,
    })
  })

  await page.route('**/api/v1/workflows/verify-running', async (route) => {
    const detail = {
      ...workflow('verify-running', 'custom/running', 'disk', 'running'),
      definition: 'os-image-verification',
      definitionVersion: 1,
      temporal: { workflowId: 'swallow-operation/verify-running', runId: 'run-verify-running' },
      steps: [
        {
          id: 'provision-srv-1', kind: 'provision-os', name: 'Verify custom/running (disk deploy) on gpu-node-01',
          executor: 'maas', dependsOn: [], targets: [{ kind: 'server', id: 'srv-1' }], status: 'running',
          attempt: 1, progress: 0, error: null, externalExecution: null, artifacts: [],
          startedAt: '2026-08-27T02:01:00Z', finishedAt: null,
        },
        {
          id: 'record-verification', kind: 'record-image-verification', name: 'Record image verification',
          executor: 'internal', dependsOn: ['provision-srv-1'], targets: [{ kind: 'server', id: 'srv-1' }], status: 'pending',
          attempt: 1, progress: 0, error: null, externalExecution: null, artifacts: [],
          startedAt: null, finishedAt: null,
        },
      ],
    }
    return json(route, detail)
  })

  await page.goto('/provisioning/images')
  const governedTitle = page.getByRole('row', { name: 'GPU baseline image', exact: true })
  const governed = page.getByRole('row', { name: 'GPU baseline catalog data', exact: true })
  await expect(governed.getByText('gpu', { exact: true })).toBeVisible()
  await expect(governed.getByText('production', { exact: true })).toBeVisible()
  await expect(governed.getByText('stable', { exact: true })).toBeVisible()
  await expect(governed.getByText(/^\+\d+$/)).toHaveCount(0)
  const tags = governed.locator('.sw-os-image-col--tags')
  await expect(tags.getByRole('button')).toHaveCount(0)
  await expect(tags.locator('.sw-resource-tag')).toHaveCount(3)
  await expect(tags.locator('.sw-resource-tag').first()).toHaveCSS('border-radius', '4px')
  await expect(governedTitle.locator('.sw-os-image-os-family')).toHaveCSS('border-radius', '0px')
  await governed.getByRole('button', { name: 'More actions for GPU baseline' }).click()
  await page.getByRole('menuitem', { name: 'Edit image settings' }).click()
  await expect(page.getByRole('heading', { name: 'Edit OS image' })).toBeVisible()
  await page.getByRole('button', { name: 'Cancel' }).click()
  await governedTitle.getByRole('button', { name: 'Show details for GPU baseline' }).click()
  const governedDetails = page.locator('.sw-os-image-detail-row').filter({ hasText: 'GPU baseline' })
  await expect(governedDetails).toContainText('gpu')
  await expect(governedDetails).toContainText('production')
  await expect(governedDetails).toContainText('stable')
  await expect(governedDetails).toContainText('cloud-user')

  const running = page.getByRole('row', { name: 'Running image test catalog data', exact: true }).filter({ hasText: 'MAAS Taipei' })
  const viewAction = running.getByRole('link', { name: 'View workflow' })
  await expect(viewAction).toHaveAttribute('href', '/workflows/verify-running')
  await expect(viewAction).toHaveText('View')
  await expect(running.getByText('Testing', { exact: true })).toBeVisible()
  await page.setViewportSize({ width: 1280, height: 800 })
  const runningActions = running.locator('.sw-os-image-col--action')
  await expect.poll(() => runningActions.evaluate((cell) => cell.scrollWidth <= cell.clientWidth)).toBe(true)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

  const providerCollision = page.getByRole('row', { name: 'Running image test catalog data', exact: true }).filter({ hasText: 'MAAS Edge' })
  await expect(providerCollision.getByRole('link', { name: 'View workflow' })).toHaveCount(0)
  await expect(providerCollision.getByRole('button', { name: 'Test deployment', exact: true })).toBeVisible()

  const attention = page.getByRole('row', { name: 'Attention image test catalog data', exact: true })
  const reviewAction = attention.getByRole('link', { name: 'Review workflow' })
  await expect(reviewAction).toHaveAttribute('href', '/workflows/verify-attention')
  await expect(reviewAction).toHaveText('Review')
  await expect(attention.getByText('Needs attention', { exact: true })).toBeVisible()

  const untestedTitle = page.getByRole('row', { name: 'Untested image image', exact: true })
  const untested = page.getByRole('row', { name: 'Untested image catalog data', exact: true })
  const testAction = untested.getByRole('button', { name: 'Test deployment', exact: true })
  await expect(testAction).toHaveText('Test')
  const actionWidths = await Promise.all([viewAction, reviewAction, testAction].map((control) => control.evaluate((element) => Math.round(element.getBoundingClientRect().width))))
  expect(new Set(actionWidths).size).toBe(1)
  await testAction.click()
  await expect(page.getByRole('heading', { name: 'Test image deployment' })).toBeVisible()
  await expect(page.getByRole('radio', { name: /Disk deploy/ })).toBeChecked()
  await expect(page.getByText(/only checks the selected deploy mode and SSH access/i)).toBeVisible()
  await expect(page.getByText(/does not certify platform compatibility/i)).toBeVisible()
  await page.getByRole('button', { name: 'Cancel' }).click()

  await expect(untestedTitle.getByText('custom', { exact: true })).toBeVisible()
  await expect(untested.getByText('Not tested', { exact: true })).toHaveCount(2)

  const provider = page.getByRole('row', { name: 'Ubuntu 22.04 LTS catalog data', exact: true })
  const deployAction = provider.getByRole('button', { name: 'Deploy Ubuntu 22.04 LTS' })
  await expect(deployAction).toHaveText('Deploy')
  await expect(deployAction.locator('svg')).toHaveCount(1)
  expect(Math.round((await deployAction.boundingBox())?.width ?? 0)).toBe(actionWidths[0])
  await deployAction.click()
  const deployDialog = page.getByRole('dialog', { name: 'Deploy OS' })
  await expect(page).toHaveURL('/provisioning/images')
  await expect(deployDialog).toContainText('The image is fixed from the catalog.')
  await expect(
    deployDialog
      .getByRole('navigation', { name: 'Deployment progress' })
      .locator('li')
      .filter({ hasText: 'Targets' }),
  ).toHaveAttribute('aria-current', 'step')
  await deployDialog.getByRole('button', { name: 'Close' }).click()


  await page.goto('/workflows/verify-running')
  await expect(page.getByRole('heading', { name: 'Test OS image custom/running for Disk deployment' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Test custom/running (disk deploy) on gpu-node-01' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Record supported deploy mode' })).toBeVisible()
  await expect(page.getByText('Verify OS Image', { exact: false })).toHaveCount(0)

  await page.goto('/workflows/verify-legacy?site=site-a')
  await expect(page.getByRole('heading', { name: 'Test image deployment' })).toBeVisible()
})

test('out-of-range pages are corrected while display state and Site scope are preserved', async ({ page }) => {
  const images = Array.from({ length: 26 }, (_, index) => ({
    id: 'ubuntu/image-' + index,
    name: 'Ubuntu image ' + String(index).padStart(2, '0'),
    providerName: 'Ubuntu image ' + String(index).padStart(2, '0'),
    osSystem: 'ubuntu',
    providerOsSystem: 'ubuntu',
    release: 'release-' + index,
    providerRelease: 'release-' + index,
    tags: ['golden'],
    defaultUser: 'ubuntu',
    architecture: 'amd64',
    sizeBytes: 1024 + index,
    verifiedDeployTargets: [],
    failedDeployTargets: [],
  }))
  await page.route('**/api/v1/provisioning/images?*', async (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() !== 'GET') return route.fallback()
    return json(route, url.searchParams.get('integrationId') === 'maas-a' ? images : [])
  })

  await page.goto('/provisioning/images?site=site-a&tag=golden&group=provider&sort=name&dir=desc&page=99')
  await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBe('2')
  await expect(page.getByRole('region', { name: 'Deployment images' })).toContainText('Showing 26–26 of 26 images')
  await page.getByRole('button', { name: 'Failed tests', exact: true }).click()
  await expect.poll(() => new URL(page.url()).searchParams.has('page')).toBe(false)
  await expect(page.getByText('No matching OS images')).toBeVisible()
  await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
  await expect.poll(() => new URL(page.url()).searchParams.get('site')).toBe('site-a')
  await expect.poll(() => new URL(page.url()).searchParams.get('group')).toBe('provider')
  await expect(page.getByText('No matching OS images')).toHaveCount(0)
})

test('desktop priorities and mobile cards avoid viewport overflow', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto('/provisioning/images?site=site-a')
  await expect(page.getByRole('columnheader', { name: 'Default user' })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: 'Source' })).toBeHidden()
  const desktopRow = page.getByRole('row', { name: 'Ubuntu 22.04 LTS catalog data', exact: true }).first()
  const desktopDeploy = desktopRow.getByRole('button', { name: 'Deploy Ubuntu 22.04 LTS' })
  await expect(desktopDeploy).toHaveText('Deploy')
  await expect(desktopDeploy.locator('svg')).toHaveCount(1)
  await expect(desktopRow).toContainText('ubuntu/jammy')
  await expect(desktopRow).toContainText('amd64 · 4 GiB')
  await expect(desktopRow).toContainText('ubuntu')

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('table', { name: 'OS images' })).toBeHidden()
  const cards = page.getByLabel('OS images').locator('article')
  await expect(cards).toHaveCount(3)
  const primaryFields = cards.first().locator('.sw-resource-card__fields').first()
  await expect(primaryFields.locator('dt').filter({ hasText: /^Image$/ })).toBeVisible()
  await expect(primaryFields.locator('dt').filter({ hasText: /^Default user$/ })).toBeVisible()
  const mobileDeploy = cards.filter({ hasText: 'Ubuntu 22.04 LTS' }).getByRole('button', { name: 'Deploy Ubuntu 22.04 LTS' })
  await expect(mobileDeploy.locator('svg')).toHaveCount(1)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})
