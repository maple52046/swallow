import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

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

// The GPU Operator is an optional deploy feature (decision 062): its options stay hidden until
// it is chosen, defaults follow the contract, the request derives gpuStackOwner server-side, and
// no node is listed or chosen because the operator's Node Feature Discovery decides.
test('the deploy wizard installs the AMD GPU Operator only when it is chosen', async ({ page }) => {
  let submitted: Record<string, unknown> | undefined
  await installApiFixtures(page, { freePlatformCandidates: true, onPlatformDeploymentRequest: (body) => { submitted = body } })
  await startKubernetesDeploy(page, ['gpu-node-02', 'gpu-node-03'])

  const install = page.getByRole('switch', { name: 'Enable AMD GPU Operator' })
  await expect(install).not.toBeChecked()
  await expect(page.getByRole('combobox', { name: 'GPU allocation mode' })).toHaveCount(0)

  await install.locator('xpath=following-sibling::*[1]').click()
  await expect(page.getByRole('combobox', { name: 'GPU Operator version' })).toContainText('v1.5.1 (verified default)')
  await expect(page.getByRole('combobox', { name: 'GPU allocation mode' })).toContainText('Device Plugin')
  await expect(page.getByRole('combobox', { name: 'DRA driver version' })).toHaveCount(0)
  await expect(page.getByRole('switch', { name: 'Node Labeller' })).toBeChecked()
  await expect(page.getByRole('switch', { name: 'Device Metrics Exporter' })).not.toBeChecked()

  await expect(page.getByRole('group', { name: 'Managed GPU nodes' })).toHaveCount(0)
  await expect(page.getByRole('checkbox', { name: 'gpu-node-02' })).toHaveCount(0)
  await expect(page.getByText(/manages every node its Node Feature Discovery finds/)).toBeVisible()

  await chooseOption(page, 'GPU allocation mode', 'DRA')
  await expect(page.getByRole('combobox', { name: 'DRA driver version' })).toContainText('v1.0.1')
  await expect(page.getByRole('combobox', { name: 'GPU resource naming' })).toHaveCount(0)

  await page.getByRole('button', { name: 'Next' }).click()
  await page.getByRole('button', { name: 'Next' }).click()
  await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
  await expect(page.getByText('v1.5.1 · DRA', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Deploy Kubernetes Platform' }).click()

  await expect(page).toHaveURL('/platforms/platform-new?site=site-a')
  expect(submitted?.gpuStackOwner).toBeUndefined()
  expect(submitted?.gpuOperator).toEqual({
    enabled: true, allocationMode: 'dra', resourceNaming: 'single',
    nodeLabeller: true, metricsExporter: false,
  })
  const section = page.getByRole('region', { name: 'AMD GPU Operator' })
  await expect(section.getByText('Installing', { exact: true })).toBeVisible()
})

test('the GPU Operator cannot be chosen without an AMD workload node', async ({ page }) => {
  let submitted: Record<string, unknown> | undefined
  await installApiFixtures(page, { freePlatformCandidates: true, onPlatformDeploymentRequest: (body) => { submitted = body } })
  await startKubernetesDeploy(page, ['gpu-node-04'])

  await expect(page.getByRole('switch', { name: 'Enable AMD GPU Operator' })).toBeDisabled()
  await expect(page.getByText('None of the selected workload Servers has an AMD compute GPU.')).toBeVisible()
  await page.getByRole('button', { name: 'Next' }).click()
  await page.getByRole('button', { name: 'Next' }).click()
  await expect(page.getByText('Not installed', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Deploy Kubernetes Platform' }).click()
  await expect(page).toHaveURL('/platforms/platform-new?site=site-a')
  expect(submitted?.gpuOperator).toBeUndefined()
})

test('a Platform shows its GPU Operator and changes the allocation mode', async ({ page }) => {
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
  await expect(section.getByText('Installing', { exact: true })).toBeVisible()
  await expect(section.locator('.sw-progress-spinner')).toBeVisible()
  await expect(section.getByRole('button', { name: 'Configure' })).toBeDisabled()
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

test('uninstalling the GPU Operator asks first and sends enabled false', async ({ page }) => {
  const requests: Array<Record<string, unknown>> = []
  await installApiFixtures(page, { onGPUOperatorRequest: (_, body) => { requests.push(body) } })
  await page.goto('/platforms/platform-a?site=site-a')
  const section = page.getByRole('region', { name: 'AMD GPU Operator' })
  await section.getByRole('button', { name: 'Uninstall' }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Uninstall AMD GPU Operator' })
  await expect(confirm).toContainText('the Platform itself keeps running')
  await confirm.getByRole('button', { name: 'Uninstall GPU Operator' }).click()
  expect(requests).toEqual([{ enabled: false }])
  await expect(section.getByText('Uninstalling', { exact: true })).toBeVisible()
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
  await dialog.getByRole('button', { name: 'Install GPU Operator' }).click()
  await expect(dialog).toBeHidden()
  expect(requests[0]).toMatchObject({ enabled: true, allocationMode: 'device-plugin' })
  expect(requests[0]).not.toHaveProperty('serverIds')
})

test('the Cluster tab shows the GPUs each node exposes', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/platforms/platform-a?site=site-a')
  await page.getByRole('tab', { name: 'Cluster' }).click()
  const table = page.getByRole('table', { name: 'Kubernetes nodes' })
  await expect(table.getByRole('row', { name: /gpu-node-01/ })).toContainText('8 × amd.com/gpu')
  await expect(table.getByRole('row', { name: /gpu-node-02/ }).getByRole('cell').nth(4)).toHaveText('-')
})
