import { expect, test, type Locator, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Platform Configuration shows durable intent and controller Conditions. Workflow history belongs
// to Activity and is never inferred from the shell's current lifecycle Workflow.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

/** One titled group of the card, found by its accessible name (its heading). */
function detailsGroup(page: Page, name: string): Locator {
  return page.getByRole('region', { name: 'Platform details' }).getByRole('group', { name, exact: true })
}

/** A group's facts as label → visible value; a value's explanation lines are joined by newlines. */
function readFacts(group: Locator): Promise<Record<string, string>> {
  return group.locator('dl > div').evaluateAll((rows) => Object.fromEntries(rows.map((row) => [
    row.querySelector('dt')?.textContent ?? '',
    row.querySelector('dd') instanceof HTMLElement ? (row.querySelector('dd') as HTMLElement).innerText : '',
  ])))
}

/** The Pool fact of a Platform whose Servers are in no Pool, as every fixture Platform's are by default. */
const NO_POOL_FACT = 'N/A\nIts servers are in no pool or in different pools; only admins manage it.'

/** How the browser under test formats an API timestamp, which the card shows in the viewer's locale. */
function localTime(page: Page, iso: string): Promise<string> {
  return page.evaluate((value) => new Date(value).toLocaleString(), iso)
}

test('a Kubernetes Platform states its deployment, spec, k0s network, and Integration', async ({ page }) => {
  await installApiFixtures(page, { platformDeploymentWorkflows: true })
  await page.goto('/platforms/platform-a/configuration?site=site-a')
  await expect(page.getByRole('heading', { name: 'Platform details', exact: true })).toBeVisible()

  const general = detailsGroup(page, 'General')
  await expect.poll(() => readFacts(general)).toEqual({
    Type: 'Kubernetes',
    Lifecycle: 'Active',
    Site: 'Taipei Lab',
    Pool: NO_POOL_FACT,
    Created: await localTime(page, '2026-08-10T00:00:00Z'),
    'Spec state': 'Applied (generation 1)',
  })
  expect(await readFacts(detailsGroup(page, 'Conditions'))).toEqual({
    Realized: `Converged\nGeneration 1 · Updated ${await localTime(page, '2026-08-27T03:00:00Z')}`,
    Drifted: `In sync\nGeneration 1 · Updated ${await localTime(page, '2026-08-27T03:00:00Z')}`,
  })

  expect(await readFacts(detailsGroup(page, 'Deployment'))).toEqual({
    Topology: 'High availability',
    Members: '3 Servers',
    'OS preparation': 'Provision OS',
    Image: 'ubuntu/noble',
    'Deploy target': 'Disk deploy',
  })
  // The module's facts; the spec omits the Service range, which is the API default.
  expect(await readFacts(detailsGroup(page, 'Kubernetes'))).toEqual({
    'k0s version': 'v1.36.3+k0s.2',
    'Pod CIDR': '10.32.0.0/16',
    'Service CIDR': '10.96.0.0/12 (default)',
    'API endpoint': '192.168.40.200/24 (virtual IP)',
  })
  await expect.poll(() => readFacts(detailsGroup(page, 'Integration and GPU'))).toEqual({
    Integration: 'production-k0s (deployed)',
    'GPU stack': 'gpu-operator',
  })
  await expect(page.getByRole('heading', { name: 'Configuration', exact: true })).toHaveCount(0)
})

test('a Platform awaiting its install approval is not deployed yet and has a direct API endpoint', async ({ page }) => {
  await installApiFixtures(page, { platformLifecycleStates: { 'platform-b': 'planned' } })
  await page.goto('/platforms/platform-b/configuration?site=site-a')

  await expect.poll(() => readFacts(detailsGroup(page, 'General'))).toEqual({
    Type: 'Kubernetes',
    Lifecycle: 'Planned',
    Site: 'Taipei Lab',
    Pool: NO_POOL_FACT,
    Created: await localTime(page, '2026-08-20T00:00:00Z'),
    'Spec state': 'Awaiting approval (generation 1)\nThe install waits for its Change Plan to be approved.',
  })
  await expect(detailsGroup(page, 'General').getByRole('link')).toHaveCount(0)
  expect(await readFacts(detailsGroup(page, 'Deployment'))).toEqual({
    Topology: 'Standalone',
    Members: '1 Server',
    'OS preparation': 'Existing OS',
  })
  // Without a virtual IP the API is reached on the control plane itself; both ranges are defaults.
  expect(await readFacts(detailsGroup(page, 'Kubernetes'))).toEqual({
    'k0s version': 'v1.36.3+k0s.2',
    'Pod CIDR': '10.244.0.0/16 (default)',
    'Service CIDR': '10.96.0.0/12 (default)',
    'API endpoint': 'Direct control-plane endpoint',
  })
  expect(await readFacts(detailsGroup(page, 'Integration and GPU'))).toEqual({
    Integration: 'Not connected',
    'GPU stack': 'provisioning',
  })

  // No deployment Workflow has run, so the Overview reads the topology from the spec's roles too.
  await page.goto('/platforms/platform-b?site=site-a')
  const posture = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Runtime posture' }) })
  await expect.poll(() => readFacts(posture)).toMatchObject({ Topology: 'Standalone' })
})

test('a Slurm Platform states its cluster, the Docker CE its container Grafana brought, and MPI', async ({ page }) => {
  await installApiFixtures(page, { slurmDeployed: true, platformDeploymentWorkflows: true })
  await page.goto('/platforms/platform-slurm-ha/configuration?site=site-a')

  const general = detailsGroup(page, 'General')
  await expect.poll(() => readFacts(general)).toEqual({
    Type: 'Slurm',
    Lifecycle: 'Active',
    Site: 'Taipei Lab',
    Pool: NO_POOL_FACT,
    Created: await localTime(page, '2026-08-18T00:00:00Z'),
    'Spec state': 'Applied (generation 1)',
  })
  expect(await readFacts(detailsGroup(page, 'Deployment'))).toEqual({
    Topology: 'High availability',
    Members: '4 Servers',
    'OS preparation': 'Provision OS',
    Image: 'ubuntu-24.04-rocm',
    'Deploy target': 'RAM deploy',
  })
  // Omnistat's container install method put the Platform Grafana in a container on the primary
  // controller; with no engine chosen for the compute nodes, that brought Docker CE there.
  expect(await readFacts(detailsGroup(page, 'Slurm'))).toEqual({
    'Cluster name': 'lab-ha',
    'slurmrestd API version': 'v0.0.42',
    'Container runtime': 'Docker CE on the primary controller for the Platform Grafana, unless it already had a container engine',
    MPI: 'OpenMPI with srun --mpi=pmix_v5',
  })
  await expect.poll(() => readFacts(detailsGroup(page, 'Integration and GPU'))).toEqual({
    Integration: 'lab-slurm-ha (deployed)',
    'GPU stack': 'provisioning',
  })
  await expect(detailsGroup(page, 'Kubernetes')).toHaveCount(0)
})

test('a Slurm Platform lists, per compute node, whether Omnistat collects the hardware counters or why not', async ({ page }) => {
  await installApiFixtures(page, { slurmDeployed: true, platformDeploymentWorkflows: true })
  await page.goto('/platforms/platform-slurm-ha/monitoring?site=site-a')

  const counters = page.getByLabel('GPU hardware counters', { exact: true })
  await expect(counters).toContainText('Collecting on 1 of 2 compute nodes.')
  // Each node is named by its hostname and links to its Server; the state is text, and the
  // unavailable node says why instead of looking failed.
  const rows = counters.getByRole('listitem')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0)).toHaveText(/slurm-cpt-01\s*Collecting/)
  await expect(rows.nth(1)).toContainText('slurm-cpt-02')
  await expect(rows.nth(1)).toContainText('Unavailable')
  await expect(rows.nth(1)).toContainText('Swallow ships no counter extension for this node\u2019s ROCm version.')
  await expect(rows.nth(0).getByRole('link', { name: 'slurm-cpt-01' })).toHaveAttribute('href', /\/servers\/slurm-cpt-1/)
})

// Omnistat's hardware counters switch on a deployed Slurm Platform (platforms.md "Omnistat") sits under
// Omnistat on Monitoring, with each compute node's outcome: the switch shows the spec, the confirmation
// names the consequence, and the write is the spec at the generation it was read at with only
// omnistat.hardwareCounters changed. It needs no Change Plan, so the counters show the change running
// and the switch waits for it.
test('a deployed Slurm Platform turns its hardware counters off where Monitoring shows them', async ({ page }) => {
  const writes: Array<{ platformId: string; hardwareCounters: unknown; generation: number }> = []
  await installApiFixtures(page, {
    slurmDeployed: true, platformDeploymentWorkflows: true,
    onHardwareCountersRequest: (platformId, hardwareCounters, generation) => { writes.push({ platformId, hardwareCounters, generation }) },
  })
  await page.goto('/platforms/platform-slurm-ha/monitoring?site=site-a')

  const counters = page.getByRole('group', { name: 'GPU hardware counters' })
  const toggle = counters.getByRole('switch', { name: 'Collect GPU hardware counters' })
  await expect(toggle).toBeChecked()
  await expect(counters).toContainText('On')
  await expect(counters).toContainText('Collecting on 1 of 2 compute nodes.')
  // Turning the counters on enables the collection; what it samples is named on its own.
  await expect(counters).toContainText('Counters collected: VALUBusy')

  await toggle.locator('..').click()
  const dialog = page.getByRole('alertdialog', { name: 'Turn off GPU hardware counters' })
  await expect(dialog).toContainText('back to Omnistat without hardware counters')
  await expect(dialog).toContainText('Slurm and running jobs are not touched')
  await expect(dialog).not.toContainText('rocprofv3')
  await dialog.getByRole('button', { name: 'Turn off counters' }).click()

  await expect(dialog).toHaveCount(0)
  expect(writes).toEqual([{ platformId: 'platform-slurm-ha', hardwareCounters: false, generation: 1 }])
  await expect(counters).toContainText('Turning off')
  await expect(counters.getByRole('listitem')).toHaveCount(0)
  await expect(toggle).not.toBeChecked()
  await expect(toggle).toBeDisabled()
  await expect(counters).toContainText('Wait for the running Omnistat change to finish.')
  await expect(page.getByText('Turning off the hardware counters')).toBeVisible()
})

// With the counters off the switch is still there, under Omnistat. Turning them on states the cost
// (the extension, CAP_PERFMON, GPU memory) and that a node that cannot collect them keeps its other
// metrics. A spec changed in between is the API's conflict: the dialog keeps its error, and
// confirming again writes the spec read anew.
test('turning the hardware counters on retries a spec that changed in between', async ({ page }) => {
  const writes: Array<{ hardwareCounters: unknown; generation: number }> = []
  await installApiFixtures(page, {
    slurmDeployed: true, platformDeploymentWorkflows: true, slurmHardwareCountersOff: true, hardwareCountersConflict: true,
    onHardwareCountersRequest: (_platformId, hardwareCounters, generation) => { writes.push({ hardwareCounters, generation }) },
  })
  await page.goto('/platforms/platform-slurm-ha/monitoring?site=site-a')

  const counters = page.getByRole('group', { name: 'GPU hardware counters' })
  const toggle = counters.getByRole('switch', { name: 'Collect GPU hardware counters' })
  await expect(toggle).not.toBeChecked()
  await expect(counters).toContainText('Every compute node runs Omnistat without the counters.')

  await toggle.locator('..').click()
  const dialog = page.getByRole('alertdialog', { name: 'Turn on GPU hardware counters' })
  await expect(dialog).toContainText('CAP_PERFMON')
  await expect(dialog).toContainText('keeps its other metrics and reports why')
  await expect(dialog).toContainText('may not get the counters, because a GPU allows one profiler session at a time')
  await expect(dialog).toContainText('It usually takes a few minutes.')
  await dialog.getByRole('button', { name: 'Turn on counters' }).click()
  await expect(dialog).toContainText('The platform spec changed since it was read')
  await expect(dialog).toContainText('req-platform-spec-conflict')

  await dialog.getByRole('button', { name: 'Turn on counters' }).click()
  await expect(dialog).toHaveCount(0)
  expect(writes).toEqual([{ hardwareCounters: true, generation: 1 }, { hardwareCounters: true, generation: 2 }])
  await expect(counters).toContainText('Turning on')
  await expect(toggle).toBeChecked()
})

test('a Slurm Platform that asked for no hardware counters shows no breakdown for them', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/platforms/platform-slurm/monitoring?site=site-a')

  await expect(page.getByLabel('Omnistat', { exact: true })).toContainText('Ready')
  // Its spec has no Omnistat to switch, so there is neither a breakdown nor a switch.
  await expect(page.getByLabel('GPU hardware counters', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('switch', { name: 'Collect GPU hardware counters' })).toHaveCount(0)
})

test('a registered Slurm Platform states its defaults, its engine on the controller, and an unresolved Integration', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/platforms/platform-slurm/configuration?site=site-a')

  await expect.poll(() => readFacts(detailsGroup(page, 'General'))).toMatchObject({
    Lifecycle: 'Registered',
  })
  // The Podman chosen for the compute and login nodes is also the Grafana container's engine.
  expect(await readFacts(detailsGroup(page, 'Slurm'))).toEqual({
    'Cluster name': 'Derived from the platform name',
    'slurmrestd API version': 'Auto-detected',
    'Container runtime': 'Podman on compute and login nodes; Podman on the primary controller for the Platform Grafana',
    MPI: 'Not set up',
  })
  // Its legacy reader Integration cannot be read, so the card falls back to the id.
  await expect.poll(() => readFacts(detailsGroup(page, 'Integration and GPU'))).toEqual({
    Integration: 'slurm-a',
    'GPU stack': 'provisioning',
  })
})

test('the Platform details facts stack into one column on a phone without overflowing', async ({ page }) => {
  await installApiFixtures(page, { platformDeploymentWorkflows: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/platforms/platform-a/configuration?site=site-a')
  const deployment = detailsGroup(page, 'Deployment')
  await expect(deployment).toBeVisible()
  const columns = await deployment.locator('dl').evaluate((list) => getComputedStyle(list).gridTemplateColumns.split(' ').length)
  expect(columns).toBe(1)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})
