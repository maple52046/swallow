import { expect, test, type Locator, type Page } from 'playwright/test'
import { installApiFixtures, type FixtureOptions } from './fixtures'

async function chooseOption(page: Page, label: string, option: string) {
  await page.getByRole('combobox', { name: label, exact: true }).click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

async function chooseOSImage(page: Page, imageName: string) {
  const image = page.getByRole('radio', { name: imageName, exact: false })
  await expect(image).toBeVisible()
  await image.locator('..').click()
  await expect(image).toBeChecked()
}

async function choiceGeometry(dialog: Locator) {
  return dialog.evaluate((element) => {
    const root = element as HTMLElement
    const rootRect = root.getBoundingClientRect()
    const cards = Array.from(
      root.querySelectorAll<HTMLElement>('.sw-deployment-choice-card'),
    ).map((card) => card.getBoundingClientRect())
    return {
      dialogTop: rootRect.top,
      dialogBottom: rootRect.bottom,
      viewportHeight: window.innerHeight,
      cardCount: cards.length,
      widthDelta: cards.length > 0
        ? Math.max(...cards.map((card) => card.width)) - Math.min(...cards.map((card) => card.width))
        : Number.POSITIVE_INFINITY,
      allInside: cards.every(
        (card) => card.left >= rootRect.left - 1 && card.right <= rootRect.right + 1,
      ),
      sameRow: cards.length === 2 && Math.abs(cards[0].top - cards[1].top) <= 1,
    }
  })
}

test('Provisioning opens OS Images without a Deploy OS tab', async ({ page }) => {
  await installApiFixtures(page, { singleSite: true })
  await page.goto('/provisioning?site=site-a')

  await expect(page).toHaveURL('/provisioning/images?site=site-a')
  await expect(page.getByRole('heading', { name: 'OS images', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Deploy OS', exact: true })).toHaveCount(0)
  await expect(page.getByRole('tab', { name: 'OS images', exact: true })).toHaveAttribute('aria-selected', 'true')
})

test('the retired Deploy OS URL returns to Site Servers and discards obsolete draft inputs', async ({ page }) => {
  await installApiFixtures(page, { readyServerCount: 1 })
  await page.goto('/provisioning/deploy?site=site-a&serverId=srv-1&imageId=ubuntu%2Fjammy&templateId=template-1')

  await expect(page).toHaveURL('/servers?site=site-a')
  await expect(page.getByRole('heading', { name: 'Servers', exact: true })).toBeVisible()
})

const invalidProvisioners: Array<{
  name: string
  options: FixtureOptions
  title: string
}> = [
  {
    name: 'missing',
    options: { noProvisioners: true },
    title: 'Connect a provisioner',
  },
  {
    name: 'multiple',
    options: { multipleProvisioners: true },
    title: 'Site configuration conflict',
  },
  {
    name: 'disabled',
    options: { disabledProvisioner: true },
    title: 'Enable the Site provisioner',
  },
]

for (const scenario of invalidProvisioners) {
  test(`${scenario.name} Site provisioner blocks catalog reads and deployment writes`, async ({ page }) => {
    const catalogReads: string[] = []
    let deploymentWrites = 0
    await installApiFixtures(page, {
      ...scenario.options,
      readyServerCount: 1,
      onOSImageCatalogRequest: (integrationId) => catalogReads.push(integrationId),
      onDeploymentRequest: () => { deploymentWrites += 1 },
    })

    await page.goto('/servers/srv-1/summary?site=site-a')
    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    await expect(dialog.getByText(scenario.title, { exact: true })).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Manage integrations' })).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Next' })).toHaveCount(0)
    expect(catalogReads).toEqual([])
    expect(deploymentWrites).toBe(0)
  })
}

test('a fixed Server from another Integration shows inconsistent source without provider calls', async ({ page }) => {
  const catalogReads: string[] = []
  let deploymentWrites = 0
  await installApiFixtures(page, {
    readyServerCount: 2,
    secondReadyServerIntegrationId: 'maas-b',
    onOSImageCatalogRequest: (integrationId) => catalogReads.push(integrationId),
    onDeploymentRequest: () => { deploymentWrites += 1 },
  })
  await page.goto('/servers?site=site-a')

  await page.getByRole('button', { name: 'Deploy OS for gpu-node-02' }).click()
  const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
  await expect(dialog.getByText('Inconsistent deployment source', { exact: true })).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Manage integrations' })).toBeVisible()
  expect(catalogReads).toEqual([])
  expect(deploymentWrites).toBe(0)
})

for (const viewport of [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'mobile', width: 390, height: 844 },
]) {
  test('single-Server dialog keeps every ' + viewport.name + ' configuration step in bounds', async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await installApiFixtures(page, { readyServerCount: 1 })
    await page.goto('/servers/srv-1/summary?site=site-a')
    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()

    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    await expect(dialog.getByRole('heading', { name: 'Operating system', exact: true })).toBeVisible()
    await expect(dialog.locator('.sw-wizard')).toHaveCount(0)
    await expect(dialog.getByRole('region', { name: 'Deployment context' })).toHaveCount(0)
    const operatingSystemHeading = dialog.locator('.sw-os-image-selection-step__heading')
    await expect(operatingSystemHeading.getByRole('button', { name: 'Refresh' })).toBeVisible()
    await expect(dialog.getByText(
      'Choose an OS family, then compare the deployable images in that family. Architecture, size, tags, deploy-mode availability, and distinct provider IDs remain visible.',
      { exact: true },
    )).toBeVisible()
    await expect(dialog.getByLabel('Configuration source')).toHaveCount(0)
    await expect(dialog.locator('.sw-deploy-os-dialog__section')).toHaveCount(0)
    await expect(dialog.locator('.sw-os-image-picker--plain')).toBeVisible()
    await expect(dialog.locator('.sw-os-image-picker-field [data-part="label"]')).toHaveCount(0)

    const shell = await dialog.evaluate((element) => {
      const root = element as HTMLElement
      const rect = root.getBoundingClientRect()
      const content = root.querySelector<HTMLElement>('[data-testid="deploy-os-dialog-viewport"]')
      const picker = root.querySelector<HTMLElement>('.sw-os-image-picker')
      const list = root.querySelector<HTMLElement>('.sw-os-image-picker__list')
      const progress = root.querySelector<HTMLElement>('.sw-deploy-os-dialog__progress')
      const active = progress?.querySelector<HTMLElement>('[aria-current="step"]')
      const footerButton = Array.from(root.querySelectorAll<HTMLButtonElement>('button'))
        .find((button) => button.textContent?.trim() === 'Cancel')
      const inside = (item?: DOMRect) => Boolean(
        item && item.left >= rect.left - 1 && item.right <= rect.right + 1,
      )
      const progressRect = progress?.getBoundingClientRect()
      const activeRect = active?.getBoundingClientRect()
      return {
        rect: { left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom },
        clipsOverflow: getComputedStyle(root).overflowX === 'hidden' &&
          Boolean(content && getComputedStyle(content).overflowX === 'hidden'),
        pickerInside: inside(picker?.getBoundingClientRect()),
        listInside: inside(list?.getBoundingClientRect()),
        listOwnsScroll: Boolean(
          list &&
            list.scrollHeight > list.clientHeight &&
            ['auto', 'scroll'].includes(getComputedStyle(list).overflowY),
        ),
        viewportAllowsScroll: Boolean(
          content && ['auto', 'scroll'].includes(getComputedStyle(content).overflowY),
        ),
        footerInside: inside(footerButton?.getBoundingClientRect()),
        activeInside: Boolean(
          progressRect && activeRect &&
            activeRect.left >= progressRect.left - 1 &&
            activeRect.right <= progressRect.right + 1,
        ),
      }
    })
    expect(shell.rect.left).toBeGreaterThanOrEqual(-1)
    expect(shell.rect.top).toBeGreaterThanOrEqual(-1)
    expect(shell.rect.right).toBeLessThanOrEqual(viewport.width + 1)
    expect(shell.rect.bottom).toBeLessThanOrEqual(viewport.height + 1)
    expect(shell.clipsOverflow).toBe(true)
    expect(shell.pickerInside).toBe(true)
    expect(shell.listInside).toBe(true)
    expect(shell.listOwnsScroll).toBe(false)
    expect(shell.viewportAllowsScroll).toBe(true)
    expect(shell.footerInside).toBe(true)
    expect(shell.activeInside).toBe(true)

    await chooseOSImage(page, 'Ubuntu 22.04 LTS')
    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Installation', exact: true })).toBeVisible()
    await expect(dialog.getByText(
      'Choose where the OS runs and whether this deployment supplies cloud-init data.',
      { exact: true },
    )).toBeVisible()
    await expect(dialog.getByRole('heading', { name: 'Install location and cloud-init' })).toHaveCount(0)
    await expect(dialog.locator('.sw-deploy-os-dialog__section')).toHaveCount(0)
    const installChoices = await choiceGeometry(dialog)
    expect(installChoices.dialogTop).toBeGreaterThanOrEqual(-1)
    expect(installChoices.dialogBottom).toBeLessThanOrEqual(installChoices.viewportHeight + 1)
    expect(installChoices.cardCount).toBe(2)
    expect(installChoices.widthDelta).toBeLessThanOrEqual(2)
    expect(installChoices.allInside).toBe(true)
    expect(installChoices.sameRow).toBe(viewport.name === 'desktop')
    await expect(dialog.getByRole('combobox', { name: 'Cloud-init', exact: true })).toBeVisible()

    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Networking', exact: true })).toBeVisible()
    await expect(dialog.getByText(
      'Choose Automatic or Static addressing for the inspected target NICs.',
      { exact: true },
    )).toBeVisible()
    await expect(dialog.getByRole('heading', { name: 'Addressing and interfaces' })).toHaveCount(0)
    await expect(dialog.locator('.sw-deploy-os-dialog__section')).toHaveCount(0)
    const networkingChoices = await choiceGeometry(dialog)
    expect(networkingChoices.dialogTop).toBeGreaterThanOrEqual(-1)
    expect(networkingChoices.dialogBottom).toBeLessThanOrEqual(networkingChoices.viewportHeight + 1)
    expect(networkingChoices.cardCount).toBe(2)
    expect(networkingChoices.widthDelta).toBeLessThanOrEqual(2)
    expect(networkingChoices.allInside).toBe(true)
    expect(networkingChoices.sameRow).toBe(viewport.name === 'desktop')

    const network = await dialog.evaluate((element) => {
      const root = element as HTMLElement
      const viewportElement = root.querySelector<HTMLElement>('[data-testid="deploy-os-dialog-viewport"]')
      const editor = root.querySelector<HTMLElement>('.sw-network-assignment-editor')
      const header = editor?.querySelector<HTMLElement>('.sw-network-assignment-editor__header')
      const rows = Array.from(editor?.querySelectorAll<HTMLElement>('.sw-network-assignment-editor__row') ?? [])
      const viewportRect = viewportElement?.getBoundingClientRect()
      const editorRect = editor?.getBoundingClientRect()
      return {
        editorInside: Boolean(
          viewportRect && editorRect &&
            editorRect.left >= viewportRect.left - 1 &&
            editorRect.right <= viewportRect.right + 1,
        ),
        headerVisible: header ? getComputedStyle(header).display !== 'none' : false,
        rowDisplay: rows[0] ? getComputedStyle(rows[0]).display : '',
        rowsInside: rows.length > 0 && rows.every((row) => {
          const rowRect = row.getBoundingClientRect()
          return Boolean(
            viewportRect &&
              rowRect.left >= viewportRect.left - 1 &&
              rowRect.right <= viewportRect.right + 1,
          )
        }),
        interfaceCount: root.querySelectorAll('[aria-label^="Interface for "]').length,
        subnetCount: root.querySelectorAll('[aria-label^="Subnet for "]').length,
      }
    })
    expect(network.editorInside).toBe(true)
    expect(network.interfaceCount).toBe(1)
    expect(network.subnetCount).toBe(1)
    if (viewport.name === 'desktop') {
      expect(network.headerVisible).toBe(true)
      expect(network.rowDisplay).toBe('grid')
    } else {
      expect(network.headerVisible).toBe(false)
      expect(network.rowDisplay).toBe('block')
      expect(network.rowsInside).toBe(true)
    }
  })
}

test('image picker groups by family, exposes tags and deploy modes, and owns no nested scroll', async ({ page }) => {
  await installApiFixtures(page, { readyServerCount: 1, extraOSImageCount: 24 })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()

  const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
  await expect(dialog.locator('.sw-os-image-selection-step')).toHaveCount(1)
  await expect(dialog.getByText('3 families · 27 images total', { exact: true })).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Ubuntu 2', exact: true })).toHaveAttribute('aria-pressed', 'true')

  const ubuntuImage = dialog.getByRole('radio', { name: 'Ubuntu 22.04 LTS', exact: false })
  const ubuntuCard = ubuntuImage.locator('..').locator('.sw-os-image-picker__card')
  await expect(ubuntuCard.getByLabel('Disk deploy: Available')).toBeVisible()
  await expect(ubuntuCard.getByLabel('RAM deploy: Available')).toBeVisible()
  await expect(ubuntuCard.getByText('general-purpose', { exact: true })).toBeVisible()
  await expect(ubuntuCard.getByText('lts', { exact: true })).toBeVisible()
  await expect(ubuntuCard.getByText('production', { exact: true })).toBeVisible()
  const imageFrame = await ubuntuImage.locator('..').evaluate((item) => {
    const card = item.querySelector<HTMLElement>('.sw-os-image-picker__card')
    if (!card) return null
    const itemStyle = getComputedStyle(item)
    const cardStyle = getComputedStyle(card)
    return {
      itemBorderWidth: itemStyle.borderTopWidth,
      itemPadding: itemStyle.paddingTop,
      itemBoxShadow: itemStyle.boxShadow,
      cardBorderWidth: cardStyle.borderTopWidth,
    }
  })
  expect(imageFrame).toEqual({
    itemBorderWidth: '0px',
    itemPadding: '0px',
    itemBoxShadow: 'none',
    cardBorderWidth: '1px',
  })

  const focusOrigin = dialog.getByRole('textbox', { name: 'Search deployment images' })
  await focusOrigin.focus()
  await page.keyboard.press('Tab')
  await expect(ubuntuImage).toBeFocused()
  await expect.poll(() => ubuntuCard.evaluate((card) => {
    const style = getComputedStyle(card)
    return { style: style.outlineStyle, width: style.outlineWidth }
  })).toEqual({ style: 'solid', width: '2px' })

  await ubuntuCard.click()
  await expect(ubuntuImage).toBeChecked()
  await expect.poll(() => ubuntuCard.evaluate((card) => getComputedStyle(card).outlineStyle))
    .toBe('none')

  await dialog.getByRole('button', { name: 'Validation Linux 24', exact: true }).click()
  await expect(dialog.getByText('24 images in Validation Linux', { exact: true })).toBeVisible()

  const geometry = await dialog.evaluate((element) => {
    const root = element as HTMLElement
    const viewport = root.querySelector<HTMLElement>('[data-testid="deploy-os-dialog-viewport"]')
    const list = root.querySelector<HTMLElement>('.sw-os-image-picker__list')
    if (!viewport || !list) return null
    return {
      listOverflowY: getComputedStyle(list).overflowY,
      listClientHeight: list.clientHeight,
      listScrollHeight: list.scrollHeight,
      listColumns: getComputedStyle(list).gridTemplateColumns.split(' ').length,
      viewportOverflowY: getComputedStyle(viewport).overflowY,
      viewportClientHeight: viewport.clientHeight,
      viewportScrollHeight: viewport.scrollHeight,
    }
  })
  expect(geometry).not.toBeNull()
  expect(geometry?.listOverflowY).toBe('visible')
  expect(geometry?.listScrollHeight).toBe(geometry?.listClientHeight)
  expect(geometry?.listColumns).toBe(2)
  expect(['auto', 'scroll']).toContain(geometry?.viewportOverflowY)
  expect(geometry?.viewportScrollHeight).toBeGreaterThan(geometry?.viewportClientHeight ?? Number.MAX_SAFE_INTEGER)

  const search = dialog.getByRole('textbox', { name: 'Search deployment images' })
  await expect(search).toHaveAttribute('placeholder', 'Search Validation Linux images')
  await search.fill('does-not-exist')
  await expect(dialog.getByText('No images match this search.')).toBeVisible()
  await expect(dialog.getByRole('radio', { name: 'Validation Linux 01', exact: false })).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Clear search' }).click()

  const image = dialog.getByRole('radio', { name: 'Validation Linux 01', exact: false })
  const imageCard = image.locator('..').locator('.sw-os-image-picker__card')
  await expect(imageCard.getByLabel('Disk deploy: Available')).toBeVisible()
  await expect(imageCard.getByLabel('RAM deploy: Available')).toBeVisible()
  await expect(imageCard.getByText('validation', { exact: true })).toBeVisible()
  await image.focus()
  await page.keyboard.press('Space')
  await expect(image).toBeChecked()

  await expect(dialog.getByText('Selected image', { exact: true })).toHaveCount(0)
  await expect(dialog.locator('.sw-os-image-picker__selection')).toHaveCount(0)
  const imageIdOccurrences = await dialog.locator('.sw-os-image-picker').evaluate(
    (element) => element.textContent?.match(/validation-01/gi)?.length ?? 0,
  )
  expect(imageIdOccurrences).toBe(1)
})



test('a fixed provider image offers only ready unlocked target cards and skips image selection', async ({ page }) => {
  await installApiFixtures(page, {
    readyServerCount: 3,
    lockedServerIds: ['srv-2'],
  })
  await page.goto('/provisioning/images?site=site-a')
  await page.getByRole('button', { name: 'Deploy Ubuntu 22.04 LTS', exact: true }).click()

  const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
  const progress = dialog.getByRole('navigation', { name: 'Deployment progress' })
  const next = dialog.getByRole('button', { name: 'Next' })
  await expect(dialog.getByText(
    'Choose up to 100 ready, unlocked Servers for this deployment.',
    { exact: true },
  )).toBeVisible()
  await expect(progress.getByText('OS image', { exact: true })).toHaveCount(0)
  await expect(progress.getByText('Targets', { exact: true })).toBeVisible()
  await expect(progress.getByText('Installation', { exact: true })).toBeVisible()
  await expect(dialog.getByRole('table', { name: 'Deployment target selection' })).toHaveCount(0)
  await expect(dialog.locator('.sw-deploy-target-card')).toHaveCount(2)
  await expect(dialog.getByLabel('Select gpu-node-01')).toBeVisible()
  await expect(dialog.getByLabel('Select gpu-node-02')).toHaveCount(0)
  await expect(dialog.getByLabel('Select gpu-node-03')).toBeVisible()
  await expect(dialog.getByLabel('Select gpu-node-04')).toHaveCount(0)

  const targetSearch = dialog.getByRole('textbox', { name: 'Search deployment targets' })
  await targetSearch.fill('192.168.40.23')
  await expect(dialog.locator('.sw-deploy-target-card')).toHaveCount(1)
  await expect(dialog.getByLabel('Select gpu-node-03')).toBeVisible()
  await targetSearch.fill('')

  await dialog.getByLabel('Select gpu-node-01').click()
  await next.click()
  await expect(dialog.getByRole('heading', { name: 'Installation', exact: true })).toBeVisible()
  await expect(dialog.getByRole('heading', { name: 'Operating system', exact: true })).toHaveCount(0)
  await expect(dialog.locator('.sw-os-image-picker')).toHaveCount(0)
  await expect(dialog.getByRole('radio', { name: /^RAM deploy/ })).toBeChecked()

  await next.click()
  await expect(dialog.getByRole('heading', { name: 'Networking', exact: true })).toBeVisible()
  await next.click()
  await expect(dialog.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
  const imageValue = dialog.locator('dt').filter({ hasText: /^Image$/ }).locator('..').locator('dd')
  await expect(imageValue).toHaveText('Ubuntu 22.04 LTS')
  await expect(page).toHaveURL('/provisioning/images?site=site-a')
})

test('image capability chooses the deploy-mode default without overwriting an operator choice', async ({ page }) => {
  await installApiFixtures(page, { readyServerCount: 1 })
  const image = (
    id: string,
    name: string,
    providerOsSystem: 'ubuntu' | 'custom',
    verifiedDeployTargets: Array<'disk' | 'ram'>,
  ) => ({
    id,
    name,
    providerName: name,
    osSystem: providerOsSystem,
    providerOsSystem,
    release: id,
    providerRelease: id,
    tags: [],
    architecture: 'amd64',
    sizeBytes: 1_073_741_824,
    verifiedDeployTargets,
    failedDeployTargets: [],
  })
  await page.route('**/api/v1/provisioning/images?*', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        image('ubuntu/both', 'Both modes', 'ubuntu', []),
        image('custom/disk', 'Disk only', 'custom', ['disk']),
        image('custom/ram', 'RAM only', 'custom', ['ram']),
        image('custom/neither', 'Neither mode', 'custom', []),
      ]),
    })
  })

  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
  const next = dialog.getByRole('button', { name: 'Next' })
  const back = dialog.getByRole('button', { name: 'Back' })
  const disk = dialog.getByRole('radio', { name: /^Disk deploy/ })
  const ram = dialog.getByRole('radio', { name: /^RAM deploy/ })

  await chooseOSImage(page, 'Both modes')
  await next.click()
  await expect(ram).toBeChecked()
  await expect(dialog.getByText('Deploy mode', { exact: true })).toBeVisible()
  await expect(dialog.getByRole('radio', { name: /RAM deploy \(ephemeral\)/ })).toHaveCount(0)

  const ramItem = ram.locator('..')
  const ramCard = ramItem.locator('.sw-deployment-choice-card')
  const modeFrame = await ramItem.evaluate((item) => {
    const card = item.querySelector<HTMLElement>('.sw-deployment-choice-card')
    if (!card) return null
    const itemStyle = getComputedStyle(item)
    const cardStyle = getComputedStyle(card)
    return {
      itemBorderWidth: itemStyle.borderTopWidth,
      itemPadding: itemStyle.paddingTop,
      itemBoxShadow: itemStyle.boxShadow,
      cardBorderWidth: cardStyle.borderTopWidth,
    }
  })
  expect(modeFrame).toEqual({
    itemBorderWidth: '0px',
    itemPadding: '0px',
    itemBoxShadow: 'none',
    cardBorderWidth: '1px',
  })

  await ram.focus()
  await page.keyboard.press('Space')
  await expect(ram).toBeFocused()
  await expect.poll(() => ramCard.evaluate((card) => {
    const style = getComputedStyle(card)
    return { style: style.outlineStyle, width: style.outlineWidth }
  })).toEqual({ style: 'solid', width: '2px' })

  const diskCard = disk.locator('..').locator('.sw-deployment-choice-card')
  await diskCard.click()
  await expect(disk).toBeChecked()
  await expect.poll(() => diskCard.evaluate((card) => getComputedStyle(card).outlineStyle))
    .toBe('none')
  await back.click()
  await next.click()
  await expect(disk).toBeChecked()

  await back.click()
  await dialog.getByRole('button', { name: 'Custom 3', exact: true }).click()
  await chooseOSImage(page, 'Disk only')
  await next.click()
  await expect(disk).toBeChecked()

  await back.click()
  await chooseOSImage(page, 'RAM only')
  await next.click()
  await expect(ram).toBeChecked()

  await back.click()
  await chooseOSImage(page, 'Neither mode')
  await next.click()
  await expect(ram).toBeChecked()
  await expect(dialog.getByText('This image does not support this deploy mode')).toBeVisible()
  await expect(dialog.getByText(/You can continue, but deployment may fail/)).toBeVisible()
  await expect(next).toBeEnabled()
  await next.click()
  await expect(dialog.getByRole('heading', { name: 'Networking', exact: true })).toBeVisible()
})

test('image picker snapshots active verification without polling or exposing internal outcomes', async ({ page }) => {
  await installApiFixtures(page, { readyServerCount: 1 })
  let workflowReads = 0
  await page.route('**/api/v1/workflows?*', async (route) => {
    const url = new URL(route.request().url())
    if (url.searchParams.get('kind') !== 'verify-os-image') return route.fallback()
    workflowReads += 1
    const workflow = {
      id: 'verify-rocm-ram',
      schemaVersion: 3,
      kind: 'verify-os-image',
      intent: 'Verify Ubuntu 24.04 ROCm for RAM deployment',
      intentSnapshot: {
        request: {
          integrationId: 'maas-a',
          imageId: 'ubuntu-24.04-rocm',
          architecture: 'amd64',
          deployTarget: 'ram',
        },
      },
      status: 'running',
      statusReason: null,
      siteId: 'site-a',
      platformId: null,
      targetServerIds: ['srv-1'],
      steps: [],
      retryOfOperationId: null,
      execution: {
        runId: 'run-verify-rocm-ram',
        playbook: '',
        status: 'running',
        statusReason: null,
        startedAt: '2026-08-27T02:00:00Z',
        finishedAt: null,
      },
      requestedBy: 'admin',
      requestedAt: '2026-08-27T02:00:00Z',
      updatedAt: '2026-08-27T02:05:00Z',
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ items: [workflow], total: 1, page: 1, pageSize: 100 }),
    })
  })

  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()

  const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
  await dialog.getByRole('button', { name: 'Custom 1', exact: true }).click()
  const customImage = dialog.getByRole('radio', { name: 'Ubuntu 24.04 ROCm', exact: false })
  const customCard = customImage.locator('..').locator('.sw-os-image-picker__card')
  await expect(customCard.getByLabel('Disk deploy: Unavailable')).toBeVisible()
  await expect(customCard.getByLabel('RAM deploy: Verification in progress')).toBeVisible()
  await expect(customCard.getByText(/Available|Unavailable|Verification in progress|Supported|Failed|Not tested/)).toHaveCount(0)
  await expect.poll(() => workflowReads).toBe(1)

  await page.waitForTimeout(5_250)
  expect(workflowReads).toBe(1)
})

test('Server updates do not reload the image catalog and manual refresh keeps the current picker visible', async ({ page }) => {
  const catalogReads: string[] = []
  await installApiFixtures(page, {
    readyServerCount: 1,
    onOSImageCatalogRequest: (integrationId) => catalogReads.push(integrationId),
  })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()

  const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
  const search = dialog.getByRole('textbox', { name: 'Search deployment images' })
  const image = dialog.getByRole('radio', { name: 'Ubuntu 22.04 LTS', exact: false })
  await expect(image).toBeVisible()
  await expect.poll(() => catalogReads).toEqual(['maas-a'])

  await page.evaluate(async () => {
    const server = await fetch('/api/v1/servers/srv-1').then((response) => response.json())
    const sources = (window as typeof window & {
      __serverEventSources?: Array<{ emitMessage: (data: string) => void }>
    }).__serverEventSources ?? []
    for (const source of sources) {
      source.emitMessage(JSON.stringify({ type: 'upsert', server }))
    }
  })
  await page.waitForTimeout(250)
  expect(catalogReads).toEqual(['maas-a'])
  await expect(dialog.getByText('Loading deployable images…')).toHaveCount(0)

  await image.locator('..').click()
  await expect(image).toBeChecked()
  await search.fill('jammy')

  let releaseRefresh = () => {}
  const refreshGate = new Promise<void>((resolve) => {
    releaseRefresh = resolve
  })
  let refreshRequests = 0
  await page.route('**/api/v1/provisioning/images?*', async (route) => {
    refreshRequests += 1
    await refreshGate
    await route.fallback()
  })

  await dialog.getByRole('button', { name: 'Refresh' }).click()
  await expect.poll(() => refreshRequests).toBe(1)
  await expect(dialog.getByText('1 image in Ubuntu · Refreshing…', { exact: true })).toBeVisible()
  await expect(search).toBeEnabled()
  await expect(search).toHaveValue('jammy')
  await expect(image).toBeVisible()
  await expect(image).toBeChecked()
  await expect(dialog.getByText('Loading deployable images…')).toHaveCount(0)

  releaseRefresh()
  await expect.poll(() => catalogReads).toEqual(['maas-a', 'maas-a'])
  await expect(dialog.getByText('1 image in Ubuntu', { exact: true })).toBeVisible()
  await expect(image).toBeChecked()
})

test('fixed-target live preflight stops before Operating system and can open Networking', async ({ page }) => {
  let deploymentWrites = 0
  await installApiFixtures(page, {
    readyServerCount: 1,
    deploymentReadinessIssues: {
      'srv-1': 'No MAAS interface is linked to a subnet.',
    },
    onDeploymentRequest: () => { deploymentWrites += 1 },
  })
  await page.goto('/servers/srv-1/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()

  const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
  await expect(dialog.getByText('gpu-node-01: Deployment blocked')).toBeVisible()
  await expect(dialog.getByRole('heading', { name: 'Operating system', exact: true })).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: 'Check again' })).toBeVisible()
  await dialog.getByRole('button', { name: 'Networking' }).click()
  await expect(page).toHaveURL('/servers/srv-1/network?site=site-a')
  expect(deploymentWrites).toBe(0)
})

test('contextual draft confirmation, focus restoration, and submitting lock stay in-page', async ({ page }) => {
  let releaseDeployment = () => {}
  const gate = new Promise<void>((resolve) => {
    releaseDeployment = resolve
  })
  let deploymentWrites = 0
  await installApiFixtures(page, {
    readyServerCount: 1,
    deploymentRequestGate: gate,
    onDeploymentRequest: () => { deploymentWrites += 1 },
  })
  await page.goto('/servers?site=site-a')
  const trigger = page.getByRole('button', { name: 'Deploy OS for gpu-node-01' })

  await trigger.click()
  let dialog = page.getByRole('dialog', { name: 'Deploy OS' })
  await expect(dialog.getByRole('heading', { name: 'Operating system', exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: 'Close' }).click()
  await expect(dialog).toHaveCount(0)
  await expect(trigger).toBeFocused()

  await trigger.click()
  dialog = page.getByRole('dialog', { name: 'Deploy OS' })
  await expect(dialog.getByRole('heading', { name: 'Operating system', exact: true })).toBeVisible()
  await chooseOSImage(page, 'Ubuntu 22.04 LTS')
  await dialog.getByRole('button', { name: 'Next' }).click()
  await expect(dialog.getByRole('heading', { name: 'Installation', exact: true })).toBeVisible()
  await chooseOption(page, 'Cloud-init', 'Replace for this deployment')
  const cloudInit = '#cloud-config\nwrite_files:\n  - content: memory-only-marker'
  await dialog.getByLabel('Cloud-init user data').fill(cloudInit)
  const leakedCloudInit = await page.evaluate((marker) => ({
    url: window.location.href.includes(marker),
    history: JSON.stringify(window.history.state).includes(marker),
    local: Object.values(window.localStorage).some((value) => value.includes(marker)),
    session: Object.values(window.sessionStorage).some((value) => value.includes(marker)),
  }), 'memory-only-marker')
  expect(leakedCloudInit).toEqual({
    url: false,
    history: false,
    local: false,
    session: false,
  })
  await dialog.getByRole('button', { name: 'Close' }).click()
  const discard = page.getByRole('alertdialog', { name: 'Discard deployment draft?' })
  await expect(discard).toBeVisible()
  await discard.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toBeVisible()

  await dialog.getByRole('button', { name: 'Next' }).click()
  await expect(dialog.getByRole('heading', { name: 'Networking', exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: 'Next' }).click()
  await expect(dialog.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
  await dialog.getByRole('button', { name: 'Deploy OS', exact: true }).click()
  await expect.poll(() => deploymentWrites).toBe(1)
  await expect(dialog.getByRole('button', { name: 'Close' })).toBeDisabled()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()

  releaseDeployment()
  await expect(dialog).toHaveCount(0)
  await expect(page).toHaveURL('/servers?site=site-a')
  await expect(page.getByText('OS deployment started')).toBeVisible()
  await expect(page.getByRole('button', { name: 'View workflow' })).toBeVisible()
})
