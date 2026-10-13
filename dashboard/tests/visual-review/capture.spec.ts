import { expect, test, type Page, type TestInfo } from 'playwright/test'
import type { SoftwareAssignment } from '@/domain/software/types'
import { installApiFixtures } from '../e2e/fixtures'

const visualCases = [
  { name: 'login', path: '/login', heading: 'Sign in', authenticated: false, representative: true },
  { name: 'overview', path: '/?site=site-a', heading: 'Overview', authenticated: true, representative: true },
  { name: 'overview-alerts-not-configured', path: '/?site=site-a', heading: 'Overview', authenticated: true, representative: false },
  { name: 'overview-alerts-unavailable', path: '/?site=site-a', heading: 'Overview', authenticated: true, representative: false },
  { name: 'servers', path: '/servers?site=site-a', heading: 'Servers', authenticated: true, representative: true },
  { name: 'platform-list', path: '/platforms?site=site-a', heading: 'Platforms', authenticated: true, representative: false },
  { name: 'workflow-list', path: '/workflows?site=site-a', heading: 'Workflows', authenticated: true, representative: false },
  { name: 'os-image-list', path: '/provisioning/images?site=site-a', heading: 'OS images', authenticated: true, representative: false },
  { name: 'boot-isos', path: '/provisioning/boot-isos?site=site-a', heading: 'Boot ISOs', authenticated: true, representative: false },
  { name: 'boot-isos-needs-rebuild', path: '/provisioning/boot-isos?site=site-a', heading: 'Boot ISOs', authenticated: true, representative: false },
  { name: 'software-catalog', path: '/software?site=site-a', heading: 'Software', authenticated: true, representative: false },
  { name: 'software-docker-detail', path: '/software/docker-ce?site=site-a', heading: 'Docker CE', authenticated: true, representative: false },
  { name: 'software-nfs-detail', path: '/software/nfs?site=site-a', heading: 'NFS', authenticated: true, representative: false },
  { name: 'monitoring', path: '/monitoring?site=site-a', heading: 'Monitoring', authenticated: true, representative: false },
  { name: 'monitoring-pool-charts', path: '/monitoring?site=site-a&chartScope=pool&chartScopeId=pool-research', heading: 'Monitoring', authenticated: true, representative: false },
  { name: 'server-monitoring', path: '/servers/srv-1/monitoring?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: false },
  { name: 'platform-detail-slurm', path: '/platforms/platform-slurm?site=site-a', heading: 'research-slurm', authenticated: true, representative: false },
  { name: 'server-detail', path: '/servers/srv-1/summary?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: true },
  { name: 'server-detail-vm-power', path: '/servers/srv-2/summary?site=site-a', heading: 'gpu-node-02', authenticated: true, representative: false },
  { name: 'server-detail-vm-boot-media', path: '/servers/srv-2/summary?site=site-a', heading: 'gpu-node-02', authenticated: true, representative: false },
  { name: 'server-activity', path: '/servers/srv-1/activity?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: false },
  { name: 'server-detail-releasing', path: '/servers/srv-1/summary?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: false },
  { name: 'server-activity-releasing', path: '/servers/srv-1/activity?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: false },
  { name: 'platform-detail', path: '/platforms/platform-a?site=site-a', heading: 'production-k0s', authenticated: true, representative: false },
  { name: 'platform-wizard', path: '/platforms/deploy?site=site-a', heading: 'Deploy platform', authenticated: true, representative: false },
  { name: 'infrastructure', path: '/infrastructure/sites?site=site-a', heading: 'Infrastructure', authenticated: true, representative: false },
] as const

const visualSoftwareAssignments: SoftwareAssignment[] = [
  {
    serverId: 'srv-5', kind: 'docker-ce', roles: [], spec: { version: '27.3.1', enableApi: true },
    state: 'installed', lastWorkflowId: 'op-docker-installed', lastAppliedAt: '2026-08-26T06:00:00Z',
    createdAt: '2026-08-25T04:00:00Z', updatedAt: '2026-08-26T06:00:00Z',
  },
  {
    serverId: 'srv-1', kind: 'docker-ce', roles: [], spec: { enableApi: false },
    state: 'pending', lastWorkflowId: 'op-docker-installing', lastAppliedAt: null,
    createdAt: '2026-08-27T02:00:00Z', updatedAt: '2026-08-27T02:05:00Z',
  },
  {
    serverId: 'srv-2', kind: 'podman', roles: [], spec: { version: '5.2' },
    state: 'failed', lastWorkflowId: 'op-podman-failed', lastAppliedAt: null,
    createdAt: '2026-08-26T02:00:00Z', updatedAt: '2026-08-26T02:15:00Z',
  },
  {
    serverId: 'srv-5', kind: 'nfs', roles: ['client'], spec: { source: 'storage.internal:/gpu-data', mountPath: '/shared/gpu-data' },
    state: 'installed', lastWorkflowId: 'op-nfs-installed', lastAppliedAt: '2026-08-26T07:00:00Z',
    createdAt: '2026-08-24T02:00:00Z', updatedAt: '2026-08-26T07:00:00Z',
  },
  {
    serverId: 'srv-3', kind: 'nfs', roles: ['server'], spec: { exportPath: '/srv/training-data', exportOptions: 'rw,sync' },
    state: 'failed', lastWorkflowId: 'op-nfs-failed', lastAppliedAt: null,
    createdAt: '2026-08-26T03:00:00Z', updatedAt: '2026-08-26T03:15:00Z',
  },
]

async function setAppearance(page: Page, appearance: 'dark' | 'light') {
  await page.addInitScript(({ appearance }) => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    localStorage.setItem('swallow.appearance', appearance)
    localStorage.removeItem('swallow.navigation.collapsed')
    localStorage.removeItem('access_token')
  }, { appearance })
}

/**
 * Waits for a stable render before a screenshot. `settled` replaces the network-idle wait on routes
 * that poll continuously (a Releasing Server refreshes its projection every 2 seconds, so the
 * network never idles); it must resolve once the content under review has rendered.
 */
async function prepareCapture(page: Page, appearance: 'dark' | 'light', settled?: () => Promise<void>) {
  if (settled) await settled()
  else await page.waitForLoadState('networkidle')
  await page.evaluate(() => document.fonts.ready.then(() => true))
  if (appearance === 'dark') await expect(page.locator('html')).toHaveClass(/dark/)
  else await expect(page.locator('html')).not.toHaveClass(/dark/)
}

async function capture(page: Page, testInfo: TestInfo, name: string, fullPage = true) {
  await page.screenshot({
    path: testInfo.outputPath(`${name}-${testInfo.project.name}.png`),
    animations: 'disabled', caret: 'hide', fullPage,
  })
}

async function expectNoHorizontalOverflow(page: Page) {
  await expect.poll(() => page.evaluate(() => (
    document.documentElement.scrollWidth <= document.documentElement.clientWidth
  ))).toBe(true)
}

async function chooseOption(page: Page, label: string, option: string) {
  await page.getByRole('combobox', { name: label, exact: true }).click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

/**
 * Captures deterministic rendered pages for direct agent or developer inspection.
 *
 * These files are intentionally disposable: they live under the ignored Playwright output tree and
 * never become expected images. Test titles expose route names plus `@representative`, allowing the
 * caller to limit a review to the changed route or to the shared-layout sample with `--grep`.
 */
test.describe('dashboard visual review', () => {
  for (const visualCase of visualCases) {
    const title = `${visualCase.name}${visualCase.representative ? ' @representative' : ''}`

    test(title, async ({ page }, testInfo) => {
      const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'

      await installApiFixtures(page, {
        signedIn: visualCase.authenticated,
        ...(visualCase.name === 'servers'
          ? { ephemeralServerIds: ['srv-1'], lockedServerIds: ['srv-1'] }
          : {}),
        ...(visualCase.name === 'server-activity' ? { serverActivityItemCount: 12 } : {}),
        // Alertmanager is optional: a Site without one is a quiet card state, a failing one warns.
        ...(visualCase.name === 'overview-alerts-not-configured' ? { overviewAlertSource: 'not-configured' as const } : {}),
        ...(visualCase.name === 'overview-alerts-unavailable' ? { overviewAlertSource: 'unavailable' as const } : {}),
        // A release whose Workflow stopped waiting for Ready (its longest banner, with two links), and
        // one no Swallow Workflow covers (the Activity empty states that explain where it is recorded).
        ...(visualCase.name === 'server-detail-releasing'
          ? { providerWorkServerIds: { 'srv-1': 'releasing' as const }, releaseWorkflows: { 'srv-1': 'requires_attention' as const } }
          : {}),
        ...(visualCase.name === 'server-activity-releasing'
          ? { providerWorkServerIds: { 'srv-1': 'releasing' as const }, releaseWorkflows: { 'srv-1': null } }
          : {}),
        // A lab VM that enlisted without a power driver, its inspection stopped for it (decision 054).
        ...(visualCase.name === 'server-detail-vm-power'
          ? { powerConfigurations: { 'srv-2': { driver: '' } }, powerAttentionServerIds: ['srv-2'] }
          : {}),
        // A lab VM on a swallow hypervisor with Boot Media enabled through it (decision 055).
        ...(visualCase.name === 'server-detail-vm-boot-media'
          ? {
              powerConfigurations: { 'srv-2': { driver: 'virsh', address: 'qemu+ssh://ubuntu@192.168.40.21/system', powerId: 'gpu-node-02' } },
              libvirtBootMediaServerIds: ['srv-2'],
              bootMediaSetting: {
                enabled: true, isoId: 'iso-taipei', updatedAt: '2026-08-27T02:00:00Z', lastAppliedAt: '2026-08-27T02:00:00Z',
                lastAppliedBy: 'ensure', bootOverride: 'Continuous', lastErrorAt: null,
              },
            }
          : {}),
        // A Boot ISO built before the UEFI hand-off (decision 057), in use by a Server.
        ...(visualCase.name === 'boot-isos-needs-rebuild'
          ? {
              bootISOs: [{ id: 'iso-taipei', name: 'taipei-rack', integrationId: 'maas-a', rackAddress: '10.0.0.2', legacyTemplate: true }],
              bootMediaSetting: {
                enabled: true, isoId: 'iso-taipei', updatedAt: '2026-08-27T02:00:00Z', lastAppliedAt: '2026-08-27T02:00:00Z',
                lastAppliedBy: 'ensure', bootOverride: 'Continuous', lastErrorAt: null,
              },
            }
          : {}),
        ...(visualCase.name.startsWith('software-')
          ? { fleetSize: 5, softwareAssignments: visualSoftwareAssignments }
          : {}),
      })
      await setAppearance(page, appearance)

      await page.goto(visualCase.path)
      await expect(page.getByRole('heading', { name: visualCase.heading, exact: true }).first()).toBeVisible()
      const releasing = visualCase.name.endsWith('-releasing')
      const releaseBanner = page.getByRole('status').filter({ hasText: /Release needs attention|The provider is releasing this Server/ })
      await prepareCapture(page, appearance, releasing
        ? async () => {
            await expect(releaseBanner).toBeVisible()
            if (visualCase.name === 'server-activity-releasing') {
              await expect(page.locator('#activity-related-operations')).toContainText('No Swallow Workflow covers the current release')
            }
          }
        : undefined)
      if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
      await capture(page, testInfo, visualCase.name)
      // The full mobile page is too long to read at its own scale; the changed card is captured too.
      if (visualCase.name === 'server-detail-vm-boot-media') {
        await page.getByRole('region', { name: 'Power control' }).screenshot({
          path: testInfo.outputPath(`${visualCase.name}-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
        })
      }
      if (releasing) {
        await releaseBanner.screenshot({
          path: testInfo.outputPath(`${visualCase.name}-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
        })
      }
      // Usage charts are long on a phone; one viewport from the section's top shows the header,
      // window picker, scope pickers, and the first charts at their real scale.
      const crop = visualCase.name === 'server-monitoring' || visualCase.name === 'monitoring-pool-charts'
        ? page.locator('.sw-usage-charts')
        : visualCase.name === 'platform-detail-slurm'
          ? page.locator('section.sw-section').filter({ has: page.getByRole('heading', { name: 'Monitoring', exact: true }) })
          : null
      if (crop) {
        const box = await crop.boundingBox()
        const viewport = page.viewportSize()
        if (box && viewport) {
          await page.screenshot({
            path: testInfo.outputPath(`${visualCase.name}-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
            fullPage: true, clip: { x: 0, y: box.y, width: viewport.width, height: Math.min(box.height, viewport.height) },
          })
        }
      }
    })
  }
})

/** Captures the experimental Add servers path for libvirt virtual machines with a listed hypervisor. */
test.describe('Add servers virtual machines visual review', () => {
  test('add-servers-virtual-machines', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page)
    await setAppearance(page, appearance)
    await page.goto('/servers?site=site-a')
    await page.getByRole('button', { name: 'Add servers' }).first().click()
    const dialog = page.getByRole('dialog', { name: 'Add servers' })
    await dialog.getByRole('button', { name: /It is a libvirt virtual machine/ }).click()
    await chooseOption(page, 'Hypervisor', 'gpu-node-01 (192.168.40.21)')
    await dialog.getByRole('button', { name: 'List virtual machines' }).click()
    const machines = dialog.getByRole('group', { name: 'Virtual machines' })
    await expect(machines).toContainText('lab-vm-3')
    await machines.getByText('lab-vm-1', { exact: true }).click()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'add-servers-virtual-machines', false)
  })
})

/**
 * Captures the Integration settings page (it replaced the Edit integration dialog): each tab of a
 * MAAS provisioner — General, Connection, Credential, HTTP proxy (External, then an unapplied change
 * with a URL error), and Subnet DNS (a site-DHCP subnet with DNS applied beside an invalid entry) —
 * and the Create integration dialog that remains.
 */
test.describe('Integration settings visual review', () => {
  test('integration-settings-tabs', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, {
      httpProxies: { 'maas-a': { mode: 'external', httpProxy: 'http://10.170.168.20:18080' } },
      provisionerSubnets: {
        'maas-a': [
          { id: '1', name: 'lab-network', cidr: '192.168.40.0/24', gatewayAddress: '192.168.40.1', managed: true, provisionerDhcp: true, attached: true, dnsServers: [] },
          { id: '7', name: '10.170.168.0/23', cidr: '10.170.168.0/23', gatewayAddress: '10.170.169.254', managed: true, provisionerDhcp: false, attached: true, dnsServers: ['165.204.124.158', '165.204.219.249'] },
          { id: '12', name: '172.17.0.0/16', cidr: '172.17.0.0/16', gatewayAddress: '', managed: true, provisionerDhcp: false, attached: false, dnsServers: [] },
          { id: '14', name: '192.168.68.0/24', cidr: '192.168.68.0/24', gatewayAddress: '', managed: true, provisionerDhcp: false, attached: false, dnsServers: [] },
        ],
      },
    })
    await setAppearance(page, appearance)
    for (const tab of ['general', 'connection', 'credential'] as const) {
      await page.goto(`/infrastructure/integrations/maas-a/${tab}?site=site-a`)
      await expect(page.getByRole('heading', { level: 1, name: 'MAAS Taipei' })).toBeVisible()
      await expect(page.getByRole('tab', { selected: true })).toBeVisible()
      await prepareCapture(page, appearance)
      if (mobile) await expectNoHorizontalOverflow(page)
      await capture(page, testInfo, `integration-settings-${tab}`, false)
    }

    await page.goto('/infrastructure/integrations/maas-a/http-proxy?site=site-a')
    const proxy = page.getByRole('region', { name: 'HTTP proxy' })
    await expect(proxy.getByRole('status')).toHaveText('MAAS uses: External (http://10.170.168.20:18080).')
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'integration-settings-http-proxy', false)
    await chooseOption(page, 'Proxy mode', 'Peer')
    await proxy.getByRole('textbox', { name: /^Proxy URL/ }).fill('http://user:secret@squid.example:3128')
    await expect(proxy.getByText('Leave the user and password out; the URL is readable back.')).toBeVisible()
    await capture(page, testInfo, 'integration-settings-http-proxy-invalid', false)

    await page.goto('/infrastructure/integrations/maas-a/subnet-dns?site=site-a')
    const dns = page.getByRole('region', { name: 'Subnet DNS' })
    await dns.getByRole('textbox', { name: 'DNS servers for 192.168.40.0/24' }).fill('192.168.40.1, dns.lab')
    await expect(dns.getByText('dns.lab is not an IP address.')).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'integration-settings-subnet-dns', false)
    await dns.getByText('Other subnets (2)').click()
    await dns.getByRole('list', { name: 'Other subnets' }).scrollIntoViewIfNeeded()
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'integration-settings-subnet-dns-other', false)
  })

  test('integration-create-dialog', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page)
    await setAppearance(page, appearance)
    await page.goto('/infrastructure/integrations?site=site-a')
    await page.locator('.sw-page-header').getByRole('button', { name: 'Create integration' }).click()
    await expect(page.getByRole('dialog', { name: 'Create integration' })).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'integration-create-dialog', false)
  })
})

/** Captures both contextual entry points of the shared Managed Software installation flow. */
test.describe('Software installation flow visual review', () => {
  test('software detail multi-node dialog', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { fleetSize: 5, softwareAssignments: visualSoftwareAssignments })
    await setAppearance(page, appearance)
    await page.goto('/software/nfs?site=site-a')
    await page.getByRole('button', { name: 'Install software', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Install software' })
    await expect(dialog.getByRole('textbox', { name: 'Search installation targets' })).toBeVisible()
    await prepareCapture(page, appearance)
    if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'software-multi-node-dialog', false)
  })

  test('Server detail fixed-target dialog', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, {
      fleetSize: 5,
      softwareAssignments: [{
        ...visualSoftwareAssignments[0],
        state: 'failed',
        lastAppliedAt: null,
        lastWorkflowId: 'op-docker-failed',
      }],
    })
    await setAppearance(page, appearance)
    await page.goto('/servers/srv-5/summary?site=site-a')
    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: /Install software/ }).click()
    const dialog = page.getByRole('dialog', { name: 'Install software' })
    await expect(dialog.getByText('Choose and configure software for compute-node-005.')).toBeVisible()
    await prepareCapture(page, appearance)
    if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'software-server-dialog', false)
  })
})

/** Captures each contextual or embedded OS deployment adapter where its differences matter. */
test.describe('OS deployment flow visual review', () => {
  test('single Server dialog', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { readyServerCount: 1 })
    await page.route('**/api/v1/workflows?*', async (route) => {
      const url = new URL(route.request().url())
      if (url.searchParams.get('kind') !== 'verify-os-image') return route.fallback()
      const workflow = {
        id: 'verify-rocm-ram',
        schemaVersion: 3,
        kind: 'verify-os-image',
        intent: 'Verify Ubuntu 24.04 ROCm for RAM deployment',
        intentSnapshot: { request: { integrationId: 'maas-a', imageId: 'ubuntu-24.04-rocm', architecture: 'amd64', deployTarget: 'ram' } },
        status: 'running',
        statusReason: null,
        siteId: 'site-a',
        platformId: null,
        targetServerIds: ['srv-1'],
        steps: [],
        retryOfOperationId: null,
        execution: { runId: 'run-verify-rocm-ram', playbook: '', status: 'running', statusReason: null, startedAt: '2026-08-27T02:00:00Z', finishedAt: null },
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
    await setAppearance(page, appearance)
    await page.goto('/servers/srv-1/summary?site=site-a')
    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    await expect(dialog.getByRole('heading', { name: 'Operating system', exact: true })).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'os-deploy-single-dialog', false)

    let releaseRefresh = () => {}
    const refreshGate = new Promise<void>((resolve) => {
      releaseRefresh = resolve
    })
    await page.route('**/api/v1/provisioning/images?*', async (route) => {
      await refreshGate
      await route.fallback()
    })
    await dialog.getByRole('button', { name: 'Refresh' }).click()
    await expect(dialog.getByText('2 images in Ubuntu · Refreshing…', { exact: true })).toBeVisible()
    await expect(dialog.getByRole('radio', { name: 'Ubuntu 22.04 LTS', exact: false })).toBeVisible()
    await capture(page, testInfo, 'os-deploy-single-image-refresh', false)
    releaseRefresh()
    await expect(dialog.getByText('2 images in Ubuntu', { exact: true })).toBeVisible()

    const providerImage = dialog.getByRole('radio', { name: 'Ubuntu 22.04 LTS', exact: false })
    const providerCard = providerImage.locator('..').locator('.sw-os-image-picker__card')
    await providerCard.click()
    await expect(providerImage).toBeChecked()
    await providerImage.scrollIntoViewIfNeeded()
    await capture(page, testInfo, 'os-deploy-provider-image-metadata', false)

    await dialog.getByRole('button', { name: 'Custom 1', exact: true }).click()
    const customImage = dialog.getByRole('radio', { name: 'Ubuntu 24.04 ROCm', exact: false })
    const customCard = customImage.locator('..').locator('.sw-os-image-picker__card')
    await expect(customCard.getByLabel('Disk deploy: Unavailable')).toBeVisible()
    await expect(customCard.getByLabel('RAM deploy: Verification in progress')).toBeVisible()
    await customImage.scrollIntoViewIfNeeded()
    await capture(page, testInfo, 'os-deploy-custom-image-metadata', false)
  })

  test('single Server OS image search', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { readyServerCount: 1 })
    await setAppearance(page, appearance)
    await page.goto('/servers/srv-1/summary?site=site-a')
    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    await expect(dialog.getByRole('heading', { name: 'Operating system', exact: true })).toBeVisible()
    await dialog.getByRole('textbox', { name: 'Search deployment images' }).fill('24.04')
    await expect(dialog.getByRole('radio', { name: 'Ubuntu 24.04 LTS', exact: false })).toBeVisible()
    await expect(dialog.getByRole('radio', { name: 'Ubuntu 22.04 LTS', exact: false })).toHaveCount(0)
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'os-deploy-single-image-search', false)
  })

  test('single Server deployment choices', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { readyServerCount: 1 })
    await setAppearance(page, appearance)
    await page.goto('/servers/srv-1/summary?site=site-a')
    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    const image = dialog.getByRole('radio', { name: 'Ubuntu 22.04 LTS', exact: false })
    await image.locator('..').click()
    await expect(image).toBeChecked()
    await capture(page, testInfo, 'os-deploy-single-image-selected', false)
    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Installation', exact: true })).toBeVisible()
    const diskMode = dialog.getByRole('radio', { name: 'Disk deploy' })
    await diskMode.locator('..').locator('.sw-deployment-choice-card').click()
    await expect(diskMode).toBeChecked()

    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'os-deploy-single-install-choices', false)

    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Networking', exact: true })).toBeVisible()
    const staticMode = dialog.getByRole('radio', { name: 'Static', exact: true })
    await staticMode.locator('..').click()
    await expect(staticMode).toBeChecked()
    await expect(
      dialog.getByLabel('Static IPv4 address for gpu-node-01'),
    ).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'os-deploy-single-network-choices', false)
    await dialog.getByTestId('deploy-os-dialog-viewport').evaluate((viewport) => {
      viewport.scrollTop = viewport.scrollHeight
    })
    await capture(page, testInfo, 'os-deploy-single-network-assignment', false)
  })

  /**
   * The platform wizard's OS networking step on a site-DHCP network, where the shared addressing
   * field spans the wizard's form grid with its DHCP suggestion (decision 058).
   */
  test('platform-wizard-site-dhcp-networking', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { freePlatformCandidates: true, readyServerCount: 2, siteDhcpNetworkServerIds: ['srv-1', 'srv-2'] })
    await setAppearance(page, appearance)
    await page.goto('/platforms/deploy?site=site-a')
    await chooseOption(page, 'Platform type', 'Slurm')
    await page.getByLabel('Platform name').fill('site-dhcp-slurm')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()
    await page.getByRole('checkbox', { name: 'Run slurmctld on gpu-node-01' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-01' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-02' }).locator('..').click()
    await next.click()
    await page.getByRole('radio', { name: 'Ubuntu 24.04 LTS', exact: false }).locator('..').click()
    await next.click()
    await expect(page.getByRole('heading', { name: 'OS installation' })).toBeVisible()
    await next.click()
    await expect(page.getByRole('heading', { name: 'OS networking' })).toBeVisible()
    await expect(page.getByRole('radio', { name: 'DHCP', exact: true })).toBeChecked()
    await prepareCapture(page, appearance)
    if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-wizard-site-dhcp-networking', false)
  })

  /** The Networking step when inspection suggests DHCP for a site-DHCP subnet (decision 058). */
  test('single Server site-dhcp networking', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { readyServerCount: 1, siteDhcpNetworkServerIds: ['srv-1'] })
    await setAppearance(page, appearance)
    await page.goto('/servers/srv-1/summary?site=site-a')
    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: 'Deploy OS', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    await dialog.getByRole('radio', { name: 'Ubuntu 22.04 LTS', exact: false }).locator('..').click()
    await dialog.getByRole('button', { name: 'Next' }).click()
    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Networking', exact: true })).toBeVisible()
    await expect(dialog.getByRole('radio', { name: 'DHCP', exact: true })).toBeChecked()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'os-deploy-single-site-dhcp-networking', false)
  })

  test('bulk Server dialog', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, { readyServerCount: 2 })
    await setAppearance(page, appearance)
    await page.goto('/servers?site=site-a')
    for (const server of ['gpu-node-01', 'gpu-node-02']) {
      await page.getByLabel(`${mobile ? 'Mobile selection:' : 'Select'} ${server}`, { exact: true }).click()
    }
    await page.getByRole('region', { name: 'Selection actions' }).getByRole('button', { name: 'Deploy OS', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    await expect(dialog.getByRole('heading', { name: 'Operating system', exact: true })).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'os-deploy-bulk-dialog', false)
  })

  test('fixed OS image dialog', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { readyServerCount: 2 })
    await setAppearance(page, appearance)
    await page.goto('/provisioning/images?site=site-a')
    await page.getByRole('button', { name: 'Deploy Ubuntu 24.04 LTS', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
    await expect(dialog.getByRole('heading', { name: 'Deployment targets' })).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'os-deploy-fixed-image-targets', false)
    const firstTarget = dialog.getByLabel('Select gpu-node-01')
    await firstTarget.click()
    await expect(firstTarget).toBeChecked()
    await capture(page, testInfo, 'os-deploy-fixed-image-target-selected', false)
    await dialog.getByRole('button', { name: 'Next' }).click()
    await expect(dialog.getByRole('heading', { name: 'Installation', exact: true })).toBeVisible()
    await expect(
      dialog.getByRole('navigation', { name: 'Deployment progress' }).getByText('OS image', { exact: true }),
    ).toHaveCount(0)
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'os-deploy-fixed-image-installation', false)
  })

  test('Platform operating system step', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { freePlatformCandidates: true, readyServerCount: 1 })
    await setAppearance(page, appearance)
    await page.goto('/platforms/deploy?site=site-a')
    await page.getByLabel('Platform name').fill('visual-k0s')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()
    await chooseOption(page, 'Topology', 'Standalone (single Server)')
    await chooseOption(page, 'Role for gpu-node-01', 'Standalone node')
    await next.click()
    await expect(page.getByRole('heading', { name: 'Operating system', exact: true })).toBeVisible()
    const sharedImageStep = page.locator('.sw-os-image-selection-step')
    await expect(sharedImageStep).toHaveCount(1)
    await expect(sharedImageStep.locator('.sw-form-grid')).toHaveCount(0)
    const headerAlignment = await sharedImageStep.locator('.sw-os-image-selection-step__heading').evaluate((element) => {
      const heading = element.querySelector('h2')?.getBoundingClientRect()
      const refresh = element.querySelector('button')?.getBoundingClientRect()
      return {
        centerDelta: heading && refresh
          ? Math.abs((heading.top + heading.bottom) / 2 - (refresh.top + refresh.bottom) / 2)
          : Number.POSITIVE_INFINITY,
        horizontalGap: heading && refresh ? refresh.left - heading.right : Number.NEGATIVE_INFINITY,
      }
    })
    expect(headerAlignment.centerDelta).toBeLessThanOrEqual(1)
    expect(headerAlignment.horizontalGap).toBeGreaterThanOrEqual(8)
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'platform-os-image-step', false)

    const image = page.getByRole('radio', { name: 'Ubuntu 22.04 LTS', exact: false })
    await image.locator('..').click()
    await next.click()
    await expect(page.getByRole('heading', { name: 'OS installation' })).toBeVisible()
    await capture(page, testInfo, 'platform-os-installation-step', false)

    await next.click()
    await expect(page.getByRole('heading', { name: 'OS networking' })).toBeVisible()
    await capture(page, testInfo, 'platform-os-networking-step', false)
  })
})
