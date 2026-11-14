import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// User journeys for swallow-owned Zones, Pools, and Server placement (decisions 071 and 082): Zone
// edits write the spec, the provisioners follow, and the status says whether they have; a Pool is
// installation-wide and its placement needs nothing from the provisioner. Every request is served by
// deterministic fixtures; no backend or provisioner is contacted.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

test('Zones show whether the provisioners hold them, and settle in place after a change', async ({ page }) => {
  await installApiFixtures(page)
  const condition = (type: string, status: string, reason: string, generation: number) =>
    ({ type, status, reason, message: '', lastTransitionTime: '2026-08-27T03:00:00Z', observedGeneration: generation })
  const zone = (id: string, name: string, generation: number, observedGeneration: number) => ({
    id, siteId: 'site-a', generation, spec: { name, description: '' },
    status: {
      observedGeneration,
      conditions: [condition('realized', observedGeneration === generation ? 'true' : 'false', observedGeneration === generation ? 'converged' : 'converging', generation)],
      provisioners: [], createdAt: '2026-08-01T00:00:00Z', updatedAt: '2026-08-27T03:00:00Z',
    },
  })
  let applied = false
  await page.route('**/api/v1/infrastructure/zones**', async (route) => {
    // The renamed zone is still being applied until the test lets the provisioner finish.
    await route.fulfill({ json: [zone('z-a', 'rack-a', 1, 1), zone('z-b', 'rack-b', 2, applied ? 2 : 1)] })
  })

  await page.goto('/infrastructure/zones?site=site-a')
  const table = page.getByRole('table')
  await expect(table.getByRole('row', { name: /rack-a/ })).toContainText('In sync')
  await expect(table.getByRole('row', { name: /rack-b/ })).toContainText('Applying')

  applied = true
  await expect(table.getByRole('row', { name: /rack-b/ })).toContainText('In sync', { timeout: 10_000 })
})

test('Placement opens on the spec and clears to the provisioner default', async ({ page }) => {
  await installApiFixtures(page, { serverPlacements: { 'srv-1': { zoneId: 'zone-a' } } })
  const bodies: unknown[] = []
  await page.route('**/api/v1/servers/srv-1/placement', async (route) => {
    bodies.push(route.request().postDataJSON())
    await route.fulfill({ json: { serverId: 'srv-1', generation: 2, zoneId: null, poolId: null } })
  })

  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.getByRole('menuitem', { name: 'Set zone / pool' }).click()
  const dialog = page.getByRole('dialog', { name: 'Set zone and pool' })
  await expect(dialog.getByRole('combobox', { name: 'Zone', exact: true })).toContainText('rack-a')
  await expect(dialog.getByRole('combobox', { name: 'Pool', exact: true })).toContainText('No pool (admins only)')
  await expect(dialog.getByRole('button', { name: 'Save placement' })).toBeDisabled()

  await dialog.getByRole('combobox', { name: 'Zone', exact: true }).click()
  await page.getByRole('option', { name: 'Provisioner default', exact: true }).click()
  await dialog.getByRole('button', { name: 'Save placement' }).click()

  await expect.poll(() => bodies).toEqual([{ zoneId: null }])
  await expect(dialog).toHaveCount(0)
})

test('The Server list shows and filters the swallow Pool, never the provisioner\'s resource pool', async ({ page }) => {
  await installApiFixtures(page, { serverPlacements: { 'srv-1': { poolId: 'pool-research' } } })
  await page.goto('/servers?site=site-a')
  const table = page.getByRole('table', { name: 'Servers' })
  // The rendered text: a claimed Server's cell also holds its disabled shortcut's hidden description.
  await expect(table.getByRole('row').filter({ hasText: 'gpu-node-01' }).locator('.sw-server-col--pool')).toHaveText('research', { useInnerText: true })
  await expect(table.getByRole('row').filter({ hasText: 'gpu-node-02' }).locator('.sw-server-col--pool')).toHaveText('N/A', { useInnerText: true })

  await page.goto('/servers?site=site-a&pool=pool-research')
  await expect(table.getByText('gpu-node-01', { exact: true })).toBeVisible()
  await expect(table.getByText('gpu-node-02', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: /^Filters/ }).click()
  await expect(page.getByRole('checkbox', { name: 'research (1)' })).toBeChecked()
})

test('An admin sets the Pool of the selected Servers, and a refused Server is listed with its reason', async ({ page }) => {
  const requests: Array<{ serverId: string; body: unknown }> = []
  await installApiFixtures(page, {
    withoutPlatformClaims: true,
    placementConflictServerIds: ['srv-3'],
    onPlacementRequest: (serverId, body) => requests.push({ serverId, body }),
  })
  await page.goto('/servers?site=site-a')
  const table = page.getByRole('table', { name: 'Servers' })
  for (const name of ['gpu-node-01', 'gpu-node-02', 'gpu-node-03']) {
    await table.getByLabel(`Select ${name}`, { exact: true }).click()
  }
  await page.getByRole('button', { name: 'Set pool', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Set pool' })
  await expect(dialog.getByText('No selected server is in a pool.')).toBeVisible()
  await dialog.getByRole('combobox', { name: 'Pool', exact: true }).click()
  await page.getByRole('option', { name: 'research', exact: true }).click()
  await expect(dialog.getByText('3 of 3 selected servers will move to research.')).toBeVisible()
  await dialog.getByRole('button', { name: 'Set pool' }).click()

  await expect(dialog.getByText('1 server was not moved')).toBeVisible()
  await expect(dialog.getByText("The server's spec changed during the request; read it and try again.", { exact: false })).toBeVisible()
  // Only the Pool is sent, so each Server keeps its Zone.
  expect(requests.map((request) => request.body)).toEqual([{ poolId: 'pool-research' }, { poolId: 'pool-research' }, { poolId: 'pool-research' }])
  await dialog.getByText('Close', { exact: true }).click()
  await expect(table.getByRole('row').filter({ hasText: 'gpu-node-01' }).locator('.sw-server-col--pool')).toHaveText('research')
  await expect(table.getByRole('row').filter({ hasText: 'gpu-node-03' }).locator('.sw-server-col--pool')).toHaveText('N/A')
})

test('An admin sets one Server\'s Pool from its Pool cell in the list', async ({ page }) => {
  const requests: Array<{ serverId: string; body: unknown }> = []
  await installApiFixtures(page, {
    withoutPlatformClaims: true,
    serverPlacements: { 'srv-1': { poolId: 'pool-research' } },
    onPlacementRequest: (serverId, body) => requests.push({ serverId, body }),
  })
  await page.goto('/servers?site=site-a')
  const table = page.getByRole('table', { name: 'Servers' })
  const pool = table.getByRole('row').filter({ hasText: 'gpu-node-02' }).locator('.sw-server-col--pool')
  await pool.getByRole('button', { name: 'Set pool for gpu-node-02' }).click()

  const dialog = page.getByRole('dialog', { name: 'Set pool' })
  await expect(dialog.getByText('gpu-node-02 is in no pool; only admins may use it.')).toBeVisible()
  await dialog.getByRole('combobox', { name: 'Pool', exact: true }).click()
  await page.getByRole('option', { name: 'research', exact: true }).click()
  await expect(dialog.getByText('gpu-node-02 will move to research.')).toBeVisible()
  await dialog.getByRole('button', { name: 'Set pool' }).click()

  await expect(dialog).toHaveCount(0)
  expect(requests).toEqual([{ serverId: 'srv-2', body: { poolId: 'pool-research' } }])
  await expect(pool).toHaveText('research')
})

test('A Server\'s page shows its Pool and lets an admin change it in place', async ({ page }) => {
  await installApiFixtures(page, { withoutPlatformClaims: true, serverPlacements: { 'srv-1': { poolId: 'pool-research' } } })
  await page.goto('/servers/srv-1/summary?site=site-a')
  const card = page.locator('.sw-server-summary-card').filter({ hasText: 'Placement and labels' })
  await expect(card.getByRole('link', { name: 'research' })).toHaveAttribute('href', /\/pools\/pool-research/)
  await expect(card.getByText('Provisioner resource pool')).toBeVisible()
  await card.getByRole('button', { name: 'Set zone and pool' }).click()
  const dialog = page.getByRole('dialog', { name: 'Set zone and pool' })
  await expect(dialog.getByRole('combobox', { name: 'Pool', exact: true })).toBeEnabled()
})

test('A Platform\'s Server keeps its Pool wherever an admin would set it on its own', async ({ page }) => {
  const requests: string[] = []
  await installApiFixtures(page, {
    serverPlacements: { 'srv-1': { poolId: 'pool-research' } },
    onPlacementRequest: (serverId) => requests.push(serverId),
  })
  await page.goto('/servers?site=site-a')
  const table = page.getByRole('table', { name: 'Servers' })

  // The row's shortcut is disabled and says why.
  const shortcut = table.getByRole('row').filter({ hasText: 'gpu-node-02' }).getByRole('button', { name: 'Set pool for gpu-node-02' })
  await expect(shortcut).toBeDisabled()
  await expect(shortcut).toHaveAccessibleDescription('gpu-node-02 belongs to Platform production-k0s; its pool changes only with the Platform.')

  // The selection's Set pool lists the Platform's Servers and moves none of them.
  for (const name of ['gpu-node-01', 'gpu-node-02']) {
    await table.getByLabel(`Select ${name}`, { exact: true }).click()
  }
  await page.getByRole('button', { name: 'Set pool', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Set pool' })
  await expect(dialog.getByText('Every selected server belongs to a Platform; move each Platform instead.')).toBeVisible()
  await expect(dialog.getByText("2 servers keep their Platform's pool")).toBeVisible()
  await expect(dialog.getByRole('listitem').filter({ hasText: 'gpu-node-01' })).toContainText('Platform production-k0s')
  await expect(dialog.getByRole('button', { name: 'Set pool' })).toBeDisabled()
  await dialog.getByRole('button', { name: 'Cancel' }).click()

  // The Server's own placement dialog changes only its Zone.
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.locator('.sw-server-summary-card').filter({ hasText: 'Placement and labels' }).getByRole('button', { name: 'Set zone and pool' }).click()
  const placement = page.getByRole('dialog', { name: 'Set zone and pool' })
  await expect(placement.getByRole('combobox', { name: 'Pool', exact: true })).toBeDisabled()
  await expect(placement.getByText('gpu-node-01 belongs to Platform production-k0s; its pool changes only with the Platform.')).toBeVisible()
  await expect(placement.getByRole('combobox', { name: 'Zone', exact: true })).toBeEnabled()
  expect(requests).toEqual([])
})

test('An admin moves a Platform with all its Servers to another Pool from its details', async ({ page }) => {
  const moves: Array<{ platformId: string; body: unknown }> = []
  await installApiFixtures(page, {
    platformPools: { 'platform-a': 'pool-research' },
    onPlatformPoolRequest: (platformId, body) => moves.push({ platformId, body }),
  })
  await page.goto('/platforms/platform-a/configuration?site=site-a')
  const details = page.getByRole('region', { name: 'Platform details' })
  await expect(details.getByRole('link', { name: 'research' })).toHaveAttribute('href', /\/pools\/pool-research/)
  await details.getByRole('button', { name: 'Move production-k0s to another pool' }).click()

  const dialog = page.getByRole('dialog', { name: 'Move to another pool' })
  await expect(dialog.getByText('production-k0s is already in research.')).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Move platform' })).toBeDisabled()
  await dialog.getByRole('combobox', { name: 'Pool', exact: true }).click()
  await page.getByRole('option', { name: 'No pool (admins only)', exact: true }).click()
  await expect(dialog.getByText('production-k0s and all its servers will leave their pool; only admins may use them then.')).toBeVisible()
  await dialog.getByRole('button', { name: 'Move platform' }).click()

  await expect(dialog).toHaveCount(0)
  expect(moves).toEqual([{ platformId: 'platform-a', body: { poolId: null } }])
  await expect(page.getByText('production-k0s left its pool')).toBeVisible()
  await expect(details.getByText('N/A', { exact: true })).toBeVisible()
  await expect(details.getByText('Its servers are in no pool or in different pools; only admins manage it.')).toBeVisible()
})

test('Only an admin is offered to move a Platform to another Pool', async ({ page }) => {
  await installApiFixtures(page, { identity: 'owner', platformPools: { 'platform-a': 'pool-research' }, serverPlacements: { 'srv-1': { poolId: 'pool-research' } } })
  await page.goto('/platforms/platform-a/configuration?site=site-a')
  const details = page.getByRole('region', { name: 'Platform details' })
  await expect(details.getByRole('link', { name: 'research' })).toBeVisible()
  await expect(details.getByRole('button', { name: /to another pool$/ })).toHaveCount(0)
})

test('A user who is not an admin sees the Pool but may not set it', async ({ page }) => {
  await installApiFixtures(page, { identity: 'owner', serverPlacements: { 'srv-1': { poolId: 'pool-research' } } })
  await page.goto('/servers?site=site-a')
  const table = page.getByRole('table', { name: 'Servers' })
  await table.getByLabel('Select gpu-node-01', { exact: true }).click()
  await expect(page.getByRole('button', { name: 'Edit tags', exact: true })).toBeVisible()
  // Neither the selection's Set pool nor any row's "Set pool for …" shortcut.
  await expect(page.getByRole('button', { name: 'Set pool' })).toHaveCount(0)
  await page.goto('/servers/srv-1/summary?site=site-a')
  const card = page.locator('.sw-server-summary-card').filter({ hasText: 'Placement and labels' })
  await expect(card.getByRole('link', { name: 'research' })).toBeVisible()
  await expect(card.getByRole('button', { name: 'Set zone and pool' })).toHaveCount(0)
})

test('Pool choices are every Pool of the installation, whatever the Server\'s Site', async ({ page }) => {
  await installApiFixtures(page, { withoutPlatformClaims: true })
  const pools = [
    { id: 'pool-research', name: 'research', description: '', createdAt: '2026-08-01T00:00:00Z', updatedAt: '2026-08-01T00:00:00Z' },
    { id: 'pool-prod', name: 'prod', description: 'Servers of every site', createdAt: '2026-08-01T00:00:00Z', updatedAt: '2026-08-01T00:00:00Z' },
  ]
  const poolQueries: string[] = []
  await page.route('**/api/v1/infrastructure/pools**', async (route) => {
    poolQueries.push(new URL(route.request().url()).search)
    await route.fulfill({ json: pools })
  })
  const bodies: unknown[] = []
  await page.route('**/api/v1/servers/srv-1/placement', async (route) => {
    bodies.push(route.request().postDataJSON())
    await route.fulfill({ json: { serverId: 'srv-1', generation: 2, zoneId: null, poolId: 'pool-prod' } })
  })

  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.getByRole('menuitem', { name: 'Set zone / pool' }).click()
  const dialog = page.getByRole('dialog', { name: 'Set zone and pool' })
  await dialog.getByRole('combobox', { name: 'Pool', exact: true }).click()
  await page.getByRole('option', { name: 'prod', exact: true }).click()
  await dialog.getByRole('button', { name: 'Save placement' }).click()

  await expect.poll(() => bodies).toEqual([{ poolId: 'pool-prod' }])
  expect(poolQueries.every((query) => query === '')).toBe(true)
})
