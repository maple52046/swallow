import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

async function chooseSelectOption(page: Page, fieldLabel: string, optionLabel: string) {
  await page.getByRole('combobox', { name: fieldLabel, exact: true }).click()
  await page.getByRole('option', { name: optionLabel, exact: true }).click()
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
    localStorage.removeItem('swallow.servers.group-by')
    localStorage.removeItem('swallow.servers.page-size')
  })
})

test('fleet overview, discovery search, quick lenses, and contextual actions stay independent', async ({ page }) => {
  await installApiFixtures(page, {
    readyServerCount: 1,
    existingServerIds: ['srv-4'],
    changingServerIds: ['srv-2'],
    ephemeralServerIds: ['srv-2'],
    lockedServerIds: ['srv-2'],
    failedServerIds: ['srv-3'],
    absentServerIds: ['srv-4'],
    unobservedHealthServerIds: ['srv-3'],
    deploymentAttentionServerIds: ['srv-3'],
    multiGpuServerIds: ['srv-3'],
    extraTags: { 'srv-1': ['east', 'training'] },
  })
  await page.goto('/servers?site=site-a')

  const overview = page.locator('.sw-server-fleet-overview')
  await expect(overview.getByRole('heading', { name: '4 managed servers in scope' })).toBeVisible()
  await expect(overview.locator('[aria-label="Inventory"]')).toContainText('Total4')
  await expect(overview.locator('[aria-label="Inventory"]')).toContainText('Absent1')
  await expect(overview.locator('[aria-label="OS deployment"]')).toContainText('Verified0')
  await expect(overview.locator('[aria-label="OS deployment"]')).toContainText('Active1')
  await expect(overview.locator('[aria-label="OS deployment"]')).toContainText('Attention1')
  await expect(page.getByText('Showing 1–3 of 3 servers')).toBeVisible()
  await expect(page.getByText('Live', { exact: true })).toBeVisible()

  const table = page.getByRole('table', { name: 'Servers' })
  const ready = table.getByRole('row').filter({ hasText: 'gpu-node-01' })
  const changing = table.getByRole('row').filter({ hasText: 'gpu-node-02' })
  await expect(table.getByText('Provider', { exact: true })).toHaveCount(0)
  const issue = table.getByRole('row').filter({ hasText: 'gpu-node-03' })
  const deployAction = ready.getByRole('link', { name: 'Deploy OS for gpu-node-01' })
  const workflowAction = changing.getByRole('link', { name: 'View workflow for gpu-node-02' })
  const reviewAction = issue.getByRole('link', { name: 'View workflow for gpu-node-03' })
  // Provider inspection is a generic in-progress state and outranks the Swallow deploy result.
  await expect(changing.getByText('Inspecting', { exact: true })).toBeVisible()
  await expect(ready.getByText('Ready', { exact: true })).toBeVisible()
  // Its running time counts from when Swallow first observed it (02:58:30 against a 03:05:00
  // page clock); an idle state has none.
  await expect(changing.getByText('Running for 6m 30s', { exact: true })).toBeVisible()
  await expect(ready.getByText(/^Running for/)).toHaveCount(0)
  await expect(deployAction).toHaveAttribute('href', /serverId=srv-1.*site=site-a|site=site-a.*serverId=srv-1/)
  await expect(workflowAction).toHaveAttribute('href', '/workflows/op-running?site=site-a')
  // A Swallow deployment that needs review opens its Workflow, which holds the failed Step.
  await expect(reviewAction).toHaveAttribute('href', '/workflows/op-deploy-failed?site=site-a')
  // The contextual step is a frameless icon-and-text link on Deployment's secondary line.
  const contextActions = [[deployAction, 'Deploy OS'], [workflowAction, 'View workflow'], [reviewAction, 'View workflow']] as const
  for (const [action, label] of contextActions) {
    await expect(action).toHaveText(label)
    const icon = action.locator('svg')
    await expect(icon).toHaveCount(1)
    await expect(action.locator('.sw-server-context-action__label')).toHaveCSS('text-decoration-line', 'underline')
    await expect(icon).toHaveCSS('text-decoration-line', 'none')
    await expect(action).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
    await expect(action).toHaveCSS('border-top-width', '0px')
    expect(await action.evaluate((link) => link.closest('td')?.classList.contains('sw-server-col--deployment'))).toBe(true)
    expect(await action.evaluate((link) => link.parentElement?.classList.contains('sw-deployment-summary__meta'))).toBe(true)
  }
  const deploymentActionPlacement = await deployAction.evaluate((link) => {
    const state = link.closest('.sw-deployment-summary')?.querySelector('.sw-deployment-summary__state')
    if (!(state instanceof HTMLElement)) throw new Error('Deployment state is missing')
    return { stateTop: state.getBoundingClientRect().top, actionTop: link.getBoundingClientRect().top }
  })
  expect(deploymentActionPlacement.actionTop).toBeGreaterThan(deploymentActionPlacement.stateTop)
  await deployAction.focus()
  await expect(deployAction).toBeFocused()
  await expect(page.getByRole('tooltip')).toHaveText('Deploy OS')
  // The Actions column holds only the labelled mutation menu, the same width on every row.
  const triggers = [ready, changing, issue].map((row) => row.locator('.sw-server-row-actions').getByRole('button', { name: /^Actions for gpu-node-0/ }))
  for (const trigger of triggers) await expect(trigger).toHaveText('Actions')
  const triggerWidths = await Promise.all(triggers.map((trigger) => trigger.evaluate((button) => button.getBoundingClientRect().width)))
  expect(new Set(triggerWidths).size).toBe(1)
  await expect(ready.locator('.sw-server-row-actions').getByRole('link')).toHaveCount(0)
  await triggers[0].click()
  await expect(page.getByRole('menuitem', { name: 'Power', exact: true })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: 'Deploy OS' })).toHaveCount(0)
  await page.keyboard.press('Escape')
  const identityOrder = await changing.locator('.sw-server-identity__copy > div').first().evaluate((line) => (
    Array.from(line.children).map((child) => child.getAttribute('aria-label') ?? child.textContent?.trim())
  ))
  expect(identityOrder.slice(0, 3)).toEqual(['Locked', 'RAM deployment', 'gpu-node-02'])
  await expect(changing.locator('.sw-server-col--power').getByLabel('RAM deployment', { exact: true })).toHaveCount(0)
  await expect(ready.getByLabel('2 more tags')).toHaveText('+2')
  const visibleTag = ready.getByText('east', { exact: true })
  await expect(visibleTag).toHaveCSS('border-radius', '4px')
  await expect(visibleTag).toHaveCSS('align-items', 'center')
  await expect(visibleTag).toHaveCSS('justify-content', 'center')
  await ready.getByRole('button', { name: 'Edit tags for gpu-node-01' }).click()
  const tagEditor = page.getByRole('dialog', { name: 'Edit tags' })
  await expect(tagEditor).toContainText('Edit the tags on gpu-node-01.')
  await tagEditor.getByRole('button', { name: 'Cancel' }).click()
  await expect(tagEditor).toHaveCount(0)

  await ready.getByRole('button', { name: 'Show details for gpu-node-01' }).click()
  const details = table.getByRole('row').filter({ hasText: 'SN0001' })
  await expect(details).toContainText('east, gpu, production, training')
  await expect(details.locator('td')).toHaveCSS('padding-top', '20px')
  await expect(details).toContainText('AMD EPYC 9554')

  await page.getByRole('button', { name: 'Active deployments', exact: true }).click()
  await expect(page).toHaveURL(/site=site-a.*view=changing|view=changing.*site=site-a/)
  await expect(table.getByText('gpu-node-02', { exact: true })).toBeVisible()
  await expect(table.getByText('gpu-node-01', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'All', exact: true }).click()
  await page.getByRole('button', { name: 'Deployable', exact: true }).click()
  await expect(table.getByText('gpu-node-01', { exact: true })).toBeVisible()
  await expect(table.getByText('gpu-node-02', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Needs attention', exact: true }).click()
  await expect(table.getByText('gpu-node-03', { exact: true })).toBeVisible()
  await expect(table.getByText('gpu-node-01', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'All', exact: true }).click()

  const search = page.getByRole('textbox', { name: 'Search Servers' })
  await search.fill('SN0002')
  await expect(page).toHaveURL(/q=SN0002/)
  await expect(table.getByText('gpu-node-02', { exact: true })).toBeVisible()
  await search.fill('training')
  await expect(page).toHaveURL(/q=training/)
  await expect(table.getByText('gpu-node-01', { exact: true })).toBeVisible()

  await page.getByLabel('Select gpu-node-01').check()
  await expect(page.getByText('1 selected')).toBeVisible()
  await search.fill('H100')
  await expect(page).toHaveURL(/q=H100/)
  await expect(page.getByText('1 selected')).toHaveCount(0)
  await expect(table.getByText('gpu-node-03', { exact: true })).toBeVisible()
  await expect(table.getByText('gpu-node-01', { exact: true })).toHaveCount(0)
  await expect(overview.getByRole('heading', { name: '4 managed servers in scope' })).toBeVisible()

  await search.fill('EPYC 9554')
  await expect(page.getByRole('heading', { name: 'No matching Servers' })).toBeVisible()
  await page.getByRole('button', { name: 'Clear filters' }).click()
  await expect(page).toHaveURL('/servers?site=site-a')
  await expect(search).toHaveValue('')

  await page.getByRole('button', { name: /Filters/ }).click()
  await page.getByRole('checkbox', { name: 'Include absent projections' }).locator('..').click()
  await expect(page).toHaveURL(/includeAbsent=true/)
  await page.keyboard.press('Escape')
  const absent = table.getByRole('row').filter({ hasText: 'gpu-node-04' })
  await expect(absent).toBeVisible()
  await expect(absent.locator('.sw-server-hardware-summary')).toHaveText('Not reported')
  await expect(absent.locator('.sw-server-hardware-capacity')).toHaveCount(0)
  await expect(absent.getByText('Unknown', { exact: true })).toBeVisible()
  await expect(absent.getByRole('link', { name: 'Review activity for gpu-node-04' })).toHaveAttribute('href', '/servers/srv-4/activity?site=site-a#activity-provider-events')
})

test('deployed status uses one tone regardless of deployment provenance', async ({ page }) => {
  await installApiFixtures(page, { existingServerIds: ['srv-2'] })
  await page.goto('/servers?site=site-a')

  const table = page.getByRole('table', { name: 'Servers' })
  const verified = table.getByRole('row').filter({ hasText: 'gpu-node-01' }).locator('.sw-deployment-summary__state')
  const providerOnly = table.getByRole('row').filter({ hasText: 'gpu-node-02' }).locator('.sw-deployment-summary__state')
  const states = [verified, providerOnly]
  for (const state of states) await expect(state).toHaveText('Deployed')
  await expect(table.locator('.sw-deployment-summary__indicator')).toHaveCount(0)
  const colors = await Promise.all(states.map((state) => state.evaluate((element) => (
    getComputedStyle(element).color
  ))))
  expect(new Set(colors).size).toBe(1)
})

test('provider-only work links to the Activity section that records it', async ({ page }) => {
  await installApiFixtures(page, {
    providerWorkServerIds: { 'srv-1': 'releasing', 'srv-2': 'inspecting', 'srv-3': 'testing' },
  })
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('/servers?site=site-a')

  const table = page.getByRole('table', { name: 'Servers' })
  const viewActivity = (name: string) => table.getByRole('row').filter({ hasText: name }).first().getByRole('link', { name: `View activity for ${name}` })
  await expect(viewActivity('gpu-node-01')).toHaveAttribute('href', '/servers/srv-1/activity?site=site-a#activity-provisioning-tasks')
  await expect(viewActivity('gpu-node-02')).toHaveAttribute('href', '/servers/srv-2/activity?site=site-a#activity-related-operations')
  await expect(viewActivity('gpu-node-03')).toHaveAttribute('href', '/servers/srv-3/activity?site=site-a#activity-provider-events')
  await expect(table.getByRole('link', { name: /^Monitor/ })).toHaveCount(0)

  // Hover without scrolling: the tooltip closes when the page scrolls under the pointer.
  const inspecting = viewActivity('gpu-node-02')
  await expect(inspecting).toBeInViewport()
  await inspecting.hover()
  await expect(page.getByRole('tooltip')).toHaveText('View activity · Related Operations')
  // A short viewport makes the Activity tab taller than the screen, so landing is observable.
  await page.setViewportSize({ width: 1280, height: 520 })
  await inspecting.click()
  await expect(page).toHaveURL('/servers/srv-2/activity?site=site-a#activity-related-operations')
  await expect(page.getByRole('heading', { name: 'Related Operations' })).toBeInViewport()
  await expect(page.getByRole('heading', { name: 'Current browser session' })).not.toBeInViewport()
})

test('in-progress rows and spinners keep moving when the OS asks for reduced motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await installApiFixtures(page, { readyServerCount: 1, changingServerIds: ['srv-2'] })
  await page.goto('/servers?site=site-a')

  const table = page.getByRole('table', { name: 'Servers' })
  const changing = table.getByRole('row').filter({ hasText: 'gpu-node-02' })
  const spinner = changing.locator('.sw-deployment-summary__state .sw-progress-spinner')
  await expect(spinner).toBeVisible()
  const runningAnimations = (element: Element) => element.getAnimations().map((animation) => ({
    name: (animation as CSSAnimation).animationName,
    state: animation.playState,
    iterations: animation.effect?.getComputedTiming().iterations,
  }))
  expect(await spinner.evaluate(runningAnimations)).toEqual([{ name: 'spin', state: 'running', iterations: Infinity }])
  // The whole row of a Server with running work carries the light-band sweep; an idle row does not.
  expect(await changing.evaluate(runningAnimations)).toEqual([{ name: 'sw-progress-sweep', state: 'running', iterations: Infinity }])
  const idle = table.getByRole('row').filter({ hasText: 'gpu-node-01' })
  expect(await idle.evaluate(runningAnimations)).toEqual([])
})

test('hardware facets, deterministic grouping, sorting, and URL fallback are shareable', async ({ page }) => {
  await installApiFixtures(page, {
    multiGpuServerIds: ['srv-3'],
    extraTags: { 'srv-1': ['east'] },
  })
  await page.goto('/servers?site=site-a')

  await page.getByRole('button', { name: /Filters/ }).click()
  await page.getByRole('checkbox', { name: 'NVIDIA (1)' }).locator('..').click()
  await page.keyboard.press('Escape')
  await expect(page).toHaveURL(/gpuVendor=NVIDIA/)
  const table = page.getByRole('table', { name: 'Servers' })
  await expect(table.getByText('gpu-node-03', { exact: true })).toBeVisible()
  await expect(table.getByText('gpu-node-01', { exact: true })).toHaveCount(0)
  const mixedGpuRow = table.getByRole('row').filter({ hasText: 'gpu-node-03' })
  const mixedGpuSummary = mixedGpuRow.locator('.sw-server-hardware-gpu')
  await expect(mixedGpuSummary).toHaveText('AMD MI300X')
  await mixedGpuSummary.hover()
  await expect(page.getByRole('tooltip')).toHaveText('8 × AMD MI300X + 4 × NVIDIA H100')

  await page.goto('/servers?site=site-a&gpu=present&architecture=amd64%2Fgeneric&systemVendor=Supermicro&systemProduct=AS-8125GS-TNHR&zone=rack-a&pool=accelerators&tag=east&tag=production&membership=assigned&health=up')
  await expect(table.getByText('gpu-node-01', { exact: true })).toBeVisible()
  await expect(table.getByText('gpu-node-02', { exact: true })).toBeVisible()
  await expect(table.getByText('gpu-node-03', { exact: true })).toHaveCount(0)

  await page.goto('/servers?site=site-a&gpu=none')
  await expect(table.getByText('gpu-node-04', { exact: true })).toBeVisible()
  await expect(table.getByText('gpu-node-01', { exact: true })).toHaveCount(0)

  await page.goto('/servers?site=site-a')
  await page.getByRole('button', { name: 'Display' }).click()
  await chooseSelectOption(page, 'Group Servers', 'GPU profile')
  await chooseSelectOption(page, 'Sort Servers', 'GPU count')
  await chooseSelectOption(page, 'Sort direction', 'Descending')
  await page.keyboard.press('Escape')
  await expect(page).toHaveURL(/group=gpu-profile/)
  await expect(page).toHaveURL(/sort=gpus/)
  await expect(page).toHaveURL(/dir=desc/)
  await expect(table.getByRole('row').filter({ hasText: '8 × AMD MI300X + 4 × NVIDIA H100' }).first()).toBeVisible()
  await expect(table.locator('tbody .sw-server-name').first()).toHaveText('gpu-node-03')

  await page.goto('/servers?site=site-a&page=99&health=invalid&group=invalid')
  await expect(page).toHaveURL('/servers?site=site-a')
  await expect(page.getByText('Showing 1–4 of 4 servers')).toBeVisible()
})

test('GPU summaries prefer compute capacity and Server detail separates display controllers', async ({ page }) => {
  await installApiFixtures(page, { displayOnlyServerIds: ['srv-2'], cpuOnlyServerIds: ['srv-3'] })
  await page.goto('/servers?site=site-a')

  const table = page.getByRole('table', { name: 'Servers' })
  const computeRow = table.getByRole('row').filter({ hasText: 'gpu-node-01' }).first()
  const computeSummary = computeRow.locator('.sw-server-hardware-gpu')
  await expect(computeSummary).toHaveText('AMD MI300X')
  await computeSummary.focus()
  await expect(page.getByRole('tooltip', { name: '8 × AMD MI300X', exact: true })).toBeVisible()
  const cpuModel = computeRow.locator('.sw-server-hardware-cpu')
  await expect(cpuModel).toHaveText('AMD EPYC 9554')
  await cpuModel.hover()
  await expect(page.getByRole('tooltip', { name: 'AMD EPYC 9554', exact: true })).toBeVisible()

  const displayRow = table.getByRole('row').filter({ hasText: 'gpu-node-02' }).first()
  const displaySummary = displayRow.locator('.sw-server-hardware-gpu')
  await expect(displaySummary).toHaveText('ASPEED Technology, Inc. ASPEED Graphics Family')
  await displaySummary.hover()
  await expect(page.getByRole('tooltip', { name: '1 × ASPEED Technology, Inc. ASPEED Graphics Family', exact: true })).toBeVisible()

  const cpuOnlyRow = table.getByRole('row').filter({ hasText: 'gpu-node-03' }).first()
  await expect(cpuOnlyRow.locator('.sw-server-hardware-gpu')).toHaveCount(0)
  await expect(cpuOnlyRow.locator('.sw-server-hardware-cpu')).toHaveText('AMD EPYC 9554')
  await expect(cpuOnlyRow.locator('.sw-server-hardware-fact--primary .sw-server-hardware-cpu')).toHaveText('AMD EPYC 9554')
  await expect(cpuOnlyRow.locator('.sw-server-hardware-label')).toHaveText('CPU')
  await expect(cpuOnlyRow.locator('.sw-server-hardware-capacity > span')).toHaveText(['64 cores', 'RAM 512 GiB'])
  await expect(cpuOnlyRow.locator('.sw-server-hardware-capacity strong')).toHaveText(['64', '512 GiB'])

  const [primaryStyle, secondaryStyle] = await Promise.all([
    computeSummary.evaluate((element) => {
      const style = getComputedStyle(element)
      return { fontSize: style.fontSize, fontWeight: style.fontWeight }
    }),
    cpuModel.evaluate((element) => {
      const style = getComputedStyle(element)
      return { fontSize: style.fontSize, fontWeight: style.fontWeight }
    }),
  ])
  expect(Number(primaryStyle.fontWeight)).toBeGreaterThan(Number(secondaryStyle.fontWeight))
  expect(Number.parseFloat(primaryStyle.fontSize)).toBeGreaterThan(Number.parseFloat(secondaryStyle.fontSize))
  await expect(computeRow.locator('.sw-server-hardware-fact--primary')).toHaveCount(1)
  await expect(computeRow.locator('.sw-server-hardware-label')).toHaveText('GPU')
  const tagStyles = await Promise.all([
    computeRow.locator('.sw-server-hardware-label').evaluate((label) => {
      const style = getComputedStyle(label)
      return { width: style.width, height: style.height, fontSize: style.fontSize, borderRadius: style.borderRadius }
    }),
    cpuOnlyRow.locator('.sw-server-hardware-label').evaluate((label) => {
      const style = getComputedStyle(label)
      return { width: style.width, height: style.height, fontSize: style.fontSize, borderRadius: style.borderRadius }
    }),
  ])
  expect(tagStyles[0]).toEqual(tagStyles[1])
  expect(tagStyles[0].borderRadius).toBe('0px')
  await expect(computeRow.locator('.sw-server-hardware-fact').first()).toHaveCSS('display', 'flex')
  const [summaryBox, capacityBox] = await Promise.all([
    computeRow.locator('.sw-server-hardware-summary').boundingBox(),
    computeRow.locator('.sw-server-hardware-capacity strong').first().boundingBox(),
  ])
  expect(summaryBox).not.toBeNull()
  expect(capacityBox).not.toBeNull()
  expect(Math.abs((summaryBox?.x ?? 0) - (capacityBox?.x ?? 0))).toBeLessThanOrEqual(1)
  const [secondaryBox, capacityLineBox] = await Promise.all([
    cpuModel.boundingBox(),
    computeRow.locator('.sw-server-hardware-capacity').boundingBox(),
  ])
  expect(secondaryBox).not.toBeNull()
  expect(capacityLineBox).not.toBeNull()
  expect((capacityLineBox?.y ?? 0) - ((secondaryBox?.y ?? 0) + (secondaryBox?.height ?? 0))).toBeLessThanOrEqual(2)
  const rowBox = await computeRow.boundingBox()
  expect(rowBox).not.toBeNull()
  expect(rowBox?.height ?? Number.POSITIVE_INFINITY).toBeLessThanOrEqual(82)

  await page.goto('/servers/srv-1/summary?site=site-a')
  const capacity = page.getByRole('heading', { name: 'Capacity', exact: true }).locator('xpath=../../..')
  const computeCapacity = capacity.locator('.sw-server-capacity-item').filter({ hasText: 'Compute GPUs' })
  await expect(computeCapacity).toContainText('8 GPUs')
  await expect(computeCapacity).toContainText('8 × AMD MI300X')
  const displayCapacity = capacity.locator('.sw-server-capacity-item').filter({ hasText: 'Display GPUs' })
  await expect(displayCapacity).toContainText('1 GPU')
  await expect(displayCapacity).toContainText('1 × ASPEED Technology, Inc. ASPEED Graphics Family')

  const hardwareProfile = page.getByRole('heading', { name: 'Hardware profile', exact: true }).locator('xpath=../../..')
  await expect(hardwareProfile).toContainText('Compute GPUs')
  await expect(hardwareProfile).toContainText('Display GPUs')
})

test('generic and opaque provider GPU names remain complete with exact tooltip detail', async ({ page }) => {
  await installApiFixtures(page, {
    genericComputeServerIds: ['srv-1'],
    displayOnlyServerIds: ['srv-2'],
    cirrusDisplayOnlyServerIds: ['srv-3'],
  })
  await page.goto('/servers?site=site-a')

  const table = page.getByRole('table', { name: 'Servers' })
  const genericCompute = table.getByRole('row').filter({ hasText: 'gpu-node-01' }).first()
  const genericSummary = genericCompute.locator('.sw-server-hardware-gpu')
  await expect(genericSummary).toHaveText('AMD GPU')
  await genericSummary.focus()
  await expect(page.getByRole('tooltip', { name: '8 × AMD AMD GPU', exact: true })).toBeVisible()

  const aspeedDisplay = table.getByRole('row').filter({ hasText: 'gpu-node-02' }).first()
  await expect(aspeedDisplay.locator('.sw-server-hardware-gpu')).toHaveText('ASPEED Technology, Inc. ASPEED Graphics Family')

  const cirrusDisplay = table.getByRole('row').filter({ hasText: 'gpu-node-03' }).first()
  const cirrusSummary = cirrusDisplay.locator('.sw-server-hardware-gpu')
  await expect(cirrusSummary).toHaveText('Cirrus Logic GD 5446')
  await cirrusSummary.hover()
  await expect(page.getByRole('tooltip', { name: '1 × Cirrus Logic GD 5446', exact: true })).toBeVisible()
})
test('display preferences migrate once while grouping and sorting preserve selection', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.servers.group-by', JSON.stringify('zone'))
    localStorage.setItem('swallow.servers.density', JSON.stringify('comfortable'))
    localStorage.setItem('swallow.servers.page-size', JSON.stringify(25))
  })
  await installApiFixtures(page, { fleetSize: 60 })
  await page.goto('/servers?site=site-a')

  await expect(page).toHaveURL(/site=site-a.*group=zone|group=zone.*site=site-a/)
  await expect(page.getByText('Showing 1–25 of 60 servers')).toBeVisible()
  await page.getByLabel('Select gpu-node-04').click()
  await expect(page.getByRole('region', { name: 'Selection actions' })).toBeVisible()

  await page.getByRole('button', { name: 'Display' }).click()
  await expect(page.getByRole('combobox', { name: 'Table density' })).toContainText('Comfortable rows')
  await expect(page.getByRole('combobox', { name: 'Rows per page' })).toContainText('25 rows')
  await chooseSelectOption(page, 'Group Servers', 'No grouping')
  await page.keyboard.press('Escape')
  await expect(page).toHaveURL('/servers?site=site-a')
  await page.waitForTimeout(50)
  await expect(page).toHaveURL('/servers?site=site-a')
  await expect(page.getByRole('region', { name: 'Selection actions' })).toBeVisible()

  await page.getByRole('button', { name: 'Display' }).click()
  await chooseSelectOption(page, 'Sort Servers', 'GPU count')
  await page.keyboard.press('Escape')
  await expect(page).toHaveURL(/sort=gpus/)
  await expect(page.getByRole('region', { name: 'Selection actions' })).toBeVisible()

  await page.goto('/servers')
  const allSitesRow = page.getByRole('row').filter({ hasText: 'compute-node-005' })
  await expect(allSitesRow).not.toContainText('Taipei Lab')
  await expect(allSitesRow.locator('.sw-server-col--identity').getByText('compute', { exact: true })).toBeVisible()
})

test('SSE reconnect resync keeps last-good rows through a refresh failure and recovers', async ({ page }) => {
  const requests: number[] = []
  await installApiFixtures(page, {
    serverListFailureRequestNumbers: [3],
    onServerListRequest: (count) => requests.push(count),
  })
  await page.goto('/servers?site=site-a')
  await expect(page.getByText('Live', { exact: true })).toBeVisible()
  await expect(page.getByRole('table', { name: 'Servers' }).getByText('gpu-node-01', { exact: true })).toBeVisible()

  await page.evaluate(() => {
    const sources = (window as typeof window & {
      __serverEventSources?: Array<{ emitError: (closed?: boolean) => void }>
    }).__serverEventSources
    sources?.at(-1)?.emitError(false)
  })
  await expect(page.getByText(/Reconnecting/)).toBeVisible()
  await page.evaluate(() => {
    const sources = (window as typeof window & {
      __serverEventSources?: Array<{ emitOpen: () => void }>
    }).__serverEventSources
    sources?.at(-1)?.emitOpen()
  })

  await expect(page.getByText("Couldn't refresh the Server inventory")).toBeVisible()
  await expect(page.getByRole('table', { name: 'Servers' }).getByText('gpu-node-01', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Retry' }).click()
  await expect(page.getByText("Couldn't refresh the Server inventory")).toHaveCount(0)
  await expect(page.getByText('Live', { exact: true })).toBeVisible()

  const beforeRemoved = requests.length
  await page.evaluate(() => {
    const sources = (window as typeof window & {
      __serverEventSources?: Array<{ emitMessage: (data: string) => void }>
    }).__serverEventSources
    sources?.at(-1)?.emitMessage(JSON.stringify({ type: 'removed', id: 'srv-1' }))
  })
  await expect.poll(() => requests.length).toBeGreaterThan(beforeRemoved)
  await expect(page.getByRole('table', { name: 'Servers' }).getByText('gpu-node-01', { exact: true })).toBeVisible()
  await page.evaluate(() => {
    const sources = (window as typeof window & {
      __serverEventSources?: Array<{ emitError: (closed?: boolean) => void }>
    }).__serverEventSources
    sources?.at(-1)?.emitError(true)
  })
  await expect(page.getByText(/Live updates unavailable/)).toBeVisible()
})

test('live updates reconnect with the current access token after the browser closes the stream', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/servers?site=site-a')
  await expect(page.getByText('Live', { exact: true })).toBeVisible()
  const sourceCount = () => page.evaluate(() => (window as typeof window & { __serverEventSources?: unknown[] }).__serverEventSources?.length ?? 0)
  const before = await sourceCount()

  // A 401 for an expired access token makes the browser close the stream for good.
  await page.evaluate(() => {
    const sources = (window as typeof window & { __serverEventSources?: Array<{ emitError: (closed?: boolean) => void }> }).__serverEventSources
    sources?.at(-1)?.emitError(true)
  })
  await expect(page.getByText(/Live updates unavailable/)).toBeVisible()

  // The adapter opens a new stream after its backoff, carrying the Session's access token again.
  await expect.poll(sourceCount, { timeout: 5_000 }).toBeGreaterThan(before)
  const url = await page.evaluate(() => (window as typeof window & { __serverEventSources?: Array<{ url: string }> }).__serverEventSources?.at(-1)?.url ?? '')
  expect(new URL(url, 'http://localhost').searchParams.get('access_token')).toBe('e2e-token')
  await page.evaluate(() => {
    const sources = (window as typeof window & { __serverEventSources?: Array<{ emitOpen: () => void }> }).__serverEventSources
    sources?.at(-1)?.emitOpen()
  })
  await expect(page.getByText('Live', { exact: true })).toBeVisible()
})

test('mobile cards preserve operational and hardware facts without horizontal overflow', async ({ page }) => {
  await installApiFixtures(page, {
    readyServerCount: 1,
    changingServerIds: ['srv-2'],
    deployedImageNames: { 'srv-3': 'Ubuntu 24.04 LTS ROCm Enterprise Accelerator Image' },
    extraTags: { 'srv-1': ['east', 'training'] },
  })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/servers?site=site-a')

  const card = page.getByRole('article').filter({ hasText: 'gpu-node-01' })
  await expect(card.locator('.sw-server-hardware-purpose')).toHaveCount(0)
  await expect(card.locator('.sw-server-hardware-gpu')).toHaveText('AMD MI300X')
  await expect(card.locator('.sw-server-hardware-fact--primary .sw-server-hardware-gpu')).toHaveCount(1)
  await expect(card.locator('.sw-server-hardware-cpu')).toHaveText('AMD EPYC 9554')
  const capacity = card.locator('.sw-server-hardware-capacity')
  await expect(card.locator('.sw-server-hardware-label')).toHaveText('GPU')
  await expect(capacity.locator(':scope > span')).toHaveText(['64 cores', 'RAM 512 GiB'])
  await expect(capacity.locator('strong')).toHaveText(['64', '512 GiB'])
  await expect(card.getByText('Zone', { exact: true })).toBeVisible()
  await expect(card.getByText('rack-a', { exact: true }).first()).toBeVisible()
  await expect(card.getByText('Pool', { exact: true })).toBeVisible()
  await expect(card.getByText('accelerators', { exact: true }).first()).toBeVisible()
  await expect(card.getByRole('button', { name: 'Edit tags for gpu-node-01' })).toBeVisible()
  const deployAction = card.getByRole('link', { name: 'Deploy OS for gpu-node-01' })
  await expect(deployAction).toHaveText('Deploy OS')
  await expect(deployAction.locator('svg')).toHaveCount(1)
  await expect(card.getByRole('button', { name: 'Actions for gpu-node-01', exact: true })).toHaveText('Actions')
  await card.locator('summary', { hasText: 'More details' }).click()
  await expect(card).toContainText('Operational context · Health')
  await expect(card).toContainText('platform-a')
  await expect(card).toContainText('SN0001')
  await expect(card).toContainText('8 × AMD MI300X')
  await expect(card).toContainText('east, gpu, production, training')

  const wrappedImage = page.getByRole('article').filter({ hasText: 'gpu-node-03' }).locator('.sw-deployment-summary__image')
  await expect(wrappedImage).toHaveCSS('white-space', 'normal')
  expect(await wrappedImage.evaluate((image) => image.getBoundingClientRect().height)).toBeGreaterThan(20)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await card.getByLabel('Mobile selection: gpu-node-01').check()
  await expect(page.getByRole('region', { name: 'Selection actions' })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

  const name = card.getByRole('link', { name: 'gpu-node-01', exact: true })
  await name.focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL('/servers/srv-1/summary?site=site-a')
})

test('13-inch layout prioritizes core columns and reveals context when space permits', async ({ page }) => {
  await installApiFixtures(page, {
    failedServerIds: ['srv-1'],
    changingServerIds: ['srv-3'],
    displayOnlyServerIds: ['srv-1'],
    deployedImageNames: { 'srv-2': 'Ubuntu 24.04 LTS ROCm Enterprise Accelerator Image' },
  })
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto('/servers?site=site-a')

  const table = page.getByRole('table', { name: 'Servers' })
  await expect(table).toBeVisible()
  for (const heading of ['Server', 'Power', 'Network', 'Deployment', 'Hardware', 'Zone', 'Pool']) {
    await expect(table.getByRole('columnheader', { name: heading, exact: true })).toBeVisible()
  }
  await expect(table.getByRole('columnheader', { name: 'Placement', exact: true })).toHaveCount(0)
  const serverHeader = table.getByRole('columnheader', { name: 'Server', exact: true })
  expect(await serverHeader.evaluate((header) => header.nextElementSibling?.textContent?.trim())).toBe('Power')
  const serverColumnWidth = await serverHeader.evaluate((header) => header.getBoundingClientRect().width)
  expect(serverColumnWidth).toBeGreaterThanOrEqual(175)
  expect(serverColumnWidth).toBeLessThanOrEqual(177)
  await expect(table.getByRole('columnheader', { name: 'Signals', exact: true })).toHaveCount(0)
  await expect(table.getByRole('columnheader', { name: 'Health', exact: true })).toBeHidden()
  await expect(table.getByRole('columnheader', { name: 'Platform', exact: true })).toHaveCount(0)
  const [deploymentWidth, hardwareWidth] = await Promise.all([
    table.getByRole('columnheader', { name: 'Deployment', exact: true }).evaluate((header) => header.getBoundingClientRect().width),
    table.getByRole('columnheader', { name: 'Hardware', exact: true }).evaluate((header) => header.getBoundingClientRect().width),
  ])
  expect(Math.abs(deploymentWidth - hardwareWidth)).toBeLessThanOrEqual(1)

  const providerFailed = table.getByRole('row').filter({ hasText: 'gpu-node-01' }).first()
  const detailsButton = providerFailed.getByRole('button', { name: 'Show details for gpu-node-01' })
  const detailsHeader = table.getByRole('columnheader', { name: 'Row details' })
  await expect(detailsHeader).toBeVisible()
  await expect(providerFailed.locator('.sw-col-details').getByRole('button', { name: 'Show details for gpu-node-01' })).toBeVisible()
  await expect(providerFailed.locator('.sw-server-col--identity').getByRole('button', { name: 'Show details for gpu-node-01' })).toHaveCount(0)
  await expect(providerFailed.locator('.sw-server-row-actions').getByRole('button', { name: 'Show details for gpu-node-01' })).toHaveCount(0)
  expect(await detailsHeader.evaluate((header) => header.previousElementSibling?.getAttribute('aria-label'))).toBe('Row selection')
  expect(await detailsHeader.evaluate((header) => header.nextElementSibling?.textContent?.trim())).toBe('Server')
  await expect(detailsButton).toBeVisible()
  const [serverNameBox, serverNameCopyBox] = await Promise.all([
    providerFailed.locator('.sw-server-name').boundingBox(),
    providerFailed.getByRole('button', { name: 'Copy Server name' }).boundingBox(),
  ])
  if (!serverNameBox || !serverNameCopyBox) throw new Error('Server name or copy action is missing')
  const serverNameCopyGap = serverNameCopyBox.x - (serverNameBox.x + serverNameBox.width)
  expect(serverNameCopyGap).toBeGreaterThanOrEqual(0)
  expect(serverNameCopyGap).toBeLessThanOrEqual(6)
  const powerStartGap = await providerFailed.locator('.sw-server-col--power').evaluate((cell) => {
    const button = cell.querySelector('.sw-power-button')
    if (!(button instanceof HTMLElement)) throw new Error('Power action button is missing')
    return button.getBoundingClientRect().left - cell.getBoundingClientRect().left
  })
  expect(powerStartGap).toBeLessThanOrEqual(12)

  await expect(providerFailed).toContainText('192.168.40.21')
  await expect(providerFailed).toContainText('02:00:00:00:00:01')
  for (const value of ['192.168.40.21', '02:00:00:00:00:01']) {
    const fact = providerFailed.getByText(value, { exact: true })
    expect(await fact.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true)
  }
  for (const label of ['Copy IP address', 'Copy MAC address']) {
    const copyButton = providerFailed.getByRole('button', { name: label })
    const geometry = await copyButton.evaluate((button) => {
      const value = button.previousElementSibling
      if (!(value instanceof HTMLElement)) throw new Error('Copy button is not adjacent to its value')
      const buttonRect = button.getBoundingClientRect()
      const valueRect = value.getBoundingClientRect()
      return { width: buttonRect.width, gap: buttonRect.left - valueRect.right }
    })
    expect(geometry.width).toBe(24)
    expect(geometry.gap).toBeLessThanOrEqual(4)
  }
  await expect(providerFailed).not.toContainText('gpu-node-01.lab.example')
  await expect(providerFailed.getByText('Failed', { exact: true })).toBeVisible()
  const hardwareGeometry = await providerFailed.locator('.sw-server-col--hardware').evaluate((cell) => {
    const content = cell.querySelector('.sw-server-hardware-gpu')
    if (!(content instanceof HTMLElement)) throw new Error('Hardware summary is missing')
    return {
      cellRight: cell.getBoundingClientRect().right,
      contentRight: content.getBoundingClientRect().right,
      clientWidth: content.clientWidth,
      scrollWidth: content.scrollWidth,
      whiteSpace: getComputedStyle(content).whiteSpace,
    }
  })
  await expect(providerFailed.locator('.sw-server-hardware-gpu')).toHaveText('ASPEED Technology, Inc. ASPEED Graphics Family')
  await expect(providerFailed.locator('.sw-server-hardware-cpu')).toHaveText('AMD EPYC 9554')
  expect(hardwareGeometry.contentRight).toBeLessThanOrEqual(hardwareGeometry.cellRight)
  expect(hardwareGeometry.scrollWidth).toBeGreaterThan(hardwareGeometry.clientWidth)
  expect(hardwareGeometry.whiteSpace).toBe('nowrap')
  await expect(providerFailed.getByRole('link', { name: 'Review activity for gpu-node-01' })).toHaveAttribute('href', '/servers/srv-1/activity?site=site-a#activity-provider-events')
  const changing = table.getByRole('row').filter({ hasText: 'gpu-node-03' }).first()
  await expect(changing.getByRole('link', { name: 'View workflow for gpu-node-03' })).toBeVisible()
  for (const [cellClass, control] of [
    ['.sw-server-col--deployment', 'a'],
    ['.sw-server-row-actions', 'button'],
  ] as const) {
    const geometry = await changing.locator(cellClass).evaluate((cell, selector) => {
      const target = cell.querySelector(selector)
      if (!target) throw new Error(`${selector} is missing`)
      return { cellRight: cell.getBoundingClientRect().right, controlRight: target.getBoundingClientRect().right }
    }, control)
    expect(geometry.controlRight).toBeLessThanOrEqual(geometry.cellRight)
  }
  const deployed = table.getByRole('row').filter({ hasText: 'gpu-node-02' }).first()
  // An idle deployed Server gets no contextual icon; its host name already opens the Summary.
  await expect(deployed.locator('.sw-server-col--deployment').getByRole('link')).toHaveCount(0)
  await expect(deployed.getByText('Deployed', { exact: true })).toBeVisible()
  const deployedImage = deployed.getByText('Ubuntu 24.04 LTS ROCm Enterprise Accelerator Image', { exact: true })
  await expect(deployedImage).toBeVisible()
  await expect(deployedImage).not.toHaveAttribute('data-scope', 'badge')
  const imageOverflow = await deployedImage.evaluate((image) => ({
    clientWidth: image.clientWidth,
    scrollWidth: image.scrollWidth,
    whiteSpace: getComputedStyle(image).whiteSpace,
  }))
  expect(imageOverflow.whiteSpace).toBe('nowrap')
  expect(imageOverflow.scrollWidth).toBeGreaterThan(imageOverflow.clientWidth)
  await deployedImage.scrollIntoViewIfNeeded()
  await deployedImage.hover()
  await expect(page.getByRole('tooltip')).toContainText('Ubuntu 24.04 LTS ROCm Enterprise Accelerator Image')
  await expect(page.getByRole('tooltip')).toContainText('An operating system is deployed on this Server.')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

  await page.setViewportSize({ width: 1800, height: 900 })
  for (const heading of ['Health', 'Power']) {
    await expect(table.getByRole('columnheader', { name: heading, exact: true })).toBeVisible()
  }
  await expect(table.getByRole('columnheader', { name: 'Platform', exact: true })).toHaveCount(0)
})
