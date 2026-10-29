import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

const visibleAppearance = (page: import('playwright/test').Page) => page.locator('button[aria-label^="Appearance:"]:visible')
const visibleSiteScope = (page: import('playwright/test').Page) => page.locator('button[aria-label^="Site scope:"]:visible')

async function chooseMenuItem(page: import('playwright/test').Page, name: string) {
  await page.getByRole('menuitem', { name, exact: true }).click()
}

async function chooseSingleSelectOption(page: import('playwright/test').Page, fieldLabel: string, optionLabel: string) {
  await page.getByRole('combobox', { name: fieldLabel, exact: true }).click()
  await page.getByRole('option', { name: optionLabel, exact: true }).click()
}

async function chooseOSImage(page: import('playwright/test').Page, imageName: string) {
  const image = page.getByRole('radio', { name: imageName, exact: false })
  await expect(image).toBeVisible()
  await image.locator('..').click()
  await expect(image).toBeChecked()
}

/** Verifies that a shared Select paints its expanded menu on an opaque readable surface. */
async function expectExpandedPlaceholderReadable(
  page: import('playwright/test').Page,
  fieldLabel: string,
) {
  await page.getByRole('combobox', { name: fieldLabel, exact: true }).click()
  const listbox = page.getByRole('listbox')
  await expect(listbox).toBeVisible()
  const paint = await listbox.evaluate((element) => {
    const style = getComputedStyle(element)
    return { text: style.color, background: style.backgroundColor }
  })
  expect(paint.background).not.toBe('rgba(0, 0, 0, 0)')
  expect(paint.background).not.toBe('transparent')
  expect(paint.text).not.toBe(paint.background)
}

/** Checks that shared table cells retain vertically centered dense alignment. */
async function expectTableCellsVerticallyCentered(page: import('playwright/test').Page, tableName: string) {
  const table = page.locator(`table[aria-label="${tableName}"]`)
  await expect(table).toBeVisible()
  const cells = table.locator('thead th, tbody td')
  await expect(cells.first()).toBeVisible()
  expect(await cells.evaluateAll((items) => items.every((item) => getComputedStyle(item).verticalAlign === 'middle'))).toBe(true)
}


test.beforeEach(async ({ page }, testInfo) => {
  const authenticated = !testInfo.title.includes('login')
  await installApiFixtures(page, { signedIn: authenticated })
  const appearance = 'light'
  await page.addInitScript(({ appearance, authenticated }) => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', appearance)
    if (authenticated) localStorage.setItem('access_token', 'e2e-token')
    else localStorage.removeItem('access_token')
  }, { appearance, authenticated })
})

test.describe('operator interactions', () => {
  test('login layout stays aligned on desktop and mobile', async ({ page }) => {
    for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
      await page.setViewportSize(viewport)
      await page.goto('/login')

      await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
      await expect(page.getByLabel('Username')).toBeVisible()
      await expect(page.getByLabel('Password')).toBeVisible()
      await expect(page.getByTestId('login-panel')).toBeVisible()
      await expect(page.getByText('Infrastructure, clearly managed.')).toBeVisible({ visible: viewport.width >= 768 })

      const layout = await page.getByTestId('login-panel').evaluate((panel) => {
        const rect = panel.getBoundingClientRect()
        return {
          hasHorizontalOverflow: document.documentElement.scrollWidth > window.innerWidth,
          fitsViewport: rect.left >= 0 && rect.right <= window.innerWidth,
          width: rect.width,
        }
      })
      expect(layout.hasHorizontalOverflow).toBe(false)
      expect(layout.fitsViewport).toBe(true)
      expect(layout.width).toBeGreaterThan(300)
    }
  })
  test('site scope is URL-owned and detail switching returns to the list', async ({ page }) => {
    await page.goto('/')
    await visibleSiteScope(page).click()
    await chooseMenuItem(page, 'Taipei Lab')
    await expect(page).toHaveURL(/\?site=site-a$/)
    await page.getByText('1 workflow failed today').click()
    await expect(page).toHaveURL('/workflows?status=failed&site=site-a')

    await page.goto('/servers/srv-1/summary?site=site-a')
    await visibleSiteScope(page).click()
    await chooseMenuItem(page, 'Hsinchu Edge')
    await expect(page).toHaveURL('/servers?site=site-b')

    await page.goto('/servers?site=missing')
    await expect(page).toHaveURL('/servers')
    await expect(page.getByText('Unknown Site')).toBeVisible()
  })

  test('Chakra theme resolves system, manual modes, surface color, and persistence', async ({ page }) => {
    await page.goto('/')
    const lightSurface = await page.evaluate(() => getComputedStyle(document.body).backgroundColor)
    await expect(page.locator('html')).not.toHaveClass(/dark/)

    await visibleAppearance(page).click()
    await chooseMenuItem(page, 'Dark')
    await expect(page.locator('html')).toHaveClass(/dark/)
    expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).not.toBe(lightSurface)
    await page.reload()
    await expect(page.locator('html')).toHaveClass(/dark/)
    await expect.poll(() => page.evaluate(() => localStorage.getItem('swallow.appearance'))).toBe('dark')

    await visibleAppearance(page).click()
    await chooseMenuItem(page, 'System')
    await page.emulateMedia({ colorScheme: 'light' })
    await expect(page.locator('html')).not.toHaveClass(/dark/)
    await page.emulateMedia({ colorScheme: 'dark' })
    await expect(page.locator('html')).toHaveClass(/dark/)
    await visibleAppearance(page).click()
    await chooseMenuItem(page, 'Light')
    await expect(page.locator('html')).not.toHaveClass(/dark/)
  })
  test('Deploy Platform renders its expanded Site placeholder in dark mode', async ({ page }) => {
    await page.goto('/platforms/deploy/kubernetes')
    await visibleAppearance(page).click()
    await chooseMenuItem(page, 'Dark')

    const site = page.getByLabel('Site', { exact: true })
    await expect(site).toContainText('Select a Site')
    await expect(page.locator('html')).toHaveCSS('color-scheme', 'dark')
    await expectExpandedPlaceholderReadable(page, 'Site')
    await expect(page.getByRole('option', { name: 'Taipei Lab', exact: true })).toBeVisible()
    await page.getByRole('option', { name: 'Taipei Lab', exact: true }).click()

    await expect(page.getByLabel('Site', { exact: true })).toContainText('Taipei Lab')
    await expect(page.getByLabel('Platform name')).toBeVisible()
  })

  test('shared tables use semantic labels and responsive spacing', async ({ page }) => {
    for (const [route, tableName] of [
      ['/', 'Recent workflows'],
      ['/servers?site=site-a', 'Servers'],
      ['/platforms?site=site-a', 'Platforms'],
      ['/workflows?site=site-a', 'Workflows'],
      ['/infrastructure/sites?site=site-a', 'Sites'],
      ['/infrastructure/integrations?site=site-a', 'Integrations'],
    ]) {
      await page.goto(route)
      await expectTableCellsVerticallyCentered(page, tableName)
    }

    await page.goto('/monitoring?site=site-a')
    await expect(page.getByRole('list', { name: 'Monitoring alerts' }).getByRole('listitem').first()).toBeVisible()

    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/servers?site=site-a')
    await expect(page.locator('.sw-resource-card').first()).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  })
  test('shared surfaces, links, and focus states use the modern system', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('.sw-metric-card').first()).toHaveCSS('border-radius', '18px')
    await expect(page.locator('.sw-section').first()).toHaveCSS('border-radius', '18px')

    const link = page.getByRole('link', { name: 'View all' })
    await expect(link).toHaveCSS('text-decoration-line', 'none')
    await link.focus()
    await expect(link).toBeFocused()
    await expect(link).toHaveCSS('outline-style', 'solid')

    await page.goto('/servers?site=site-a')
    await expect(page.locator('.sw-server-inventory')).toBeVisible()

    for (const route of ['/provisioning/templates?site=site-a', '/infrastructure/sites?site=site-a', '/infrastructure/integrations?site=site-a']) {
      await page.goto(route)
      await expect(page.locator('.sw-data-toolbar').first()).toHaveClass(/sw-data-toolbar--plain/)
    }
    await page.goto('/provisioning/images?site=site-a')
    await expect(page.locator('.sw-os-image-toolbar-layout')).toBeVisible()

    await page.goto('/workflows?site=site-a')
    await expect(page.locator('.sw-workflow-inventory')).toBeVisible()
  })
  test('Monitoring is alert-first and charts usage in swallow', async ({ page }) => {
    await page.goto('/monitoring?site=site-a')
    const alerts = page.getByRole('heading', { name: 'Alerts', exact: true })
    const charts = page.getByRole('heading', { name: 'Usage charts', exact: true })
    const metrics = page.getByRole('heading', { name: 'Server metrics', exact: true })
    await expect(alerts).toBeVisible()
    await expect(charts).toBeVisible()
    await expect(metrics).toBeVisible()
    await expect(page.getByText('Current values per Server. Tick Servers to chart their usage together above.')).toBeVisible()
    const alertBox = await alerts.boundingBox()
    const chartBox = await charts.boundingBox()
    const metricBox = await metrics.boundingBox()
    expect(alertBox?.y ?? 0).toBeLessThan(chartBox?.y ?? 0)
    expect(chartBox?.y ?? 0).toBeLessThan(metricBox?.y ?? 0)
    // Nothing is charted until a scope is chosen, and Grafana is a secondary escape hatch.
    await expect(page.getByText('Nothing to chart yet')).toBeVisible()
    await expect(page.getByRole('link', { name: 'Explore in Grafana' })).toBeVisible()
  })
  test('desktop navigation collapses to icons with tooltip and persists', async ({ page }) => {
    await page.goto('/')
    const rail = page.getByTestId('desktop-navigation')
    const toggle = page.getByRole('button', { name: 'Toggle navigation' })
    await expect.poll(() => rail.evaluate((element) => Math.round(element.getBoundingClientRect().width))).toBe(208)
    await expect(rail.getByRole('navigation', { name: 'Primary navigation' })).toBeVisible()
    await toggle.click()
    await expect.poll(() => rail.evaluate((element) => Math.round(element.getBoundingClientRect().width))).toBe(64)
    await expect.poll(() => page.evaluate(() => localStorage.getItem('swallow.shell.sidebar-collapsed'))).toBe('true')
    await rail.getByRole('link', { name: 'Servers', exact: true }).hover()
    await expect(page.getByRole('tooltip', { name: 'Servers' })).toBeVisible()
    await page.reload()
    await expect.poll(() => rail.evaluate((element) => Math.round(element.getBoundingClientRect().width))).toBe(64)
  })
  test('authenticated workspace fills available width without page overflow', async ({ page }) => {
    await page.setViewportSize({ width: 1920, height: 900 })
    await page.goto('/')
    const rail = page.getByTestId('desktop-navigation')
    const main = page.locator('#swallow-main-content')
    const measure = () => page.evaluate(() => {
      const rail = document.querySelector<HTMLElement>('[data-testid="desktop-navigation"]')
      const main = document.querySelector<HTMLElement>('#swallow-main-content')
      const page = document.querySelector<HTMLElement>('.operator-page')
      if (!rail || !main || !page) return null
      return {
        railRight: Math.round(rail.getBoundingClientRect().right),
        mainLeft: Math.round(main.getBoundingClientRect().left),
        pageWidth: Math.round(page.getBoundingClientRect().width),
        overflow: document.documentElement.scrollWidth > window.innerWidth,
      }
    })
    await expect(main).toBeVisible()
    const expanded = await measure()
    expect(expanded?.mainLeft).toBe(expanded?.railRight)
    expect(expanded?.overflow).toBe(false)
    await page.getByRole('button', { name: 'Toggle navigation' }).click()
    await expect.poll(() => rail.evaluate((element) => Math.round(element.getBoundingClientRect().width))).toBe(64)
    const collapsed = await measure()
    expect(collapsed?.mainLeft).toBe(collapsed?.railRight)
    expect(collapsed?.pageWidth ?? 0).toBeGreaterThan(expanded?.pageWidth ?? 0)
    expect(collapsed?.overflow).toBe(false)
  })
  test('mobile drawer traps focus, closes with Escape, and restores the toggle', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/')
    const toggle = page.getByRole('button', { name: 'Open navigation' })
    await expect(page.getByTestId('operator-header').getByText('Swallow', { exact: true })).toBeVisible()
    await toggle.click()
    const navigation = page.getByRole('navigation', { name: 'Primary navigation' })
    await expect(navigation).toBeVisible()
    await expect.poll(() => navigation.evaluate((element) => element.contains(document.activeElement))).toBe(true)
    await page.keyboard.press('Escape')
    await expect(navigation).toBeHidden()
    await expect(toggle).toBeFocused()
  })
  test('multi-select action docks do not reflow their tables', async ({ page }) => {
    const cases = [
      { path: '/servers?site=site-a', table: 'Servers', selectAll: 'Select all on this page' },
      { path: '/provisioning/images?site=site-a', table: 'OS images', selectAll: 'Select all on this page' },
      { path: '/platforms?site=site-a', table: 'Platforms', selectAll: 'Select all platforms' },
    ]

    for (const entry of cases) {
      await page.goto(entry.path)
      const table = page.getByRole('table', { name: entry.table })
      await expect(table).toBeVisible()
      await expect.poll(() => table.boundingBox()).not.toBeNull()
      const before = await table.boundingBox()
      expect(before).not.toBeNull()

      await page.getByLabel(entry.selectAll, { exact: true }).click()
      const actionDock = page.getByRole('region', { name: 'Selection actions' })
      await expect(actionDock).toBeVisible()
      await expect(actionDock).toHaveCSS('position', 'fixed')
      await expect(actionDock.getByRole('status')).toContainText(/\d+ selected/)
      const after = await table.boundingBox()
      const dockBox = await actionDock.boundingBox()
      const mainBox = await page.locator('#swallow-main-content').boundingBox()
      expect(after).not.toBeNull()
      expect(dockBox).not.toBeNull()
      expect(mainBox).not.toBeNull()
      expect(Math.abs((after?.y ?? 0) - (before?.y ?? 0))).toBeLessThan(1)
      expect(Math.abs(
        ((dockBox?.x ?? 0) + (dockBox?.width ?? 0) / 2)
          - ((mainBox?.x ?? 0) + (mainBox?.width ?? 0) / 2),
      )).toBeLessThan(1)

      await actionDock.getByRole('button', { name: 'Clear', exact: true }).click()
      await expect(actionDock).toHaveCount(0)
    }

    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/servers?site=site-a')
    await page.getByLabel('Mobile selection: gpu-node-01', { exact: true }).click()
    const mobileDock = page.getByRole('region', { name: 'Selection actions' })
    await expect(mobileDock).toBeVisible()
    await expect(mobileDock.getByRole('button', { name: 'Deploy OS', exact: true })).toBeInViewport()
    await expect(mobileDock).toHaveCSS('opacity', '1')
    const mobileBox = await mobileDock.boundingBox()
    expect(mobileBox).not.toBeNull()
    expect(mobileBox?.x ?? -1).toBeGreaterThanOrEqual(0)
    expect((mobileBox?.x ?? 390) + (mobileBox?.width ?? 1)).toBeLessThanOrEqual(390)
    expect(mobileBox?.height ?? 844).toBeLessThanOrEqual(72)
  })

  test('Server fleet keeps discovery summaries concise and exposes full hardware details', async ({ page }) => {
    await page.goto('/servers?site=site-a')
    const table = page.getByRole('table', { name: 'Servers' })
    for (const heading of ['Server', 'Network', 'Deployment', 'Hardware', 'Zone', 'Pool']) {
      await expect(table.getByRole('columnheader', { name: heading, exact: true })).toBeVisible()
    }
    await expect(table.getByRole('columnheader', { name: 'Power', exact: true })).toHaveCount(0)
    await expect(table.getByRole('columnheader', { name: 'Signals', exact: true })).toHaveCount(0)
    await expect(table.getByRole('columnheader', { name: 'MAC address' })).toHaveCount(0)

    const firstRow = table.getByRole('row').filter({ hasText: 'gpu-node-01' }).first()
    await expect(firstRow).toContainText('AMD MI300X')
    await expect(firstRow).toContainText('64 cores')
    await expect(firstRow).toContainText('RAM 512 GiB')
    await expect(firstRow.getByText('rack-a', { exact: true })).toBeVisible()
    await expect(firstRow.getByText('accelerators', { exact: true })).toBeVisible()
    const identity = firstRow.locator('.sw-server-col--identity')
    await expect(identity.getByText('gpu', { exact: true })).toBeVisible()
    await expect(identity.getByText('production', { exact: true })).toBeVisible()
    await expect(firstRow.getByRole('link', { name: 'gpu-node-01' })).toHaveAttribute('href', '/servers/srv-1/summary?site=site-a')
    await expect(firstRow).toContainText('192.168.40.21')
    await expect(firstRow).toContainText('02:00:00:00:00:01')
    await expect(firstRow).not.toContainText('gpu-node-01.lab.example')
    await expect(firstRow.getByText('Ubuntu 24.04 LTS', { exact: true })).toBeVisible()
    await page.setViewportSize({ width: 1800, height: 900 })
    await expect(table.getByRole('columnheader', { name: 'Health', exact: true })).toBeVisible()

    await firstRow.getByRole('button', { name: 'Show details for gpu-node-01' }).click()
    const details = table.getByRole('row').filter({ hasText: 'SN0001' })
    await expect(details).toContainText('02:00:00:00:00:01')
    await expect(details).toContainText('AMD EPYC 9554')
    await expect(details).toContainText('3840 GB')

    await page.getByLabel('Select all on this page').click()
    await expect(page.getByText('4 selected')).toBeVisible()
    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: 'Power', exact: true }).hover()
    await expect(page.getByRole('menuitem', { name: 'Power on' })).toBeVisible()
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: 'Clear', exact: true }).click()
  })

  test('legacy hidden-column preference no longer changes the canonical inventory', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('swallow.servers.hidden-columns', JSON.stringify(['placement', 'hardware'])))
    await page.goto('/servers?site=site-a')
    const table = page.getByRole('table', { name: 'Servers' })
    await expect(table.getByRole('columnheader', { name: 'Hardware', exact: true })).toBeVisible()
    await expect(table.getByRole('columnheader', { name: 'Zone', exact: true })).toBeVisible()
    await expect(table.getByRole('columnheader', { name: 'Pool', exact: true })).toBeVisible()
    await expect(page.getByLabel('Configure columns')).toHaveCount(0)
  })

  test('Cockpit machine detail retains tabs and real action controls', async ({ page }) => {
    await page.goto('/servers/srv-1/summary?site=site-a')
    await expect(page.getByText('Current state', { exact: true })).toBeVisible()
    await expect(page.getByText('Hardware profile', { exact: true })).toBeVisible()
    const currentState = page.getByRole('heading', { name: 'Current state' }).locator('xpath=../../..')
    const power = currentState.locator('dl > div').filter({ hasText: 'Power' }).first()
    await expect(power).toContainText('Powered on')
    await expect(power.locator('svg')).toBeVisible()
    const deployedOS = currentState.getByRole('link', { name: 'Ubuntu 24.04 LTS' })
    await expect(deployedOS).toHaveAttribute('href', /\/provisioning\/images\?.*integrationId=maas-a/)
    const management = page.getByRole('heading', { name: 'Management controller' }).locator('xpath=../../..')
    await expect(management).toContainText('IPMI')
    await expect(management).toContainText('192.0.2.20')
    await expect(management).toContainText('bmc-admin')
    await expect(management).toContainText('LAN_2_0')
    await expect(management).toContainText('OPERATOR')
    await expect(management.getByRole('button', { name: 'Copy address' })).toBeVisible()
    await expect(management.getByRole('button', { name: 'Copy username' })).toBeVisible()
    await expect(management.getByText('bmc-secret', { exact: true })).toHaveCount(0)
    await expect(management.getByText('••••••••', { exact: true })).toBeVisible()
    await expect(management.getByRole('button', { name: 'Copy password' })).toBeVisible()
    await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async (value: string) => {
          (window as unknown as { __e2eCopiedText?: string }).__e2eCopiedText = value
        },
      },
    }))
    await management.getByRole('button', { name: 'Copy password' }).click()
    await expect.poll(() => page.evaluate(
      () => (window as unknown as { __e2eCopiedText?: string }).__e2eCopiedText,
    )).toBe('bmc-secret')
    await management.getByRole('button', { name: 'Show password' }).click()
    await expect(management.getByText('bmc-secret', { exact: true })).toBeVisible()
    await management.getByRole('button', { name: 'Hide password' }).click()
    await expect(management.getByText('bmc-secret', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Identity and inspection' })).toHaveCount(0)
    const summaryTab = page.getByRole('tab', { name: 'Summary' })
    await expect(summaryTab.locator('svg')).toBeVisible()
    const tabList = page.getByRole('tablist')
    await expect(tabList).toHaveCSS('scrollbar-width', 'none')
    await page.setViewportSize({ width: 390, height: 844 })
    expect(await tabList.evaluate((element) => element.scrollWidth > element.clientWidth)).toBe(true)
    await page.setViewportSize({ width: 1440, height: 900 })
    await expect(page.getByRole('button', { name: 'Edit', exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Take action' }).click()
    for (const category of ['Power', 'Hardware checks', 'State & recovery']) {
      await expect(page.getByRole('menuitem', { name: category, exact: true })).toBeVisible()
    }
    await expect(page.getByRole('menuitem', { name: /^Inspect hardware/ })).toHaveCount(0)
    await page.getByRole('menuitem', { name: 'Hardware checks', exact: true }).hover()
    await expect(page.getByRole('menuitem', { name: /^Inspect hardware/ })).toBeVisible()
    await expect(page.getByRole('menuitem', { name: /^Commission/ })).toHaveCount(0)
    await page.keyboard.press('Escape')
    await page.getByRole('tab', { name: 'Monitoring' }).click()
    await expect(page.getByRole('heading', { name: 'Current metrics' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Usage history' })).toBeVisible()
    await page.getByRole('tab', { name: 'Networking' }).click()
    await expect(page).toHaveURL('/servers/srv-1/network?site=site-a')
    await expect(page.getByText('eno1')).toBeVisible()
    await page.getByRole('tab', { name: 'PCI devices' }).click()
    await expect(page.getByText('03:00.0')).toBeVisible()

    await page.goto('/servers/srv-1/summary?site=site-a')
    await page.getByRole('link', { name: 'Ubuntu 24.04 LTS' }).click()
    await expect.poll(() => new URL(page.url()).searchParams.get('imageName')).toBe('Ubuntu 24.04 LTS')
    await expect(page.getByRole('textbox', { name: 'Search OS images' })).toHaveValue('')
    const imageTable = page.getByRole('table', { name: 'OS images' })
    await expect(imageTable.getByRole('row', { name: 'Ubuntu 24.04 LTS catalog data', exact: true })).toHaveCount(1)
    await expect(imageTable.getByRole('row', { name: 'Ubuntu 22.04 LTS catalog data', exact: true })).toHaveCount(0)
  })

  test('Server headline reports failed verification while retaining provider OS detail', async ({ page }) => {
    await page.goto('/servers?site=site-a')
    const row = page.getByRole('row').filter({ hasText: 'gpu-node-04' })
    await expect(row.getByText('Failed', { exact: true })).toBeVisible()
    await expect(row.getByText('Deployment unverified', { exact: true })).toHaveCount(0)

    await page.goto('/servers/srv-4/summary?site=site-a')
    await expect(page.getByText('Operating system deployment failed', { exact: true })).toBeVisible()
    // The concise, code-keyed root cause is shown; the verbose executor reason is tucked
    // behind "Show details" instead of dominating the page.
    await expect(page.getByText('The server did not obtain a network address (DHCP) after the OS was installed.')).toBeVisible()
    await expect(page.getByText('No provider address was observed after OS installation.')).not.toBeVisible()
    await page.getByRole('button', { name: 'Show details' }).click()
    await expect(page.getByText('No provider address was observed after OS installation.')).toBeVisible()
    const statusCard = page.getByRole('heading', { name: 'Current state' }).locator('xpath=../../..')
    await expect(statusCard.getByText('deployed', { exact: true }).first()).toBeVisible()
    await expect(statusCard.getByText('Power', { exact: true })).toBeVisible()
    await expect(statusCard.getByText('Deployed OS', { exact: true })).toBeVisible()
    // The installed image is still the catalog image, so it stays a link after a failed verification.
    await expect(statusCard.getByRole('link', { name: 'Ubuntu 24.04 LTS' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'View operation' })).toBeVisible()
  })

  // lab-afde-mi308-1 lost its Deployed OS link when a canceled platform deployment rewrote its
  // result, and Servers installed outside Swallow never had one. The link follows the installed
  // catalog image, not Swallow's deployment result or who deployed the OS.
  test('Deployed OS links to the catalog image whatever the deployment result', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, { existingServerIds: ['srv-2'] })
    for (const serverId of ['srv-2', 'srv-4']) {
      await page.goto(`/servers/${serverId}/summary?site=site-a`)
      const statusCard = page.getByRole('heading', { name: 'Current state' }).locator('xpath=../../..')
      await expect(statusCard.getByRole('link', { name: 'Ubuntu 24.04 LTS' }))
        .toHaveAttribute('href', /\/provisioning\/images\?.*integrationId=maas-a.*imageName=Ubuntu/)
    }
  })

  test('Server Network separates physical state from Swallow configuration actions', async ({ page }) => {
    const requests: Array<{ method: string; linkId: string | null; body: Record<string, unknown> | null }> = []
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, {
      readyServerCount: 1,
      onNetworkLinkRequest: (method, _serverId, _interfaceId, linkId, body) => {
        requests.push({ method, linkId, body })
      },
    })
    await page.goto('/servers/srv-1/network?site=site-a')

    const table = page.getByRole('table', { name: 'Server network interfaces' })
    const row = table.getByRole('row', { name: /eno1/ })
    await expect(row).toContainText('Provider-managed (MAAS AUTO)')
    await expect(row.getByRole('cell', { name: 'up' })).toBeVisible()
    await row.getByRole('button', { name: 'Configure' }).click()

    const editor = page.getByRole('dialog', { name: 'Configure eno1' })
    await expect(editor.getByRole('radio', { name: 'DHCP' })).toBeVisible()
    await expect(editor.getByRole('radio', { name: 'Static' })).toBeVisible()
    await expect(editor.getByRole('radio', { name: 'Link only' })).toBeVisible()
    await expect(editor.getByText(/Keep current|AUTO/)).toHaveCount(0)
    await editor.getByRole('radio', { name: 'Static' }).locator('..').click()
    await editor.getByLabel('IPv4 address').fill('192.168.40.90')
    await editor.getByRole('button', { name: 'Save configuration' }).click()
    expect(requests[0]).toEqual({ method: 'PUT', linkId: 'link-srv-1', body: { mode: 'static', subnetId: 'subnet-a', ipAddress: '192.168.40.90', defaultGateway: true } })
    await expect(row).toContainText('STATIC')

    await row.getByRole('button', { name: 'Unbind' }).click()
    await page.getByRole('dialog', { name: 'Unbind network link' }).getByRole('button', { name: 'Unbind' }).click()
    expect(requests[1]).toEqual({ method: 'DELETE', linkId: 'link-srv-1', body: null })
    await expect(row).toContainText('Unconfigured')
  })

  test('provider-backed server deletion requires typed confirmation', async ({ page }) => {
    // A Server no Platform claims; a claimed one cannot be deleted (server-platform-claim.spec.ts).
    await installApiFixtures(page, { withoutPlatformClaims: true })
    await page.goto('/servers/srv-1/summary?site=site-a')
    await page.getByRole('button', { name: 'Take action' }).click()
    await chooseMenuItem(page, 'Delete only')

    const dialog = page.getByRole('alertdialog', { name: 'Delete server' })
    await expect(dialog).toContainText('permanently delete the Machine')
    const submit = dialog.getByRole('button', { name: 'Delete server' })
    await expect(submit).toBeDisabled()
    await dialog.getByLabel('Server name confirmation').fill('gpu-node-01')
    await submit.click()

    await expect(page).toHaveURL('/servers?site=site-a')
    await expect(page.getByRole('table', { name: 'Servers' }).getByText('gpu-node-01', { exact: true })).toHaveCount(0)
  })

  test('Platform wizard explains and excludes Servers already claimed by Platforms', async ({ page }) => {
    await page.goto('/platforms/deploy/kubernetes?site=site-a')
    await page.getByLabel('Platform name').fill('protected-targets')
    await page.getByRole('button', { name: 'Next' }).click()

    await expect(page.getByText('Some Servers are already assigned')).toBeVisible()
    const table = page.getByRole('table', { name: 'Deployable Servers' })
    const active = table.getByRole('row', { name: /gpu-node-01/ })
    await expect(active).toContainText('In use')
    await expect(active).toContainText('production-k0s')
    await expect(active).toContainText('Active')
    await expect(active.getByRole('combobox', {
      name: /unavailable because it is assigned to production-k0s/,
    })).toBeDisabled()

    const failed = table.getByRole('row', { name: /gpu-node-04/ })
    await expect(failed).toContainText('edge-staging')
    await expect(failed).toContainText('Deployment failed')
    await expect(failed.getByRole('combobox', {
      name: /unavailable because it is assigned to edge-staging/,
    })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Next' })).toBeDisabled()
  })

  test('Chakra wizard selects machines before networking and keeps Platform context', async ({ page }) => {
    await installApiFixtures(page, { freePlatformCandidates: true })
    await page.goto('/platforms/deploy/kubernetes?site=site-a')
    const next = page.getByRole('button', { name: 'Next' })
    await expect(next).toBeDisabled()
    await page.getByLabel('Platform name').fill('compute-k0s')
    await next.click()
    await expect(page.getByRole('heading', { name: 'Topology and machines' })).toBeVisible()
    for (const server of ['gpu-node-01', 'gpu-node-02', 'gpu-node-03']) {
      await chooseSingleSelectOption(page, `Role for ${server}`, 'Control-plane')
    }
    await chooseSingleSelectOption(page, 'Role for gpu-node-04', 'Worker')
    await next.click()
    await expect(page.getByRole('heading', { name: 'Kubernetes features' })).toBeVisible()
    await next.click()
    await expect(page.getByRole('heading', { name: 'Selected machine addresses', exact: true })).toBeVisible()
    await page.getByLabel('API virtual IP').fill('192.168.40.200')
    await next.click()
    await expect(page.getByText('3 control-plane, 1 workload-capable')).toBeVisible()
    await page.getByRole('button', { name: 'Back' }).click()
    await expect(page.getByLabel('API virtual IP')).toHaveValue('192.168.40.200')
    await next.click()
    await page.getByRole('button', { name: 'Deploy Kubernetes Platform' }).click()
    await expect(page).toHaveURL('/platforms/platform-new?site=site-a')
    await expect(page.getByText('Platform deployment is running')).toBeVisible()
    await expect(page.getByRole('button', { name: 'View automation details' })).toBeVisible()
  })

  test('Platform wizard deploys a Slurm platform with per-daemon node roles', async ({ page }) => {
    let deployBody: Record<string, unknown> | undefined
    await installApiFixtures(page, {
      freePlatformCandidates: true,
      onPlatformDeploymentRequest: (body) => { deployBody = body },
    })
    await page.goto('/platforms/deploy/slurm?site=site-a')
    await page.getByLabel('Platform name').fill('research-slurm-2')
    // Slurm swaps the k0s version field for a cluster name.
    await expect(page.getByLabel('k0s version')).toHaveCount(0)
    await page.getByText('Advanced settings', { exact: true }).click()
    await expect(page.getByLabel('Cluster name')).toBeVisible()
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()

    await expect(page.getByRole('heading', { name: 'Nodes and daemons' })).toBeVisible()
    await page.getByRole('checkbox', { name: 'Run slurmctld on gpu-node-01' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-01' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-02' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-03' }).locator('..').click()
    await expect(page.getByText('1 controller', { exact: true })).toBeVisible()
    await next.click()

    // Optional features follow the machine choice: the container runtime goes to the compute and
    // login nodes, and the GPU monitoring stack's Prometheus and Grafana run on the primary
    // controller just picked. The Device Metrics Exporter is the default stack; Omnistat is chosen.
    await expect(page.getByRole('heading', { name: 'Slurm features' })).toBeVisible()
    // The Container runtime card starts with native OCI only; this run uses Podman alone.
    await page.getByRole('switch', { name: 'Enable Container runtime' }).locator('xpath=following-sibling::*[1]').click()
    await expect(page.getByRole('checkbox', { name: /Native OCI/ })).toBeChecked()
    await expect(page.getByRole('checkbox', { name: /Docker CE/ })).not.toBeChecked()
    await expect(page.getByRole('checkbox', { name: /Podman/ })).not.toBeChecked()
    await page.getByRole('checkbox', { name: /Podman/ }).locator('xpath=following-sibling::*[1]').click()
    await page.getByRole('checkbox', { name: /Native OCI/ }).locator('xpath=following-sibling::*[1]').click()
    await expect(page.getByRole('checkbox', { name: /Native OCI/ })).not.toBeChecked()
    await expect(page.getByRole('checkbox', { name: /Podman/ })).toBeChecked()
    await page.getByRole('switch', { name: 'Enable GPU monitoring' }).locator('xpath=following-sibling::*[1]').click()
    await expect(page.getByRole('radio', { name: /AMD Device Metrics Exporter/ })).toBeChecked()
    await page.getByRole('radio', { name: /Omnistat/ }).locator('xpath=following-sibling::*[1]').click()
    await expect(page.getByText('Each compute node runs one job at a time so Omnistat metrics map to a single job.')).toBeVisible()
    await expect(page.getByText('Run both as containers on gpu-node-01.')).toBeVisible()
    await page.getByRole('radio', { name: /Native/ }).locator('xpath=following-sibling::*[1]').click()
    await expect(page.getByText(/official Prometheus release and Grafana package repository/)).toBeVisible()

    // GPU settings are optional overrides: a custom GRES type and driver options are validated
    // with the API grammar before the step can be left.
    await page.getByText('Advanced GPU settings', { exact: true }).click()
    await page.getByRole('radio', { name: /Custom name/ }).locator('xpath=following-sibling::*[1]').click()
    await expect(next).toBeDisabled()
    await page.getByLabel('GRES type name').fill('mi308x-vf')
    await expect(page.getByText(/Use letters, digits, and underscores, starting with a letter/)).toBeVisible()
    await expect(next).toBeDisabled()
    await page.getByLabel('GRES type name').fill('mi308x_vf')
    await expect(page.getByText('Jobs request it as --gres=gpu:mi308x_vf:N.', { exact: false })).toBeVisible()
    await page.getByLabel('GPU driver module options').fill('pcie_p2p=0\nPCIe=1')
    await expect(page.getByText(/Line 2: use name=value with a lower-case name/)).toBeVisible()
    await expect(next).toBeDisabled()
    await page.getByLabel('GPU driver module options').fill('pcie_p2p=0')
    await next.click()

    // Slurm has no platform networking step, so Features is followed by Review.
    await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await expect(page.getByText('Controllers')).toBeVisible()
    await expect(page.getByText('Podman on compute and login nodes')).toBeVisible()
    await expect(page.getByText('Omnistat; Prometheus and Grafana on gpu-node-01 as native packages')).toBeVisible()
    await expect(page.getByText('Included automatically for AMD GPU compute nodes as type mi308x_vf')).toBeVisible()
    await expect(page.getByText('pcie_p2p=0', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Deploy Slurm Platform' }).click()
    await expect(page).toHaveURL('/platforms/platform-new?site=site-a')
    await expect(page.getByText('Platform deployment is running')).toBeVisible()
    expect(deployBody?.containerRuntime).toEqual({ kind: 'podman' })
    expect((deployBody?.slurm as { ociContainers?: boolean } | undefined)?.ociContainers).toBeUndefined()
    expect(deployBody?.omnistat).toEqual({ enabled: true, installMethod: 'native' })
    expect(deployBody?.deviceMetricsExporter).toBeUndefined()
    expect(deployBody?.gpuDriver).toEqual({ moduleOptions: { pcie_p2p: '0' } })
    expect((deployBody?.slurm as { gpuGresType?: string } | undefined)?.gpuGresType).toBe('mi308x_vf')
    await expect(page.getByLabel('Omnistat')).toContainText('Installing')
    await expect(page.getByLabel('Omnistat')).toContainText('installed natively')
    await expect(page.getByText('GPU GRES type', { exact: true })).toBeVisible()
    await expect(page.getByText('mi308x_vf', { exact: true })).toBeVisible()
    await expect(page.getByText('GPU driver options', { exact: true })).toBeVisible()
  })

  test('Platform wizard deploys Slurm with the Device Metrics Exporter, OCI containers, and PMIx', async ({ page }) => {
    let deployBody: Record<string, unknown> | undefined
    await installApiFixtures(page, {
      freePlatformCandidates: true,
      onPlatformDeploymentRequest: (body) => { deployBody = body },
    })
    await page.goto('/platforms/deploy/slurm?site=site-a')
    await page.getByLabel('Platform name').fill('dme-slurm')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()
    await page.getByRole('checkbox', { name: 'Run slurmctld on gpu-node-01' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-02' }).locator('..').click()
    await next.click()

    // The exporter is the default monitoring stack; it and Omnistat are one choice, never both.
    await page.getByRole('switch', { name: 'Enable GPU monitoring' }).locator('xpath=following-sibling::*[1]').click()
    await expect(page.getByRole('radio', { name: /AMD Device Metrics Exporter/ })).toBeChecked()
    await expect(page.getByText(/Platform reports the GPU identity as collapsed/)).toBeVisible()
    // Native OCI and a container engine are independent choices of one card, and a card with nothing
    // chosen blocks the step.
    // Choosing Podman replaces Docker CE: the engines are at most one.
    await page.getByRole('switch', { name: 'Enable Container runtime' }).locator('xpath=following-sibling::*[1]').click()
    await page.getByRole('checkbox', { name: /Docker CE/ }).locator('xpath=following-sibling::*[1]').click()
    await page.getByRole('checkbox', { name: /Podman/ }).locator('xpath=following-sibling::*[1]').click()
    await expect(page.getByRole('checkbox', { name: /Docker CE/ })).not.toBeChecked()
    await expect(page.getByRole('checkbox', { name: /Podman/ })).toBeChecked()
    await page.getByRole('checkbox', { name: /Podman/ }).locator('xpath=following-sibling::*[1]').click()
    await page.getByRole('checkbox', { name: /Native OCI/ }).locator('xpath=following-sibling::*[1]').click()
    await expect(page.getByText('Nothing selected')).toBeVisible()
    await expect(next).toBeDisabled()
    await page.getByRole('checkbox', { name: /Native OCI/ }).locator('xpath=following-sibling::*[1]').click()
    await page.getByRole('checkbox', { name: /Docker CE/ }).locator('xpath=following-sibling::*[1]').click()
    await page.getByRole('switch', { name: 'Enable MPI with PMIx' }).locator('xpath=following-sibling::*[1]').click()
    await next.click()

    await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await expect(page.getByText('AMD Device Metrics Exporter; Prometheus and Grafana on gpu-node-01 as containers')).toBeVisible()
    await expect(page.getByText('crun with srun --container on compute nodes')).toBeVisible()
    await expect(page.getByText('Docker CE on compute and login nodes')).toBeVisible()
    await expect(page.getByText('OpenMPI with srun --mpi=pmix_v5')).toBeVisible()
    await page.getByRole('button', { name: 'Deploy Slurm Platform' }).click()
    await expect(page).toHaveURL('/platforms/platform-new?site=site-a')
    expect(deployBody?.containerRuntime).toEqual({ kind: 'docker-ce' })
    expect(deployBody?.deviceMetricsExporter).toEqual({ enabled: true, installMethod: 'container' })
    expect(deployBody?.omnistat).toBeUndefined()
    const slurm = deployBody?.slurm as { ociContainers?: boolean; pmix?: boolean } | undefined
    expect(slurm?.ociContainers).toBe(true)
    expect(slurm?.pmix).toBe(true)
    await expect(page.getByLabel('Device Metrics Exporter')).toContainText('Installing')
    await expect(page.getByText('Native OCI (srun --container); Docker CE on compute and login nodes', { exact: true })).toBeVisible()
  })

  test('A Slurm Platform states a collapsed Device Metrics Exporter GPU identity', async ({ page }) => {
    await installApiFixtures(page, { slurmDeviceMetricsExporter: true })
    await page.goto('/platforms/platform-slurm?site=site-a')
    const exporter = page.getByLabel('Device Metrics Exporter')
    await expect(exporter).toContainText('Device Metrics Exporter 1.5.3')
    await expect(exporter).toContainText('Ready')
    await expect(exporter).toContainText('installed natively')
    await expect(exporter.getByText('GPU identity collapsed')).toBeVisible()
    await expect(page.getByRole('link', { name: 'Device Metrics Exporter Grafana' })).toHaveAttribute('href', 'https://dme.example/grafana')
    await expect(page.getByText('OpenMPI with srun --mpi=pmix_v5', { exact: true })).toBeVisible()
  })

  test('Platform deployment review shows ephemeral OS deployment', async ({ page }) => {
    await installApiFixtures(page, { freePlatformCandidates: true, readyServerCount: 2 })
    await page.goto('/platforms/deploy/slurm?site=site-a')
    await page.getByLabel('Platform name').fill('ephemeral-slurm')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()

    await page.getByRole('checkbox', { name: 'Run slurmctld on gpu-node-01' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-01' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-02' }).locator('..').click()
    await next.click()
    await expect(page.getByRole('heading', { name: 'Slurm features' })).toBeVisible()
    await next.click()

    await expect(page.getByRole('heading', { name: 'Operating system', exact: true })).toBeVisible()
    const platformOSStep = page.locator('.sw-os-image-selection-step')
    await expect(platformOSStep).toHaveCount(1)
    await expect(platformOSStep.locator('.sw-form-grid')).toHaveCount(0)
    await expect(platformOSStep.getByText(
      'Choose an OS family, then compare the deployable images in that family. Architecture, size, tags, deploy-mode availability, and distinct provider IDs remain visible.',
      { exact: true },
    )).toBeVisible()
    await chooseOSImage(page, 'Ubuntu 24.04 LTS')
    await expect(page.getByRole('heading', { name: 'OS installation' })).toBeVisible()
    await expect(page.getByRole('radio', { name: 'RAM deploy' })).toBeChecked()
    await expect(page.getByRole('heading', { name: 'OS networking' })).toBeVisible()
    await next.click()

    await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await expect(page.getByText('Deploy mode', { exact: true })).toBeVisible()
    await expect(page.getByText('Ephemeral (memory-backed)', { exact: true })).toBeVisible()
  })

  test('Platform OS installation warns about an unsupported deploy mode without blocking Next', async ({ page }) => {
    await installApiFixtures(page, { freePlatformCandidates: true, readyServerCount: 1 })
    await page.goto('/platforms/deploy/kubernetes?site=site-a')
    await page.getByLabel('Platform name').fill('unsupported-mode-k0s')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()

    await page.getByRole('radio', { name: /Standalone/ }).locator('xpath=following-sibling::*[1]').click()
    await chooseSingleSelectOption(page, 'Role for gpu-node-01', 'Standalone node')
    await next.click()
    await next.click()

    await expect(page.getByRole('heading', { name: 'Operating system', exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Custom 1', exact: true }).click()
    await chooseOSImage(page, 'Ubuntu 24.04 ROCm')

    await expect(page.getByRole('heading', { name: 'OS installation' })).toBeVisible()
    await expect(page.getByText('Deploy mode', { exact: true })).toBeVisible()
    const disk = page.getByRole('radio', { name: 'Disk deploy' })
    await disk.locator('..').locator('.sw-deployment-choice-card').click()
    await expect(page.getByText('This image does not support this deploy mode')).toBeVisible()
    await expect(page.getByText(/You can continue, but deployment may fail/)).toBeVisible()
    await expect(next).toBeEnabled()
    await next.click()
    await expect(page.getByRole('heading', { name: 'Platform network' })).toBeVisible()
  })

  test('Kubernetes ephemeral deployment warns about volatile state and submits the intent', async ({ page }) => {
    let submitted: Record<string, unknown> | undefined
    await installApiFixtures(page, {
      freePlatformCandidates: true,
      readyServerCount: 1,
      onPlatformDeploymentRequest: (body) => { submitted = body },
    })
    await page.goto('/platforms/deploy/kubernetes?site=site-a')
    await page.getByLabel('Platform name').fill('ephemeral-k0s')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()

    await page.getByRole('radio', { name: /Standalone/ }).locator('xpath=following-sibling::*[1]').click()
    await chooseSingleSelectOption(page, 'Role for gpu-node-01', 'Standalone node')
    await next.click()
    await next.click()

    await expect(page.getByRole('heading', { name: 'Operating system', exact: true })).toBeVisible()
    await chooseOSImage(page, 'Ubuntu 24.04 LTS')
    await expect(page.getByRole('heading', { name: 'OS installation' })).toBeVisible()
    await expect(page.getByRole('radio', { name: 'RAM deploy' })).toBeChecked()
    await expect(page.getByText('Ephemeral Kubernetes is disposable')).toBeVisible()
    await expect(page.getByText(/control-plane state, container runtime, and workloads are held in memory/)).toBeVisible()
    await next.click()

    await expect(page.getByRole('heading', { name: 'Platform network' })).toBeVisible()
    await next.click()

    await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await expect(page.getByText('Ephemeral (memory-backed)', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Deploy Kubernetes Platform' }).click()
    expect(submitted).toMatchObject({
      machinePreparation: {
        mode: 'provision_os',
        settings: { ephemeral: true },
      },
    })
  })

  test('Platform wizard supports standalone and non-HA multi-node without a VIP', async ({ page }) => {
    await installApiFixtures(page, { freePlatformCandidates: true })
    await page.goto('/platforms/deploy/kubernetes?site=site-a')
    await page.getByLabel('Platform name').fill('edge-k0s')
    await page.getByRole('button', { name: 'Next' }).click()
    await page.getByRole('radio', { name: /Standalone/ }).locator('xpath=following-sibling::*[1]').click()
    await chooseSingleSelectOption(page, 'Role for gpu-node-01', 'Standalone node')
    await page.getByRole('button', { name: 'Next' }).click()
    await page.getByRole('button', { name: 'Next' }).click()
    await expect(page.getByLabel('API virtual IP')).toHaveCount(0)
    await expect(page.getByText(/192\.168\.40\.21:6443/)).toBeVisible()

    await page.getByRole('button', { name: 'Back' }).click()
    await page.getByRole('button', { name: 'Back' }).click()
    await page.getByRole('radio', { name: /Multi-node \(non-HA\)/ }).locator('xpath=following-sibling::*[1]').click()
    await chooseSingleSelectOption(page, 'Role for gpu-node-01', 'Control-plane')
    await chooseSingleSelectOption(page, 'Role for gpu-node-02', 'Worker')
    await page.getByRole('button', { name: 'Next' }).click()
    await page.getByRole('button', { name: 'Next' }).click()
    await expect(page.getByLabel('API virtual IP')).toHaveCount(0)
    await expect(page.getByText('Direct control-plane endpoint')).toBeVisible()
  })

  test('Headlamp language is type-aware and Slurm stays neutral', async ({ page }) => {
    await page.goto('/platforms/platform-a?site=site-a')
    const kubernetesStats = page.locator('.sw-metric-grid')
    await expect(kubernetesStats.getByText('Topology', { exact: true })).toBeVisible()
    await expect(kubernetesStats.getByText('High availability', { exact: true })).toBeVisible()
    await expect(kubernetesStats.getByText('Control-plane', { exact: true })).toBeVisible()
    await expect(kubernetesStats.getByText('Workload-capable', { exact: true })).toBeVisible()
    await expect(kubernetesStats.getByText('1 also runs workloads', { exact: true })).toBeVisible()
    await expect(
      page.getByRole('table', { name: 'Platform members' }).getByRole('row', { name: /gpu-node-01/ }),
    ).toContainText('Runs workloads')
    await expect(page.getByText(/Kubernetes membership/)).toBeVisible()
    await page.goto('/platforms/platform-slurm?site=site-a')
    const slurmStats = page.locator('.sw-metric-grid')
    await expect(slurmStats.getByText('Managers')).toBeVisible()
    await expect(slurmStats.getByText('Compute nodes')).toBeVisible()
    // A registered Slurm platform has no live slurmrestd read, so the Slurm view degrades and
    // never borrows Kubernetes vocabulary.
    await expect(page.getByText('Live Slurm state is unavailable')).toBeVisible()
    await expect(page.getByText(/Kubernetes/)).toHaveCount(0)
  })

  test('Platform detail keeps configuration inside Overview and Activity separate', async ({ page }) => {
    await page.goto('/platforms/platform-a?site=site-a')

    const tabList = page.getByRole('tablist')
    const configuration = page.getByRole('heading', { name: 'Configuration', exact: true })
    await expect(tabList).toBeVisible()
    await expect(configuration).toBeVisible()

    const tabBounds = await tabList.boundingBox()
    const configurationBounds = await configuration.boundingBox()
    expect(tabBounds).not.toBeNull()
    expect(configurationBounds).not.toBeNull()
    expect(tabBounds!.y).toBeLessThan(configurationBounds!.y)

    await page.getByRole('tab', { name: 'Activity' }).click()
    await expect(configuration).toBeHidden()
    await expect(page.getByRole('heading', { name: 'Related workflows', exact: true })).toBeVisible()

    await page.getByRole('tab', { name: 'Overview' }).click()
    await expect(configuration).toBeVisible()

    await page.setViewportSize({ width: 390, height: 844 })
    const memberCard = page.locator('.sw-resource-card').filter({ hasText: 'gpu-node-01' })
    await expect(memberCard).toBeVisible()
    await expect(memberCard.getByRole('button', { name: 'Open server' })).toBeVisible()
    await expect(page.getByRole('table', { name: 'Platform members' })).toBeHidden()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

    await page.getByRole('tab', { name: 'Activity' }).click()
    await expect(page.getByRole('button', { name: 'Open workflow' }).first()).toBeVisible()
  })

  test('deployed Slurm platform shows the Slurm-native cluster view (controllers, partitions, node states)', async ({ page }) => {
    // The Slurm view reads live cluster state from slurmrestd: controllers via ping, partitions,
    // and compute node scheduler state - each in its own section, not the k0s member table.
    await installApiFixtures(page, { slurmDeployed: true })
    await page.goto('/platforms/platform-slurm-ha?site=site-a')

    const stats = page.locator('.sw-metric-grid')
    await expect(stats.locator('.sw-metric-card').filter({ hasText: 'Managers' })).toContainText('2')
    await expect(stats.locator('.sw-metric-card').filter({ hasText: 'Compute nodes' })).toContainText('2')

    // Controllers come from slurmrestd ping with a real up/down status, in failover order.
    const controllers = page.getByRole('table', { name: 'Slurm controllers' })
    const primaryRow = controllers.getByRole('row', { name: /slurm-ctl-01/ })
    await expect(primaryRow).toContainText('primary')
    await expect(primaryRow).toContainText('up')
    await expect(controllers.getByRole('row', { name: /slurm-ctl-02/ })).toContainText('backup')

    // Partitions are a first-class Slurm concept, shown in their own section.
    await expect(page.getByRole('table', { name: 'Slurm partitions' }).getByRole('row', { name: /main/ })).toBeVisible()

    // Compute nodes show the real Slurm scheduler state; health/power is never conflated in.
    const nodes = page.getByRole('table', { name: 'Slurm compute nodes' })
    await expect(nodes.getByRole('row', { name: /slurm-cpt-01/ })).toContainText('idle')
    await expect(nodes.getByRole('row', { name: /slurm-cpt-02/ })).toContainText('allocated')
    await expect(nodes.getByRole('row', { name: /slurm-cpt-01/ })).not.toContainText('host on')
  })

  test('deployed Kubernetes platform exposes the live cluster explorer (applications, namespaces, cluster)', async ({ page }) => {
    await page.goto('/platforms/platform-a?site=site-a')

    // The explorer tabs appear only for a Swallow-deployed Kubernetes platform with a credential.
    await expect(page.getByRole('tab', { name: 'Applications' })).toBeVisible()
    await expect(page.getByRole('tab', { name: 'Namespaces' })).toBeVisible()
    await expect(page.getByRole('tab', { name: 'Cluster' })).toBeVisible()

    // Applications aggregate workloads and bare pods from the live cluster.
    await page.getByRole('tab', { name: 'Applications' }).click()
    const applications = page.getByRole('table', { name: 'Kubernetes applications' })
    await expect(applications.getByRole('row', { name: /nginx/ })).toContainText('Deployment')
    await expect(applications.getByRole('row', { name: /debug/ })).toContainText('Pod')

    // Cluster tab shows the live summary and node table with cordon controls.
    await page.getByRole('tab', { name: 'Cluster' }).click()
    await expect(page.locator('.sw-metric-grid').getByText('Version', { exact: true })).toBeVisible()
    const nodes = page.getByRole('table', { name: 'Kubernetes nodes' })
    await expect(nodes.getByRole('row', { name: /gpu-node-01/ })).toContainText('control-plane')
    await expect(nodes.getByRole('row', { name: /gpu-node-02/ }).getByRole('button', { name: 'Cordon' })).toBeVisible()

    // Namespaces flag system namespaces and block their deletion.
    await page.getByRole('tab', { name: 'Namespaces' }).click()
    const namespaces = page.getByRole('table', { name: 'Kubernetes namespaces' })
    const systemRow = namespaces.getByRole('row', { name: /kube-system/ })
    await expect(systemRow.getByRole('button', { name: 'Delete' })).toBeDisabled()
  })

  test('registered platform hides the Kubernetes cluster explorer', async ({ page }) => {
    // research-slurm is a Slurm platform, and platform-b is a deploy-failed Kubernetes platform
    // with no credential; neither is an eligible deployed Kubernetes cluster, so the explorer
    // tabs must not appear.
    await page.goto('/platforms/platform-b?site=site-a')
    await expect(page.getByRole('tab', { name: 'Applications' })).toHaveCount(0)
    await expect(page.getByRole('tab', { name: 'Cluster' })).toHaveCount(0)
  })

  test('standalone Platform summary shows one control-plane is workload-capable', async ({ page }) => {
    await page.goto('/platforms/platform-b?site=site-a')
    const stats = page.locator('.sw-metric-grid')
    await expect(stats.locator('.sw-metric-card').filter({ hasText: 'Topology' })).toContainText('Standalone')
    await expect(stats.locator('.sw-metric-card').filter({ hasText: 'Control-plane' }))
      .toContainText('1 also runs workloads')
    await expect(stats.locator('.sw-metric-card').filter({ hasText: 'Workload-capable' }))
      .toContainText('1')
  })

  test('Platform list behaves as a PaaS runtime control plane', async ({ page }) => {
    await page.goto('/platforms?site=site-a')

    const overview = page.locator('.sw-platform-fleet-overview')
    await expect(overview).toContainText('2 platforms need attention')
    await expect(overview).toContainText('Total3')
    await expect(overview).toContainText('Active1')
    await expect(overview).toContainText('Failed1')
    await expect(overview).toContainText('Unmatched1')

    const table = page.getByRole('table', { name: 'Platforms' })
    const production = table.getByRole('row', { name: /production-k0s/ })
    const failed = table.getByRole('row', { name: /edge-staging/ })
    const registered = table.getByRole('row', { name: /research-slurm/ })
    await expect(production).toContainText('Active')
    await expect(production).toContainText('1 unmatched')
    await expect(production.getByRole('link', { name: 'Open platform' })).toBeVisible()
    await expect(production.getByRole('link', { name: 'View workflow' })).toHaveCount(0)
    await expect(failed).toContainText('Deployment failed')
    await expect(failed).toContainText('Unreachable')
    await expect(failed.getByRole('link', { name: 'Review platform' })).toBeVisible()
    await expect(failed.getByRole('link', { name: 'View workflow' }))
      .toHaveAttribute('href', '/workflows/op-deploy-failed?site=site-a')
    await expect(page.getByText('Swallow-deployed', { exact: true })).toHaveCount(0)
    await expect(registered).toContainText('Registered')
    await expect(registered).toContainText('External record')

    const resultCount = page.getByRole('status').filter({ hasText: /Showing .* platforms/ })
    await page.getByLabel('Search platforms', { exact: true }).fill('research')
    await expect.poll(() => new URL(page.url()).searchParams.get('q')).toBe('research')
    await expect(resultCount).toHaveText('Showing 1 of 3 platforms')
    await expect(table.getByRole('row', { name: /research-slurm/ })).toBeVisible()
    await expect(table.getByRole('row', { name: /production-k0s/ })).toHaveCount(0)

    await page.getByRole('button', { name: 'Clear search' }).click()
    await page.getByRole('button', { name: 'Kubernetes', exact: true }).click()
    await expect.poll(() => new URL(page.url()).searchParams.get('type')).toBe('kubernetes')
    await expect(resultCount).toHaveText('Showing 2 of 3 platforms')

    await table.getByLabel('Select edge-staging', { exact: true }).click()
    await expect(page.getByRole('region', { name: 'Selection actions' })).toBeVisible()
    await page.getByRole('button', { name: 'Needs attention', exact: true }).click()
    await expect.poll(() => new URL(page.url()).searchParams.get('status')).toBe('attention')
    await expect(page.getByRole('region', { name: 'Selection actions' })).toHaveCount(0)
    await expect(resultCount).toHaveText('Showing 2 of 3 platforms')

    await page.getByRole('button', { name: 'In progress', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'No matching platforms' })).toBeVisible()
    await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
    await expect.poll(() => new URL(page.url()).searchParams.toString()).toBe('site=site-a')
    await expect(resultCount).toHaveText('Showing 3 of 3 platforms')

    await page.setViewportSize({ width: 390, height: 844 })
    await expect(page.locator('.sw-platform-runtime-card').first()).toBeVisible()
    await expect(page.locator('.sw-platform-runtime-card').first().getByRole('link', { name: 'Review platform' })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

    // research-slurm is externally registered (not Swallow-deployed), so its only removal is the
    // record-only Delete. Slurm platforms Swallow deploys are uninstallable; the block is the
    // origin, not the type.
    await page.goto('/platforms/platform-slurm?site=site-a')
    await expect(page.getByRole('button', { name: 'Uninstall platform' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'More removal options' })).toHaveCount(0)
    await page.getByRole('button', { name: 'Delete platform', exact: true }).click()
    const deleteDialog = page.getByRole('dialog', { name: 'Delete platform record' })
    await expect(deleteDialog.getByText(/was not deployed by Swallow, so Swallow cannot uninstall it/)).toBeVisible()
    await expect(deleteDialog.getByLabel('Platform name confirmation')).toBeFocused()
  })

  test('failed Platform repairs from its detail workflow and remains in lifecycle progress', async ({ page }) => {
    await page.goto('/platforms/platform-b?site=site-a')
    await expect(page.getByText('Platform deployment failed')).toBeVisible()
    await expect(page.getByText(/no provider address was observed/)).toBeVisible()
    await expect(page.getByText(/Platform installation did not start/)).toBeVisible()

    await page.getByRole('button', { name: 'View automation details' }).click()
    await expect(page).toHaveURL('/workflows/op-deploy-failed?site=site-a')
    await expect(page.getByText('Dashboard could not render this page')).toHaveCount(0)
    await page.getByRole('tab', { name: 'Artifacts' }).click()
    await expect(page.getByText('No artifacts')).toBeVisible()
    await page.goBack()

    await page.getByRole('button', { name: 'Repair deployment' }).click()
    const dialog = page.getByRole('dialog', { name: 'Repair platform deployment' })
    await expect(dialog.getByText('Provisioning recovery may redeploy failed Servers')).toBeVisible()
    await expect(dialog.getByText(/no MAAS address is released, returned to Ready, and redeployed/)).toBeVisible()
    await expect(dialog.getByText(/1 original deployment target/)).toBeVisible()
    await expect(dialog.getByText(/same machines, roles, network settings/)).toBeVisible()

    await dialog.getByRole('button', { name: 'Repair deployment' }).click()

    await expect(page).toHaveURL('/platforms/platform-b?site=site-a')
    await expect(page.getByText('Platform repair started')).toBeVisible()
    await expect(page.getByText(/Provision and verify operating system on srv-4 will retry in Operation op-deploy-failed/)).toBeVisible()
    await expect(page.getByText('Platform deployment is running')).toBeVisible()
    await expect(page.getByText('Deployment failed')).toHaveCount(0)
    await page.getByRole('tab', { name: 'Activity' }).click()
    await expect(
      page.getByRole('table', { name: 'Related workflows' })
        .getByRole('row', { name: /Deploy edge-staging k0s platform/ })
        .first(),
    ).toContainText('running')

    await page.goto('/platforms?site=site-a')
    const inProgressRow = page.getByRole('table', { name: 'Platforms' })
      .getByRole('row', { name: /edge-staging/ })
    await expect(inProgressRow).toContainText('Deploying')
    await expect(inProgressRow.locator('.sw-platform-lifecycle-badge .sw-progress-spinner')).toBeVisible()
    await expect(inProgressRow.getByRole('link', { name: 'View workflow' }))
      .toHaveAttribute('href', '/workflows/op-deploy-failed?site=site-a')
    await page.getByRole('button', { name: 'In progress', exact: true }).click()
    await expect(page.getByRole('status').filter({ hasText: /Showing .* platforms/ }))
      .toHaveText('Showing 1 of 3 platforms')
  })

  test('failed Platform repair recovers via rerun when the workflow execution was lost', async ({ page }) => {
    // The durable execution is gone (a host restart), so signalling the failed Step returns a
    // conflict. Repair must fall back to a rerun on the same Platform instead of dead-ending, and
    // it must not require deleting the Platform.
    await installApiFixtures(page, { deployExecutionLost: true })
    await page.goto('/platforms/platform-b?site=site-a')
    await expect(page.getByText('Platform deployment failed')).toBeVisible()

    await page.getByRole('button', { name: 'Repair deployment' }).click()
    const dialog = page.getByRole('dialog', { name: 'Repair platform deployment' })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Repair deployment' }).click()

    await expect(page.getByText('Platform repair started')).toBeVisible()
    await expect(page.getByText(/Operation op-rerun is rerunning the deployment on the same platform/)).toBeVisible()
    await expect(page.getByText('Platform deployment is running')).toBeVisible()
  })

  test('missing-address OS Step retry requires release and redeploy confirmation', async ({ page }) => {
    await page.goto('/workflows/op-deploy-failed?site=site-a')
    const row = page.getByRole('row', { name: /Provision and verify operating system on srv-4/ })
    await row.getByRole('button', { name: /Retry Provision and verify/ }).click()
    const dialog = page.getByRole('dialog', { name: 'Retry failed OS deployment' })
    await expect(dialog.getByText('This retry may redeploy the Server')).toBeVisible()
    await expect(dialog.getByText(/same image, network settings, and protected cloud-init/)).toBeVisible()
    // A deployment MAAS reports as failed is returned to Ready first, so the retry can redeploy it.
    await expect(dialog.getByText(/reports the deployment\s+failed.*returns it to Ready the way Recover does/s)).toBeVisible()
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    await expect(dialog).toBeHidden()
  })

  test('Uninstall and record-only Delete are separate actions of one red split control', async ({ page }) => {
    const uninstalls: Array<{ platformId: string; body: Record<string, unknown> | null }> = []
    await installApiFixtures(page, {
      onPlatformUninstallRequest: (platformId, body) => uninstalls.push({ platformId, body }),
    })
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/platforms/platform-a?site=site-a')
    // The old menu is gone: Uninstall leads, and record-only Delete sits behind the chevron.
    await expect(page.getByRole('button', { name: 'Platform actions' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Delete platform' })).toHaveCount(0)
    await page.getByRole('button', { name: 'Uninstall platform', exact: true }).click()

    const dialog = page.getByRole('dialog', { name: 'Uninstall platform' })
    const confirmation = dialog.getByLabel('Platform name confirmation')
    await expect(confirmation).toBeFocused()
    await expect(dialog.locator('p').filter({ hasText: 'Targets: 3 original deployment targets' })).toBeVisible()
    await expect(dialog.getByText(/operating system, user data, and other installed packages remain/)).toBeVisible()
    // The record choice lives on the submit: Uninstall keeps it, the chevron variant deletes it.
    await expect(dialog.getByText(/“Uninstall platform” keeps the Swallow record\. To also delete it, choose “Uninstall and delete record”/)).toBeVisible()
    const uninstall = dialog.getByRole('button', { name: 'Uninstall platform' })
    await expect(uninstall).toBeDisabled()
    await expect(dialog.getByRole('button', { name: 'More uninstall options' })).toBeDisabled()
    await confirmation.fill('wrong-name')
    await expect(uninstall).toBeDisabled()
    await confirmation.fill('production-k0s')
    // The deployment still running on these targets must be canceled first, and the operator says so.
    await expect(dialog.getByText('An unfinished workflow still holds these servers')).toBeVisible()
    await expect(dialog.getByRole('list', { name: "Workflows holding the platform's servers" })).toContainText('Deploy production k0s platform')
    await expect(uninstall).toBeDisabled()
    await dialog.getByLabel('Cancel these workflows, then uninstall').locator('..').click()
    await uninstall.click()
    await expect(page).toHaveURL('/workflows/op-uninstall?site=site-a')
    await expect(page.getByText('Platform uninstall accepted')).toBeVisible()
    await expect(page.getByText(/The Swallow record will be retained\. It starts once the canceled workflows have ended/)).toBeVisible()
    // A plain uninstall never asks the backend to delete the record.
    expect(uninstalls.map((request) => request.platformId)).toEqual(['platform-a'])
    expect(uninstalls[0].body).not.toHaveProperty('deleteRecord')

    await page.goto('/platforms/platform-a?site=site-a')
    await page.getByRole('button', { name: 'More removal options' }).click()
    await page.getByRole('menuitem', { name: 'Delete only…' }).click()
    const deleteDialog = page.getByRole('dialog', { name: 'Delete platform record' })
    await expect(deleteDialog.locator('p').filter({ hasText: 'Target: Swallow record “production-k0s”. Hosts are not targeted.' })).toBeVisible()
    await expect(deleteDialog.getByText('Hosts will not be cleaned up')).toBeVisible()
    await expect(deleteDialog.getByText(/If you want host cleanup, cancel this dialog and use Uninstall platform instead/)).toBeVisible()
    // Record-only Delete has nothing to release and no Workflow to cancel on the hosts' behalf.
    await expect(deleteDialog.getByLabel('Release all servers instead of uninstalling platform software')).toHaveCount(0)
    await expect(deleteDialog.getByRole('list', { name: "Workflows holding the platform's servers" })).toHaveCount(0)
    await expect(deleteDialog.getByLabel('Platform name confirmation')).toBeFocused()
    await deleteDialog.getByLabel('Platform name confirmation').fill('production-k0s')
    await deleteDialog.getByRole('button', { name: 'Delete record' }).click()
    await expect(page).toHaveURL('/platforms?site=site-a')
    await expect(page.getByText('Platform record deleted')).toBeVisible()
    await expect(page.getByRole('row', { name: /production-k0s/ })).toHaveCount(0)
    expect(uninstalls).toHaveLength(1)
  })

  test('Uninstall can also delete the record, which the backend does only after success', async ({ page }) => {
    const uninstalls: Array<{ platformId: string; body: Record<string, unknown> | null }> = []
    await installApiFixtures(page, {
      onPlatformUninstallRequest: (platformId, body) => uninstalls.push({ platformId, body }),
    })
    await page.goto('/platforms/platform-a?site=site-a')
    await page.getByRole('button', { name: 'Uninstall platform', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Uninstall platform' })
    await expect(dialog.getByText(/the record is deleted only after the operation succeeds, and a failed, canceled, or indeterminate operation keeps it/)).toBeVisible()
    await dialog.getByLabel('Platform name confirmation').fill('production-k0s')
    await dialog.getByLabel('Cancel these workflows, then uninstall').locator('..').click()
    // The variant sits behind the submit chevron, inside the dialog, and submits directly.
    await dialog.getByRole('button', { name: 'More uninstall options' }).click()
    await dialog.getByRole('menuitem', { name: 'Uninstall and delete record' }).click()

    await expect(page).toHaveURL('/workflows/op-uninstall?site=site-a')
    await expect(page.getByText('Uninstall and delete accepted')).toBeVisible()
    expect(uninstalls).toEqual([{
      platformId: 'platform-a',
      body: expect.objectContaining({ releaseServers: false, cancelActiveWorkflows: true, deleteRecord: true }),
    }])
  })

  test('A running uninstall disables only the Uninstall part of the split control', async ({ page }) => {
    await installApiFixtures(page, { platformLifecycleStates: { 'platform-a': 'uninstalling' } })
    await page.goto('/platforms/platform-a?site=site-a')
    await expect(page.getByRole('button', { name: 'Uninstall platform: Uninstall is already running.' })).toBeDisabled()
    // Deleting the record never mutates hosts, so it stays reachable through the chevron.
    const more = page.getByRole('button', { name: 'More removal options' })
    await expect(more).toBeEnabled()
    await more.focus()
    await page.keyboard.press('Enter')
    await page.getByRole('menuitem', { name: 'Delete only…' }).click()
    const dialog = page.getByRole('dialog', { name: 'Delete platform record' })
    await expect(dialog.getByText(/if an uninstall is already running, let it finish/)).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Delete record' })).toBeDisabled()
  })

  test('An uninstalled Platform offers only the record-only Delete', async ({ page }) => {
    await installApiFixtures(page, { platformLifecycleStates: { 'platform-a': 'uninstalled' } })
    await page.goto('/platforms/platform-a?site=site-a')
    await expect(page.getByText(/Use Delete platform above to permanently delete the record/)).toBeVisible()
    await expect(page.getByRole('button', { name: 'More removal options' })).toHaveCount(0)
    await page.getByRole('button', { name: 'Delete platform', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Delete platform record' })
    await expect(dialog.getByText('Delete the remaining record')).toBeVisible()
    await expect(dialog.getByText(/Platform software has already been removed from the original deployment targets/)).toBeVisible()
    const confirmation = dialog.getByLabel('Platform name confirmation')
    await expect(confirmation).toBeFocused()
    await confirmation.fill('production-k0s')
    await dialog.getByRole('button', { name: 'Delete record' }).click()
    await expect(page).toHaveURL('/platforms?site=site-a')
    await expect(page.getByText('Platform record deleted')).toBeVisible()
  })

  test('Release shortcut skips platform uninstall and preserves release options', async ({ page }) => {
    const uninstalls: Array<{ platformId: string; body: Record<string, unknown> | null }> = []
    await installApiFixtures(page, {
      onPlatformUninstallRequest: (platformId, body) => uninstalls.push({ platformId, body }),
    })
    await page.goto('/platforms/platform-a?site=site-a')
    await page.getByRole('button', { name: 'Uninstall platform', exact: true }).click()

    const dialog = page.getByRole('dialog', { name: 'Uninstall platform' })
    await expect(dialog.getByText(/platform services, configuration, credentials, and managed state from the original deployment targets\. The operating system/)).toBeVisible()
    await expect(dialog.getByText(/k0s services|After k0s/)).toHaveCount(0)
    // Release is an alternative execution path, and its options stay hidden until selected.
    await expect(dialog.getByLabel('Erase disks before release')).toHaveCount(0)
    await dialog.getByLabel('Release all servers instead of uninstalling platform software').locator('..').click()
    await expect(dialog.getByText('Servers will be released', { exact: true })).toBeVisible()
    await expect(dialog.getByText(/directly to its provider instead of uninstalling platform software\. Releasing removes each deployed operating system/)).toBeVisible()
    await expect(dialog.getByText(/leaves this platform|host exporters/)).toHaveCount(0)
    await dialog.getByLabel('Erase disks before release').locator('..').click()
    await dialog.getByLabel('Use secure erase when supported').locator('..').click()
    await dialog.getByLabel('Remove static IP bindings after release').locator('..').click()
    await dialog.getByLabel('Platform name confirmation').fill('production-k0s')
    await dialog.getByLabel('Cancel these workflows, then release the servers').locator('..').click()
    await dialog.getByRole('button', { name: 'Release all servers' }).click()

    await expect(page).toHaveURL('/workflows/op-uninstall?site=site-a')
    await expect(page.getByText('Server release accepted')).toBeVisible()
    await expect.poll(() => uninstalls.length).toBe(1)
    expect(uninstalls[0]).toEqual({
      platformId: 'platform-a',
      body: {
        releaseServers: true,
        releaseOptions: { erase: true, secureErase: true, quickErase: false, unbindStaticIps: true },
        cancelActiveWorkflows: true,
      },
    })
  })

  // A failed deployment parked for attention still holds its Servers, so an uninstall used to fail
  // with "already has active durable work" until the operator found and canceled that Workflow. The
  // dialog now names it, the operator confirms, and the backend cancels it; the browser never
  // cancels or waits, so the dialog closes with the request.
  test('Uninstall of a failed deployment cancels the Workflow holding its Servers in the backend', async ({ page }) => {
    const uninstalls: Array<{ platformId: string; body: Record<string, unknown> | null }> = []
    const superseded: string[] = []
    const browserCancels: string[] = []
    await installApiFixtures(page, {
      onPlatformUninstallRequest: (platformId, body) => uninstalls.push({ platformId, body }),
      onWorkflowSuperseded: (operationId) => superseded.push(operationId),
    })
    page.on('request', (request) => {
      if (/\/api\/v1\/workflows\/[^/]+\/cancel$/.test(new URL(request.url()).pathname)) browserCancels.push(request.url())
    })
    await page.goto('/platforms/platform-b?site=site-a')
    await page.getByRole('button', { name: 'Uninstall platform', exact: true }).click()

    const dialog = page.getByRole('dialog', { name: 'Uninstall platform' })
    await expect(dialog.getByRole('list', { name: "Workflows holding the platform's servers" })).toContainText('Deploy edge-staging k0s platform')
    await dialog.getByLabel('Release all servers instead of uninstalling platform software').locator('..').click()
    await dialog.getByLabel('Platform name confirmation').fill('edge-staging')
    const release = dialog.getByRole('button', { name: 'Release all servers' })
    await expect(release).toBeDisabled()
    await dialog.getByLabel('Cancel these workflows, then release the servers').locator('..').click()
    await release.click()

    await expect(dialog).toBeHidden({ timeout: 2_000 })
    await expect(page).toHaveURL('/workflows/op-uninstall?site=site-a')
    expect(uninstalls).toEqual([{ platformId: 'platform-b', body: expect.objectContaining({ releaseServers: true, cancelActiveWorkflows: true }) }])
    expect(superseded).toEqual(['op-deploy-failed'])
    expect(browserCancels).toEqual([])
  })

  test('A deploying Platform can be uninstalled once the operator confirms canceling the deployment', async ({ page }) => {
    await installApiFixtures(page, { platformLifecycleStates: { 'platform-a': 'deploying' } })
    await page.goto('/platforms/platform-a?site=site-a')
    await page.getByRole('button', { name: 'Uninstall platform', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Uninstall platform' })
    await expect(dialog.getByRole('list', { name: "Workflows holding the platform's servers" })).toContainText('Deploy production k0s platform')
    await dialog.getByLabel('Platform name confirmation').fill('production-k0s')
    await expect(dialog.getByRole('button', { name: 'Uninstall platform' })).toBeDisabled()
  })

  test('Bulk uninstall names the Workflows holding the selected Platforms and sends the cancel', async ({ page }) => {
    const uninstalls: Array<{ platformId: string; body: Record<string, unknown> | null }> = []
    await installApiFixtures(page, {
      onPlatformUninstallRequest: (platformId, body) => uninstalls.push({ platformId, body }),
    })
    await page.goto('/platforms?site=site-a')
    await page.getByRole('table', { name: 'Platforms' }).getByLabel('Select edge-staging').locator('..').click()
    await page.getByRole('button', { name: 'Uninstall selected', exact: true }).click()

    const dialog = page.getByRole('dialog', { name: 'Uninstall selected platforms' })
    await expect(dialog.getByRole('list', { name: 'Platforms to uninstall' })).toHaveText('edge-staging')
    await expect(dialog.getByRole('list', { name: "Workflows holding the selected platforms' servers" })).toContainText('Deploy edge-staging k0s platform')
    await dialog.getByLabel('Type uninstall to confirm').fill('uninstall')
    const submit = dialog.getByRole('button', { name: 'Uninstall software from 1 platform' })
    await expect(submit).toBeDisabled()
    await dialog.getByLabel('Cancel these workflows, then uninstall the selected platforms').locator('..').click()
    await submit.click()
    await expect(dialog).toBeHidden()
    expect(uninstalls).toEqual([{ platformId: 'platform-b', body: expect.objectContaining({ releaseServers: false, cancelActiveWorkflows: true }) }])
    expect(uninstalls[0].body).not.toHaveProperty('deleteRecord')
    await expect(page.getByText('Bulk uninstall accepted')).toBeVisible()
  })

  test('Bulk removal of a mixed selection uninstalls the eligible Platforms and deletes records separately', async ({ page }) => {
    const uninstalls: Array<{ platformId: string; body: Record<string, unknown> | null }> = []
    const deletes: string[] = []
    await installApiFixtures(page, {
      onPlatformUninstallRequest: (platformId, body) => uninstalls.push({ platformId, body }),
    })
    page.on('request', (request) => {
      const match = new URL(request.url()).pathname.match(/^\/api\/v1\/platforms\/([^/]+)$/)
      if (match && request.method() === 'DELETE') deletes.push(match[1])
    })
    await page.goto('/platforms?site=site-a')
    const table = page.getByRole('table', { name: 'Platforms' })
    await table.getByLabel('Select edge-staging').locator('..').click()
    await table.getByLabel('Select research-slurm').locator('..').click()
    // One leads with Uninstall and one can only be deleted, so Uninstall names its narrower reach.
    await expect(page.getByRole('button', { name: 'Uninstall selected', exact: true })).toHaveCount(0)

    // The chevron's record-only Delete covers every selected Platform; leave it unconfirmed.
    await page.getByRole('button', { name: 'More removal options' }).click()
    await page.getByRole('menuitem', { name: 'Delete only…' }).click()
    const deleteDialog = page.getByRole('dialog', { name: 'Delete selected platform records' })
    await expect(deleteDialog.getByRole('list', { name: 'Platform records to delete' }).getByRole('listitem')).toHaveText(['edge-staging', 'research-slurm'])
    await deleteDialog.getByRole('button', { name: 'Cancel' }).click()
    await expect(deleteDialog).toBeHidden()

    await page.getByRole('button', { name: 'Uninstall eligible', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Uninstall selected platforms' })
    await expect(dialog.getByRole('list', { name: 'Platforms to uninstall' })).toHaveText('edge-staging')
    await expect(dialog.getByRole('list', { name: 'Platforms skipped by uninstall' }))
      .toHaveText('research-slurm — Externally registered platforms can only be deleted.')
    await expect(dialog.getByText(/each record is deleted only after that Platform’s operation succeeds/)).toBeVisible()
    await dialog.getByLabel('Type uninstall to confirm').fill('uninstall')
    await dialog.getByLabel('Cancel these workflows, then uninstall the selected platforms').locator('..').click()
    await dialog.getByRole('button', { name: 'More uninstall options' }).click()
    await dialog.getByRole('menuitem', { name: 'Uninstall, then delete 1 record' }).click()
    await expect(dialog).toBeHidden()
    expect(uninstalls).toEqual([{ platformId: 'platform-b', body: expect.objectContaining({ deleteRecord: true, cancelActiveWorkflows: true }) }])
    expect(deletes).toEqual([])
    await expect(page.getByText('Bulk uninstall and delete accepted')).toBeVisible()

    // The accepted Platform left the selection; the skipped one stays for a follow-up Delete,
    // which is now the only removal left, so it is a single button without a chevron.
    await expect(table.getByLabel('Select research-slurm')).toBeChecked()
    await expect(table.getByLabel('Select edge-staging')).not.toBeChecked()
    await expect(page.getByRole('button', { name: 'More removal options' })).toHaveCount(0)
    await page.getByRole('button', { name: 'Delete selected', exact: true }).click()
    await expect(deleteDialog.getByRole('list', { name: 'Platform records to delete' })).toHaveText('research-slurm')
    await deleteDialog.getByLabel('Type delete to confirm').fill('delete')
    await deleteDialog.getByRole('button', { name: 'Delete 1 Swallow record' }).click()
    await expect(deleteDialog).toBeHidden()
    expect(deletes).toEqual(['platform-slurm'])
    await expect(page.getByText('Platform records deleted')).toBeVisible()
  })


  test('Workflow list behaves as a live orchestration console', async ({ page }) => {
    await page.goto('/workflows?site=site-a')

    const inventory = page.locator('.sw-workflow-inventory')
    const results = inventory.getByRole('status').first()
    await expect(page.getByRole('heading', { name: 'Workflow activity', exact: true })).toBeVisible()
    await expect(results).toContainText('Showing 1–4 of 4 workflows')
    await expect(results).toContainText('Live updates every 5s')

    const table = page.getByRole('table', { name: 'Workflows' })
    await expect(table.getByRole('columnheader', { name: 'Action' })).toBeVisible()
    const rows = table.locator('tbody tr')
    await expect(rows).toHaveCount(4)
    await expect(rows.first()).toContainText('Deploy production k0s platform')

    const running = table.getByRole('row', { name: /Deploy production k0s platform/ })
    await expect(running).toContainText('Deploying')
    await expect(running).toContainText('deploy-k0s.yml')
    await expect(running).toContainText('Legacy execution')
    await expect(running.getByRole('link', { name: 'View workflow' }))
      .toHaveAttribute('href', '/workflows/op-running?site=site-a')

    const attention = table.getByRole('row', { name: /Deploy edge-staging k0s platform/ })
    await expect(attention).toContainText('requires attention')
    await expect(attention).toContainText('Provision and verify operating system on srv-4')
    await expect(attention).toContainText('2 Tasks')
    await expect(attention.getByRole('link', { name: 'Review workflow' }))
      .toHaveAttribute('href', '/workflows/op-deploy-failed?site=site-a')
    await expect(attention.getByRole('link', { name: 'Open Platform platform-b' }))
      .toHaveAttribute('href', '/platforms/platform-b?site=site-a')

    const failed = table.getByRole('row', { name: /Install GPU exporters/ })
    await expect(failed).toContainText('Host unreachable')
    await expect(failed.getByRole('link', { name: 'Review workflow' })).toBeVisible()

    const succeeded = table.getByRole('row', { name: /Reconcile accelerator inventory/ })
    await expect(succeeded).toContainText('succeeded')
    await expect(succeeded).toContainText('1 Task')
    await expect(succeeded).toContainText('1 succeeded')
    await expect(succeeded.getByRole('link', { name: 'View workflow' })).toBeVisible()

    const workflowName = running.getByRole('link', { name: 'Deploy production k0s platform' })
    await workflowName.focus()
    await expect(workflowName).toBeFocused()
    const workflowAction = running.getByRole('link', { name: 'View workflow' })
    await workflowAction.focus()
    await expect(workflowAction).toBeFocused()

    await page.getByLabel('Filter by Workflow kind').fill('inventory.reconcile')
    await expect.poll(() => new URL(page.url()).searchParams.get('kind')).toBe('inventory.reconcile')
    await expect(results).toContainText('Showing 1–1 of 1 workflows')
    await expect(table.getByRole('row', { name: /Reconcile accelerator inventory/ })).toBeVisible()

    await page.getByRole('button', { name: 'Clear search' }).click()
    await expect.poll(() => new URL(page.url()).searchParams.get('kind')).toBeNull()
    await page.goto('/workflows?page=1&site=site-a')
    await page.getByRole('button', { name: 'Active', exact: true }).click()
    await expect.poll(() => new URL(page.url()).searchParams.get('active')).toBe('true')
    await expect.poll(() => new URL(page.url()).searchParams.get('status')).toBeNull()
    await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBeNull()
    await expect.poll(() => new URL(page.url()).searchParams.get('site')).toBe('site-a')
    await expect(results).toContainText('Showing 1–2 of 2 workflows')

    await page.getByRole('button', { name: 'Needs attention', exact: true }).click()
    await expect.poll(() => new URL(page.url()).searchParams.get('status')).toBe('requires_attention')
    await expect.poll(() => new URL(page.url()).searchParams.get('active')).toBeNull()
    await expect(results).toContainText('Showing 1–1 of 1 workflows')

    await page.getByRole('button', { name: 'Failed', exact: true }).click()
    await expect.poll(() => new URL(page.url()).searchParams.get('status')).toBe('failed')
    await expect(table.getByRole('row', { name: /Install GPU exporters/ })).toBeVisible()

    await chooseSingleSelectOption(page, 'Filter by another Workflow status', 'Succeeded')
    await expect.poll(() => new URL(page.url()).searchParams.get('status')).toBe('succeeded')
    await expect(table.getByRole('row', { name: /Reconcile accelerator inventory/ })).toBeVisible()

    await page.goto('/workflows?active=active&site=site-a')
    await expect(page.getByRole('button', { name: 'Active', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await expect(results).toContainText('Showing 1–2 of 2 workflows')

    await page.goto('/workflows?status=not-a-status&site=site-a')
    await expect.poll(() => new URL(page.url()).searchParams.get('status')).toBeNull()
    await expect(results).toContainText('Showing 1–4 of 4 workflows')

    await page.goto('/workflows?page=99&site=site-a')
    await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBe('1')
    await expect(results).toContainText('Showing 1–4 of 4 workflows')

    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/workflows?site=site-a')
    const cards = page.locator('.sw-workflow-runtime-card')
    await expect(cards).toHaveCount(4)
    await expect(cards.first().getByRole('link', { name: 'View workflow' })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  })

  test('Workflow list distinguishes filtered and true empty histories', async ({ page }) => {
    await page.route('**/api/v1/workflows*', async (route) => {
      const url = new URL(route.request().url())
      if (url.pathname !== '/api/v1/workflows') {
        await route.fallback()
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [], total: 0, page: 1, pageSize: 30 }),
      })
    })

    await page.goto('/workflows?kind=does-not-exist&site=site-a')
    await expect(page.getByRole('heading', { name: 'No workflows match this view' })).toBeVisible()
    await page.getByRole('button', { name: 'Clear filters' }).click()
    await expect(page).toHaveURL('/workflows?site=site-a')
    await expect(page.getByRole('heading', { name: 'No workflows yet' })).toBeVisible()
    await expect(page.getByRole('link', { name: /create workflow/i })).toHaveCount(0)
  })

  test('Workflow list polling preserves last-good data and stops after convergence', async ({ page }) => {
    let requests = 0
    await page.clock.install({ time: new Date('2026-08-27T03:05:00Z') })
    await page.route('**/api/v1/workflows*', async (route) => {
      const url = new URL(route.request().url())
      if (url.pathname !== '/api/v1/workflows') {
        await route.fallback()
        return
      }
      requests += 1
      if (requests === 3) {
        await route.fulfill({
          status: 503,
          contentType: 'application/json',
          body: JSON.stringify({ error: { code: 'provider_unavailable', message: 'Workflow refresh unavailable' } }),
        })
        return
      }
      const succeeded = requests >= 4
      const status = succeeded ? 'succeeded' : 'running'
      const operation = {
        id: 'op-polling', kind: 'custom', intent: 'Observe polling convergence',
        siteId: 'site-a', platformId: null, targetServerIds: [], retryOfOperationId: null,
        execution: {
          runId: 'run-polling', playbook: 'observe.yml', status, statusReason: null,
          startedAt: '2026-08-27T03:00:00Z', finishedAt: succeeded ? '2026-08-27T03:05:00Z' : null,
        },
        requestedBy: 'admin', requestedAt: '2026-08-27T02:59:00Z',
        updatedAt: '2026-08-27T03:05:00Z',
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [operation], total: 1, page: 1, pageSize: 30 }),
      })
    })

    await page.goto('/workflows?site=site-a')
    const row = page.getByRole('table', { name: 'Workflows' })
      .getByRole('row', { name: /Observe polling convergence/ })
    await expect(row).toContainText('running')
    await expect(page.getByText('Live updates every 5s')).toBeVisible()

    await page.clock.fastForward(5000)
    await expect.poll(() => requests).toBe(3)
    await expect(page.getByText('Live updates interrupted')).toBeVisible()
    await expect(row).toContainText('running')

    await page.clock.fastForward(5000)
    await expect.poll(() => requests).toBe(4)
    await expect(row).toContainText('succeeded')
    await expect(page.getByText('Live updates interrupted')).toHaveCount(0)
    await expect(page.getByText(/Updated/).first()).toBeVisible()

    await page.getByRole('button', { name: 'Refresh', exact: true }).click()
    await expect.poll(() => requests).toBe(5)

    await page.clock.fastForward(6000)
    expect(requests).toBe(5)
  })

  test('AWX stdout is first and supports search, navigation, copy, download, events, and retry', async ({ page }) => {
    await page.goto('/workflows?site=site-a')
    await page.getByRole('button', { name: 'Failed', exact: true }).click()
    await expect(page).toHaveURL(/status=failed/)
    await page.goto('/workflows/op-running?site=site-a')
    await expect(page.getByText('Deploying', { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('tab', { name: 'Stdout' })).toHaveAttribute('aria-selected', 'true')
    await page.getByPlaceholder('Search stdout').fill('TASK')
    await expect(page.getByText('1 / 3')).toBeVisible()
    await page.getByLabel('Next').click()
    await expect(page.getByText('2 / 3')).toBeVisible()
    await page.getByLabel('Previous').click()
    await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: async (text: string) => localStorage.setItem('e2e.clipboard', text) },
    }))
    await page.getByLabel('Copy stdout').click()
    await expect.poll(() => page.evaluate(() => localStorage.getItem('e2e.clipboard'))).toContain('PLAY [Prepare hosts]')
    await expect(page.getByText('stdout copied', { exact: true })).toBeVisible()
    const download = page.waitForEvent('download')
    await page.getByLabel('Download stdout').click()
    expect((await download).suggestedFilename()).toBe('swallow-operation-op-running-stdout.log')
    await page.getByRole('tab', { name: 'Events' }).click()
    await page.getByText('Errors only', { exact: true }).click()
    await expect(page.getByRole('button', { name: 'Start controller', exact: true })).toBeVisible()
    await expect(page.getByText('Gather facts', { exact: true })).toHaveCount(0)
    await page.goto('/workflows/op-failed?site=site-a')
    await page.getByRole('button', { name: 'Retry' }).click()
    await expect(page).toHaveURL('/workflows/op-retry?site=site-a')
  })

  test('Workflow Step events copy task names and explain failed hosts', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    const operation = {
      id: 'op-event-diagnostics', schemaVersion: 3, kind: 'configure-slurm',
      intent: 'Configure Slurm platform', intentSnapshot: {},
      definition: 'platform-deployment', definitionVersion: 1,
      status: 'requires_attention', statusReason: 'A failed Step requires operator attention.',
      startState: 'started', temporal: { workflowId: 'swallow-operation/op-event-diagnostics' },
      siteId: 'site-a', platformId: 'platform-b',
      targetResources: [{ kind: 'server', id: 'srv-1' }], targetServerIds: ['srv-1'],
      retryOfOperationId: null,
      steps: [{
        id: 'configure-slurm', kind: 'ansible-playbook', name: 'Configure Slurm', job: 'configure-slurm',
        executor: 'ansible', dependsOn: [], targets: [{ kind: 'server', id: 'srv-1' }],
        status: 'failed', attempt: 1, progress: 0,
        error: { code: 'runner_failed', message: 'Ansible task failed.', retryable: true, stage: 'configuration' },
        externalExecution: { provider: 'ansible-runner', id: 'run-event-diagnostics', generation: 1 },
        artifacts: [], startedAt: '2026-08-27T02:50:00Z', finishedAt: '2026-08-27T02:51:00Z',
      }],
      leases: [], requestCorrelation: 'req-event-diagnostics',
      execution: {
        runId: 'run-event-diagnostics', playbook: '', status: 'requires_attention',
        statusReason: 'A failed Step requires operator attention.',
        startedAt: '2026-08-27T02:50:00Z', finishedAt: null,
      },
      requestedBy: 'admin', requestedAt: '2026-08-27T02:49:00Z',
      startedAt: '2026-08-27T02:50:00Z', finishedAt: null, updatedAt: '2026-08-27T02:51:00Z',
    }
    await page.route('**/api/v1/workflows/op-event-diagnostics**', async (route) => {
      const path = new URL(route.request().url()).pathname
      if (path === '/api/v1/workflows/op-event-diagnostics') {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(operation) })
        return
      }
      if (path.endsWith('/events')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            runId: 'run-event-diagnostics', status: 'failed', okCount: 0, changedCount: 0,
            failedCount: 1, unreachableCount: 0, skippedCount: 0,
            events: [{
              play: 'Configure Slurm', task: 'Start Slurm controller', host: 'srv-1',
              status: 'failed', changed: false,
              startedAt: '2026-08-27T02:50:00Z', endedAt: '2026-08-27T02:51:00Z',
            }],
          }),
        })
        return
      }
      if (path.endsWith('/stderr')) {
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body: '==> FAILED  task: Start Slurm controller  host: srv-1\nmsg: slurmctld.service entered the failed state\nrc: 1\nstderr:\nslurmctld: fatal: Unable to determine this server name\n',
        })
        return
      }
      if (path.endsWith('/logs')) {
        await route.fulfill({ status: 200, contentType: 'text/plain', body: 'fatal: [srv-1]: FAILED\n' })
        return
      }
      if (path.endsWith('/artifacts')) {
        await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' })
        return
      }
      await route.fallback()
    })
    await page.addInitScript(() => Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: async (text: string) => localStorage.setItem('e2e.eventName', text) },
    }))

    await page.goto('/workflows/op-event-diagnostics?site=site-a')
    await page.getByRole('tab', { name: 'Events' }).click()
    const failureReason = page.locator('.sw-event-group__diagnostic pre')
    await expect(failureReason).toContainText('slurmctld.service entered the failed state')
    await expect(failureReason).toContainText('slurmctld: fatal: Unable to determine this server name')
    await page.getByLabel('Copy event name: Start Slurm controller').click()
    await expect.poll(() => page.evaluate(() => localStorage.getItem('e2e.eventName'))).toBe('Start Slurm controller')
    const eventBounds = await page.locator('.sw-event-workspace').evaluate((workspace) => {
      const workspaceRect = workspace.getBoundingClientRect()
      const reasonRect = workspace.querySelector('.sw-event-group__diagnostic pre')?.getBoundingClientRect()
      return {
        workspaceFits: workspaceRect.left >= 0 && workspaceRect.right <= window.innerWidth + 1,
        reasonFits: Boolean(reasonRect && reasonRect.left >= workspaceRect.left && reasonRect.right <= workspaceRect.right + 1),
      }
    })
    expect(eventBounds).toEqual({ workspaceFits: true, reasonFits: true })
  })

  test('Monitoring filters, acknowledges, and exposes the provider Grafana link', async ({ page }) => {
    await page.goto('/monitoring?site=site-a')
    const grafana = page.getByRole('link', { name: 'Explore in Grafana' }).first()
    await expect(grafana).toHaveAttribute('href', 'https://grafana.example')
    await expect(grafana).toHaveAttribute('target', '_blank')
    await chooseSingleSelectOption(page, 'Filter alert severity', 'Critical')
    await expect(page).toHaveURL(/severity=critical/)
    const alertsList = page.getByRole('list', { name: 'Monitoring alerts' })
    await expect(alertsList.getByText('NodeDown')).toBeVisible()
    await expect(alertsList.getByRole('listitem')).toHaveCount(1)
    await alertsList.getByRole('listitem').filter({ hasText: 'NodeDown' }).getByRole('button', { name: 'Acknowledge' }).click()
    const acknowledgeDialog = page.getByRole('dialog', { name: 'Acknowledge alert' })
    await expect(acknowledgeDialog).toBeVisible()
    await acknowledgeDialog.getByLabel('Comment').fill('Investigating host power')
    await page.getByRole('dialog').getByRole('button', { name: 'Acknowledge' }).click()
    await expect(page.getByText('Alert acknowledged')).toBeVisible()
  })

  test('Monitoring uses 200-ID batches, at most two workers, and keeps partial results', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    const batches: number[] = []
    let maxActive = 0
    await installApiFixtures(page, { fleetSize: 205, metricsDelayMs: 50, failMetricsBatchIndex: 1, onMetricsRequest: (ids) => batches.push(ids.length), onMetricsActive: (active) => { maxActive = Math.max(maxActive, active) } })
    await page.goto('/monitoring?site=site-a')
    await expect.poll(() => [...batches].sort((left, right) => right - left)).toEqual([200, 5])
    await expect(page.getByText('Some metric batches are unavailable')).toBeVisible()
    expect(maxActive).toBeLessThanOrEqual(2)
    expect(maxActive).toBe(2)
    await expect(page.getByRole('cell', { name: /30%/ }).first()).toBeVisible()
    await expect(page.getByText('No data').first()).toBeVisible()
  })

  test('Monitoring surfaces acknowledge provider errors without losing alerts', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, { acknowledgeFails: true })
    await page.goto('/monitoring?site=site-a')
    await page.getByRole('button', { name: 'Acknowledge' }).first().click()
    await page.getByRole('dialog').getByRole('button', { name: 'Acknowledge' }).click()
    await expect(page.getByText('Alertmanager is unavailable')).toBeVisible()
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click()
    await expect(page.getByRole('listitem').filter({ hasText: 'NodeDown' })).toBeVisible()
  })

  test('Server Detail opens a fixed-target OS deployment dialog in place', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, { readyServerCount: 1 })
    await page.goto('/servers/srv-1/summary?site=site-a')
    await expect(page.getByText('Deploy operating system')).toHaveCount(0)
    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()
    await expect(page).toHaveURL('/servers/srv-1/summary?site=site-a')
    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    await expect(dialog.getByText('Configure the selected Servers without leaving this page.')).toBeVisible()
    await expect(dialog.getByRole('tab', { name: 'Targets' })).toHaveCount(0)
    await expect(dialog.getByRole('heading', { name: 'Operating system', exact: true })).toBeVisible()
    await expect(dialog.getByRole('region', { name: 'Deployment context' })).toHaveCount(0)
  })

  test('multi-node deploy preserves targets, saves a template, and returns to Servers', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    let deploymentRequest: Record<string, unknown> | undefined
    await installApiFixtures(page, {
      readyServerCount: 2,
      deploymentFailureIds: ['srv-2'],
      onDeploymentRequest: (body) => { deploymentRequest = body },
    })
    await page.goto('/servers?site=site-a')
    await page.getByLabel('Select gpu-node-01').check()
    await page.getByLabel('Select gpu-node-02').check()
    await page.getByRole('button', { name: 'Deploy OS', exact: true }).click()
    await expect(page).toHaveURL('/servers?site=site-a')
    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    await expect(dialog.getByRole('tab', { name: 'Targets' })).toHaveCount(0)
    await expect(dialog.getByRole('region', { name: 'Deployment context' })).toHaveCount(0)
    await expect(dialog.getByLabel('Configuration source')).toHaveCount(0)
    await chooseOSImage(page, 'Ubuntu 22.04 LTS')
    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Installation', exact: true })).toBeVisible()
    await chooseSingleSelectOption(page, 'Cloud-init', 'Replace for this deployment')
    await dialog.getByLabel('Cloud-init user data').fill('#cloud-config\nhostname: batch')
    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Networking', exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await expect(dialog.getByRole('heading', { name: '2 Servers' })).toBeVisible()
    await expect(dialog.getByText('Site', { exact: true })).toBeVisible()
    await expect(dialog.getByText('Provisioner', { exact: true })).toBeVisible()
    await page.getByLabel('Save as a deployment template').locator('..').click()
    await page.getByLabel('New deployment template name').fill('Scale-out baseline')
    await dialog.getByRole('button', { name: 'Deploy OS', exact: true }).click()

    await expect(page).toHaveURL('/servers?site=site-a')
    await expect(page.getByText('OS deployment started')).toBeVisible()
    await expect(page.getByText('Server status will update automatically.')).toBeVisible()
    await expect(page.getByRole('button', { name: 'View workflow' })).toBeVisible()
    await expect(page.getByRole('table', { name: 'Servers' })).toBeVisible()
    expect(deploymentRequest?.serverIds).toEqual(['srv-1', 'srv-2'])
    expect(deploymentRequest?.network).toEqual({ mode: 'automatic', defaultGateway: false, assignments: [{ serverId: 'srv-1', interfaceId: 'nic-srv-1', subnetId: 'subnet-a' }, { serverId: 'srv-2', interfaceId: 'nic-srv-2', subnetId: 'subnet-a' }] })
    expect(JSON.stringify(deploymentRequest)).toContain('#cloud-config')
    expect(await page.evaluate(() => JSON.stringify({
      local: { ...localStorage },
      session: { ...sessionStorage },
    }))).not.toContain('#cloud-config')
  })

  test('Deploy OS preserves an existing Static binding as the network default', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, {
      readyServerCount: 2,
      staticNetworkServerIds: ['srv-1'],
      networkSubnetName: '192.168.40.0/24',
    })

    await page.goto('/servers?site=site-a')
    await page.getByLabel('Select gpu-node-01').check()
    await page.getByLabel('Select gpu-node-02').check()
    await page.getByRole('button', { name: 'Deploy OS', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    await chooseOSImage(page, 'Ubuntu 22.04 LTS')
    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Installation', exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Networking', exact: true })).toBeVisible()

    const staticMode = dialog.getByRole('radio', { name: 'Static' })
    const automaticMode = dialog.getByRole('radio', { name: 'Automatic' })
    await expect(staticMode).toBeChecked()
    await expect(automaticMode).not.toBeChecked()
    const subnet = page.getByRole('combobox', { name: 'Subnet for gpu-node-01' })
    await expect(subnet).toHaveText('192.168.40.0/24')
    await expect(subnet).not.toContainText('(')
    await expect(dialog.getByLabel('Use the selected subnet for the default route')).toBeChecked()
    await expect(page.getByLabel('Static IPv4 address for gpu-node-01')).toHaveValue('192.168.40.21')
    await expect(page.getByLabel('Static IPv4 address for gpu-node-02')).toHaveValue('')

    await expect(page.getByRole('button', { name: 'Next' })).toBeDisabled()

    await automaticMode.locator('..').click()
    await expect(automaticMode).toBeChecked()
    await expect(page.getByLabel('Static IPv4 address for gpu-node-01')).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Next' })).toBeEnabled()
  })

  test('cross-integration selection is blocked before contextual deployment', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, {
      readyServerCount: 2,
      secondReadyServerIntegrationId: 'maas-b',
    })
    await page.goto('/servers?site=site-a')
    await page.getByLabel('Select gpu-node-01').check()
    await page.getByLabel('Select gpu-node-02').check()
    await expect(page.getByRole('button', { name: 'Deploy OS', exact: true })).toBeDisabled()
    await expect(page.getByText('Selected Servers must use the same provisioner integration.')).toBeVisible()

    await page.getByLabel('Select gpu-node-02').uncheck()
    await page.getByRole('button', { name: 'Deploy OS', exact: true }).click()
    await expect(page).toHaveURL('/servers?site=site-a')
    await expect(page.getByRole('dialog', { name: 'Deploy OS' })).toBeVisible()
    await page.getByRole('dialog', { name: 'Deploy OS' }).getByRole('button', { name: 'Close' }).click()
  })

  test('Static OS deployment requires and reviews a unique IPv4 address per target', async ({ page }) => {
    let deploymentRequest: Record<string, unknown> | undefined
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, {
      readyServerCount: 2,
      deploymentConvergesAfterRefreshes: 1,
      onDeploymentRequest: (body) => { deploymentRequest = body },
    })
    await page.goto('/servers?site=site-a')
    await page.getByLabel('Select gpu-node-01').check()
    await page.getByLabel('Select gpu-node-02').check()
    await page.getByRole('button', { name: 'Deploy OS', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    await chooseOSImage(page, 'Ubuntu 22.04 LTS')
    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Installation', exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Networking', exact: true })).toBeVisible()
    await dialog.getByRole('radio', { name: 'Static' }).locator('..').click()

    const next = dialog.getByRole('button', { name: 'Next' })
    await expect(next).toBeDisabled()
    await page.getByLabel('Static IPv4 address for gpu-node-01').fill('192.168.40.91')
    await page.getByLabel('Static IPv4 address for gpu-node-02').fill('192.168.40.91')
    await expect(next).toBeDisabled()
    await page.getByLabel('Static IPv4 address for gpu-node-02').fill('192.168.40.92')
    await expect(next).toBeEnabled()
    await next.click()

    const review = dialog.getByRole('table', { name: 'Deployment review targets' })
    await expect(review).toContainText('192.168.40.91')
    await expect(review).toContainText('192.168.40.92')
    await dialog.getByRole('button', { name: 'Deploy OS', exact: true }).click()
    await expect(page).toHaveURL('/servers?site=site-a')
    expect(deploymentRequest?.network).toEqual({
      mode: 'static',
      defaultGateway: false,
      assignments: [
        { serverId: 'srv-1', interfaceId: 'nic-srv-1', subnetId: 'subnet-a', ipAddress: '192.168.40.91' },
        { serverId: 'srv-2', interfaceId: 'nic-srv-2', subnetId: 'subnet-a', ipAddress: '192.168.40.92' },
      ],
    })
    await expect(page.getByText('OS deployment started')).toBeVisible()
    await expect(page.getByRole('table', { name: 'Servers' })).toBeVisible()
  })

  // ADR 058: on a subnet whose DHCP the provisioner does not serve, network inspection suggests
  // DHCP; the dialog starts there, says why, keeps the warning if the operator picks Automatic, and
  // sends `dhcp` without addresses.
  test('a site-DHCP subnet starts the deployment on DHCP and explains the suggestion', async ({ page }) => {
    let deploymentRequest: Record<string, unknown> | undefined
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, {
      readyServerCount: 2,
      siteDhcpNetworkServerIds: ['srv-1', 'srv-2'],
      deploymentConvergesAfterRefreshes: 1,
      onDeploymentRequest: (body) => { deploymentRequest = body },
    })
    await page.goto('/servers?site=site-a')
    await page.getByLabel('Select gpu-node-01').check()
    await page.getByLabel('Select gpu-node-02').check()
    await page.getByRole('button', { name: 'Deploy OS', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    await chooseOSImage(page, 'Ubuntu 22.04 LTS')
    await dialog.getByRole('button', { name: 'Next' }).click()
    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Networking', exact: true })).toBeVisible()

    await expect(dialog.getByRole('radio', { name: 'DHCP' })).toBeChecked()
    const explanation = dialog.getByText(/DHCP is suggested because the provisioner runs no DHCP/)
    await expect(explanation).toBeVisible()
    await expect(dialog.getByLabel(/^Static IPv4 address for/)).toHaveCount(0)
    await dialog.getByRole('radio', { name: 'Automatic' }).locator('..').click()
    await expect(explanation).toBeVisible()
    await dialog.getByRole('radio', { name: 'DHCP' }).locator('..').click()

    await dialog.getByRole('button', { name: 'Next' }).click()
    const review = dialog.getByRole('table', { name: 'Deployment review targets' })
    await expect(review.getByRole('cell', { name: 'DHCP', exact: true })).toHaveCount(2)
    await dialog.getByRole('button', { name: 'Deploy OS', exact: true }).click()
    await expect(page).toHaveURL('/servers?site=site-a')
    expect(deploymentRequest?.network).toEqual({
      mode: 'dhcp',
      defaultGateway: false,
      assignments: [
        { serverId: 'srv-1', interfaceId: 'nic-srv-1', subnetId: 'subnet-a' },
        { serverId: 'srv-2', interfaceId: 'nic-srv-2', subnetId: 'subnet-a' },
      ],
    })
  })

  test('OS Images presents its title and inventory facts as paired rows', async ({ page }) => {
    await page.goto('/provisioning/images?site=site-a')
    await expect(page.getByRole('columnheader', { name: 'Default user' })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: 'Artifact' })).toHaveCount(0)
    await expect(page.getByRole('columnheader', { name: 'Image ID' })).toHaveCount(0)
    await expect(page.getByRole('columnheader', { name: 'Refreshed' })).toHaveCount(0)

    const syncedTitle = page.getByRole('row', { name: 'Ubuntu 22.04 LTS image', exact: true }).first()
    const syncedData = page.getByRole('row', { name: 'Ubuntu 22.04 LTS catalog data', exact: true }).first()
    await expect(syncedTitle.locator('.sw-os-image-os-family')).toHaveText('ubuntu')
    await expect(syncedTitle.locator('.sw-os-image-title strong')).toHaveText('Ubuntu 22.04 LTS')
    const identity = syncedData.locator('.sw-os-image-col--identity')
    await expect(identity).toContainText('amd64 · 4 GiB')
    await expect(identity.locator('.sw-image-id .sw-mono')).toHaveText('ubuntu/jammy')
    await expect(identity.locator('.sw-image-id > .sw-mono + button[aria-label="Copy image ID"]')).toHaveCount(1)
    await expect(syncedData.locator('.sw-os-image-col--default-user')).toHaveText('ubuntu')

    const customTitle = page.getByRole('row', { name: 'Ubuntu 24.04 ROCm image', exact: true }).first()
    const customData = page.getByRole('row', { name: 'Ubuntu 24.04 ROCm catalog data', exact: true }).first()
    await expect(customData.locator('.sw-os-image-col--identity')).toContainText('Unknown')
    await customTitle.getByRole('button', { name: 'Show details for Ubuntu 24.04 ROCm' }).click()
    await expect(page.getByRole('row', { name: /Artifact and source/ }).first()).toContainText('Catalog refreshed')
  })

  test('OS Images uploads a new image to the selected provisioner', async ({ page }) => {
    const uploads: string[] = []
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, { onOSImageUploadRequest: (integrationId) => uploads.push(integrationId) })

    await page.goto('/provisioning/images?site=site-a')
    await page.getByRole('button', { name: 'Upload image' }).click()

    // Site-a has two provisioners, so the target is chosen explicitly; the caller never sends a
    // custom flag — the provisioner classifies the uploaded image.
    await chooseSingleSelectOption(page, 'Provisioner integration', 'MAAS Taipei')
    await page.locator('#upload-image-name').fill('ubuntu-24.04-rocm')
    await page.locator('#upload-image-file').setInputFiles({
      name: 'image.tar.gz',
      mimeType: 'application/gzip',
      buffer: Buffer.from('fake-image-bytes'),
    })
    await page.getByRole('button', { name: 'Upload', exact: true }).click()

    await expect(page.getByText('OS image uploaded')).toBeVisible()
    await expect.poll(() => uploads).toEqual(['maas-a'])
  })

  test('template CRUD remains write-only and OS Images preserves partial provider results', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, { failImageIntegrationIds: ['maas-b'] })
    await page.goto('/provisioning/templates?site=site-a')
    await expect(page.getByRole('table', { name: 'Deployment templates' }).getByText('GPU compute baseline')).toBeVisible()
    await page.getByRole('button', { name: 'Create template' }).click()
    await chooseSingleSelectOption(page, 'Provisioner integration', 'MAAS Taipei')
    await page.getByLabel('Name').fill('Scale-out template')
    await chooseOSImage(page, 'Ubuntu 24.04 LTS')
    await expect(page.getByRole('radio', { name: 'RAM deploy' })).toBeChecked()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    const row = page.getByRole('row', { name: /Scale-out template/ })
    await expect(row).toBeVisible()
    await row.getByRole('button', { name: 'Add cloud-init' }).click()
    await page.getByLabel('New cloud-init').fill('#cloud-config\nusers: []')
    await page.getByRole('heading', { name: 'Replace cloud-init for Scale-out template' }).locator('..').getByRole('button', { name: 'Replace cloud-init' }).click()
    await expect(page.getByRole('row', { name: /Scale-out template/ })).toContainText('Configured')
    expect(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain('#cloud-config')

    await page.goto('/provisioning/images')
    await expect(page.getByText('MAAS Edge image catalog unavailable')).toBeVisible()
    await expect(page.getByRole('row', { name: 'Ubuntu 22.04 LTS image', exact: true }).first()).toBeVisible()
    await expectTableCellsVerticallyCentered(page, 'OS images')
  })

  test('Infrastructure exposes the Site hierarchy and OS Image provider sources', async ({ page }) => {
    await page.goto('/infrastructure/sites')
    await expect(page.getByRole('heading', { name: 'Infrastructure' })).toBeVisible()
    await expect(page.getByRole('tab', { name: 'Sites' })).toHaveAttribute('aria-selected', 'true')
    const taipei = page.getByRole('row', { name: /Taipei Lab/ })
    await expect(taipei).toContainText('Primary accelerator lab')
    await expect(taipei.getByRole('cell', { name: '2', exact: true })).toBeVisible()
    await taipei.getByRole('button', { name: 'Delete' }).click()
    await page.getByLabel('Site name confirmation').fill('Taipei Lab')
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete site' }).click()
    await expect(page.getByText('This site still has integrations. Delete them first.')).toBeVisible()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Cancel' }).click()

    await page.getByRole('tab', { name: 'Integrations' }).click()
    const maas = page.getByRole('row', { name: /MAAS Taipei/ })
    await expect(maas).toContainText('Taipei Lab')
    await expect(maas).toContainText('Provisioner')
    await expect(maas).toContainText('maas')

    await page.goto('/provisioning/images?site=site-a')
    await expect(page.getByRole('columnheader', { name: 'Source' })).toBeVisible()
    const imageSource = page.getByRole('row', { name: 'Ubuntu 22.04 LTS catalog data', exact: true }).filter({ hasText: 'MAAS Taipei' })
    await imageSource.getByRole('link', { name: 'MAAS Taipei' }).click()
    await expect(page).toHaveURL('/infrastructure/integrations?site=site-a#integration-maas-a')
    await expect(page.locator('#integration-maas-a')).toContainText('Taipei Lab')
  })

  test('Sites and Integrations can be configured without persisting credentials', async ({ page }) => {
    await page.goto('/infrastructure/sites')
    await page.getByRole('button', { name: 'Create site' }).click()
    const siteDialog = page.getByRole('dialog', { name: 'Create site' })
    await expect(siteDialog.getByRole('heading', { name: 'Create site' }).locator('..')).toHaveCSS('flex-direction', 'column')
    await expect(siteDialog).toHaveAccessibleDescription(/physical or logical infrastructure location/)
    await expect(page.getByLabel('Name')).toBeFocused()
    await page.getByLabel('Name').fill('Singapore DC')
    await page.getByLabel('Description').fill('Regional compute facility')
    await page.getByRole('dialog').getByRole('button', { name: 'Create site' }).click()
    await expect(page.getByRole('row', { name: /Singapore DC/ })).toBeVisible()

    await visibleSiteScope(page).click()
    await chooseMenuItem(page, 'Singapore DC')
    await expect(page).toHaveURL('/infrastructure/sites?site=site-3')
    await page.getByRole('tab', { name: 'Integrations' }).click()
    await expect(page).toHaveURL('/infrastructure/integrations?site=site-3')

    await page.locator('.sw-page-header').getByRole('button', { name: 'Create integration' }).click()
    await expect(page.getByRole('dialog').getByRole('combobox', { name: 'Site', exact: true })).toHaveText('Singapore DC')
    await page.getByLabel('Name').fill('MAAS Singapore')
    await page.getByLabel('Endpoint').fill('https://maas.sg.example')
    await page.getByLabel('MAAS API key').fill('consumer:token:initial-secret')
    await page.getByLabel('Request timeout').fill('45s')
    await page.getByRole('dialog').getByRole('button', { name: 'Create integration' }).click()

    // A new MAAS provisioner opens on its settings page, where the rest of its settings live.
    await expect(page).toHaveURL(/\/infrastructure\/integrations\/[^/]+\/general\?site=site-3$/)
    await expect(page.getByRole('heading', { level: 1, name: 'MAAS Singapore' })).toBeVisible()
    const general = page.getByRole('region', { name: 'General' })
    await expect(general.getByText('Singapore DC')).toBeVisible()
    await expect(general.getByRole('combobox')).toHaveCount(0)
    await page.getByRole('textbox', { name: /^Name/ }).fill('MAAS Singapore Primary')
    await page.getByRole('checkbox', { name: 'Enabled' }).locator('..').click()
    await page.getByRole('button', { name: 'Save changes' }).click()
    await expect(page.getByText('Integration updated')).toBeVisible()

    await page.getByRole('tab', { name: 'Credential' }).click()
    await page.getByLabel('New MAAS API key').fill('replacement-secret')
    await page.getByRole('button', { name: 'Replace credential' }).click()
    await expect(page.getByText('Credential replaced')).toBeVisible()
    expect(await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }))).not.toContain('replacement-secret')

    await page.getByRole('link', { name: 'Integrations' }).click()
    const integration = page.getByRole('row', { name: /MAAS Singapore Primary/ })
    await expect(integration).toContainText('Singapore DC')
    await expect(integration).toContainText('Paused')
    await expect(integration).toContainText('Configured')
    await integration.getByRole('button', { name: 'Delete' }).click()
    await page.getByLabel('Integration name confirmation').fill('MAAS Singapore Primary')
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete integration' }).click()
    await expect(page.getByRole('row', { name: /MAAS Singapore Primary/ })).toHaveCount(0)

    await page.getByRole('tab', { name: 'Sites' }).click()
    const site = page.getByRole('row', { name: /Singapore DC/ })
    await site.getByRole('button', { name: 'Delete' }).click()
    await page.getByLabel('Site name confirmation').fill('Singapore DC')
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete site' }).click()
    await expect(page).toHaveURL('/infrastructure/sites')
    await expect(page.getByRole('row', { name: /Singapore DC/ })).toHaveCount(0)

    await page.setViewportSize({ width: 390, height: 844 })
    await page.evaluate(() => localStorage.setItem('swallow.appearance', 'dark'))
    await page.goto('/infrastructure/integrations')
    await expect(page.locator('html')).toHaveClass(/dark/)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    await expect(page.locator('.sw-resource-card').first()).toBeVisible()
    await expect(page.locator('table[aria-label="Integrations"]')).toBeHidden()
  })

  test('keyboard can traverse primary navigation', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: 'Servers', exact: true }).focus()
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL('/servers')
  })
})
