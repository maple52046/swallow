import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

const visibleGlobalNavigation = (page: import('playwright/test').Page) => page.locator('button[aria-label="Global navigation"]:visible')
const visibleAppearance = (page: import('playwright/test').Page) => page.locator('button[aria-label^="Appearance:"]:visible')
const visibleSiteScope = (page: import('playwright/test').Page) => page.locator('button[aria-label^="Site scope:"]:visible')

async function chooseMenuItem(page: import('playwright/test').Page, name: string) {
  await page.getByRole('menuitem', { name, exact: true }).click()
}

async function chooseSingleSelectOption(page: import('playwright/test').Page, fieldLabel: string, optionLabel: string) {
  await page.getByLabel(fieldLabel, { exact: true }).click()
  await page.getByRole('option', { name: optionLabel, exact: true }).click()
}

/**
 * Opens a DOM-rendered PatternFly Select and verifies its disabled placeholder
 * is painted against a real menu surface before any pointer hover occurs.
 */
async function expectExpandedPlaceholderReadable(
  page: import('playwright/test').Page,
  fieldLabel: string,
  placeholderLabel: string,
) {
  await page.getByLabel(fieldLabel, { exact: true }).click()
  const placeholder = page.getByRole('option', { name: placeholderLabel, exact: true })
  await expect(placeholder).toBeVisible()
  await expect(placeholder).toBeDisabled()
  const paint = await placeholder.evaluate((element) => {
    const text = getComputedStyle(element).color
    let background = 'rgba(0, 0, 0, 0)'
    let current: Element | null = element
    while (current && (background === 'rgba(0, 0, 0, 0)' || background === 'transparent')) {
      background = getComputedStyle(current).backgroundColor
      current = current.parentElement
    }
    return { text, background }
  })
  expect(paint.background).not.toBe('rgba(0, 0, 0, 0)')
  expect(paint.background).not.toBe('transparent')
  expect(paint.text).not.toBe(paint.background)
}

/** Reads computed styles because PatternFly gives header, body, check, and action cells different defaults. */
async function expectTableCellsVerticallyCentered(page: import('playwright/test').Page, tableName: string) {
  const table = page.locator(`table[aria-label="${tableName}"]`)
  await expect(table).toBeVisible()
  const cells = table.locator('thead th, tbody td')
  await expect(cells.first()).toBeVisible()
  expect(await cells.evaluateAll((items) => items.every((item) => getComputedStyle(item).verticalAlign === 'middle'))).toBe(true)
}

async function expectMediumBlockSpacing(locator: import('playwright/test').Locator) {
  await expect(locator).toHaveCSS('padding-block-start', '16px')
  await expect(locator).toHaveCSS('padding-block-end', '16px')
}

test.beforeEach(async ({ page }, testInfo) => {
  await installApiFixtures(page)
  const appearance = 'light'
  const authenticated = !testInfo.title.includes('login')
  await page.addInitScript(({ appearance, authenticated }) => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', JSON.stringify(appearance))
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
      await expect(page.getByText('Swallow', { exact: true })).toHaveCount(1)
      await expect(page.getByLabel('Username')).toBeVisible()
      await expect(page.getByLabel('Password')).toBeVisible()
      await expect(page.locator('.pf-v6-c-login__main-header')).toHaveCount(1)
      await expect(page.locator('.pf-v6-c-login__main-body')).toHaveCount(1)

      const layout = await page.evaluate(() => {
        const main = document.querySelector<HTMLElement>('.pf-v6-c-login__main')
        const brand = document.querySelector<HTMLElement>('.sw-login-brand')
        if (!main || !brand) return null
        const mainRect = main.getBoundingClientRect()
        const brandRect = brand.getBoundingClientRect()
        return {
          hasHorizontalOverflow: document.documentElement.scrollWidth > window.innerWidth,
          mainFitsViewport: mainRect.left >= 0 && mainRect.right <= window.innerWidth,
          mainWidth: mainRect.width,
          brandHeight: brandRect.height,
        }
      })
      expect(layout).not.toBeNull()
      expect(layout?.hasHorizontalOverflow).toBe(false)
      expect(layout?.mainFitsViewport).toBe(true)
      expect(layout?.mainWidth).toBeGreaterThan(280)
      expect(layout?.brandHeight).toBeLessThan(56)
    }
  })

  test('site scope is URL-owned and detail switching returns to the list', async ({ page }) => {
    await page.goto('/')
    await visibleSiteScope(page).click()
    await chooseMenuItem(page, 'Taipei Lab')
    await expect(page).toHaveURL(/\?site=site-a$/)
    await page.getByText('1 failed operations in 24h').click()
    await expect(page).toHaveURL('/operations?status=failed&site=site-a')

    await page.goto('/servers/srv-1/summary?site=site-a')
    await visibleSiteScope(page).click()
    await chooseMenuItem(page, 'Hsinchu Edge')
    await expect(page).toHaveURL('/servers?site=site-b')

    await page.goto('/servers?site=missing')
    await expect(page).toHaveURL('/servers')
    await expect(page.getByText('Unknown Site')).toBeVisible()
  })

  test('PatternFly theme resolves system, manual modes, surface color, and persistence', async ({ page }) => {
    await page.goto('/')
    const lightSurface = await page.evaluate(() => getComputedStyle(document.body).backgroundColor)
    await expect(page.locator('html')).not.toHaveClass(/pf-v6-theme-dark/)

    await visibleAppearance(page).click(); await chooseMenuItem(page, 'Dark')
    await expect(page.locator('html')).toHaveClass(/pf-v6-theme-dark/)
    const darkSurface = await page.evaluate(() => getComputedStyle(document.body).backgroundColor)
    expect(darkSurface).not.toBe(lightSurface)
    await page.reload()
    await expect(page.locator('html')).toHaveClass(/pf-v6-theme-dark/)
    await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('swallow.appearance') ?? 'null'))).toBe('dark')

    await visibleAppearance(page).click(); await chooseMenuItem(page, 'System')
    await page.emulateMedia({ colorScheme: 'light' })
    await expect(page.locator('html')).not.toHaveClass(/pf-v6-theme-dark/)
    await page.emulateMedia({ colorScheme: 'dark' })
    await expect(page.locator('html')).toHaveClass(/pf-v6-theme-dark/)
    await visibleAppearance(page).click(); await chooseMenuItem(page, 'Light')
    await expect(page.locator('html')).not.toHaveClass(/pf-v6-theme-dark/)
  })

  test('Deploy OS renders its expanded Integration placeholder in dark mode', async ({ page }) => {
    await page.goto('/provisioning/deploy?site=site-a')
    await visibleAppearance(page).click()
    await chooseMenuItem(page, 'Dark')
    const wizard = page.locator('.sw-deploy-wizard')
    await expect(wizard).toHaveCSS('border-radius', '6px')
    await expect(wizard).toHaveCSS('overflow', 'hidden')
    const integration = page.getByLabel('Provisioner integration')
    await expect(integration).toContainText('Select an integration')
    await expect(page.locator('html')).toHaveCSS('color-scheme', 'dark')
    await expectExpandedPlaceholderReadable(page, 'Provisioner integration', 'Select an integration')
    await expect(page.getByRole('option', { name: 'MAAS Taipei', exact: true })).toBeVisible()
    await page.getByRole('option', { name: 'MAAS Taipei', exact: true }).click()
    await expect(integration).toContainText('MAAS Taipei')
  })

  test('Deploy Platform renders its expanded Site placeholder in dark mode', async ({ page }) => {
    await page.goto('/platforms/deploy')
    await visibleAppearance(page).click()
    await chooseMenuItem(page, 'Dark')

    const site = page.getByLabel('Site', { exact: true })
    await expect(site).toContainText('Select a Site')
    await expect(page.locator('html')).toHaveCSS('color-scheme', 'dark')
    await expectExpandedPlaceholderReadable(page, 'Site', 'Select a Site')
    await expect(page.getByRole('option', { name: 'Taipei Lab', exact: true })).toBeVisible()
    await page.getByRole('option', { name: 'Taipei Lab', exact: true }).click()

    await expect(page.getByLabel('Site', { exact: true })).toContainText('Taipei Lab')
    await expect(page.getByLabel('Platform name')).toBeVisible()
  })

  test('shared tables center cells and separators keep PatternFly spacing', async ({ page }) => {
    await page.goto('/')
    await expectTableCellsVerticallyCentered(page, 'Recent operations')
    await expect(page.locator('.sw-page-header')).toHaveCSS('padding-block-end', '16px')
    await expect(page.locator('.sw-page-header')).toHaveCSS('align-items', 'center')

    await page.goto('/servers?site=site-a')
    await expectTableCellsVerticallyCentered(page, 'Servers')

    await page.goto('/platforms?site=site-a')
    await expectTableCellsVerticallyCentered(page, 'Platforms')

    await page.goto('/monitoring?site=site-a')
    await expectTableCellsVerticallyCentered(page, 'Monitoring alerts')
    await expectMediumBlockSpacing(page.locator('.sw-data-toolbar').first())
    await expect(page.locator('.sw-data-toolbar').first().locator('.pf-v6-c-toolbar__content-section')).toHaveCSS('align-items', 'center')

    await page.goto('/operations?site=site-a')
    await expectTableCellsVerticallyCentered(page, 'Operations')

    await page.goto('/infrastructure/sites?site=site-a')
    await expectTableCellsVerticallyCentered(page, 'Sites')

    await page.goto('/infrastructure/integrations?site=site-a')
    await expectTableCellsVerticallyCentered(page, 'Integrations')

    await page.goto('/operations/op-running?site=site-a')
    await page.getByRole('tab', { name: 'Details' }).click()
    const metadataRow = page.locator('.sw-key-value-grid > div').first()
    await expect(metadataRow).toHaveCSS('align-items', 'center')
    await expectMediumBlockSpacing(metadataRow)
    await expect(page.locator('.sw-operation-debugger .sw-tab-content:visible')).toHaveCSS('padding-top', '24px')

    await installApiFixtures(page, { freePlatformCandidates: true })
    await page.goto('/platforms/deploy?site=site-a')
    await page.getByLabel('Platform name').fill('alignment-audit')
    await page.getByRole('button', { name: 'Next' }).click()
    for (const server of ['gpu-node-01', 'gpu-node-02', 'gpu-node-03']) {
      await page.getByLabel(`Role for ${server}`).selectOption('control-plane')
    }
    await page.getByLabel('Role for gpu-node-04').selectOption('worker')
    await expectTableCellsVerticallyCentered(page, 'Deployable Servers')
    await page.getByRole('button', { name: 'Next' }).click()
    await page.getByLabel('API virtual IP').fill('192.168.40.200')

    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/monitoring?site=site-a')
    await expect(page.locator('.sw-page-header')).toHaveCSS('align-items', 'flex-start')
    await expectMediumBlockSpacing(page.locator('.sw-data-toolbar').first())
  })

  test('shared surfaces are rounded and hyperlinks stay undecorated', async ({ page }) => {
    await page.goto('/')

    await expect(page.locator('.sw-stat-strip')).toHaveCSS('border-radius', '6px')
    await expect(page.locator('.sw-section').first()).toHaveCSS('border-radius', '6px')

    const link = page.getByRole('link', { name: 'View all' }).first()
    await expect(link).toHaveCSS('text-decoration-line', 'none')
    await expect(link).toHaveCSS('text-decoration-style', 'solid')
    await link.hover()
    await expect(link).toHaveCSS('text-decoration-line', 'none')
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur())
    for (let index = 0; index < 30 && !await link.evaluate((element) => element === document.activeElement); index += 1) {
      await page.keyboard.press('Tab')
    }
    await expect(link).toBeFocused()
    await expect(link).toHaveCSS('text-decoration-line', 'none')
    await expect(link).toHaveCSS('outline-style', 'solid')
    await expect(link).toHaveCSS('outline-width', '2px')

    const expectPlainListToolbar = async () => {
      const toolbar = page.locator('.sw-data-toolbar')
      await expect(toolbar).toHaveClass(/sw-data-toolbar--plain/)
      await expect(toolbar).toHaveCSS('border-bottom-style', 'none')
      await expect(toolbar).toHaveCSS('border-radius', '0px')
      await expect(toolbar).toHaveCSS('box-shadow', 'none')
      const surfaces = await toolbar.evaluate((element) => ({
        toolbar: getComputedStyle(element).backgroundColor,
        page: getComputedStyle(document.body).backgroundColor,
      }))
      expect(surfaces.toolbar).toBe(surfaces.page)
    }

    for (const route of ['/servers?site=site-a', '/operations?site=site-a', '/provisioning/templates?site=site-a', '/provisioning/images?site=site-a', '/infrastructure/sites?site=site-a', '/infrastructure/integrations?site=site-a']) {
      await page.goto(route)
      await expectPlainListToolbar()
    }

    await page.goto('/servers?site=site-a')
    await expect(page.locator('.sw-table-frame')).toHaveCSS('border-radius', '6px')

    await page.goto('/operations/op-running?site=site-a')
    await expect(page.locator('.sw-operation-timeline')).toHaveCSS('border-radius', '6px')
    await expect(page.locator('.sw-operation-debugger')).toHaveCSS('border-radius', '6px')
  })

  test('Monitoring headings and descriptions sit above their data containers', async ({ page }) => {
    await page.goto('/monitoring?site=site-a')

    for (const [title, description, searchLabel] of [
      ['Alerts', 'Firing and suppressed alerts from Alertmanager, ordered by provider severity.', 'Search alerts'],
      ['Server metrics', 'Current named metrics only. Missing samples remain No data; history belongs in Grafana.', 'Search server metrics'],
    ]) {
      const heading = page.getByRole('heading', { name: title, exact: true })
      const group = page.locator('.sw-section-group').filter({ has: heading })
      const header = group.locator(':scope > .sw-section-header--plain')
      const container = group.locator(':scope > .sw-section')
      await expect(group.getByText(description, { exact: true })).toBeVisible()
      await expect(container.getByLabel(searchLabel)).toBeVisible()
      await expect(container.locator('.sw-data-toolbar')).not.toHaveClass(/sw-data-toolbar--plain/)
      expect(await heading.evaluate((element) => element.closest('.sw-section') === null)).toBe(true)
      const positions = await Promise.all([header.boundingBox(), container.boundingBox()])
      expect(positions[0]).not.toBeNull()
      expect(positions[1]).not.toBeNull()
      expect((positions[0]?.y ?? 0) + (positions[0]?.height ?? 0)).toBeLessThan(positions[1]?.y ?? 0)
    }
  })

  test('desktop dock collapses to icons with tooltip and persists', async ({ page }) => {
    await page.goto('/')
    const masthead = page.locator('#swallow-docked-masthead')
    const brand = masthead.getByRole('link', { name: 'Swallow home' })
    const toggle = masthead.getByRole('button', { name: 'Global navigation' })
    const dock = page.locator('.pf-v6-c-page__dock')
    await expect.poll(() => dock.evaluate((element) => Math.round(element.getBoundingClientRect().width))).toBe(186)
    const logo = brand.locator('svg')
    await expect(logo).toBeVisible()
    await expect(logo).toHaveAttribute('viewBox', '0 0 104 88')
    const logoBox = await logo.boundingBox()
    if (!logoBox) throw new Error('Swallow logo is not measurable')
    expect(logoBox.width).toBeGreaterThan(logoBox.height)
    await expect(masthead.locator('.pf-v6-c-divider')).toHaveCount(0)
    expect(await brand.evaluate((element) => getComputedStyle(element).textDecorationLine)).toBe('none')
    const brandBox = await brand.boundingBox()
    const toggleBox = await toggle.boundingBox()
    if (!brandBox || !toggleBox) throw new Error('Docked brand row is not measurable')
    const brandCenter = brandBox.y + brandBox.height / 2
    const toggleCenter = toggleBox.y + toggleBox.height / 2
    expect(Math.abs(brandCenter - toggleCenter)).toBeLessThan(2)
    expect(brandBox.x + brandBox.width).toBeLessThan(toggleBox.x)
    await visibleGlobalNavigation(page).click()
    await expect(dock).not.toHaveClass(/pf-m-text-expanded/)
    await expect.poll(() => dock.evaluate((element) => Math.round(element.getBoundingClientRect().width))).toBe(64)
    await expect.poll(() => page.evaluate(() => localStorage.getItem('swallow.shell.sidebar-collapsed'))).toBe('true')
    await dock.getByRole('link', { name: 'Servers', exact: true }).hover()
    await expect(page.getByRole('tooltip', { name: 'Servers' })).toBeVisible()
    await page.reload()
    await expect(dock).not.toHaveClass(/pf-m-text-expanded/)
  })

  test('authenticated body uses a flat full-width workspace instead of a container panel', async ({ page }) => {
    await page.setViewportSize({ width: 1920, height: 900 })
    await page.goto('/')

    const dock = page.locator('.pf-v6-c-page__dock')
    const mainContainer = page.locator('.pf-v6-c-page__main-container')
    const section = page.locator('.sw-page-section')
    const body = page.locator('.operator-page')
    await expect(page.locator('.sw-page-content')).toHaveCount(0)

    const measureWorkspace = () => page.evaluate(() => {
      const dockElement = document.querySelector<HTMLElement>('.pf-v6-c-page__dock')
      const mainElement = document.querySelector<HTMLElement>('.pf-v6-c-page__main-container')
      const sectionElement = document.querySelector<HTMLElement>('.sw-page-section')
      const bodyElement = document.querySelector<HTMLElement>('.operator-page')
      if (!dockElement || !mainElement || !sectionElement || !bodyElement) return null
      const dockRect = dockElement.getBoundingClientRect()
      const mainRect = mainElement.getBoundingClientRect()
      const sectionRect = sectionElement.getBoundingClientRect()
      const bodyRect = bodyElement.getBoundingClientRect()
      return {
        dockRight: Math.round(dockRect.right),
        mainLeft: Math.round(mainRect.left),
        mainRight: Math.round(mainRect.right),
        mainTop: Math.round(mainRect.top),
        sectionLeft: Math.round(sectionRect.left),
        sectionRight: Math.round(sectionRect.right),
        contentLeftInset: Math.round(bodyRect.left - sectionRect.left),
        contentRightInset: Math.round(sectionRect.right - bodyRect.right),
        bodyWidth: Math.round(bodyRect.width),
        hasHorizontalOverflow: document.documentElement.scrollWidth > window.innerWidth,
      }
    })

    await expect(mainContainer).toHaveCSS('margin-left', '0px')
    await expect(mainContainer).toHaveCSS('margin-right', '0px')
    await expect(mainContainer).toHaveCSS('border-radius', '0px')
    await expect(mainContainer).toHaveCSS('box-shadow', 'none')
    await expect(section).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
    await expect(section).toHaveCSS('padding-left', '24px')
    await expect(section).toHaveCSS('padding-right', '24px')
    await expect(body).toBeVisible()
    const expanded = await measureWorkspace()
    expect(expanded).not.toBeNull()
    expect(expanded?.mainLeft).toBe(expanded?.dockRight)
    expect(expanded?.mainRight).toBe(1920)
    expect(expanded?.mainTop).toBe(0)
    expect(expanded?.sectionLeft).toBe(expanded?.mainLeft)
    expect(expanded?.sectionRight).toBe(expanded?.mainRight)
    expect(expanded?.contentLeftInset).toBe(24)
    expect(expanded?.contentRightInset).toBe(24)
    expect(expanded?.bodyWidth).toBeGreaterThan(1600)
    expect(expanded?.hasHorizontalOverflow).toBe(false)

    await dock.getByRole('button', { name: 'Global navigation' }).click()
    const collapsed = await measureWorkspace()
    expect(collapsed).not.toBeNull()
    expect(collapsed?.mainLeft).toBe(collapsed?.dockRight)
    expect(collapsed?.mainRight).toBe(1920)
    expect(collapsed?.contentLeftInset).toBe(24)
    expect(collapsed?.contentRightInset).toBe(24)
    expect(collapsed?.bodyWidth).toBeGreaterThan((expanded?.bodyWidth ?? 0) + 100)
    expect(collapsed?.hasHorizontalOverflow).toBe(false)
  })

  test('mobile drawer traps focus, closes with Escape, and restores the toggle', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/')
    const toggle = visibleGlobalNavigation(page)
    const brand = page.locator('#swallow-mobile-masthead').getByRole('link', { name: 'Swallow home' })
    await expect(brand).toContainText('Swallow')
    await expect(brand.locator('..')).toHaveCSS('border-bottom-style', 'none')
    const brandBox = await brand.boundingBox()
    const toggleBox = await toggle.boundingBox()
    if (!brandBox || !toggleBox) throw new Error('Mobile brand row is not measurable')
    expect(Math.abs((brandBox.y + brandBox.height / 2) - (toggleBox.y + toggleBox.height / 2))).toBeLessThan(2)
    await toggle.click()
    const dock = page.locator('.pf-v6-c-page__dock')
    await expect(dock).toBeVisible()
    await expect.poll(() => dock.evaluate((element) => element.contains(document.activeElement))).toBe(true)
    await page.keyboard.press('Escape')
    await expect(dock).toBeHidden()
    await expect(toggle).toBeFocused()
  })

  test('NetBox views, columns, selection, and MAAS actions work together', async ({ page }) => {
    await page.goto('/servers?site=site-a')
    await page.getByLabel('Select all on this page').click()
    const table = page.getByRole('grid', { name: 'Servers' })
    await expect(table.getByRole('columnheader', { name: 'Deployment' })).toBeVisible()
    await expect(table.getByRole('columnheader', { name: 'MAC address' })).toBeVisible()
    await expect(table.getByRole('columnheader', { name: 'Zone', exact: true })).toBeVisible()
    await expect(table.getByRole('columnheader', { name: 'Pool', exact: true })).toBeVisible()
    for (const heading of ['Architecture', 'CPU cores', 'CPU model', 'Memory', 'Storage', 'System vendor', 'System product']) {
      await expect(table.getByRole('columnheader', { name: heading, exact: true })).toHaveCount(1)
    }
    for (const [heading, minimumWidth] of Object.entries({ 'CPU cores': 112, Memory: 104, Storage: 112, 'System vendor': 160 })) {
      expect((await table.getByRole('columnheader', { name: heading, exact: true }).boundingBox())?.width).toBeGreaterThanOrEqual(minimumWidth)
    }
    await expect(table.getByRole('columnheader', { name: 'Hardware', exact: true })).toHaveCount(0)
    const firstRow = table.getByRole('row').filter({ hasText: 'gpu-node-01' }).first()
    await expect(firstRow.locator('td[data-label="Deployment"]')).toHaveText('Deployed')
    const selectionCell = firstRow.locator('td[data-label="Selection"]')
    const powerCell = firstRow.locator('td[data-label="Power"]')
    await expect(selectionCell).toHaveCSS('text-align', 'center')
    await expect(selectionCell).toHaveCSS('vertical-align', 'middle')
    await expect(selectionCell).toHaveCSS('position', 'sticky')
    await expect(powerCell).toHaveCSS('text-align', 'center')
    expect((await powerCell.boundingBox())?.width).toBeGreaterThanOrEqual(96)
    await expect(powerCell).toHaveCSS('vertical-align', 'middle')
    expect(await firstRow.locator('td').evaluateAll((cells) => cells.every((cell) => getComputedStyle(cell).verticalAlign === 'middle'))).toBe(true)
    await expect(firstRow.locator('td[data-label="Machine"]')).not.toContainText('02:00:00:00:00:01')
    await expect(firstRow.locator('td[data-label="MAC address"]')).toHaveText('02:00:00:00:00:01')
    await expect(firstRow.locator('td[data-label="Zone"]')).toHaveText('rack-a')
    await expect(firstRow.locator('td[data-label="Pool"]')).toHaveText('accelerators')
    await expect(firstRow.locator('td[data-label="Architecture"]')).toHaveText('amd64/generic')
    await expect(firstRow.locator('td[data-label="CPU cores"]')).toHaveText('64')
    await expect(firstRow.locator('td[data-label="CPU model"]')).toHaveText('AMD EPYC 9554')
    await expect(firstRow.locator('td[data-label="Memory"]')).toHaveText('512 GiB')
    await expect(firstRow.locator('td[data-label="Storage"]')).toHaveText('3840 GB')
    await expect(firstRow.locator('td[data-label="System vendor"]')).toHaveText('Supermicro')
    await expect(firstRow.locator('td[data-label="System product"]')).toHaveText('AS-8125GS-TNHR')
    expect(parseFloat(await firstRow.locator('.sw-tag-list').evaluate((element) => getComputedStyle(element).columnGap))).toBeGreaterThan(0)
    await expect(firstRow.locator('td[data-label="GPUs"]')).toContainText('8 x')
    const vendorLogo = firstRow.getByRole('img', { name: 'AMD GPU vendor logo' })
    await expect(vendorLogo).toBeVisible()
    await vendorLogo.focus()
    await expect(page.getByRole('tooltip')).toHaveText('AMD MI300X')
    const missingRow = table.getByRole('row').filter({ hasText: 'gpu-node-04' }).first()
    await expect(missingRow.locator('td[data-label="Deployment"]')).toHaveText('Failed')
    for (const label of ['Address', 'MAC address', 'Zone', 'Pool', 'Tags', 'Architecture', 'CPU cores', 'CPU model', 'Memory', 'Storage', 'System vendor', 'System product', 'GPUs']) {
      await expect(missingRow.locator(`td[data-label="${label}"]`)).toHaveText('-')
    }
    await expect(page.getByText('4 selected')).toBeVisible()
    await page.getByRole('button', { name: 'Take action' }).click()
    await expect(page.getByRole('menuitem', { name: 'Power on' })).toBeVisible()
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: 'Clear', exact: true }).click()

    await page.getByLabel('Configure columns').click()
    await page.getByLabel('GPUs').uncheck()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('columnheader', { name: 'GPUs' })).toHaveCount(0)

    await page.getByRole('button', { name: /Saved views/ }).click()
    await chooseMenuItem(page, 'Save current view')
    await page.getByRole('textbox', { name: 'Name' }).fill('Compute view')
    await page.getByRole('button', { name: 'Save view' }).click()
    await page.getByRole('button', { name: /Saved views/ }).click()
    await expect(page.getByText('Compute view', { exact: true })).toBeVisible()
    await page.getByLabel('Rename Compute view').click()
    await page.getByRole('textbox', { name: 'Name' }).fill('Operators')
    await page.getByRole('button', { name: 'Rename' }).click()
    await page.getByRole('button', { name: /Saved views/ }).click()
    await page.getByLabel('Delete Operators').click()
    await page.getByRole('button', { name: /Saved views/ }).click()
    await expect(page.getByText('Operators', { exact: true })).toHaveCount(0)
  })


  test('legacy composed visibility migrates to independent columns', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('swallow.servers.hidden-columns', JSON.stringify(['placement', 'hardware'])))
    await page.goto('/servers?site=site-a')
    const table = page.getByRole('grid', { name: 'Servers' })
    await expect(table.getByRole('columnheader', { name: 'Zone', exact: true })).toHaveCount(0)
    await expect(table.getByRole('columnheader', { name: 'Pool', exact: true })).toHaveCount(0)
    for (const heading of ['Architecture', 'CPU cores', 'CPU model', 'Memory', 'Storage', 'System vendor', 'System product']) {
      await expect(table.getByRole('columnheader', { name: heading, exact: true })).toHaveCount(0)
    }
    await expect(table.getByRole('columnheader', { name: 'Address', exact: true })).toBeVisible()
  })
  test('Cockpit machine detail retains tabs and real action controls', async ({ page }) => {
    await page.goto('/servers/srv-1/summary?site=site-a')
    await expect(page.getByText('Power and provisioning')).toBeVisible()
    await expect(page.getByText('Hardware inventory')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Take action' })).toBeVisible()
    await page.getByRole('tab', { name: 'Network' }).click()
    await expect(page).toHaveURL('/servers/srv-1/network?site=site-a')
    await expect(page.getByText('eno1')).toBeVisible()
    await page.getByRole('tab', { name: 'PCI devices' }).click()
    await expect(page.getByText('03:00.0')).toBeVisible()
  })

  test('Server headline reports failed verification while retaining provider OS detail', async ({ page }) => {
    await page.goto('/servers?site=site-a')
    const row = page.getByRole('row').filter({ hasText: 'gpu-node-04' })
    await expect(row.getByText('Failed', { exact: true })).toBeVisible()
    await expect(row.getByText('Deployment unverified', { exact: true })).toHaveCount(0)

    await page.goto('/servers/srv-4/summary?site=site-a')
    await expect(page.getByRole('heading', { name: 'Operating system deployment failed' })).toBeVisible()
    await expect(page.getByText('No provider address was observed after OS installation.')).toBeVisible()
    const statusCard = page.locator('.pf-v6-c-card').filter({
      has: page.getByText('Power and provisioning', { exact: true }),
    })
    await expect(statusCard.getByText('deployed', { exact: true })).toBeVisible()
    await expect(statusCard.getByText('Power', { exact: true })).toBeVisible()
    await expect(statusCard.getByText('Deployed OS', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'View operation' })).toBeVisible()
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

    const table = page.getByRole('grid', { name: 'Server network interfaces' })
    const row = table.getByRole('row', { name: /eno1/ })
    await expect(row).toContainText('Provider-managed (MAAS AUTO)')
    await expect(row.getByRole('gridcell', { name: 'up' })).toBeVisible()
    await row.getByRole('button', { name: 'Configure' }).click()

    const editor = page.getByRole('dialog', { name: 'Configure eno1' })
    await expect(editor.getByRole('button', { name: 'DHCP' })).toBeVisible()
    await expect(editor.getByRole('button', { name: 'Static' })).toBeVisible()
    await expect(editor.getByRole('button', { name: 'Link only' })).toBeVisible()
    await expect(editor.getByText(/Keep current|AUTO/)).toHaveCount(0)
    await editor.getByRole('button', { name: 'Static' }).click()
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
    await page.goto('/servers/srv-1/summary?site=site-a')
    await page.getByRole('button', { name: 'Take action' }).click()
    await chooseMenuItem(page, 'Delete server')

    const dialog = page.getByRole('dialog', { name: 'Delete server' })
    await expect(dialog).toContainText("provisioner's Machine")
    const submit = dialog.getByRole('button', { name: 'Delete server' })
    await expect(submit).toBeDisabled()
    await dialog.getByLabel('Server name confirmation').fill('gpu-node-01')
    await submit.click()

    await expect(page).toHaveURL('/servers?site=site-a')
    await expect(page.getByRole('grid', { name: 'Servers' }).getByText('gpu-node-01', { exact: true })).toHaveCount(0)
  })

  test('Platform wizard explains and excludes Servers already claimed by Platforms', async ({ page }) => {
    await page.goto('/platforms/deploy?site=site-a')
    await page.getByLabel('Platform name').fill('protected-targets')
    await page.getByRole('button', { name: 'Next' }).click()

    await expect(page.getByText('Some Servers are already assigned')).toBeVisible()
    const table = page.getByRole('grid', { name: 'Deployable Servers' })
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

  test('PatternFly wizard selects machines before networking and keeps Platform context', async ({ page }) => {
    await installApiFixtures(page, { freePlatformCandidates: true })
    await page.goto('/platforms/deploy?site=site-a')
    const next = page.getByRole('button', { name: 'Next' })
    await expect(next).toBeDisabled()
    await page.getByLabel('Platform name').fill('compute-k0s')
    await next.click()
    await expect(page.getByRole('heading', { name: 'Topology and machines' })).toBeVisible()
    for (const server of ['gpu-node-01', 'gpu-node-02', 'gpu-node-03']) {
      await page.getByLabel(`Role for ${server}`).selectOption('control-plane')
    }
    await page.getByLabel('Role for gpu-node-04').selectOption('worker')
    await next.click()
    await expect(page.getByRole('heading', { name: 'Selected machine addresses', exact: true })).toBeVisible()
    await page.getByLabel('API virtual IP').fill('192.168.40.200')
    await next.click()
    await expect(page.getByText('3 control-plane, 1 workload-capable')).toBeVisible()
    await page.getByRole('button', { name: 'Back' }).click()
    await expect(page.getByLabel('API virtual IP')).toHaveValue('192.168.40.200')
    await next.click()
    await page.getByRole('button', { name: 'Deploy platform' }).click()
    await expect(page).toHaveURL('/platforms/platform-new?site=site-a')
    await expect(page.getByText('Platform deployment is running')).toBeVisible()
    await expect(page.getByRole('button', { name: 'View automation details' })).toBeVisible()
  })

  test('Platform wizard supports standalone and non-HA multi-node without a VIP', async ({ page }) => {
    await installApiFixtures(page, { freePlatformCandidates: true })
    await page.goto('/platforms/deploy?site=site-a')
    await page.getByLabel('Platform name').fill('edge-k0s')
    await page.getByRole('button', { name: 'Next' }).click()
    await page.locator('#platform-topology').selectOption('standalone')
    await page.getByLabel('Role for gpu-node-01').selectOption('control-plane')
    await page.getByRole('button', { name: 'Next' }).click()
    await expect(page.getByLabel('API virtual IP')).toHaveCount(0)
    await expect(page.getByText(/192\.168\.40\.21:6443/)).toBeVisible()

    await page.getByRole('button', { name: 'Back' }).click()
    await page.locator('#platform-topology').selectOption('multi-node')
    await page.getByLabel('Role for gpu-node-01').selectOption('control-plane')
    await page.getByLabel('Role for gpu-node-02').selectOption('worker')
    await page.getByRole('button', { name: 'Next' }).click()
    await expect(page.getByLabel('API virtual IP')).toHaveCount(0)
    await expect(page.getByText('Direct control-plane endpoint')).toBeVisible()
  })

  test('Headlamp language is type-aware and Slurm stays neutral', async ({ page }) => {
    await page.goto('/platforms/platform-a?site=site-a')
    const kubernetesStats = page.locator('.sw-stat-strip')
    await expect(kubernetesStats.getByText('Topology', { exact: true })).toBeVisible()
    await expect(kubernetesStats.getByText('High availability', { exact: true })).toBeVisible()
    await expect(kubernetesStats.getByText('Control-plane', { exact: true })).toBeVisible()
    await expect(kubernetesStats.getByText('Workload-capable', { exact: true })).toBeVisible()
    await expect(kubernetesStats.getByText('1 also runs workloads', { exact: true })).toBeVisible()
    await expect(
      page.getByRole('grid', { name: 'Platform members' }).getByRole('row', { name: /gpu-node-01/ }),
    ).toContainText('Runs workloads')
    await expect(page.getByText(/Kubernetes membership/)).toBeVisible()
    await page.goto('/platforms/platform-slurm?site=site-a')
    await expect(page.getByText('Managers')).toBeVisible()
    await expect(page.getByText('Compute members')).toBeVisible()
    await expect(page.getByText(/Kubernetes/)).toHaveCount(0)
  })

  test('standalone Platform summary shows one control-plane is workload-capable', async ({ page }) => {
    await page.goto('/platforms/platform-b?site=site-a')
    const stats = page.locator('.sw-stat-strip')
    await expect(stats.locator('.sw-stat').filter({ hasText: 'Topology' })).toContainText('Standalone')
    await expect(stats.locator('.sw-stat').filter({ hasText: 'Control-plane' }))
      .toContainText('1 also runs workloads')
    await expect(stats.locator('.sw-stat').filter({ hasText: 'Workload-capable' }))
      .toContainText('1')
  })

  test('Platform list uses backend lifecycle labels and keeps Slurm uninstall disabled', async ({ page }) => {
    await page.goto('/platforms?site=site-a')
    const table = page.getByRole('grid', { name: 'Platforms' })
    await expect(table.getByRole('row', { name: /production-k0s/ })).toContainText('Active')
    await expect(table.getByRole('row', { name: /edge-staging/ })).toContainText('Deployment failed')
    await expect(table.getByRole('row', { name: /research-slurm/ })).toContainText('Registered')

    await page.goto('/platforms/platform-slurm?site=site-a')
    await page.getByRole('button', { name: 'Platform actions' }).click()
    const uninstall = page.getByRole('menuitem', { name: /Uninstall platform/ })
    await expect(uninstall).toBeDisabled()
    await expect(page.getByText('Only Kubernetes platforms can be uninstalled.')).toBeVisible()
  })

  test('failed Platform repairs from its detail workflow and remains in lifecycle progress', async ({ page }) => {
    await page.goto('/platforms/platform-b?site=site-a')
    await expect(page.getByText('Platform deployment failed')).toBeVisible()
    await expect(page.getByText(/no provider address was observed/)).toBeVisible()
    await expect(page.getByText(/k0s installation did not start/)).toBeVisible()

    await page.getByRole('button', { name: 'View automation details' }).click()
    await expect(page).toHaveURL('/operations/op-deploy-failed?site=site-a')
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
    await expect(
      page.getByRole('grid', { name: 'Related operations' })
        .getByRole('row', { name: /Deploy edge-staging k0s platform/ })
        .first(),
    ).toContainText('running')
  })

  test('missing-address OS Step retry requires release and redeploy confirmation', async ({ page }) => {
    await page.goto('/operations/op-deploy-failed?site=site-a')
    const row = page.getByRole('row', { name: /Provision and verify operating system on srv-4/ })
    await row.getByRole('button', { name: /Retry Provision and verify/ }).click()
    const dialog = page.getByRole('dialog', { name: 'Retry failed OS deployment' })
    await expect(dialog.getByText('This retry may redeploy the Server')).toBeVisible()
    await expect(dialog.getByText(/same image, network settings, and protected cloud-init/)).toBeVisible()
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    await expect(dialog).toBeHidden()
  })

  test('typed Uninstall and Delete confirmations keep host and record actions separate', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/platforms/platform-a?site=site-a')
    await page.getByRole('button', { name: 'Platform actions' }).click()
    await page.getByRole('menuitem', { name: 'Uninstall platform', exact: true }).click()

    const dialog = page.getByRole('dialog', { name: 'Uninstall platform' })
    const confirmation = dialog.getByLabel('Platform name confirmation')
    await expect(confirmation).toBeFocused()
    await expect(dialog.locator('p').filter({ hasText: 'Targets: 3 original deployment targets' })).toBeVisible()
    await expect(dialog.getByText(/operating system, user data, and shared packages/)).toBeVisible()
    const uninstall = dialog.getByRole('button', { name: 'Uninstall platform' })
    await expect(uninstall).toBeDisabled()
    await confirmation.fill('wrong-name')
    await expect(uninstall).toBeDisabled()
    await confirmation.fill('production-k0s')
    await uninstall.click()
    await expect(page).toHaveURL('/operations/op-uninstall?site=site-a')
    await expect(page.getByText('Platform uninstall accepted')).toBeVisible()

    await page.goto('/platforms/platform-a?site=site-a')
    await page.getByRole('button', { name: 'Platform actions' }).click()
    await page.getByRole('menuitem', { name: 'Delete platform', exact: true }).click()
    const deleteDialog = page.getByRole('dialog', { name: 'Delete platform' })
    await expect(deleteDialog.getByText('Hosts will not be uninstalled')).toBeVisible()
    await expect(deleteDialog.getByText(/Accepted operations will also continue/)).toBeVisible()
    await deleteDialog.getByLabel('Platform name confirmation').fill('production-k0s')
    await deleteDialog.getByRole('button', { name: 'Delete platform' }).click()
    await expect(page).toHaveURL('/platforms?site=site-a')
    await expect(page.getByText('Platform deleted')).toBeVisible()
    await expect(page.getByRole('row', { name: /production-k0s/ })).toHaveCount(0)
  })


  test('AWX stdout is first and supports search, navigation, copy, download, events, and retry', async ({ page }) => {
    await page.goto('/operations?site=site-a')
    await page.getByLabel('Filter by status').selectOption('failed')
    await expect(page).toHaveURL(/status=failed/)
    await page.goto('/operations/op-running?site=site-a')
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
    await expect(page.getByRole('heading', { name: /Stdout copied/ })).toBeVisible()
    const download = page.waitForEvent('download')
    await page.getByLabel('Download stdout').click()
    expect((await download).suggestedFilename()).toBe('swallow-operation-op-running.log')
    await page.getByRole('tab', { name: 'Events' }).click()
    await page.getByLabel('Filter events by status').selectOption('failed')
    await expect(page.getByRole('gridcell', { name: 'Start controller' })).toBeVisible()
    await page.goto('/operations/op-failed?site=site-a')
    await page.getByRole('button', { name: 'Retry' }).click()
    await expect(page).toHaveURL('/operations/op-retry?site=site-a')
  })

  test('Monitoring filters, acknowledges, and exposes the provider Grafana link', async ({ page }) => {
    await page.goto('/monitoring?site=site-a')
    const grafana = page.getByRole('link', { name: 'Open Grafana' })
    await expect(grafana).toHaveAttribute('href', 'https://grafana.example')
    await expect(grafana).toHaveAttribute('target', '_blank')
    await page.getByLabel('Filter alert severity').selectOption('critical')
    await expect(page).toHaveURL(/severity=critical/)
    await expect(page.getByText('NodeDown')).toBeVisible()
    await page.getByRole('row', { name: /NodeDown/ }).getByRole('button', { name: 'Acknowledge' }).click()
    await page.getByLabel('Comment').fill('Investigating host power')
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
    await expect(page.getByRole('gridcell', { name: '30.0%' }).first()).toBeVisible()
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
    await expect(page.getByRole('row', { name: /NodeDown/ })).toBeVisible()
  })

  test('Server Detail opens the standalone OS deployment workflow with its target', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, { readyServerCount: 1 })
    await page.goto('/servers/srv-1/summary?site=site-a')
    await expect(page.getByText('Deploy operating system')).toHaveCount(0)
    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()
    await expect(page).toHaveURL(/\/provisioning\/deploy\?.*site=site-a.*serverId=srv-1/)
    await expect(page.getByText('1 of 100 selected')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Next' })).toBeEnabled()
  })

  test('MAAS network readiness blocks deployment before provider dispatch', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    let deploymentRequests = 0
    await installApiFixtures(page, {
      readyServerCount: 1,
      deploymentReadinessIssues: {
        'srv-1': "No MAAS interface is linked to a subnet. Configure the machine's Network in MAAS, then check deployment readiness again.",
      },
      onDeploymentRequest: () => { deploymentRequests += 1 },
    })

    await page.goto('/provisioning/deploy?site=site-a&serverId=srv-1')
    await page.getByRole('button', { name: 'Next' }).click()

    await expect(page.getByText('gpu-node-01: Deployment blocked')).toBeVisible()
    await expect(page.getByText(/No MAAS interface is linked to a subnet/)).toBeVisible()
    await expect(page.getByRole('button', { name: 'Check again' })).toBeVisible()
    await expect(page.getByLabel('Configuration source')).toHaveCount(0)
    expect(deploymentRequests).toBe(0)

    await page.getByRole('button', { name: 'Review Server Network' }).click()
    await expect(page).toHaveURL('/servers/srv-1/network?site=site-a')
  })

  test('multi-node deploy preserves targets, customizes a template, and returns to Servers', async ({ page }) => {
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
    await page.getByRole('button', { name: 'Deploy OS' }).click()
    await expect(page).toHaveURL(/serverId=srv-1.*serverId=srv-2/)
    await expect(page.getByText('2 of 100 selected')).toBeVisible()

    await page.getByRole('button', { name: 'Next' }).click()
    await page.getByLabel('Configuration source').selectOption('template-a')
    await expect(page.getByLabel('OS image')).toBeDisabled()
    await page.getByRole('button', { name: 'Customize' }).click()
    await expect(page.getByLabel('OS image')).toBeEnabled()
    await page.getByLabel('Cloud-init').first().selectOption('replace')
    await page.locator('textarea#deploy-user-data').fill('#cloud-config\nhostname: batch')
    await page.getByRole('button', { name: 'Next' }).click()
    await page.getByLabel('Save as a deployment template').check()
    await page.getByLabel('New deployment template name').fill('Scale-out baseline')
    await page.getByRole('button', { name: 'Deploy OS' }).click()

    await expect(page).toHaveURL('/servers?site=site-a')
    await expect(page.getByText('OS deployment started')).toBeVisible()
    await expect(page.getByText(/Operation op-deploy-os-1 is running in the background/)).toBeVisible()
    await expect(page.getByRole('grid', { name: 'Servers' })).toBeVisible()
    expect(deploymentRequest?.serverIds).toEqual(['srv-1', 'srv-2'])
    expect(deploymentRequest?.network).toEqual({ mode: 'dhcp', subnetId: 'subnet-a', defaultGateway: false, assignments: [{ serverId: 'srv-1', interfaceId: 'nic-srv-1', subnetId: 'subnet-a' }, { serverId: 'srv-2', interfaceId: 'nic-srv-2', subnetId: 'subnet-a' }] })
    expect(JSON.stringify(deploymentRequest)).toContain('#cloud-config')
    expect(await page.evaluate(() => JSON.stringify({
      local: { ...localStorage },
      session: { ...sessionStorage },
    }))).not.toContain('#cloud-config')
  })

  test('Deploy OS renders its expanded dark image placeholder and refreshes uploaded images', async ({ page }) => {
    const catalogRequests: string[] = []
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, {
      readyServerCount: 1,
      onOSImageCatalogRequest: (integrationId) => catalogRequests.push(integrationId),
    })
    await page.goto('/provisioning/deploy?site=site-a&serverId=srv-1')
    await visibleAppearance(page).click()
    await chooseMenuItem(page, 'Dark')
    await page.getByRole('button', { name: 'Next' }).click()
    await expect(page.getByLabel('OS image')).toContainText('Select an image')
    await expectExpandedPlaceholderReadable(page, 'OS image', 'Select an image')
    await expect(page.getByRole('option', { name: 'Ubuntu 24.04 ROCm (amd64)', exact: true })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByText('3 deployable images returned by the provider.')).toBeVisible()
    await expect.poll(() => catalogRequests).toEqual(['maas-a'])

    await page.getByRole('button', { name: 'Refresh' }).click()
    await expect.poll(() => catalogRequests).toEqual(['maas-a', 'maas-a'])

    const networkSection = page.locator('section.sw-section').filter({ hasText: 'Network configuration' })
    const sectionBox = await networkSection.boundingBox()
    const dhcpBox = await page.getByRole('button', { name: 'DHCP' }).boundingBox()
    expect(sectionBox).not.toBeNull()
    expect(dhcpBox).not.toBeNull()
    if (!sectionBox || !dhcpBox) throw new Error('Network configuration controls are not visible')
    expect(dhcpBox.x - sectionBox.x).toBeGreaterThan(16)
  })

  test('Deploy OS preserves an existing Static binding as the network default', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, {
      readyServerCount: 2,
      staticNetworkServerIds: ['srv-1'],
      networkSubnetName: '192.168.40.0/24',
    })

    await page.goto('/provisioning/deploy?site=site-a&serverId=srv-1&serverId=srv-2')
    await page.getByRole('button', { name: 'Next' }).click()

    const staticMode = page.getByRole('button', { name: 'Static' })
    const dhcpMode = page.getByRole('button', { name: 'DHCP' })
    await expect(staticMode).toHaveAttribute('aria-pressed', 'true')
    await expect(dhcpMode).toHaveAttribute('aria-pressed', 'false')
    const subnet = page.getByLabel('Subnet for gpu-node-01')
    await expect(subnet.locator('option:checked')).toHaveText('192.168.40.0/24')
    await expect(subnet.locator('option:checked')).not.toContainText('(')
    const currentModeHeading = page.getByRole('columnheader', { name: 'Current mode' })
    await expect(currentModeHeading).toBeVisible()
    expect(await currentModeHeading.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true)
    await expect(page.getByLabel('Use the selected subnet for the default route')).toBeChecked()
    await expect(page.getByText('the provider makes this Static link the IPv4 default route', { exact: false })).toBeVisible()
    await expect(page.getByLabel('Static IPv4 address for gpu-node-01')).toHaveValue('192.168.40.21')
    await expect(page.getByLabel('Static IPv4 address for gpu-node-02')).toHaveValue('')

    await chooseSingleSelectOption(page, 'OS image', 'Ubuntu 22.04 LTS (amd64)')
    await expect(page.getByRole('button', { name: 'Next' })).toBeDisabled()

    await dhcpMode.click()
    await expect(dhcpMode).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByLabel('Static IPv4 address for gpu-node-01')).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Next' })).toBeEnabled()
  })

  test('cross-integration selection is blocked and Site changes clear provisioning drafts', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, {
      readyServerCount: 2,
      secondReadyServerIntegrationId: 'maas-b',
    })
    await page.goto('/servers?site=site-a')
    await page.getByLabel('Select gpu-node-01').check()
    await page.getByLabel('Select gpu-node-02').check()
    await expect(page.getByRole('button', { name: 'Deploy OS' })).toBeDisabled()
    await expect(page.getByText('Selected Servers must use the same provisioner integration.')).toBeVisible()

    await page.getByLabel('Select gpu-node-02').uncheck()
    await page.getByRole('button', { name: 'Deploy OS' }).click()
    await expect(page).toHaveURL(/serverId=srv-1/)
    await visibleSiteScope(page).click()
    await chooseMenuItem(page, 'Hsinchu Edge')
    await expect(page).toHaveURL('/provisioning/deploy?site=site-b')
    await expect(page.getByText('Provisioning draft cleared')).toBeVisible()
  })

  test('Static OS deployment requires and reviews a unique IPv4 address per target', async ({ page }) => {
    let deploymentRequest: Record<string, unknown> | undefined
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, {
      readyServerCount: 2,
      deploymentConvergesAfterRefreshes: 1,
      onDeploymentRequest: (body) => { deploymentRequest = body },
    })
    await page.goto('/provisioning/deploy?site=site-a&serverId=srv-1&serverId=srv-2')
    await page.getByRole('button', { name: 'Next' }).click()
    await chooseSingleSelectOption(page, 'OS image', 'Ubuntu 22.04 LTS (amd64)')
    await page.getByRole('button', { name: 'Static' }).click()

    const next = page.getByRole('button', { name: 'Next' })
    await expect(next).toBeDisabled()
    await page.getByLabel('Static IPv4 address for gpu-node-01').fill('192.168.40.91')
    await page.getByLabel('Static IPv4 address for gpu-node-02').fill('192.168.40.91')
    await expect(next).toBeDisabled()
    await page.getByLabel('Static IPv4 address for gpu-node-02').fill('192.168.40.92')
    await expect(next).toBeEnabled()
    await next.click()

    const review = page.getByRole('grid', { name: 'Deployment review targets' })
    await expect(review).toContainText('192.168.40.91')
    await expect(review).toContainText('192.168.40.92')
    await page.getByRole('button', { name: 'Deploy OS' }).click()
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
    await expect(page.getByRole('grid', { name: 'Servers' })).toBeVisible()
  })

  test('template CRUD remains write-only and OS Images preserves partial provider results', async ({ page }) => {
    await page.unroute('**/api/v1/**')
    await installApiFixtures(page, { failImageIntegrationIds: ['maas-b'] })
    await page.goto('/provisioning/templates?site=site-a')
    await expect(page.getByText('GPU compute baseline')).toBeVisible()
    await page.getByRole('button', { name: 'Create template' }).click()
    await page.getByLabel('Provisioner integration').selectOption('maas-a')
    await page.getByLabel('Name').fill('Scale-out template')
    await page.getByLabel('OS image').selectOption('ubuntu/noble')
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    const row = page.getByRole('row', { name: /Scale-out template/ })
    await expect(row).toBeVisible()
    await row.getByRole('button', { name: 'Add cloud-init' }).click()
    await page.locator('textarea#template-user-data').fill('#cloud-config\nusers: []')
    await page.locator('.pf-v6-c-card').filter({ hasText: 'Replace cloud-init for Scale-out template' }).getByRole('button', { name: 'Replace cloud-init' }).click()
    await expect(page.getByRole('row', { name: /Scale-out template/ })).toContainText('Configured')
    expect(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain('#cloud-config')

    await page.goto('/provisioning/images?site=site-a')
    await expect(page.getByText('MAAS Edge image catalog unavailable')).toBeVisible()
    await expect(page.getByRole('row', { name: /Ubuntu 22.04 LTS/ })).toBeVisible()
    await expectTableCellsVerticallyCentered(page, 'OS images')
  })

  test('Infrastructure exposes the Site hierarchy and OS Image provider sources', async ({ page }) => {
    await page.goto('/infrastructure/sites')
    await expect(page.getByRole('heading', { name: 'Infrastructure' })).toBeVisible()
    await expect(page.getByText('Sites define infrastructure locations. Each Integration connects one Site to an external provider.')).toBeVisible()
    const taipei = page.getByRole('row', { name: /Taipei Lab/ })
    await expect(taipei).toContainText('Primary accelerator lab')
    await expect(taipei.getByRole('gridcell', { name: '3' })).toBeVisible()
    await taipei.getByRole('button', { name: 'Delete' }).click()
    await page.getByLabel('Site name confirmation').fill('Taipei Lab')
    await page.getByRole('dialog').getByRole('button', { name: 'Delete site' }).click()
    await expect(page.getByText('This site still has integrations. Delete them first.')).toBeVisible()
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click()

    await page.getByRole('tab', { name: 'Integrations' }).click()
    const maas = page.getByRole('row', { name: /MAAS Taipei/ })
    await expect(maas).toContainText('Taipei Lab')
    await expect(maas).toContainText('Provisioner')
    await expect(maas).toContainText('maas')

    await page.goto('/provisioning/images?site=site-a')
    await expect(page.getByRole('columnheader', { name: 'Provider integration' })).toBeVisible()
    await page.getByRole('row', { name: /Ubuntu 22.04 LTS/ }).getByRole('link', { name: 'MAAS Taipei' }).click()
    await expect(page).toHaveURL('/infrastructure/integrations?site=site-a#integration-maas-a')
    await expect(page.locator('#integration-maas-a')).toContainText('Taipei Lab')
  })

  test('Sites and Integrations can be configured without persisting credentials', async ({ page }) => {
    await page.goto('/infrastructure/sites')
    await page.getByRole('button', { name: 'Create site' }).click()
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
    await expect(page.getByRole('dialog').getByLabel('Site')).toHaveValue('site-3')
    await page.getByLabel('Name').fill('MAAS Singapore')
    await page.getByLabel('Endpoint').fill('https://maas.sg.example')
    await page.getByLabel('Credential').fill('consumer:token:initial-secret')
    await page.getByLabel('Request timeout').fill('45s')
    await page.getByRole('dialog').getByRole('button', { name: 'Create integration' }).click()

    let integration = page.getByRole('row', { name: /MAAS Singapore/ })
    await expect(integration).toContainText('Singapore DC')
    await expect(integration).toContainText('Configured')
    await integration.getByRole('button', { name: 'Edit' }).click()
    await expect(page.getByRole('dialog').getByLabel('Site')).toBeDisabled()
    await expect(page.getByRole('dialog').getByLabel('Role')).toBeDisabled()
    await expect(page.getByRole('dialog').getByLabel('Provider')).toBeDisabled()
    await page.getByLabel('Name').fill('MAAS Singapore Primary')
    await page.getByLabel('Enabled').uncheck()
    await page.getByRole('dialog').getByRole('button', { name: 'Save changes' }).click()

    integration = page.getByRole('row', { name: /MAAS Singapore Primary/ })
    await expect(integration).toContainText('Paused')
    await integration.getByRole('button', { name: 'Credential' }).click()
    await page.getByLabel('New credential').fill('replacement-secret')
    await page.getByRole('dialog').getByRole('button', { name: 'Replace credential' }).click()
    await expect(page.getByText('Credential replaced')).toBeVisible()
    expect(await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }))).not.toContain('replacement-secret')

    await integration.getByRole('button', { name: 'Delete' }).click()
    await page.getByLabel('Integration name confirmation').fill('MAAS Singapore Primary')
    await page.getByRole('dialog').getByRole('button', { name: 'Delete integration' }).click()
    await expect(page.getByRole('row', { name: /MAAS Singapore Primary/ })).toHaveCount(0)

    await page.getByRole('tab', { name: 'Sites' }).click()
    const site = page.getByRole('row', { name: /Singapore DC/ })
    await site.getByRole('button', { name: 'Delete' }).click()
    await page.getByLabel('Site name confirmation').fill('Singapore DC')
    await page.getByRole('dialog').getByRole('button', { name: 'Delete site' }).click()
    await expect(page).toHaveURL('/infrastructure/sites')
    await expect(page.getByRole('row', { name: /Singapore DC/ })).toHaveCount(0)

    await page.setViewportSize({ width: 390, height: 844 })
    await page.evaluate(() => localStorage.setItem('swallow.appearance', JSON.stringify('dark')))
    await page.goto('/infrastructure/integrations')
    await expect(page.locator('html')).toHaveClass(/pf-v6-theme-dark/)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    const mobileRowLayout = await page.locator('table[aria-label="Integrations"] tbody tr').first().evaluate((row) => {
      const rowWidth = row.getBoundingClientRect().width
      const rowStyle = getComputedStyle(row)
      const availableWidth = rowWidth - Number.parseFloat(rowStyle.paddingInlineStart) - Number.parseFloat(rowStyle.paddingInlineEnd)
      const cells = [...row.querySelectorAll('td')]
      const action = row.querySelector('.pf-v6-c-table__action')
      return {
        allCellsUseRowWidth: cells.every((cell) => cell.getBoundingClientRect().width >= availableWidth * 0.95),
        allCellsUseFirstColumn: cells.every((cell) => getComputedStyle(cell).gridColumnStart === '1'),
        actionColumn: action ? getComputedStyle(action).gridColumnStart : '',
      }
    })
    expect(mobileRowLayout.allCellsUseRowWidth).toBe(true)
    expect(mobileRowLayout.allCellsUseFirstColumn).toBe(true)
    expect(mobileRowLayout.actionColumn).toBe('1')
  })

  test('keyboard can traverse primary navigation', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: 'Servers', exact: true }).focus()
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL('/servers')
  })
})
