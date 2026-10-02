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
  const deployAction = ready.getByRole('link', { name: 'Deploy OS' })
  const monitorAction = changing.getByRole('link', { name: 'Monitor workflow' })
  await expect(deployAction).toHaveAttribute('href', /serverId=srv-1.*site=site-a|site=site-a.*serverId=srv-1/)
  await expect(monitorAction).toHaveAttribute('href', '/workflows/op-running?site=site-a')
  for (const action of [deployAction, monitorAction]) {
    expect(await action.locator('span').evaluate((label) => label.scrollWidth <= label.clientWidth)).toBe(true)
  }
  const identityOrder = await changing.locator('.sw-server-identity__copy > div').first().evaluate((line) => (
    Array.from(line.children).map((child) => child.getAttribute('aria-label') ?? child.textContent?.trim())
  ))
  expect(identityOrder.slice(0, 3)).toEqual(['Locked', 'RAM deployment', 'gpu-node-02'])
  await expect(changing.locator('.sw-server-col--power').getByLabel('RAM deployment', { exact: true })).toHaveCount(0)
  await expect(issue.getByRole('link', { name: 'Review server' })).toHaveAttribute('href', '/servers/srv-3/activity?site=site-a')
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

  await page.getByRole('button', { name: /Filters/ }).click()
  await page.getByRole('checkbox', { name: 'Include absent projections' }).locator('..').click()
  await expect(page).toHaveURL(/includeAbsent=true/)
  await page.keyboard.press('Escape')
  const absent = table.getByRole('row').filter({ hasText: 'gpu-node-04' })
  await expect(absent).toBeVisible()
  await expect(absent).toContainText('CPU only')
  await expect(absent.getByRole('link', { name: 'Review server' })).toHaveAttribute('href', '/servers/srv-4/activity?site=site-a')
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
    extraTags: { 'srv-1': ['east', 'training'] },
  })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/servers?site=site-a')

  const card = page.getByRole('article').filter({ hasText: 'gpu-node-01' })
  await expect(card).toContainText('8 × AMD MI300X')
  await expect(card).toContainText('64 cores · 512 GiB')
  await expect(card.getByText('Zone', { exact: true })).toBeVisible()
  await expect(card.getByText('rack-a', { exact: true }).first()).toBeVisible()
  await expect(card.getByText('Pool', { exact: true })).toBeVisible()
  await expect(card.getByText('accelerators', { exact: true }).first()).toBeVisible()
  await expect(card.getByRole('button', { name: 'Edit tags for gpu-node-01' })).toBeVisible()
  await card.locator('summary', { hasText: 'More details' }).click()
  await expect(card).toContainText('Operational context · Health')
  await expect(card).toContainText('platform-a')
  await expect(card).toContainText('SN0001')
  await expect(card).toContainText('east, gpu, production, training')

  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await card.getByLabel('Mobile selection: gpu-node-01').check()
  await expect(page.getByRole('region', { name: 'Selection actions' })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

  const name = card.getByRole('link', { name: 'gpu-node-01' })
  await name.focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL('/servers/srv-1/summary?site=site-a')
})

test('13-inch layout prioritizes core columns and reveals context when space permits', async ({ page }) => {
  await installApiFixtures(page, {
    failedServerIds: ['srv-1'],
    changingServerIds: ['srv-3'],
    multiGpuServerIds: ['srv-1'],
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
  await expect(table.getByRole('columnheader', { name: 'Signals', exact: true })).toHaveCount(0)
  for (const heading of ['Health', 'Platform']) {
    await expect(table.getByRole('columnheader', { name: heading, exact: true })).toBeHidden()
  }

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
  await expect(providerFailed.getByText('Failed', { exact: true })).toHaveCount(0)
  await expect(providerFailed.getByText('Not deployed', { exact: true })).toBeVisible()
  const hardwareGeometry = await providerFailed.locator('.sw-server-col--hardware').evaluate((cell) => {
    const content = cell.querySelector('strong')
    if (!(content instanceof HTMLElement)) throw new Error('Hardware summary is missing')
    return {
      cellRight: cell.getBoundingClientRect().right,
      contentRight: content.getBoundingClientRect().right,
      clientWidth: content.clientWidth,
      scrollWidth: content.scrollWidth,
    }
  })
  expect(hardwareGeometry.contentRight).toBeLessThanOrEqual(hardwareGeometry.cellRight)
  expect(hardwareGeometry.scrollWidth).toBeGreaterThan(hardwareGeometry.clientWidth)
  const changing = table.getByRole('row').filter({ hasText: 'gpu-node-03' }).first()
  await expect(changing.getByRole('link', { name: 'Monitor workflow' })).toBeVisible()
  const actionGeometry = await changing.locator('.sw-server-row-actions').evaluate((cell) => {
    const cellRight = cell.getBoundingClientRect().right
    const childRights = Array.from(cell.querySelectorAll(':scope > div > *')).map((child) => child.getBoundingClientRect().right)
    return { cellRight, childRight: Math.max(...childRights) }
  })
  expect(actionGeometry.childRight).toBeLessThanOrEqual(actionGeometry.cellRight)
  const deployed = table.getByRole('row').filter({ hasText: 'gpu-node-02' }).first()
  const deployedImage = deployed.getByText('Ubuntu 24.04 LTS', { exact: true })
  await expect(deployedImage).toBeVisible()
  await expect(deployedImage).not.toHaveAttribute('data-scope', 'badge')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

  await page.setViewportSize({ width: 1800, height: 900 })
  for (const heading of ['Health', 'Power', 'Platform']) {
    await expect(table.getByRole('columnheader', { name: heading, exact: true })).toBeVisible()
  }
})
