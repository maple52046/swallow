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

test('Pool choices are every Pool of the installation, whatever the Server\'s Site', async ({ page }) => {
  await installApiFixtures(page)
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
