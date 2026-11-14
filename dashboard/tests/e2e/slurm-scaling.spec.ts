import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// Slurm compute scaling (platforms.md "Node groups and partitions", "Remove members", "Rescan A Slurm
// Node"; decision 083) and the Deployment Template floor it relies on (provisioning.md "Deployment
// Templates").

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

test('the Groups & partitions route shows the spec and explains why the control plane is not scaled', async ({ page }) => {
  await installApiFixtures(page, { slurmDeployed: true })
  await page.goto('/platforms/platform-slurm-ha/partitions?site=site-a')

  // A Platform from before node groups reads as one group per role and the partition main.
  await expect(page.getByText('Groups from the Platform’s roles')).toBeVisible()
  const partitions = page.getByRole('table', { name: 'Partitions in the spec' })
  await expect(partitions.getByRole('row', { name: /main.*Default.*compute/ })).toBeVisible()
  const controllers = page.getByRole('group', { name: 'Node group controller' })
  await expect(controllers.getByText(/Controllers are not added or removed here/)).toBeVisible()
  await expect(controllers.getByRole('button', { name: /Add nodes/ })).toHaveCount(0)
  const compute = page.getByRole('group', { name: 'Node group compute' })
  await expect(compute.getByRole('button', { name: 'slurm-cpt-01', exact: true })).toBeVisible()
  await expect(compute.getByRole('button', { name: 'Remove slurm-cpt-01' })).toBeVisible()
  // The live partitions slurmrestd reports stay below the spec.
  await expect(page.getByRole('heading', { name: 'Live partitions' })).toBeVisible()
})

test('removing a compute node states the drain and release choices and approves its plan', async ({ page }) => {
  const writes: Array<{ spec: Record<string, unknown>; approved: boolean }> = []
  await installApiFixtures(page, { slurmDeployed: true, onSlurmTopologyRequest: (_, spec, approved) => writes.push({ spec, approved }) })
  await page.goto('/platforms/platform-slurm-ha/partitions?site=site-a')

  await page.getByRole('group', { name: 'Node group compute' }).getByRole('button', { name: 'Remove slurm-cpt-02' }).click()
  const dialog = page.getByRole('dialog', { name: 'Remove slurm-cpt-02' })
  await expect(dialog.getByText(/waits for the running jobs, reconfigures the cluster without it/)).toBeVisible()
  await expect(dialog.getByText(/the removal stops with the node drained/)).toBeVisible()
  await dialog.getByLabel('Drain timeout (minutes)').fill('30')
  await dialog.getByRole('checkbox', { name: 'Cancel jobs still running at the timeout' }).locator('xpath=following-sibling::*[1]').click()
  await expect(dialog.getByText(/are cancelled and the removal continues/)).toBeVisible()
  await dialog.getByRole('checkbox', { name: /Release the Server afterwards/ }).locator('xpath=following-sibling::*[1]').click()
  await dialog.getByRole('button', { name: 'Remove node' }).click()

  await expect(page.getByText('Removing slurm-cpt-02')).toBeVisible()
  expect(writes).toHaveLength(1)
  expect(writes[0].approved).toBe(true)
  expect(writes[0].spec.members).toEqual([
    { serverId: 'slurm-ctl-1', roles: ['controller'] },
    { serverId: 'slurm-ctl-2', roles: ['controller'] },
    { serverId: 'slurm-cpt-1', roles: ['compute'] },
  ])
  expect(writes[0].spec.memberRemovals).toEqual([{
    serverId: 'slurm-cpt-2', drainTimeoutSeconds: 1800, force: true, releaseServer: true,
    erase: false, secureErase: false, quickErase: false, unbindStaticIps: false,
  }])
  const slurm = (writes[0].spec.settings as { slurm: { nodeGroups: Array<{ name: string; members: string[] }> } }).slurm
  expect(slurm.nodeGroups.find((group) => group.name === 'compute')?.members).toEqual(['slurm-cpt-1'])
})

test('the deploy wizard proposes a gpu group for GPU nodes whose inventory names no model', async ({ page }) => {
  await installApiFixtures(page, { freePlatformCandidates: true, unnamedComputeServerIds: ['srv-2'] })
  await page.goto('/platforms/deploy/slurm?site=site-a')
  await page.getByLabel('Platform name').fill('unnamed-gpus')
  const next = page.getByRole('button', { name: 'Next' })
  await next.click()
  await page.getByRole('checkbox', { name: 'Run slurmctld on gpu-node-01' }).locator('..').click()
  await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-02' }).locator('..').click()
  await next.click()
  await next.click()
  await expect(page.getByRole('heading', { name: 'Node groups and partitions' })).toBeVisible()
  await expect(page.getByRole('textbox', { name: 'Name of the compute group gpu' })).toHaveValue('gpu')
  await expect(page.getByRole('textbox', { name: 'Name of partition gpu' })).toHaveValue('gpu')
  await expect(page.getByLabel('Expected GPUs per node in gpu')).toHaveValue('16')
})

test('a free Server joins a compute group', async ({ page }) => {
  const writes: Array<Record<string, unknown>> = []
  await installApiFixtures(page, { slurmDeployed: true, freePlatformCandidates: true, onSlurmTopologyRequest: (_, spec) => writes.push(spec) })
  await page.goto('/platforms/platform-slurm-ha/partitions?site=site-a')

  await page.getByRole('button', { name: 'Add nodes to compute' }).click()
  const addDialog = page.getByRole('dialog', { name: 'Add nodes to compute' })
  await expect(addDialog.getByText(/installed from the group’s Deployment Template/)).toBeVisible()
  await addDialog.getByRole('checkbox', { name: /^gpu-node-04/ }).locator('xpath=following-sibling::*[1]').click()
  await addDialog.getByRole('button', { name: 'Add node' }).click()
  await expect(page.getByText('Adding gpu-node-04 to compute')).toBeVisible()
  const added = writes.at(-1) as { members: Array<{ serverId: string; roles: string[] }>; settings: { slurm: { nodeGroups: Array<{ name: string; members: string[] }> } } }
  expect(added.members.at(-1)).toEqual({ serverId: 'srv-4', roles: ['compute'] })
  expect(added.settings.slurm.nodeGroups.find((group) => group.name === 'compute')?.members).toEqual(['slurm-cpt-1', 'slurm-cpt-2', 'srv-4'])
})

test('a node added to a Platform that installs its OS may choose its interface and static address', async ({ page }) => {
  const writes: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    slurmDeployed: true, slurmProvisionOS: true, freePlatformCandidates: true,
    onSlurmTopologyRequest: (_, spec) => writes.push(spec),
  })
  await page.goto('/platforms/platform-slurm-ha/partitions?site=site-a')

  await page.getByRole('button', { name: 'Add nodes to compute' }).click()
  const dialog = page.getByRole('dialog', { name: 'Add nodes to compute' })
  await dialog.getByRole('checkbox', { name: /^gpu-node-04/ }).locator('xpath=following-sibling::*[1]').click()
  await expect(dialog.getByText(/use the network the template describes on their boot interface/)).toBeVisible()
  await dialog.getByRole('checkbox', { name: /Choose the new nodes’ network/ }).locator('xpath=following-sibling::*[1]').click()
  await expect(dialog.getByText(/The template deploys static addresses/)).toBeVisible()
  const address = dialog.getByLabel('Static IPv4 address for gpu-node-04')
  await address.fill('192.168.40.999')
  await expect(dialog.getByText('Enter a valid IPv4 address for gpu-node-04.')).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Add node' })).toBeDisabled()
  await address.fill('192.168.40.50')
  await dialog.getByRole('button', { name: 'Add node' }).click()
  await expect(page.getByText('Adding gpu-node-04 to compute')).toBeVisible()

  const written = writes.at(-1) as { machinePreparation: { mode: string; network: { mode: string; assignments: unknown[] } } }
  expect(written.machinePreparation.mode).toBe('provision_os')
  expect(written.machinePreparation.network.mode).toBe('static')
  expect(written.machinePreparation.network.assignments).toEqual([
    { serverId: 'srv-4', interfaceId: 'nic-srv-4', subnetId: 'subnet-a', ipAddress: '192.168.40.50' },
  ])
})

test('with an automatic template a new node chooses only its interface and subnet', async ({ page }) => {
  const writes: Array<Record<string, unknown>> = []
  await installApiFixtures(page, { slurmDeployed: true, freePlatformCandidates: true, onSlurmTopologyRequest: (_, spec) => writes.push(spec) })
  await page.goto('/platforms/platform-slurm-ha/partitions?site=site-a')
  await page.getByRole('button', { name: 'Add nodes to compute' }).click()
  const dialog = page.getByRole('dialog', { name: 'Add nodes to compute' })
  await dialog.getByRole('checkbox', { name: /^gpu-node-04/ }).locator('xpath=following-sibling::*[1]').click()
  await dialog.getByRole('checkbox', { name: /Choose the new nodes’ network/ }).locator('xpath=following-sibling::*[1]').click()
  await expect(dialog.getByText(/the template’s network mode assigns the address/)).toBeVisible()
  await expect(dialog.getByLabel('Interface for gpu-node-04')).toBeVisible()
  await expect(dialog.getByLabel('Static IPv4 address for gpu-node-04')).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Add node' }).click()
  await expect(page.getByText('Adding gpu-node-04 to compute')).toBeVisible()
  const written = writes.at(-1) as { machinePreparation: { network: { assignments: unknown[] } } }
  expect(written.machinePreparation.network.assignments).toEqual([{ serverId: 'srv-4', interfaceId: 'nic-srv-4', subnetId: 'subnet-a' }])
})

test('a group change that leaves nodes short of GPUs shows the API warnings until dismissed', async ({ page }) => {
  await installApiFixtures(page, { slurmDeployed: true })
  await page.goto('/platforms/platform-slurm-ha/partitions?site=site-a')

  // Expect more GPUs than the fixture nodes have, so the write answers a warning.
  await page.getByRole('button', { name: 'Change group compute' }).click()
  const groupDialog = page.getByRole('dialog', { name: 'Change compute' })
  await groupDialog.getByLabel('Expected GPUs per node').fill('16')
  await groupDialog.getByRole('button', { name: 'Save group' }).click()
  await expect(page.getByText('The change was accepted with warnings')).toBeVisible()
  await expect(page.getByText(/has 8 of the 16 GPUs the group expects/).first()).toBeVisible()
  await page.getByRole('button', { name: 'Dismiss warnings' }).click()
  await expect(page.getByText('The change was accepted with warnings')).toHaveCount(0)
})

test('a partition is added over compute groups', async ({ page }) => {
  const writes: Array<Record<string, unknown>> = []
  await installApiFixtures(page, { slurmDeployed: true, onSlurmTopologyRequest: (_, spec) => writes.push(spec) })
  await page.goto('/platforms/platform-slurm-ha/partitions?site=site-a')

  await page.getByRole('button', { name: 'Add partition' }).click()
  const dialog = page.getByRole('dialog', { name: 'Add partition' })
  await dialog.getByLabel('Partition name').fill('long')
  await dialog.getByRole('checkbox', { name: 'compute', exact: true }).locator('xpath=following-sibling::*[1]').click()
  await dialog.getByLabel('Time limit').fill('7-00:00:00')
  await dialog.getByRole('button', { name: 'Add partition' }).click()
  await expect(page.getByText('Adding partition long')).toBeVisible()
  const slurm = (writes[0].settings as { slurm: { partitions: unknown[] } }).slurm
  expect(slurm.partitions).toEqual([
    { name: 'main', default: true, groups: ['compute'], maxTime: 'INFINITE' },
    { name: 'long', default: false, groups: ['compute'], maxTime: '7-00:00:00' },
  ])
})

test('rescanning a compute node asks first and starts the rescan', async ({ page }) => {
  const rescans: string[] = []
  await installApiFixtures(page, { slurmDeployed: true, onSlurmRescanRequest: (_, serverId) => rescans.push(serverId) })
  await page.goto('/platforms/platform-slurm-ha/partitions?site=site-a')
  await page.getByRole('button', { name: 'Rescan GPUs of slurm-cpt-01' }).click()
  const dialog = page.getByRole('alertdialog', { name: 'Rescan the GPUs of slurm-cpt-01' })
  await expect(dialog.getByText(/re-detects the node’s GPUs/)).toBeVisible()
  await dialog.getByRole('button', { name: 'Rescan' }).click()
  await expect(page.getByText('Re-detecting the GPUs of slurm-cpt-01')).toBeVisible()
  expect(rescans).toEqual(['slurm-cpt-1'])
})

test('a member of the Pool sees the topology without its write controls', async ({ page }) => {
  await installApiFixtures(page, { slurmDeployed: true, identity: 'member' })
  await page.goto('/platforms/platform-slurm-ha/partitions?site=site-a')
  await expect(page.getByText(/Only an owner of the Platform’s Pool or an admin can change nodes/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add partition' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /Remove slurm-cpt-01/ })).toHaveCount(0)
})

test('Deployment Templates carry a minimum hardware floor and refuse deletion while a group uses them', async ({ page }) => {
  const bodies: Array<{ method: string; body: Record<string, unknown> }> = []
  await installApiFixtures(page, { onTemplateRequest: (method, body) => bodies.push({ method, body }) })
  await page.goto('/provisioning/templates?site=site-a')

  const table = page.getByRole('table', { name: 'Deployment templates' })
  const used = table.getByRole('row', { name: /GPU compute baseline/ })
  await expect(used).toContainText('32 cores · 8 MI300X GPUs')
  await expect(used).toContainText('lab-slurm-ha (compute)')
  await page.on('dialog', (dialog) => void dialog.accept())
  await used.getByRole('button', { name: 'Delete' }).click()
  await expect(page.getByText(/cannot be deleted while lab-slurm-ha \(compute\) installs from it/)).toBeVisible()

  await table.getByRole('row', { name: /CPU login/ }).getByRole('button', { name: 'Edit' }).click()
  await page.getByLabel('Minimum CPU cores').fill('0')
  await expect(page.getByText('CPU cores must be a whole number greater than 0.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save' })).toBeDisabled()
  await page.getByLabel('Minimum CPU cores').fill('16')
  await page.getByLabel('Minimum memory (GiB)').fill('64')
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByText('Deployment template updated')).toBeVisible()
  expect(bodies.at(-1)?.body.minimumHardware).toEqual({ cpuCores: 16, memoryMiB: 65536, storageGB: null, gpuModel: null, gpuCount: null })
  await expect(table.getByRole('row', { name: /CPU login/ })).toContainText('16 cores · 64 GiB memory')
})
