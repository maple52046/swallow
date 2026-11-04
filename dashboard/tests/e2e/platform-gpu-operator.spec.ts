import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures, approvePlatformInstall } from './fixtures'

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

async function chooseOption(page: Page, fieldLabel: string, optionLabel: string) {
  await page.getByRole('combobox', { name: fieldLabel, exact: true }).click()
  await page.getByRole('option', { name: optionLabel, exact: true }).click()
}

/** Fills Basics and assigns gpu-node-01 as control plane with the given workers. */
async function startKubernetesDeploy(page: Page, workers: string[]) {
  await page.goto('/platforms/deploy/kubernetes?site=site-a')
  await page.getByLabel('Platform name').fill('gpu-k0s')
  await page.getByRole('button', { name: 'Next' }).click()
  await page.getByRole('radio', { name: /Multi-node \(non-HA\)/ }).locator('xpath=following-sibling::*[1]').click()
  await chooseOption(page, 'Role for gpu-node-01', 'Control-plane')
  for (const worker of workers) await chooseOption(page, `Role for ${worker}`, 'Worker')
  await page.getByRole('button', { name: 'Next' }).click()
  await expect(page.getByRole('heading', { name: 'Kubernetes features' })).toBeVisible()
}

/** Returns from the Features step to the Topology step, gives the listed Servers a role, and comes back. */
async function changeRoles(page: Page, roles: Record<string, 'Worker' | 'Not included'>) {
  await page.getByRole('button', { name: 'Back' }).click()
  await expect(page.getByRole('heading', { name: 'Topology and machines' })).toBeVisible()
  for (const [server, role] of Object.entries(roles)) await chooseOption(page, `Role for ${server}`, role)
  await page.getByRole('button', { name: 'Next' }).click()
  await expect(page.getByRole('heading', { name: 'Kubernetes features' })).toBeVisible()
}

/** Goes from the Features step to Review and reads its Features card as label → value pairs. */
async function reviewFeatures(page: Page): Promise<Record<string, string>> {
  await page.getByRole('button', { name: 'Next' }).click()
  await page.getByRole('button', { name: 'Next' }).click()
  await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
  const card = page.locator('.sw-platform-review-card').filter({ has: page.getByRole('heading', { name: 'Features', exact: true }) })
  const labels = await card.getByRole('term').allInnerTexts()
  const values = await card.getByRole('definition').allInnerTexts()
  return Object.fromEntries(labels.map((label, index) => [label, values[index]]))
}

const CLOCK_CHECK_REVIEW = { 'Clock check': 'Required: an unsynchronized node fails the deployment' }

// The GPU Operator is a deploy feature (decision 062) the wizard proposes, with its Device Metrics
// Exporter as the Platform's GPU monitoring (monitoring.md §4.2), whenever a selected workload
// Server has an AMD compute GPU. The other defaults follow the contract, the request derives
// gpuStackOwner server-side, and no node is listed or chosen because the operator's Node Feature
// Discovery decides.
test('the deploy wizard proposes the AMD GPU Operator with GPU monitoring for AMD workload Servers', async ({ page }) => {
  let submitted: Record<string, unknown> | undefined
  await installApiFixtures(page, { freePlatformCandidates: true, onPlatformDeploymentRequest: (body) => { submitted = body } })
  await startKubernetesDeploy(page, ['gpu-node-02', 'gpu-node-03'])

  await expect(page.getByRole('switch', { name: 'Enable AMD GPU Operator' })).toBeChecked()
  await expect(page.getByRole('combobox', { name: 'GPU Operator version' })).toContainText('v1.5.1 (verified default)')
  await expect(page.getByRole('combobox', { name: 'GPU allocation mode' })).toContainText('Device Plugin')
  await expect(page.getByRole('combobox', { name: 'DRA driver version' })).toHaveCount(0)
  await expect(page.getByRole('switch', { name: 'Node Labeller' })).toBeChecked()
  await expect(page.getByRole('switch', { name: 'Device Metrics Exporter' })).toBeChecked()

  await expect(page.getByRole('group', { name: 'Managed GPU nodes' })).toHaveCount(0)
  await expect(page.getByRole('checkbox', { name: 'gpu-node-02' })).toHaveCount(0)
  await expect(page.getByText(/manages every node its Node Feature Discovery finds/)).toBeVisible()

  await chooseOption(page, 'GPU allocation mode', 'DRA')
  await expect(page.getByRole('combobox', { name: 'DRA driver version' })).toContainText('v1.0.1')
  await expect(page.getByRole('combobox', { name: 'GPU resource naming' })).toHaveCount(0)

  expect(await reviewFeatures(page)).toEqual({
    'AMD GPU Operator': 'v1.5.1 · DRA',
    'DRA driver': 'v1.0.1',
    'Node Labeller': 'On',
    'GPU monitoring': 'Device Metrics Exporter, collected by the Site Prometheus',
    ...CLOCK_CHECK_REVIEW,
  })
  await page.getByRole('button', { name: 'Deploy Kubernetes Platform' }).click()

  await approvePlatformInstall(page)
  expect(submitted?.gpuStackOwner).toBeUndefined()
  expect(submitted?.gpuOperator).toEqual({
    enabled: true, allocationMode: 'dra', resourceNaming: 'single',
    nodeLabeller: true, metricsExporter: true,
  })
  const section = page.getByRole('region', { name: 'AMD GPU Operator' })
  await expect(section.getByText('Installing', { exact: true })).toBeVisible()
})

// A pointer user must be able to turn the proposed exporter off; the labeller's switch shares the
// same markup. The review then says what the deploy leaves out.
test('a click turns off the GPU Operator metrics exporter and its node labeller', async ({ page }) => {
  let submitted: Record<string, unknown> | undefined
  await installApiFixtures(page, { freePlatformCandidates: true, onPlatformDeploymentRequest: (body) => { submitted = body } })
  await startKubernetesDeploy(page, ['gpu-node-02', 'gpu-node-03'])

  const exporter = page.getByRole('switch', { name: 'Device Metrics Exporter' })
  const labeller = page.getByRole('switch', { name: 'Node Labeller' })
  await exporter.locator('xpath=following-sibling::*[1]').click()
  await labeller.locator('xpath=following-sibling::*[1]').click()
  await expect(exporter).not.toBeChecked()
  await expect(labeller).not.toBeChecked()

  expect(await reviewFeatures(page)).toEqual({
    'AMD GPU Operator': 'v1.5.1 · Device Plugin',
    'Resource naming': 'Single (amd.com/gpu)',
    'Node Labeller': 'Off',
    'GPU monitoring': 'Off',
    ...CLOCK_CHECK_REVIEW,
  })
  await page.getByRole('button', { name: 'Deploy Kubernetes Platform' }).click()
  await approvePlatformInstall(page)
  expect(submitted?.gpuOperator).toEqual({
    enabled: true, allocationMode: 'device-plugin', resourceNaming: 'single',
    nodeLabeller: false, metricsExporter: false,
  })
})

test('the GPU Operator is not proposed and cannot be chosen without an AMD workload node', async ({ page }) => {
  let submitted: Record<string, unknown> | undefined
  await installApiFixtures(page, { freePlatformCandidates: true, onPlatformDeploymentRequest: (body) => { submitted = body } })
  await startKubernetesDeploy(page, ['gpu-node-04'])

  const install = page.getByRole('switch', { name: 'Enable AMD GPU Operator' })
  await expect(install).not.toBeChecked()
  await expect(install).toBeDisabled()
  await expect(page.getByText('None of the selected workload Servers has an AMD compute GPU.')).toBeVisible()
  await expect(page.getByRole('combobox', { name: 'GPU allocation mode' })).toHaveCount(0)
  expect(await reviewFeatures(page)).toEqual({ 'AMD GPU Operator': 'Not installed', ...CLOCK_CHECK_REVIEW })
  await page.getByRole('button', { name: 'Deploy Kubernetes Platform' }).click()
  await approvePlatformInstall(page)
  expect(submitted?.gpuOperator).toBeUndefined()
})

// Until the card is touched the proposal follows the workers both ways, so losing the AMD workers
// never leaves an operator the backend refuses.
test('the untouched GPU Operator proposal follows the workload Servers', async ({ page }) => {
  let submitted: Record<string, unknown> | undefined
  await installApiFixtures(page, { freePlatformCandidates: true, onPlatformDeploymentRequest: (body) => { submitted = body } })
  await startKubernetesDeploy(page, ['gpu-node-04'])
  const install = page.getByRole('switch', { name: 'Enable AMD GPU Operator' })
  await expect(install).not.toBeChecked()

  await changeRoles(page, { 'gpu-node-02': 'Worker' })
  await expect(install).toBeChecked()
  await expect(page.getByRole('switch', { name: 'Device Metrics Exporter' })).toBeChecked()

  await changeRoles(page, { 'gpu-node-02': 'Not included' })
  await expect(install).not.toBeChecked()
  await expect(page.getByRole('button', { name: 'Next' })).toBeEnabled()
  expect(await reviewFeatures(page)).toEqual({ 'AMD GPU Operator': 'Not installed', ...CLOCK_CHECK_REVIEW })
  await page.getByRole('button', { name: 'Deploy Kubernetes Platform' }).click()
  await approvePlatformInstall(page)
  expect(submitted?.gpuOperator).toBeUndefined()
})

test('an explicit GPU Operator off survives changing the workload Servers', async ({ page }) => {
  let submitted: Record<string, unknown> | undefined
  await installApiFixtures(page, { freePlatformCandidates: true, onPlatformDeploymentRequest: (body) => { submitted = body } })
  await startKubernetesDeploy(page, ['gpu-node-02'])
  const install = page.getByRole('switch', { name: 'Enable AMD GPU Operator' })
  await expect(install).toBeChecked()
  await install.locator('xpath=following-sibling::*[1]').click()
  await expect(install).not.toBeChecked()

  await changeRoles(page, { 'gpu-node-02': 'Not included', 'gpu-node-03': 'Worker' })
  await expect(install).not.toBeChecked()
  await changeRoles(page, { 'gpu-node-02': 'Worker' })
  await expect(install).not.toBeChecked()

  expect(await reviewFeatures(page)).toEqual({ 'AMD GPU Operator': 'Not installed', ...CLOCK_CHECK_REVIEW })
  await page.getByRole('button', { name: 'Deploy Kubernetes Platform' }).click()
  await approvePlatformInstall(page)
  expect(submitted?.gpuOperator).toBeUndefined()
})

// Changing an option is a choice too: the operator stays on when its AMD workers go, and the step
// says why it cannot continue instead of sending what the backend refuses.
test('a touched GPU Operator stays on without AMD workers and blocks the Features step', async ({ page }) => {
  await installApiFixtures(page, { freePlatformCandidates: true })
  await startKubernetesDeploy(page, ['gpu-node-02'])
  const exporter = page.getByRole('switch', { name: 'Device Metrics Exporter' })
  await exporter.locator('xpath=following-sibling::*[1]').click()
  await expect(exporter).not.toBeChecked()

  await changeRoles(page, { 'gpu-node-02': 'Not included', 'gpu-node-04': 'Worker' })
  const install = page.getByRole('switch', { name: 'Enable AMD GPU Operator' })
  await expect(install).toBeChecked()
  await expect(exporter).not.toBeChecked()
  await expect(page.getByRole('button', { name: 'Next' })).toBeDisabled()
  await expect(page.getByRole('status').filter({ hasText: 'No workload Server in this Platform has an AMD compute GPU.' })).toBeVisible()

  await install.locator('xpath=following-sibling::*[1]').click()
  await expect(install).not.toBeChecked()
  await expect(page.getByRole('button', { name: 'Next' })).toBeEnabled()
})

test('a Platform shows its GPU Operator and proposes changing the allocation mode', async ({ page }) => {
  const requests: Array<Record<string, unknown>> = []
  await installApiFixtures(page, { onGPUOperatorRequest: (_, body) => { requests.push(body) } })
  await page.goto('/platforms/platform-a?site=site-a')

  const section = page.getByRole('region', { name: 'AMD GPU Operator' })
  await expect(section.getByText('Ready', { exact: true })).toBeVisible()
  await expect(section).toContainText('v1.5.1')
  await expect(section).toContainText('Device Plugin')
  await expect(section).not.toContainText('Managed nodes')

  await section.getByRole('button', { name: 'Configure' }).click()
  const dialog = page.getByRole('dialog', { name: 'Configure AMD GPU Operator' })
  await expect(dialog.getByRole('switch', { name: 'Enable AMD GPU Operator' })).toHaveCount(0)
  await dialog.getByRole('combobox', { name: 'GPU allocation mode', exact: true }).click()
  await page.getByRole('option', { name: 'DRA', exact: true }).click()
  await dialog.getByRole('button', { name: 'Apply changes' }).click()

  await expect(dialog).toBeHidden()
  expect(requests).toEqual([{
    enabled: true, version: 'v1.5.1', allocationMode: 'dra', resourceNaming: 'single',
    nodeLabeller: true, metricsExporter: false,
  }])
  // Changing a running operator restarts GPU workloads, so it waits on a Change Plan.
  await expect(page).toHaveURL('/change-plans?site=site-a')
  await expect(page.getByText('GPU Operator change proposed')).toBeVisible()
  await expect(page.getByRole('article', { name: 'Change Plan for Platform production-k0s' }).first()).toContainText('change-gpu-operator')
})

test('a refused GPU Operator change stays in the dialog with the reason', async ({ page }) => {
  await installApiFixtures(page, { gpuOperatorBusy: true })
  await page.goto('/platforms/platform-a?site=site-a')
  const section = page.getByRole('region', { name: 'AMD GPU Operator' })
  await section.getByRole('button', { name: 'Configure' }).click()
  const dialog = page.getByRole('dialog', { name: 'Configure AMD GPU Operator' })
  await dialog.getByRole('button', { name: 'Apply changes' }).click()
  await expect(dialog).toContainText('targets are busy')
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(section.getByText('Ready', { exact: true })).toBeVisible()
})

test('uninstalling the GPU Operator asks first and proposes enabled false', async ({ page }) => {
  const requests: Array<Record<string, unknown>> = []
  await installApiFixtures(page, { onGPUOperatorRequest: (_, body) => { requests.push(body) } })
  await page.goto('/platforms/platform-a?site=site-a')
  const section = page.getByRole('region', { name: 'AMD GPU Operator' })
  await section.getByRole('button', { name: 'Uninstall' }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Uninstall AMD GPU Operator' })
  await expect(confirm).toContainText('the Platform itself keeps running')
  await confirm.getByRole('button', { name: 'Uninstall GPU Operator' }).click()
  await expect(page).toHaveURL('/change-plans?site=site-a')
  expect(requests).toEqual([{ enabled: false }])
  await expect(page.getByRole('article', { name: 'Change Plan for Platform production-k0s' }).first()).toContainText('disable-gpu-operator')
})

test('a Platform without a GPU Operator offers to install it', async ({ page }) => {
  const requests: Array<Record<string, unknown>> = []
  await installApiFixtures(page, { withoutPlatformGPUOperator: true, onGPUOperatorRequest: (_, body) => { requests.push(body) } })
  await page.goto('/platforms/platform-a?site=site-a')
  const section = page.getByRole('region', { name: 'AMD GPU Operator' })
  await expect(section).toContainText('Not installed. Install it to schedule workloads on the AMD GPUs of this Platform.')
  await section.getByRole('button', { name: 'Install' }).click()
  const dialog = page.getByRole('dialog', { name: 'Install AMD GPU Operator' })
  await expect(dialog.getByRole('group', { name: 'Managed GPU nodes' })).toHaveCount(0)
  // A new operator proposes the same defaults as the deploy wizard, GPU monitoring included.
  await expect(dialog.getByRole('switch', { name: 'Node Labeller' })).toBeChecked()
  await expect(dialog.getByRole('switch', { name: 'Device Metrics Exporter' })).toBeChecked()
  await dialog.getByRole('button', { name: 'Install GPU Operator' }).click()
  await expect(dialog).toBeHidden()
  // Turning the operator on converges without approval.
  await expect(section.getByText('Installing', { exact: true })).toBeVisible()
  expect(requests).toEqual([{
    enabled: true, allocationMode: 'device-plugin', resourceNaming: 'single',
    nodeLabeller: true, metricsExporter: true,
  }])
})

test('the Cluster tab shows the GPUs each node exposes', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/platforms/platform-a?site=site-a')
  await page.getByRole('tab', { name: 'Cluster' }).click()
  const table = page.getByRole('table', { name: 'Kubernetes nodes' })
  await expect(table.getByRole('row', { name: /gpu-node-01/ })).toContainText('8 × amd.com/gpu')
  await expect(table.getByRole('row', { name: /gpu-node-02/ }).getByRole('cell').nth(4)).toHaveText('-')
})
