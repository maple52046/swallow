import { expect, test, type Locator, type Page, type TestInfo } from 'playwright/test'
import type {
  OperationNormalizedError,
  OperationStepWaiting,
  OperationTaskDiagnostic,
  OperationTimelineEvent,
} from '@/domain/operation/types'
import type { SoftwareAssignment } from '@/domain/software/types'
import { CONTROLLER_WORKFLOW_ID, expectPlatformInstallStarted, installApiFixtures } from '../e2e/fixtures'

const visualCases = [
  { name: 'login', path: '/login', heading: 'Sign in', authenticated: false, representative: true },
  { name: 'overview', path: '/?site=site-a', heading: 'Overview', authenticated: true, representative: true },
  { name: 'overview-alerts-not-configured', path: '/?site=site-a', heading: 'Overview', authenticated: true, representative: false },
  { name: 'overview-alerts-unavailable', path: '/?site=site-a', heading: 'Overview', authenticated: true, representative: false },
  { name: 'overview-without-alerts', path: '/?site=site-a', heading: 'Overview', authenticated: true, representative: false },
  { name: 'servers', path: '/servers?site=site-a', heading: 'Servers', authenticated: true, representative: true },
  { name: 'servers-hardware', path: '/servers?site=site-a&includeAbsent=true', heading: 'Servers', authenticated: true, representative: false },
  { name: 'platform-list', path: '/platforms?site=site-a', heading: 'Platforms', authenticated: true, representative: false },
  { name: 'workflow-list', path: '/workflows?site=site-a', heading: 'Activity', authenticated: true, representative: false },
  { name: 'change-plans', path: '/change-plans?site=site-a', heading: 'Activity', authenticated: true, representative: false },
  { name: 'os-image-list', path: '/provisioning/images?site=site-a', heading: 'OS Images', authenticated: true, representative: false },
  { name: 'os-image-templates', path: '/provisioning/templates?site=site-a', heading: 'OS Images', authenticated: true, representative: false },
  { name: 'boot-isos', path: '/infrastructure/boot-isos?site=site-a', heading: 'Infrastructure', authenticated: true, representative: false },
  { name: 'boot-isos-needs-rebuild', path: '/infrastructure/boot-isos?site=site-a', heading: 'Infrastructure', authenticated: true, representative: false },
  { name: 'software-catalog', path: '/software?site=site-a', heading: 'Software', authenticated: true, representative: false },
  { name: 'software-docker-detail', path: '/software/docker-ce?site=site-a', heading: 'Docker CE', authenticated: true, representative: false },
  { name: 'software-nfs-detail', path: '/software/nfs?site=site-a', heading: 'NFS', authenticated: true, representative: false },
  { name: 'monitoring', path: '/monitoring?site=site-a', heading: 'Monitoring', authenticated: true, representative: false },
  { name: 'monitoring-without-alerts', path: '/monitoring?site=site-a', heading: 'Monitoring', authenticated: true, representative: false },
  { name: 'monitoring-pool-charts', path: '/monitoring?site=site-a&chartScope=pool&chartScopeId=pool-research', heading: 'Monitoring', authenticated: true, representative: false },
  { name: 'server-monitoring', path: '/servers/srv-1/monitoring?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: false },
  { name: 'platform-detail-slurm', path: '/platforms/platform-slurm/overview?site=site-a', heading: 'research-slurm', authenticated: true, representative: false },
  { name: 'platform-detail-slurm-deployed', path: '/platforms/platform-slurm-ha/nodes?site=site-a', heading: 'lab-slurm-ha', authenticated: true, representative: false },
  { name: 'platform-detail-slurm-overview', path: '/platforms/platform-slurm-ha/overview?site=site-a', heading: 'lab-slurm-ha', authenticated: true, representative: false },
  { name: 'platform-detail-slurm-configuration', path: '/platforms/platform-slurm-ha/configuration?site=site-a', heading: 'lab-slurm-ha', authenticated: true, representative: false },
  { name: 'platform-detail-slurm-monitoring', path: '/platforms/platform-slurm-ha/monitoring?site=site-a', heading: 'lab-slurm-ha', authenticated: true, representative: false },
  { name: 'platform-detail-slurm-partitions', path: '/platforms/platform-slurm-ha/partitions?site=site-a', heading: 'lab-slurm-ha', authenticated: true, representative: false },
  { name: 'server-detail', path: '/servers/srv-1/summary?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: true },
  { name: 'server-detail-vm-power', path: '/servers/srv-2/summary?site=site-a', heading: 'gpu-node-02', authenticated: true, representative: false },
  { name: 'server-detail-vm-boot-media', path: '/servers/srv-2/summary?site=site-a', heading: 'gpu-node-02', authenticated: true, representative: false },
  { name: 'server-detail-boot-media-disabled', path: '/servers/srv-1/summary?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: false },
  { name: 'server-activity', path: '/servers/srv-1/activity?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: false },
  { name: 'server-detail-releasing', path: '/servers/srv-1/summary?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: false },
  { name: 'server-detail-failed', path: '/servers/srv-1/summary?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: false },
  { name: 'server-detail-inspection-required', path: '/servers/srv-1/summary?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: false },
  { name: 'server-activity-releasing', path: '/servers/srv-1/activity?site=site-a', heading: 'gpu-node-01', authenticated: true, representative: false },
  { name: 'platform-detail', path: '/platforms/platform-a/overview?site=site-a', heading: 'production-k0s', authenticated: true, representative: false },
  { name: 'platform-detail-kubernetes-configuration', path: '/platforms/platform-a/configuration?site=site-a', heading: 'production-k0s', authenticated: true, representative: false },
  { name: 'platform-detail-kubernetes-monitoring', path: '/platforms/platform-a/monitoring?site=site-a', heading: 'production-k0s', authenticated: true, representative: false },
  { name: 'platform-detail-kubernetes-cluster', path: '/platforms/platform-a/cluster?site=site-a', heading: 'production-k0s', authenticated: true, representative: false },
  { name: 'platform-detail-kubernetes-single-credential', path: '/platforms/platform-a/overview?site=site-a', heading: 'production-k0s', authenticated: true, representative: false },
  { name: 'platform-detail-uninstall-waiting', path: '/platforms/platform-a?site=site-a', heading: 'production-k0s', authenticated: true, representative: false },
  { name: 'platform-detail-uninstall-plan-required', path: '/platforms/platform-a?site=site-a', heading: 'production-k0s', authenticated: true, representative: false },
  { name: 'platform-detail-uninstall-starting', path: '/platforms/platform-a?site=site-a', heading: 'production-k0s', authenticated: true, representative: false },
  { name: 'platform-selector', path: '/platforms/deploy?site=site-a', heading: 'Deploy platform', authenticated: true, representative: false },
  { name: 'infrastructure', path: '/infrastructure/sites?site=site-a', heading: 'Infrastructure', authenticated: true, representative: false },
] as const

/** Cases whose header carries tabs (an area's or a detail page's); see the phone `-top` capture. */
const TAB_HEADER_CASES: ReadonlySet<string> = new Set([
  'workflow-list', 'change-plans', 'os-image-list', 'os-image-templates', 'boot-isos', 'infrastructure',
  'server-detail', 'platform-detail', 'platform-detail-kubernetes-cluster', 'platform-detail-slurm-deployed',
])

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
        // One Server in each health state: srv-1 N/A, srv-2 up, srv-3 unknown, srv-4 down.
        ...(visualCase.name === 'servers'
          ? {
              ephemeralServerIds: ['srv-1'], lockedServerIds: ['srv-1'], serverSync: { 'srv-2': 'drifted' as const, 'srv-3': 'requires_attention' as const },
              serverHealth: { 'srv-1': 'not_applicable' as const, 'srv-3': 'unknown' as const },
            }
          : {}),
        ...(visualCase.name === 'servers-hardware'
          ? { displayOnlyServerIds: ['srv-2'], cpuOnlyServerIds: ['srv-3'] }
          : {}),
        ...(visualCase.name.startsWith('monitoring')
          ? { serverHealth: { 'srv-2': 'unknown' as const, 'srv-3': 'not_applicable' as const } }
          : {}),
        ...(visualCase.name === 'server-activity' ? { serverActivityItemCount: 12 } : {}),
        // Alertmanager is optional: a Site without one is a quiet card state, a failing one warns.
        ...(visualCase.name === 'overview-alerts-not-configured' ? { overviewAlertSource: 'not-configured' as const } : {}),
        ...(visualCase.name === 'overview-alerts-unavailable' ? { overviewAlertSource: 'unavailable' as const } : {}),
        // A release whose Workflow stopped waiting for Ready (its longest banner, with two links), and
        // one no swallow Workflow covers (the Activity empty states that explain where it is recorded).
        ...(visualCase.name === 'server-detail-releasing'
          ? { providerWorkServerIds: { 'srv-1': 'releasing' as const }, releaseWorkflows: { 'srv-1': 'requires_attention' as const } }
          : {}),
        ...(visualCase.name === 'server-activity-releasing'
          ? { providerWorkServerIds: { 'srv-1': 'releasing' as const }, releaseWorkflows: { 'srv-1': null } }
          : {}),
        // A failed Server still held by the unfinished platform deployment: the provider-failure
        // banner lists the blocking Workflow as a link and explains retrying it there.
        ...(visualCase.name === 'server-detail-failed' ? { failedServerIds: ['srv-1'] } : {}),
        // vm-gpu-03: a failed disk deployment onto a layout that needs a new inspection.
        ...(visualCase.name === 'server-detail-inspection-required'
          ? { failedServerIds: ['srv-1'], inspectionRequiredServerIds: ['srv-1'] }
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
        // Boot Media disabled after use: the setting still names the last Boot ISO, not attached.
        ...(visualCase.name === 'server-detail-boot-media-disabled'
          ? {
              bootMediaSetting: {
                enabled: false, isoId: 'iso-taipei', updatedAt: '2026-08-27T02:00:00Z', lastAppliedAt: '2026-08-27T01:00:00Z',
                lastAppliedBy: 'preflight', bootOverride: 'Once', lastErrorAt: null,
              },
            }
          : {}),
        // A Boot ISO built before the UEFI hand-off (decision 047), in use by a Server.
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
        // A controller-requested Workflow whose long requester must truncate inside its column.
        ...(visualCase.name === 'workflow-list' ? { controllerWorkflow: 'succeeded' as const } : {}),
        // A deployed Slurm Platform with controller-only nodes, counted as members.
        ...(visualCase.name === 'platform-list' ? { slurmDeployed: true } : {}),
        // Platform details name the last deployment, so the deployed Platforms get their succeeded
        // deployment Workflows; the HA Slurm Platform's container Grafana brought Docker CE.
        ...(visualCase.name === 'platform-detail' || visualCase.name.startsWith('platform-detail-kubernetes-') ? { platformDeploymentWorkflows: true } : {}),
        // An uninstall that has not started: waiting under the safety switch, rejected, or approved.
        ...(visualCase.name === 'platform-detail-uninstall-waiting' ? { platformRemovals: { 'platform-a': 'awaiting_approval' as const } } : {}),
        ...(visualCase.name === 'platform-detail-uninstall-plan-required' ? { platformRemovals: { 'platform-a': 'plan_required' as const } } : {}),
        ...(visualCase.name === 'platform-detail-uninstall-starting' ? { platformRemovals: { 'platform-a': 'converging' as const } } : {}),
        ...(visualCase.name.startsWith('platform-detail-slurm-') && visualCase.name !== 'platform-detail-slurm' ? { slurmDeployed: true, platformDeploymentWorkflows: true } : {}),
        // A Kubernetes Platform last deployed before the credential split: Cluster access says so.
        ...(visualCase.name === 'platform-detail-kubernetes-single-credential' ? { singleClusterCredential: true } : {}),
      })
      await setAppearance(page, appearance)
      // The release view of basic monitoring: alerts are the one monitoring surface still in
      // development, so these cases switch them off through the dev-only setting.
      if (visualCase.name.endsWith('-without-alerts')) {
        await page.addInitScript(() => {
          localStorage.setItem('swallow.dev.experimentalFeatures', JSON.stringify({ alerts: false }))
        })
      }

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
      // The provider-failure banner and its blocking list load after the heading; wait for them so
      // the capture never shows the page before the Server's failed state arrived.
      if (visualCase.name === 'server-detail-failed' || visualCase.name === 'server-detail-inspection-required') {
        await expect(page.getByRole('list', { name: 'Operations blocking recovery' })).toBeVisible()
      }
      if (visualCase.name === 'server-detail-inspection-required') {
        await expect(page.getByRole('button', { name: 'Inspect hardware' })).toBeVisible()
      }
      if (visualCase.name === 'platform-detail-kubernetes-single-credential') {
        await expect(page.getByText('Cluster credentials are not separated yet')).toBeVisible()
      }
      if (visualCase.name === 'servers-hardware') {
        const mobile = testInfo.project.name.startsWith('mobile-')
        const serverSurface = (name: string) => (
          mobile
            ? page.getByRole('article').filter({ hasText: name })
            : page.getByRole('table', { name: 'Servers' }).getByRole('row').filter({ hasText: name }).first()
        )
        await expect(serverSurface('gpu-node-01').getByRole('group', { name: 'GPU: AMD, 8 GPUs' })).toBeVisible()
        await expect(serverSurface('gpu-node-02').getByRole('group', { name: /^GPU:/ })).toHaveCount(0)
        await expect(serverSurface('gpu-node-04').locator('.sw-server-hardware-summary')).toHaveText('Not reported')
      }
      // Plan cards are titled by name once the Platform and Server reads answer; the expired plan's
      // deleted Platform keeps its id.
      if (visualCase.name === 'change-plans') {
        await expect(page.getByRole('article', { name: 'Change Plan for Platform research-slurm' })).toContainText('gpu-node-03')
        await expect(page.getByRole('article', { name: 'Change Plan for Platform platform-retired' })).toBeVisible()
      }
      if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
      await capture(page, testInfo, visualCase.name)
      // A long phone page is unreadable at full-page scale; its first screen shows the area header,
      // its actions, and the tab bar at real scale.
      if (testInfo.project.name.startsWith('mobile-') && TAB_HEADER_CASES.has(visualCase.name)) {
        await capture(page, testInfo, `${visualCase.name}-top`, false)
      }
      if (visualCase.name.startsWith('platform-detail') && testInfo.project.name.startsWith('desktop-')) {
        await page.setViewportSize({ width: 1280, height: 800 })
        await expectNoHorizontalOverflow(page)
        await capture(page, testInfo, `${visualCase.name}-1280`)
        await page.setViewportSize({ width: 1440, height: 900 })
      }
      // The first plan card at its real scale (names, ids, copy buttons), then the approval
      // confirmation, opened on Cancel, with its singular-or-plural destructive count.
      if (visualCase.name === 'change-plans') {
        // A page clip rather than an element capture, so the sticky shell does not cover the card.
        const box = await page.getByRole('article', { name: 'Change Plan for Platform research-slurm' }).boundingBox()
        const viewport = page.viewportSize()
        if (box && viewport) {
          await page.screenshot({
            path: testInfo.outputPath(`${visualCase.name}-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
            fullPage: true, clip: { x: 0, y: box.y, width: viewport.width, height: Math.min(box.height, viewport.height) },
          })
        }
        await page.getByRole('article', { name: 'Change Plan for Platform research-slurm' }).getByRole('button', { name: 'Approve plan' }).click()
        await expect(page.getByRole('alertdialog', { name: 'Approve Change Plan' }).getByRole('button', { name: 'Cancel' })).toBeFocused()
        await capture(page, testInfo, `${visualCase.name}-approve-dialog`, false)
      }
      // The Health column appears only when the table has room (87rem), which the 1440 baseline does
      // not give it, so the desktop runs also capture the table at a wide viewport with its badges.
      if (visualCase.name === 'servers' && testInfo.project.name.startsWith('desktop-')) {
        await page.setViewportSize({ width: 1800, height: 900 })
        const table = page.getByRole('table', { name: 'Servers' })
        await expect(table.getByRole('columnheader', { name: 'Health', exact: true })).toBeVisible()
        await table.screenshot({
          path: testInfo.outputPath(`${visualCase.name}-health-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
        })
      }
      // The full mobile page is too long to read at its own scale; the changed card is captured too.
      if (visualCase.name === 'server-detail-vm-boot-media') {
        await page.getByRole('region', { name: 'Power control' }).screenshot({
          path: testInfo.outputPath(`${visualCase.name}-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
        })
      }
      if (visualCase.name === 'server-detail-boot-media-disabled') {
        await page.getByRole('region', { name: 'Boot media' }).screenshot({
          path: testInfo.outputPath(`${visualCase.name}-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
        })
      }
      // The uninstall notice is the first thing in the Overview; it is read at its real scale.
      if (visualCase.name.startsWith('platform-detail-uninstall-')) {
        await page.locator('.sw-platform-lifecycle-notice').first().screenshot({
          path: testInfo.outputPath(`${visualCase.name}-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
        })
      }
      if (releasing) {
        await releaseBanner.screenshot({
          path: testInfo.outputPath(`${visualCase.name}-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
        })
      }
      if (visualCase.name === 'server-detail-inspection-required') {
        await page.getByText('Inspect this Server before a disk deployment', { exact: true })
          .locator('xpath=ancestor::div[.//button[normalize-space()="Inspect hardware"]][1]').screenshot({
          path: testInfo.outputPath(`${visualCase.name}-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
        })
      }
      if (visualCase.name === 'server-detail-failed') {
        // The banner root has no role, so crop the nearest ancestor holding its title and its action.
        await page.getByText('Provider lifecycle failed', { exact: true })
          .locator('xpath=ancestor::div[.//button[normalize-space()="Cancel blocking Operations and Recover"]][1]').screenshot({
          path: testInfo.outputPath(`${visualCase.name}-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
        })
      }
      // Usage charts are long on a phone; capture one viewport from the section's top at real scale.
      const crop = visualCase.name === 'server-monitoring' || visualCase.name === 'monitoring-pool-charts'
        ? page.locator('.sw-usage-charts')
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

/**
 * Captures the Server list's power dialog for a Server enrolled with its OS kept and no power
 * driver (decision 054): the Power Configuration form it opens straight to, the power actions with
 * the live read after a save, and the warning for a driver the provisioner cannot read. Then the
 * Add servers existing-OS step with its opt-in BMC setup.
 */
test.describe('Server power setup visual review', () => {
  test('server-power-setup', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, {
      powerStates: { 'srv-1': 'unknown', 'srv-2': 'error' },
      powerConfigurations: { 'srv-1': { driver: '' } },
    })
    await setAppearance(page, appearance)
    await page.goto('/servers?site=site-a')
    const powerButton = (name: string) => page.getByRole('button', { name: new RegExp(`^Power actions for ${name}`) }).filter({ visible: true }).first()

    // The button itself says the Server has no power driver instead of an unexplained UNKNOWN.
    await expect(powerButton('gpu-node-01')).toHaveText('NO DRIVER')
    await prepareCapture(page, appearance)
    const surface = mobile
      ? page.getByRole('article').filter({ hasText: 'gpu-node-01' }).first()
      : page.getByRole('table', { name: 'Servers' }).getByRole('row').filter({ hasText: 'gpu-node-01' }).first()
    await surface.screenshot({ path: testInfo.outputPath(`server-power-button-no-driver-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide' })

    await powerButton('gpu-node-01').click()
    const setup = page.getByRole('dialog', { name: 'Power configuration' })
    await expect(setup).toContainText('has no power driver the provisioner can use')
    await setup.getByRole('combobox', { name: 'Driver' }).click()
    await page.getByRole('option', { name: 'IPMI — BMC', exact: true }).click()
    await setup.getByRole('textbox', { name: /^BMC address/ }).fill('192.0.2.30')
    await setup.getByRole('textbox', { name: 'Account' }).fill('maas')
    await setup.getByLabel('Password', { exact: true }).fill('bmc-secret')
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'server-power-dialog-setup', false)
    await setup.getByRole('button', { name: 'Show password' }).click()
    await expect(setup.getByLabel('Password', { exact: true })).toHaveAttribute('type', 'text')
    await capture(page, testInfo, 'server-power-dialog-password-shown', false)

    // A wrong password: the provisioner cannot read the power, so the save is refused in place.
    await page.route('**/api/v1/servers/srv-1/power-configuration', (route) => (
      route.request().method() === 'PUT'
        ? route.fulfill({ status: 400, json: { error: {
            code: 'validation_error', requestId: 'req-power-check',
            message: 'The provisioner cannot read the power through this configuration (Failed to query power: password invalid); the previous configuration was kept',
          } } })
        : route.fallback()
    ))
    await setup.getByRole('button', { name: 'Save' }).click()
    await expect(setup.getByText('The power configuration was not changed')).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'server-power-dialog-refused', false)
    await setup.getByRole('button', { name: 'Cancel' }).click()

    const actions = page.getByRole('dialog', { name: 'Power actions' })
    await powerButton('gpu-node-02').click()
    await expect(actions.getByText('The provisioner cannot read its power')).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'server-power-dialog-unreadable', false)
    await actions.getByRole('button', { name: 'Cancel' }).click()

    await page.getByRole('button', { name: 'Add servers' }).first().click()
    const guide = page.getByRole('dialog', { name: 'Add servers' })
    await guide.getByRole('button', { name: /Yes, keep its OS/ }).click()
    await guide.getByText('Also set up its BMC over IPMI').click()
    await expect(guide.getByLabel('Enrollment command', { exact: true })).toContainText('--configure-bmc')
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'add-servers-existing-os-bmc', false)
  })
})

/**
 * Captures the Server and Platform operations polish: a deploying Platform's running cue on the
 * list and detail header, the Deployed OS link of a Server whose last deployment failed, the bulk
 * delete dialog with its Platform and running-work warnings, and the Release dialog opting in to
 * cancel the running work.
 */
test.describe('Server and Platform operations visual review', () => {
  test('ops-polish', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    // Without Platform Claims so the Release dialog can open for these Servers; the claim-disabled
    // Release has its own capture below.
    await installApiFixtures(page, { platformLifecycleStates: { 'platform-a': 'deploying' }, withoutPlatformClaims: true })
    await setAppearance(page, appearance)

    await page.goto('/platforms?site=site-a')
    await expect(page.getByText('Deploying', { exact: true }).filter({ visible: true }).first()).toBeVisible()
    const listSpinner = page.locator('.sw-platform-lifecycle-badge .sw-progress-spinner').filter({ visible: true }).first()
    await prepareCapture(page, appearance, () => expect(listSpinner).toBeVisible())
    if (mobile) {
      await expectNoHorizontalOverflow(page)
      await page.locator('.sw-platform-lifecycle-badge:has(.sw-progress-spinner)').filter({ visible: true }).first()
        .evaluate((badge) => badge.scrollIntoView({ block: 'center' }))
    }
    await capture(page, testInfo, 'platform-list-deploying', false)

    await page.goto('/platforms/platform-a?site=site-a')
    await prepareCapture(page, appearance, () => expect(page.locator('.sw-platform-lifecycle-badge .sw-progress-spinner')).toBeVisible())
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-detail-deploying', false)

    await page.goto('/servers/srv-4/summary?site=site-a')
    const statusCard = page.getByRole('heading', { name: 'Current state' }).locator('xpath=../../..')
    await expect(statusCard.getByRole('link', { name: 'Ubuntu 24.04 LTS' })).toBeVisible()
    await prepareCapture(page, appearance)
    await statusCard.screenshot({ path: testInfo.outputPath(`server-detail-os-link-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide' })

    await page.goto('/servers?site=site-a')
    // Mobile cards label their selection control differently from the desktop table rows.
    const selection = mobile ? 'Mobile selection: ' : 'Select '
    await page.getByLabel(`${selection}gpu-node-01`, { exact: true }).check()
    await page.getByLabel(`${selection}gpu-node-04`, { exact: true }).check()
    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: 'Delete only', exact: true }).click()
    const deleteDialog = page.getByRole('alertdialog', { name: 'Delete 2 servers' })
    await expect(deleteDialog).toContainText('2 of these servers have running operations')
    await deleteDialog.getByLabel('Server count confirmation').fill('2')
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'server-bulk-delete-dialog', false)
    await deleteDialog.getByRole('button', { name: 'Cancel' }).click()

    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: 'Release', exact: true }).click()
    const releaseDialog = page.getByRole('dialog', { name: 'Release 2 servers' })
    await expect(releaseDialog).toContainText('These servers have 2 running operations')
    await releaseDialog.getByLabel('Cancel running operations before releasing').locator('..').click()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'server-release-cancel-running', false)
  })
})

/**
 * Captures Platform removal through a Change Plan (decision 076): the Uninstall dialog for a server
 * release with the cancel consent, the Platform page after the confirmed release started, the same
 * release waiting in Change Plans while the safety switch is on (the toast and the policy card),
 * and a Platform whose install has not started, with the Delete dialog that withdraws it.
 */
test.describe('Platform removal plan visual review', () => {
  test('platform-removal-plan', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, { platformLifecycleStates: { 'platform-b': 'planned' } })
    await setAppearance(page, appearance)

    await page.goto('/platforms/platform-a?site=site-a')
    await page.getByRole('button', { name: 'Uninstall platform', exact: true }).click()
    await page.getByRole('dialog', { name: 'Uninstall platform' })
      .getByLabel('Release all servers instead of uninstalling platform software').locator('..').click()
    // Releasing the servers retitles the dialog.
    const dialog = page.getByRole('dialog', { name: 'Release platform servers' })
    await dialog.getByLabel('Platform name confirmation').fill('production-k0s')
    await dialog.getByLabel('Cancel these workflows, then release the servers').locator('..').click()
    await expect(dialog.getByRole('button', { name: 'Release servers' })).toBeEnabled()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'platform-uninstall-proposal-dialog', false)

    await dialog.getByRole('button', { name: 'Release servers' }).click()
    await expect(page.getByText('Server release started')).toBeVisible()
    await expect(page.getByText('Platform uninstall is running')).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-uninstall-started', false)

    await page.goto('/platforms/platform-b?site=site-a')
    await expect(page.getByText('Planned').first()).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-detail-planned', false)
    await page.getByRole('button', { name: 'Delete platform', exact: true }).click()
    const deleteDialog = page.getByRole('dialog', { name: 'Delete platform record' })
    await expect(deleteDialog.getByText('Nothing was installed')).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'platform-planned-delete-dialog', false)
  })

  test('platform-removal-waiting', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, { requireSeparateApproval: true })
    await setAppearance(page, appearance)

    await page.goto('/platforms/platform-a?site=site-a')
    await page.getByRole('button', { name: 'Uninstall platform', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Uninstall platform' })
    await dialog.getByLabel('Platform name confirmation').fill('production-k0s')
    await dialog.getByLabel('Cancel these workflows, then uninstall').locator('..').click()
    await dialog.getByRole('button', { name: 'Uninstall', exact: true }).click()
    await expect(page.getByText('Uninstall waits for approval')).toBeVisible()
    await expect(page.getByText('Why it waits: the Change Plan policy requires a separate approval.').first()).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-uninstall-waiting', false)
  })
})

/**
 * Captures Release and Delete disabled by a Platform Claim: the row Actions menu, the bulk Take
 * action menu with two claimed Servers, and Server Detail for a failed deployment's target, each
 * with the one note naming the Platform above those two actions.
 */
test.describe('Platform Claim visual review', () => {
  test('server-platform-claim', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page)
    await setAppearance(page, appearance)
    const note = (text: string) => page.getByRole('group', { name: text })

    await page.goto('/servers?site=site-a')
    await page.getByRole('button', { name: 'Actions for gpu-node-01', exact: true }).filter({ visible: true }).click()
    await expect(note('This server belongs to Platform production-k0s and cannot be released or deleted.')).toBeVisible()
    await prepareCapture(page, appearance)
    // The note wraps inside the menu rather than widening it past a phone screen.
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'server-row-release-claimed', false)
    await page.keyboard.press('Escape')

    const selection = mobile ? 'Mobile selection: ' : 'Select '
    await page.getByLabel(`${selection}gpu-node-01`, { exact: true }).check()
    await page.getByLabel(`${selection}gpu-node-02`, { exact: true }).check()
    await page.getByRole('button', { name: 'Take action' }).click()
    await expect(note('2 selected servers belong to Platform production-k0s and cannot be released or deleted.')).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'server-bulk-release-claimed', false)
    await page.keyboard.press('Escape')

    await page.goto('/servers/srv-4/summary?site=site-a')
    await page.getByRole('button', { name: 'Take action' }).click()
    await expect(note('This server belongs to Platform edge-staging and cannot be released or deleted.')).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'server-detail-release-claimed', false)
  })
})

/**
 * Captures the AMD GPU Operator feature (decision 062): the deploy wizard's Features step with the
 * proposed operator expanded in DRA mode, the Review step listing its settings and GPU monitoring,
 * the Platform's GPU Operator section, its Configure dialog, the Cluster tab's GPU column, and the
 * Install dialog of a Platform without an operator.
 */
test.describe('AMD GPU Operator visual review', () => {
  test('gpu-operator', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, { freePlatformCandidates: true })
    await setAppearance(page, appearance)

    await page.goto('/platforms/deploy/kubernetes?site=site-a')
    await page.getByLabel('Platform name').fill('gpu-k0s')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()
    await page.getByRole('radio', { name: /Multi-node \(non-HA\)/ }).locator('xpath=following-sibling::*[1]').click()
    await chooseOption(page, 'Role for gpu-node-01', 'Control-plane')
    await chooseOption(page, 'Role for gpu-node-02', 'Worker')
    await chooseOption(page, 'Role for gpu-node-03', 'Worker')
    await next.click()
    // The AMD workers make the wizard propose the operator with its Device Metrics Exporter.
    await expect(page.getByRole('switch', { name: 'Enable AMD GPU Operator' })).toBeChecked()
    await expect(page.getByRole('switch', { name: 'Device Metrics Exporter' })).toBeChecked()
    await chooseOption(page, 'GPU allocation mode', 'DRA')
    await expect(page.getByRole('combobox', { name: 'DRA driver version' })).toContainText('v1.0.1')
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'gpu-operator-wizard-features')

    await next.click()
    await next.click()
    await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await expect(page.getByText('Device Metrics Exporter, collected by the Site Prometheus', { exact: true })).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'gpu-operator-wizard-review')

    await page.goto('/platforms/platform-a/configuration?site=site-a')
    const section = page.getByRole('region', { name: 'AMD GPU Operator' })
    await expect(section.getByText('Ready', { exact: true })).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await section.screenshot({ path: testInfo.outputPath(`gpu-operator-platform-section-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide' })

    await section.getByRole('button', { name: 'Configure' }).click()
    const dialog = page.getByRole('dialog', { name: 'Configure AMD GPU Operator' })
    await expect(dialog.getByRole('combobox', { name: 'GPU Operator version' })).toContainText('v1.5.1')
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'gpu-operator-configure-dialog', false)
    await dialog.getByRole('button', { name: 'Cancel' }).click()

    await page.getByRole('tablist', { name: 'Platform workspace' }).getByRole('tab', { name: 'Cluster' }).click()
    await expect(page.getByText('8 × amd.com/gpu')).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'gpu-operator-cluster-nodes')
  })

  test('gpu-operator-install-dialog', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { withoutPlatformGPUOperator: true })
    await setAppearance(page, appearance)

    await page.goto('/platforms/platform-a/configuration?site=site-a')
    const section = page.getByRole('region', { name: 'AMD GPU Operator' })
    await section.getByRole('button', { name: 'Install' }).click()
    const dialog = page.getByRole('dialog', { name: 'Install AMD GPU Operator' })
    await expect(dialog.getByRole('combobox', { name: 'GPU Operator version' })).toContainText('v1.5.1')
    await expect(dialog.getByRole('switch', { name: 'Device Metrics Exporter' })).toBeChecked()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'gpu-operator-install-dialog', false)
  })
})

/**
 * Captures the Kubernetes Platform Grafana (monitoring.md §6.2): its card in a CPU-only deploy's
 * Features step, its line on the Review step, and the Monitoring section of a Kubernetes Platform
 * that links it. Element captures keep the sticky shell out of the card being inspected.
 */
test.describe('Kubernetes Platform Grafana visual review', () => {
  test('kubernetes-platform-grafana', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, { freePlatformCandidates: true })
    await setAppearance(page, appearance)

    await page.goto('/platforms/deploy/kubernetes?site=site-a')
    await page.getByLabel('Platform name').fill('cpu-k0s')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()
    await page.getByRole('radio', { name: /Standalone/ }).locator('xpath=following-sibling::*[1]').click()
    await chooseOption(page, 'Role for gpu-node-04', 'Standalone node')
    await next.click()
    const card = page.getByRole('region', { name: 'Platform Grafana' })
    await expect(page.getByRole('switch', { name: 'Enable Platform Grafana' })).toBeChecked()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await card.screenshot({ path: testInfo.outputPath(`kubernetes-grafana-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide' })

    await next.click()
    await next.click()
    await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'kubernetes-grafana-review')

    await page.goto('/platforms/platform-a/monitoring?site=site-a')
    const monitoring = page.getByRole('heading', { name: 'Monitoring entries', exact: true }).locator('xpath=ancestor::section[1]')
    await expect(monitoring.getByRole('link', { name: 'Open Platform Grafana' })).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await monitoring.screenshot({ path: testInfo.outputPath(`kubernetes-grafana-monitoring-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide' })
  })
})

/**
/**
 * Captures the runtime-specific Kubernetes workspace at topology, network, and grouped Review.
 * Desktop runs at 1280px to exercise the compact 13-inch hierarchy; mobile verifies card parity.
 */
test.describe('Kubernetes deployment studio visual review', () => {
  test('kubernetes-deployment-studio', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    if (!mobile) await page.setViewportSize({ width: 1280, height: 800 })
    // gpu-node-01 reports the unrounded storage a provider can; the table rounds it.
    await installApiFixtures(page, { freePlatformCandidates: true, serverStorageGB: { 'srv-1': 2000.398934016 } })
    await setAppearance(page, appearance)

    await page.goto('/platforms/deploy/kubernetes?site=site-a')
    await page.getByLabel('Platform name').fill('studio-k0s')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()
    // A new deploy starts multi-node (non-HA); the capture shows that default before HA is chosen.
    await expect(page.getByRole('radio', { name: /Multi-node \(non-HA\)/ })).toBeChecked()
    await prepareCapture(page, appearance)
    await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'kubernetes-wizard-topology-default')
    await page.getByRole('radio', { name: /High availability/ }).locator('xpath=following-sibling::*[1]').click()
    for (const server of ['gpu-node-01', 'gpu-node-02', 'gpu-node-03']) {
      await chooseOption(page, `Role for ${server}`, 'Control-plane')
    }
    await chooseOption(page, 'Role for gpu-node-04', 'Worker')
    await prepareCapture(page, appearance)
    await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'kubernetes-wizard-topology')

    await next.click()
    await next.click()
    await page.getByLabel('API virtual IP').fill('192.168.40.200')
    await prepareCapture(page, appearance)
    await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'kubernetes-wizard-networking')

    await next.click()
    await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await prepareCapture(page, appearance)
    await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'kubernetes-wizard-review')
  })
})

/**
 * The direct API endpoint of a standalone Kubernetes Platform whose control plane is provisioned:
 * with Automatic addressing the address is known only after provisioning; with Static it is the
 * address the user enters (platform-deployment.md §2).
 */
test.describe('Kubernetes provisioned endpoint visual review', () => {
  test('kubernetes-provisioned-endpoint', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, { freePlatformCandidates: true, readyServerCount: 1 })
    await setAppearance(page, appearance)

    await page.goto('/platforms/deploy/kubernetes?site=site-a')
    await page.getByLabel('Platform name').fill('edge-k0s')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()
    await page.getByRole('radio', { name: /Standalone/ }).locator('xpath=following-sibling::*[1]').click()
    await chooseOption(page, 'Role for gpu-node-01', 'Standalone node')
    await next.click()
    await next.click()
    await page.getByRole('radio', { name: 'Ubuntu 24.04 LTS', exact: false }).locator('..').click()
    await expect(page.getByRole('heading', { name: 'OS networking' })).toBeVisible()
    await expect(page.getByRole('radio', { name: 'Automatic', exact: true })).toBeChecked()
    await next.click()
    await expect(page.getByText("gpu-node-01's address after provisioning, port 6443", { exact: false })).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'kubernetes-provisioned-endpoint-automatic')

    await page.getByRole('button', { name: 'Back' }).click()
    await page.getByRole('radio', { name: 'Static', exact: true }).locator('xpath=following-sibling::*[1]').click()
    await page.getByLabel('Static IPv4 address for gpu-node-01').fill('192.168.40.50')
    await next.click()
    await expect(page.getByText('192.168.40.50:6443', { exact: false }).first()).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'kubernetes-provisioned-endpoint-static')
    await next.click()
    await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await expect(page.getByText('192.168.40.50:6443 (direct)', { exact: true })).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'kubernetes-provisioned-endpoint-review')
  })
})

/**
 * Captures the Device Metrics Exporter paths (decision 066): the Features step with the exporter
 * chosen, the Container runtime card with native OCI and Docker CE, and PMIx, and a Platform whose exporter reports a collapsed GPU
 * identity, which the page must state in words.
 */
test.describe('Slurm Device Metrics Exporter visual review', () => {
  test('slurm-dme', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, { freePlatformCandidates: true })
    await setAppearance(page, appearance)

    await page.goto('/platforms/deploy/slurm?site=site-a')
    await page.getByLabel('Platform name').fill('dme-slurm')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()
    await page.getByRole('checkbox', { name: 'Run slurmctld on gpu-node-01' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-02' }).locator('..').click()
    await next.click()
    await page.getByRole('radio', { name: /AMD Device Metrics Exporter/ }).locator('xpath=following-sibling::*[1]').click()
    await page.getByRole('switch', { name: 'Enable Container runtime' }).locator('xpath=following-sibling::*[1]').click()
    await page.getByRole('checkbox', { name: /Docker CE/ }).locator('xpath=following-sibling::*[1]').click()
    await expect(page.getByRole('checkbox', { name: /Docker CE/ })).toBeChecked()
    await page.getByRole('switch', { name: 'Enable MPI with PMIx' }).locator('xpath=following-sibling::*[1]').click()
    await expect(page.getByRole('radio', { name: /AMD Device Metrics Exporter/ })).toBeChecked()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'slurm-dme-wizard-features')
    await next.click()
    await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'slurm-dme-wizard-review')
  })

  test('slurm-dme-platform', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, { slurmDeviceMetricsExporter: true })
    await setAppearance(page, appearance)
    await page.goto('/platforms/platform-slurm/monitoring?site=site-a')
    await expect(page.getByText('GPU identity collapsed')).toBeVisible()
    await page.getByText('GPU identity collapsed').scrollIntoViewIfNeeded()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'slurm-dme-platform-monitoring')
  })
})

/**
 * Captures the Slurm deploy wizard's Features step with Podman as the container runtime and the
 * proposed Omnistat job monitoring (decision 063): the runtime choice, the monitoring toggle with its
 * exporter choice, the Platform Grafana toggle with its install-method choice naming the primary
 * controller, and the automatic GPU GRES notice.
 */
test.describe('Slurm Omnistat visual review', () => {
  test('slurm-omnistat', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, { freePlatformCandidates: true })
    await setAppearance(page, appearance)

    await page.goto('/platforms/deploy/slurm?site=site-a')
    await page.getByLabel('Platform name').fill('omnistat-slurm')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()
    await page.getByRole('checkbox', { name: 'Run slurmctld on gpu-node-01' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-02' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-03' }).locator('..').click()
    await next.click()
    // Without a requested engine the Grafana container brings Docker CE to the primary controller.
    await expect(page.getByText('Run Grafana in a container on gpu-node-01, installing Docker CE there unless it already has a container engine.')).toBeVisible()
    await prepareCapture(page, appearance)
    await page.getByRole('region', { name: 'Platform Grafana' }).screenshot({
      path: testInfo.outputPath(`slurm-grafana-card-docker-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
    })
    await page.getByRole('switch', { name: 'Enable Container runtime' }).locator('xpath=following-sibling::*[1]').click()
    await page.getByRole('checkbox', { name: /Podman/ }).locator('xpath=following-sibling::*[1]').click()
    await expect(page.getByRole('switch', { name: 'Enable Job monitoring' })).toBeChecked()
    await expect(page.getByRole('radio', { name: /Omnistat/ })).toBeChecked()
    await expect(page.getByRole('switch', { name: 'Enable Platform Grafana' })).toBeChecked()
    await expect(page.getByText('Run Grafana in a container on gpu-node-01, installing Podman there, the container engine chosen above.')).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'slurm-omnistat-wizard-features')
    await page.getByRole('region', { name: 'Platform Grafana' }).screenshot({
      path: testInfo.outputPath(`slurm-grafana-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
    })
    await page.getByText('Advanced GPU settings', { exact: true }).click()
    await page.getByRole('radio', { name: /Custom name/ }).locator('xpath=following-sibling::*[1]').click()
    await page.getByLabel('GRES type name').fill('mi308x_vf')
    await page.getByLabel('GPU driver module options').fill('pcie_p2p=0\nBad=1')
    await expect(page.getByText(/Line 2: use name=value/)).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'slurm-gpu-advanced-settings')
    await page.getByLabel('GPU driver module options').fill('pcie_p2p=0')
    await next.click()
    await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'slurm-omnistat-wizard-review')
    await page.getByRole('button', { name: 'Deploy Slurm Platform' }).click()
    await expectPlatformInstallStarted(page)
    await page.getByRole('tablist', { name: 'Platform workspace' }).getByRole('tab', { name: 'Configuration' }).click()
    await expect(page.getByText('GPU GRES type', { exact: true })).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'slurm-gpu-platform-configuration')
  })
})

/**
 * Captures the hardware counters switch under Omnistat on a deployed Slurm Platform's Monitoring route
 * (platforms.md "Omnistat"): on with each node's outcome, both confirmations, a change running, and
 * off. Element captures of the Omnistat component keep the sticky shell out of what is inspected.
 */
test.describe('Slurm hardware counters visual review', () => {
  test('slurm-hardware-counters', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, { slurmDeployed: true, platformDeploymentWorkflows: true })
    await setAppearance(page, appearance)

    await page.goto('/platforms/platform-slurm-ha/monitoring?site=site-a')
    const omnistat = page.getByLabel('Omnistat', { exact: true })
    const counters = page.getByRole('group', { name: 'GPU hardware counters' })
    await expect(counters).toContainText('Collecting on 1 of 2 compute nodes.')
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'slurm-hardware-counters-monitoring')
    await omnistat.screenshot({ path: testInfo.outputPath(`slurm-hardware-counters-on-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide' })

    await counters.getByRole('switch', { name: 'Collect GPU hardware counters' }).locator('..').click()
    await expect(page.getByRole('alertdialog', { name: 'Turn off GPU hardware counters' })).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'slurm-hardware-counters-off-dialog', false)
    await page.getByRole('alertdialog').getByRole('button', { name: 'Turn off counters' }).click()

    await expect(counters).toContainText('Turning off')
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await omnistat.screenshot({ path: testInfo.outputPath(`slurm-hardware-counters-turning-off-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide' })
  })

  test('slurm-hardware-counters-on-dialog', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { slurmDeployed: true, platformDeploymentWorkflows: true, slurmHardwareCountersOff: true })
    await setAppearance(page, appearance)

    await page.goto('/platforms/platform-slurm-ha/monitoring?site=site-a')
    const omnistat = page.getByLabel('Omnistat', { exact: true })
    const counters = page.getByRole('group', { name: 'GPU hardware counters' })
    await expect(counters).toContainText('Every compute node runs Omnistat without the counters.')
    await prepareCapture(page, appearance)
    await omnistat.screenshot({ path: testInfo.outputPath(`slurm-hardware-counters-off-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide' })
    await counters.getByRole('switch', { name: 'Collect GPU hardware counters' }).locator('..').click()
    await expect(page.getByRole('alertdialog', { name: 'Turn on GPU hardware counters' })).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'slurm-hardware-counters-on-dialog', false)
  })
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
      httpProxies: { 'maas-a': { mode: 'external', httpProxy: 'http://10.140.168.20:18080' } },
      provisionerSubnets: {
        'maas-a': [
          { id: '1', name: 'lab-network', cidr: '192.168.40.0/24', gatewayAddress: '192.168.40.1', managed: true, provisionerDhcp: true, attached: true, dnsServers: [] },
          { id: '7', name: '10.140.168.0/23', cidr: '10.140.168.0/23', gatewayAddress: '10.140.169.254', managed: true, provisionerDhcp: false, attached: true, dnsServers: ['198.51.100.53', '198.51.100.54'] },
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
    await expect(proxy.getByRole('status')).toContainText('MAAS reports: External (http://10.140.168.20:18080).')
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

/**
 * The red Platform removal control (decision 010, 2026-10-29 amendment): the split button with its
 * record-only menu open, the Uninstall dialog with the release shortcut, record deletion after
 * success, and a Workflow still holding the Servers (decision 061), the split button while an
 * uninstall runs (only the Uninstall part disabled), the record-only dialogs reached from the menu
 * and from an uninstalled Platform, and the bulk toolbar and dialogs for a mixed selection.
 */
test.describe('Platform removal visual review', () => {
  test('split button menu and record-only dialog', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page)
    await setAppearance(page, appearance)
    await page.goto('/platforms/platform-a?site=site-a')
    await page.getByRole('button', { name: 'More removal options' }).click()
    await expect(page.getByRole('menuitem', { name: 'Delete only…' })).toBeVisible()
    await prepareCapture(page, appearance)
    if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-removal-split-menu', false)
    await page.getByRole('menuitem', { name: 'Delete only…' }).click()
    const dialog = page.getByRole('dialog', { name: 'Delete platform record' })
    await expect(dialog.getByText('Hosts will not be cleaned up')).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'platform-removal-record-only-installed', false)
  })

  test('uninstall dialog with a blocking workflow', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page)
    await setAppearance(page, appearance)
    await page.goto('/platforms/platform-b?site=site-a')
    await page.getByRole('button', { name: 'Uninstall platform', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Uninstall platform' })
    await expect(dialog.getByText('An unfinished workflow still holds these servers')).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'platform-uninstall-dialog', false)
    await dialog.getByLabel('Release all servers instead of uninstalling platform software').locator('..').click()
    const releaseDialog = page.getByRole('dialog', { name: 'Release platform servers' })
    // The retitled heading and intro, before the dialog scrolls to the later controls.
    await releaseDialog.getByRole('heading', { name: 'Release platform servers' }).scrollIntoViewIfNeeded()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'platform-uninstall-release-title', false)
    await releaseDialog.getByLabel('Cancel these workflows, then release the servers').locator('..').click()
    await releaseDialog.getByLabel('Platform name confirmation').fill('edge-staging')
    await expect(releaseDialog.getByRole('button', { name: 'Release servers' })).toBeEnabled()
    await prepareCapture(page, appearance)
    if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-uninstall-release-servers', false)
    // The proposal in flight: every choice, the release options included, is locked.
    await page.route(/\/api\/v1\/platforms\/platform-b(\?.*)?$/, async (route) => {
      if (route.request().method() === 'DELETE') await new Promise<void>(() => {})
      await route.fallback()
    })
    await releaseDialog.getByLabel('Erase disks before release').locator('..').click()
    await releaseDialog.getByRole('button', { name: 'Release servers' }).click()
    await expect(releaseDialog.getByLabel('Erase disks before release')).toBeDisabled()
    await capture(page, testInfo, 'platform-uninstall-release-submitting', false)
  })

  test('bulk uninstall dialog with release checked', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page)
    await setAppearance(page, appearance)
    await page.goto('/platforms?site=site-a')
    const scope = mobile ? page.locator('.sw-platform-runtime-card').filter({ hasText: 'edge-staging' }) : page.getByRole('table', { name: 'Platforms' })
    await scope.getByLabel('Select edge-staging').check({ force: true })
    // Phone-width docks drop the qualifier from the button.
    await page.getByRole('button', { name: mobile ? 'Uninstall' : 'Uninstall selected', exact: true }).click()
    await page.getByRole('dialog', { name: 'Uninstall selected platforms' })
      .getByLabel('Release all servers instead of uninstalling platform software').locator('..').click()
    const dialog = page.getByRole('dialog', { name: 'Release servers of selected platforms' })
    await expect(dialog.getByLabel('Erase disks before release')).toBeVisible()
    await dialog.getByRole('heading', { name: 'Release servers of selected platforms' }).scrollIntoViewIfNeeded()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-bulk-uninstall-release', false)
  })

  test('split button while an uninstall runs', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { platformLifecycleStates: { 'platform-a': 'uninstalling' } })
    await setAppearance(page, appearance)
    await page.goto('/platforms/platform-a?site=site-a')
    await expect(page.getByRole('button', { name: 'Uninstall platform: Uninstall is already running.' })).toBeDisabled()
    await prepareCapture(page, appearance)
    if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-removal-uninstalling', false)
  })

  test('record-only dialog of an uninstalled platform', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { platformLifecycleStates: { 'platform-a': 'uninstalled' } })
    await setAppearance(page, appearance)
    await page.goto('/platforms/platform-a?site=site-a')
    await page.getByRole('button', { name: 'Delete platform', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Delete platform record' })
    await expect(dialog.getByText('Delete the remaining record')).toBeVisible()
    await prepareCapture(page, appearance)
    if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-removal-record-only', false)
  })

  test('bulk removal of a mixed selection', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page)
    await setAppearance(page, appearance)
    await page.goto('/platforms?site=site-a')
    const mobile = testInfo.project.name.startsWith('mobile-')
    for (const name of ['edge-staging', 'research-slurm']) {
      const scope = mobile ? page.locator('.sw-platform-runtime-card').filter({ hasText: name }) : page.getByRole('table', { name: 'Platforms' })
      await scope.getByLabel(`Select ${name}`).check({ force: true })
    }
    await page.getByRole('button', { name: 'More removal options' }).click()
    await expect(page.getByRole('menuitem', { name: 'Delete only…' })).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-bulk-removal-toolbar', false)
    await page.getByRole('menuitem', { name: 'Delete only…' }).click()
    const deleteDialog = page.getByRole('dialog', { name: 'Delete selected platform records' })
    await expect(deleteDialog.getByText('Hosts will not be changed', { exact: true })).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'platform-bulk-delete-dialog', false)
    await deleteDialog.getByRole('button', { name: 'Cancel' }).click()
    // Phone-width docks drop the "eligible" qualifier so the chevron stays in view.
    await page.getByRole('button', { name: mobile ? 'Uninstall' : 'Uninstall eligible', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Uninstall selected platforms' })
    await expect(dialog.getByText('An unfinished workflow still holds these platforms’ servers')).toBeVisible()
    await expect(dialog.getByRole('list', { name: 'Platforms skipped by uninstall' })).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-bulk-uninstall-dialog', false)
  })
})

/** Captures a Workflow's Job groups, titled with the labels the Workflow declares. */
test.describe('Workflow detail visual review', () => {
  test('workflow-detail-jobs', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page)
    await setAppearance(page, appearance)
    await page.goto('/workflows/op-running?site=site-a')
    await expect(page.getByRole('button', { name: /^(Expand|Collapse) Configure Kubernetes$/ })).toBeVisible()
    await prepareCapture(page, appearance)
    if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'workflow-detail-jobs', false)
  })

  // A controller-requested Workflow with a uuid id: the header stacks its commands on a phone and
  // truncates the id breadcrumb; the details say the ended Workflow released its resources, and a
  // Workflow not started yet has none yet.
  for (const state of ['succeeded', 'pending'] as const) {
    test(`workflow-detail-header-${state}`, async ({ page }, testInfo) => {
      const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
      await installApiFixtures(page, { controllerWorkflow: state })
      await setAppearance(page, appearance)
      await page.goto(`/workflows/${CONTROLLER_WORKFLOW_ID}?site=site-a`)
      await expect(page.getByRole('heading', { level: 1, name: 'Install the Node Exporter on gpu-node-02' })).toBeVisible()
      await expect(page.getByText(state === 'succeeded' ? 'Released when the Workflow ended' : 'None yet; the Workflow takes them when it starts')).toBeVisible()
      await prepareCapture(page, appearance)
      if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
      await capture(page, testInfo, `workflow-detail-header-${state}`, false)
    })
  }
})

// Dated just before setAppearance's frozen clock (2026-08-27T03:05:00Z), so a running Task's
// duration and a wait's remaining budget read as they would live.
const stalledDiagnostic: OperationTaskDiagnostic = {
  failureClass: 'transient',
  target: { kind: 'server', id: 'srv-1' },
  endpoint: 'https://provisioner.lab:5240/api/2.0/machines/abc123/',
  operation: 'read machine status',
  cause: 'GET https://provisioner.lab:5240/api/2.0/machines/abc123/: still Deploying after 12 polls\nAuthorization: [REDACTED]',
  remediation: 'Open the Server console to see where the installer stopped, then retry the Task.',
  observation: 'srv-1 is Deploying (installing the OS)',
  observedAt: '2026-08-27T02:59:30Z',
  attempts: 12,
  elapsedSeconds: 1800,
  budgetSeconds: 1800,
  providerEvents: [
    { type: 'Deploying', description: 'Started deploying the machine', at: '2026-08-27T02:30:00Z' },
    { type: 'Node changed status', description: 'From Allocated to Deploying' },
  ],
}

const stalledError: OperationNormalizedError = {
  code: 'deployment_stalled',
  message: 'The OS deployment of srv-1 did not finish within its budget.\nThe provider still reports Deploying.',
  retryable: true,
  stage: 'deploying',
  diagnostic: stalledDiagnostic,
}

const longProvisionerEndpoint =
  'https://maas-region-controller-01.taipei-datacenter.lab.example.internal:5240/MAAS/api/2.0/machines/4y3h7p-gpu-node-01-rack-07-u12/?op=[REDACTED]&include_power_parameters=[REDACTED]'

// Evidence that is hard to wrap on a phone: an endpoint that breaks only at its punctuation, a
// multi-line cause with unbroken device paths, and provider events with long descriptions, one
// of them without a time.
const longStalledError: OperationNormalizedError = {
  ...stalledError,
  diagnostic: {
    ...stalledDiagnostic,
    endpoint: longProvisionerEndpoint,
    cause: [
      `read machine status: GET ${longProvisionerEndpoint}: still Deploying after 12 polls over 30m0s`,
      'caused by: the machine has not reported back since curtin started writing the root filesystem to /dev/disk/by-id/nvme-SAMSUNG_MZQL23T8HCLS-00A07_S64HNE0T512345-part2',
      'caused by: the last rack controller message was "cloud-init did not reach its final stage"; Authorization: [REDACTED]',
    ].join('\n'),
    providerEvents: [
      { type: 'Deploying', description: 'Started deploying the machine', at: '2026-08-27T02:30:00Z' },
      { type: 'Node changed status', description: 'From Allocated to Deploying', at: '2026-08-27T02:30:02Z' },
      { type: 'PXE Request', description: 'Machine powered on and requested a boot image over PXE from rack controller rack-07 (192.168.40.2)', at: '2026-08-27T02:31:10Z' },
      { type: 'Loading ephemeral', description: 'Loading the ephemeral environment ubuntu/noble/amd64/ga-24.04/stable', at: '2026-08-27T02:32:40Z' },
      { type: 'Installation', description: 'curtin: writing the root filesystem to /dev/disk/by-id/nvme-SAMSUNG_MZQL23T8HCLS-00A07_S64HNE0T512345-part2', at: '2026-08-27T02:36:05Z' },
      { type: 'Node status event', description: 'cloud-init: modules-config stage started' },
    ],
  },
}

/** srv-1's `provision-os` Task as the workflows contract returns it: failed on its first attempt unless overridden. */
function diagnosticTask(overrides: Record<string, unknown>) {
  return {
    id: 'provision-srv-1', kind: 'provision-os', name: 'Provision OS on srv-1', executor: 'maas', dependsOn: [],
    targets: [{ kind: 'server', id: 'srv-1' }], status: 'failed', attempt: 1, progress: 0,
    error: null, externalExecution: null, waiting: null, artifacts: [],
    startedAt: '2026-08-27T02:29:00Z', finishedAt: '2026-08-27T03:00:00Z',
    ...overrides,
  }
}

/** A one-Server OS deployment Workflow holding `tasks`, as `GET /workflows/{id}` returns it. */
function diagnosticWorkflow(id: string, status: string, tasks: Array<Record<string, unknown>>) {
  return {
    id, schemaVersion: 3, kind: 'deploy-os', intent: 'Deploy Ubuntu on srv-1', intentSnapshot: {},
    definition: 'deploy-os', definitionVersion: 1, status, statusReason: null,
    startState: 'started', temporal: { workflowId: `swallow-operation/${id}` }, siteId: 'site-a', platformId: null,
    targetResources: [{ kind: 'server', id: 'srv-1' }], targetServerIds: ['srv-1'], retryOfOperationId: null,
    steps: tasks, jobs: [], leases: [], requestCorrelation: 'req-diagnostics',
    execution: { runId: '', playbook: 'provision-os', status, statusReason: null, startedAt: '2026-08-27T02:28:30Z', finishedAt: null },
    requestedBy: 'admin', requestedAt: '2026-08-27T02:28:00Z', startedAt: '2026-08-27T02:28:30Z', finishedAt: null, updatedAt: '2026-08-27T03:00:00Z',
  }
}

function diagnosticRequestedEvent(workflowId: string, createdAt: string): OperationTimelineEvent {
  return {
    id: `${workflowId}/requested`, operationId: workflowId, type: 'operation_requested',
    message: 'Operation accepted and waiting for durable workflow start.', createdAt,
  }
}

/**
 * A `step_status` event of srv-1's Task. An attempt that ended with `error` records its code,
 * message, stage, and retryable, and its diagnostic's failure class and endpoint when it has one,
 * as the workflows contract's Timeline section describes.
 */
function diagnosticTaskEvent(
  workflowId: string, status: string, attempt: number, createdAt: string, error?: OperationNormalizedError,
): OperationTimelineEvent {
  const failure = error
    ? {
        code: error.code, message: error.message, stage: error.stage ?? '', retryable: error.retryable,
        ...(error.diagnostic ? { failureClass: error.diagnostic.failureClass, endpoint: error.diagnostic.endpoint } : {}),
      }
    : {}
  return {
    id: `${workflowId}/step/provision-srv-1/${status}/${attempt}`, operationId: workflowId, stepId: 'provision-srv-1',
    type: 'step_status', message: `Provision OS on srv-1 changed to ${status}.`, createdAt, details: { status, attempt, ...failure },
  }
}

/** The timeline of a Workflow whose Task failed on its first attempt with `error`. */
function failedOnceTimeline(workflowId: string, error: OperationNormalizedError): OperationTimelineEvent[] {
  return [
    diagnosticRequestedEvent(workflowId, '2026-08-27T02:28:00Z'),
    diagnosticTaskEvent(workflowId, 'running', 1, '2026-08-27T02:29:00Z'),
    diagnosticTaskEvent(workflowId, 'failed', 1, '2026-08-27T03:00:00Z', error),
  ]
}

/**
 * Opens `workflow`'s detail page with `timeline` served for it; every other request falls back to
 * the shared fixtures. Resolves once the heading and the timeline have rendered.
 */
async function openDiagnosticWorkflow(
  page: Page, appearance: 'dark' | 'light', workflow: ReturnType<typeof diagnosticWorkflow>, timeline: OperationTimelineEvent[],
) {
  await installApiFixtures(page)
  await setAppearance(page, appearance)
  await page.route(`**/api/v1/workflows/${workflow.id}**`, async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === `/api/v1/workflows/${workflow.id}` && route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(workflow) })
    }
    if (path === `/api/v1/workflows/${workflow.id}/timeline`) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(timeline) })
    }
    return route.fallback()
  })
  await page.goto(`/workflows/${workflow.id}?site=site-a`)
  await expect(page.getByRole('heading', { level: 1, name: workflow.intent })).toBeVisible()
  await expect(page.locator('ol.sw-timeline')).toBeVisible()
}

/**
 * Captures the whole Workflow page, then the block under review at its real scale as a page clip:
 * both start from the top of the page so the sticky shell never covers the block, and the clip
 * keeps all of a block taller than a phone screen.
 */
async function captureDiagnosticCase(page: Page, testInfo: TestInfo, name: string, block: Locator) {
  await page.evaluate(() => window.scrollTo(0, 0))
  if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
  await capture(page, testInfo, name)
  const box = await block.boundingBox()
  const viewport = page.viewportSize()
  if (!box || !viewport) throw new Error(`${name}: the block under review is not rendered`)
  await page.screenshot({
    path: testInfo.outputPath(`${name}-card-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
    fullPage: true, clip: { x: 0, y: box.y, width: viewport.width, height: box.height },
  })
}

/**
 * Captures Task Diagnostics on the Workflow detail page (workflows contract, "Task errors and
 * diagnostics"): a failed Task's summary with its details closed, the same details open with long
 * evidence, a running Task's waiting notice, earlier attempts' failures on the timeline, and an
 * older error without a diagnostic. `--grep workflow-diagnostics` selects all five.
 */
test.describe('Workflow diagnostics visual review', () => {
  test('workflow-diagnostics-failed-collapsed', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await openDiagnosticWorkflow(
      page, appearance,
      diagnosticWorkflow('op-diagnostic', 'failed', [diagnosticTask({ error: stalledError })]),
      failedOnceTimeline('op-diagnostic', stalledError),
    )
    const debuggerSection = page.locator('section.sw-operation-debugger')
    await expect(debuggerSection).toContainText('Next step: Open the Server console to see where the installer stopped, then retry the Task.')
    await expect(debuggerSection.getByRole('button', { name: 'Show diagnostic details' })).toHaveAttribute('aria-expanded', 'false')
    await prepareCapture(page, appearance)
    await captureDiagnosticCase(page, testInfo, 'workflow-diagnostics-failed-collapsed', debuggerSection)
  })

  test('workflow-diagnostics-failed-expanded', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await openDiagnosticWorkflow(
      page, appearance,
      diagnosticWorkflow('op-diagnostic-long', 'failed', [diagnosticTask({ error: longStalledError })]),
      failedOnceTimeline('op-diagnostic-long', longStalledError),
    )
    const debuggerSection = page.locator('section.sw-operation-debugger')
    await debuggerSection.getByRole('button', { name: 'Show diagnostic details' }).click()
    const details = page.getByRole('region', { name: 'Failure diagnostic details' })
    await expect(details).toContainText(longProvisionerEndpoint)
    // Six provider events and the header row.
    await expect(details.getByRole('table', { name: 'Provider events around the failure' }).getByRole('row')).toHaveCount(7)
    await prepareCapture(page, appearance)
    await captureDiagnosticCase(page, testInfo, 'workflow-diagnostics-failed-expanded', debuggerSection)
  })

  test('workflow-diagnostics-waiting', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const waiting: OperationStepWaiting = {
      observation: 'srv-1 is Deploying (installing the OS)', observedAt: '2026-08-27T03:04:30Z',
      attempts: 4, elapsedSeconds: 125, budgetSeconds: 1800,
    }
    await openDiagnosticWorkflow(
      page, appearance,
      diagnosticWorkflow('op-waiting', 'waiting_external', [
        diagnosticTask({ status: 'running', startedAt: '2026-08-27T03:02:25Z', finishedAt: null, waiting }),
      ]),
      [
        diagnosticRequestedEvent('op-waiting', '2026-08-27T03:02:00Z'),
        diagnosticTaskEvent('op-waiting', 'running', 1, '2026-08-27T03:02:25Z'),
      ],
    )
    const debuggerSection = page.locator('section.sw-operation-debugger')
    await expect(debuggerSection).toContainText('Waited 2m 5s of a 30m 0s budget (27m 55s left) · 4 observations')
    await prepareCapture(page, appearance)
    await captureDiagnosticCase(page, testInfo, 'workflow-diagnostics-waiting', debuggerSection)
  })

  // A transient first attempt with a long endpoint and a permanent second one without an endpoint,
  // both retried; the Task itself now carries only its running third attempt.
  test('workflow-diagnostics-timeline-attempts', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const unreachable: OperationNormalizedError = {
      code: 'provider_unreachable', message: 'The provisioner API did not answer.\nGET timed out.', retryable: true, stage: '',
      diagnostic: { failureClass: 'transient', endpoint: 'https://maas-region-controller-01.taipei-datacenter.lab.example.internal:5240/MAAS/api/2.0/' },
    }
    const missingImage: OperationNormalizedError = {
      code: 'deployment_failed', message: 'The provider reported Failed deployment: ubuntu/noble has no amd64 boot resource.\nSync the image, then retry.',
      retryable: true, stage: 'deployment', diagnostic: { failureClass: 'permanent' },
    }
    await openDiagnosticWorkflow(
      page, appearance,
      diagnosticWorkflow('op-retried', 'waiting_external', [
        diagnosticTask({ status: 'running', attempt: 3, startedAt: '2026-08-27T02:58:00Z', finishedAt: null }),
      ]),
      [
        diagnosticRequestedEvent('op-retried', '2026-08-27T02:20:00Z'),
        diagnosticTaskEvent('op-retried', 'running', 1, '2026-08-27T02:20:30Z'),
        diagnosticTaskEvent('op-retried', 'failed', 1, '2026-08-27T02:26:00Z', unreachable),
        diagnosticTaskEvent('op-retried', 'running', 2, '2026-08-27T02:40:00Z'),
        diagnosticTaskEvent('op-retried', 'failed', 2, '2026-08-27T02:52:00Z', missingImage),
        diagnosticTaskEvent('op-retried', 'running', 3, '2026-08-27T02:58:00Z'),
      ],
    )
    const timeline = page.locator('ol.sw-timeline')
    await expect(timeline).toContainText('Attempt 1 · provider_unreachable · Transient failure')
    await expect(timeline).toContainText('Attempt 2 · deployment_failed · Permanent failure')
    await prepareCapture(page, appearance)
    await captureDiagnosticCase(page, testInfo, 'workflow-diagnostics-timeline-attempts', timeline)
  })

  // An error stored before diagnostics existed: no affected target, next step, or disclosure, and a
  // timeline event without a failure class.
  test('workflow-diagnostics-without-diagnostic', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const legacyError: OperationNormalizedError = {
      code: 'deployment_failed',
      message: 'The provider reported Failed deployment.\nMAAS: Installation failed (refer to the installation log for more information).',
      retryable: false,
      diagnostic: null,
    }
    await openDiagnosticWorkflow(
      page, appearance,
      diagnosticWorkflow('op-plain-error', 'failed', [diagnosticTask({ error: legacyError })]),
      failedOnceTimeline('op-plain-error', legacyError),
    )
    const debuggerSection = page.locator('section.sw-operation-debugger')
    await expect(debuggerSection).toContainText('The provider reported Failed deployment.')
    await expect(debuggerSection.getByRole('button', { name: /diagnostic details/ })).toHaveCount(0)
    await prepareCapture(page, appearance)
    await captureDiagnosticCase(page, testInfo, 'workflow-diagnostics-without-diagnostic', debuggerSection)
  })
})

/**
 * Captures the Server Release dialog while its request is in flight: every choice, the comment
 * included, is disabled.
 */
test.describe('Server release dialog visual review', () => {
  test('server-release-dialog-submitting', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { withoutPlatformClaims: true })
    await page.route('**/api/v1/provisioning/release-operations', async () => {
      await new Promise<void>(() => {})
    })
    await setAppearance(page, appearance)
    await page.goto('/servers/srv-1/summary?site=site-a')
    await page.getByRole('button', { name: 'Take action' }).click()
    await page.getByRole('menuitem', { name: 'Release', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Release server', exact: true })
    await dialog.getByLabel('Erase disks before release').locator('..').click()
    await dialog.getByLabel('Provider event comment (optional)').fill('Return to the ready pool')
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'server-release-dialog', false)
    await dialog.getByRole('button', { name: 'Release server', exact: true }).click()
    await expect(dialog.getByLabel('Provider event comment (optional)')).toBeDisabled()
    await capture(page, testInfo, 'server-release-dialog-submitting', false)
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

  // A kind without dedicated settings (NFS) is configured and shown through its module's schema.
  test('NFS settings generated from its module schema', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, { fleetSize: 5, softwareAssignments: visualSoftwareAssignments })
    await setAppearance(page, appearance)
    await page.goto('/software/nfs?site=site-a')
    await page.getByRole('button', { name: 'Install software', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Install software' })
    await dialog.getByRole('textbox', { name: 'Search installation targets' }).fill('gpu-node-01')
    await dialog.getByText('Select all available results', { exact: true }).click()
    await dialog.getByRole('button', { name: 'Next', exact: true }).click()
    await dialog.getByRole('checkbox', { name: 'server' }).click({ force: true })
    await dialog.getByRole('textbox', { name: 'Export path (server)' }).fill('/export/data')
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'software-nfs-generated-settings', false)
    await dialog.getByRole('button', { name: 'Cancel' }).click()

    // The table and the card list share the label; only the one for this viewport is visible.
    const deployments = page.locator('[aria-label="NFS deployments"]:visible')
    await deployments.getByRole('button', { name: 'More' }).first().click()
    await page.getByRole('menuitem', { name: 'View settings' }).click()
    await expect(page.getByRole('dialog', { name: 'NFS settings' })).toBeVisible()
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'software-nfs-recorded-settings', false)
  })

  // VictoriaMetrics: its detail page with an installed deployment's summary, and the install form its
  // module's schema generates, with a retention the schema's format refuses.
  test('VictoriaMetrics detail and generated settings', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, {
      fleetSize: 5,
      softwareAssignments: [
        ...visualSoftwareAssignments,
        {
          serverId: 'srv-5', kind: 'victoriametrics', roles: [], spec: { version: '1.155.0', listen: 'management', retention: '90d' },
          state: 'installed', lastWorkflowId: 'op-vm-srv-5', lastAppliedAt: '2026-08-26T08:00:00Z',
          createdAt: '2026-08-26T07:50:00Z', updatedAt: '2026-08-26T08:00:00Z',
        },
      ],
    })
    await setAppearance(page, appearance)
    await page.goto('/software/victoriametrics?site=site-a')
    await expect(page.locator('[aria-label="VictoriaMetrics deployments"]:visible')).toContainText('Retention 90d')
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'software-victoriametrics-detail')

    await page.locator('header').getByRole('button', { name: 'Install software', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Install software' })
    await dialog.getByRole('textbox', { name: 'Search installation targets' }).fill('gpu-node-01')
    await dialog.getByText('Select all available results', { exact: true }).click()
    await dialog.getByRole('button', { name: 'Next', exact: true }).click()
    await dialog.getByRole('textbox', { name: 'Retention' }).fill('12h')
    await expect(dialog.getByText('Retention does not have the expected format.')).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'software-victoriametrics-generated-settings', false)
  })

  // The AMD Device Metrics Exporter (decision 068): its catalog card under Observability, a
  // deployment its Platform manages, and the install picker's GPU and Platform blockers.
  test('AMD Device Metrics Exporter', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    const mobile = testInfo.project.name.startsWith('mobile-')
    await installApiFixtures(page, {
      fleetSize: 5,
      softwareAssignments: [
        ...visualSoftwareAssignments,
        {
          serverId: 'srv-2', kind: 'device-metrics-exporter', roles: [], spec: { version: '1.5.3' }, state: 'installed',
          lastWorkflowId: 'op-dme-srv-2', lastAppliedAt: '2026-08-20T00:00:00Z', createdAt: '2026-08-20T00:00:00Z', updatedAt: '2026-08-20T00:00:00Z',
        },
      ],
    })
    await setAppearance(page, appearance)
    await page.goto('/software?site=site-a')
    const card = page.getByRole('link', { name: 'Open AMD Device Metrics Exporter' })
    await expect(card).toBeVisible()
    await card.scrollIntoViewIfNeeded()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'software-dme-catalog', false)

    await page.goto('/software/device-metrics-exporter?site=site-a')
    await expect(page.getByRole('link', { name: 'Managed by Platform' }).filter({ visible: true })).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'software-dme-detail', false)

    await page.getByRole('button', { name: 'Install software', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Install software' })
    await expect(dialog.getByText('This Server has no AMD compute GPU.')).toBeVisible()
    await prepareCapture(page, appearance)
    if (mobile) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'software-dme-install-blockers', false)
  })

  test('install method with a missing container runtime', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { fleetSize: 5, softwareAssignments: visualSoftwareAssignments })
    await setAppearance(page, appearance)
    await page.goto('/software/grafana?site=site-a')
    await page.getByRole('button', { name: 'Install software', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Install software' })
    await dialog.getByRole('checkbox', { name: 'Select compute-node-005' }).locator('..').click()
    await dialog.getByRole('checkbox', { name: 'Select gpu-node-03' }).locator('..').click()
    await dialog.getByRole('button', { name: 'Next', exact: true }).click()
    await expect(dialog.getByText('No container runtime installed')).toBeVisible()
    await prepareCapture(page, appearance)
    if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'software-install-method-missing-runtime', false)
  })

  test('Prometheus listen and retention', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { fleetSize: 5, softwareAssignments: visualSoftwareAssignments })
    await setAppearance(page, appearance)
    await page.goto('/software/prometheus?site=site-a')
    await page.getByRole('button', { name: 'Install software', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Install software' })
    await dialog.getByRole('checkbox', { name: 'Select compute-node-005' }).locator('..').click()
    await dialog.getByRole('button', { name: 'Next', exact: true }).click()
    await dialog.getByRole('textbox', { name: 'Retention' }).fill('7 days')
    await expect(dialog.getByText('Use a Prometheus duration such as 15d, 7d, 1w, or 12h.')).toBeVisible()
    await dialog.getByText('Use a Prometheus duration such as 15d, 7d, 1w, or 12h.').scrollIntoViewIfNeeded()
    await prepareCapture(page, appearance)
    if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'software-prometheus-listen-retention', false)
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
   * field spans the wizard's form grid on Automatic and the site DHCP warning follows it
   * (decision 058).
   */
  test('platform-wizard-site-dhcp-networking', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { freePlatformCandidates: true, readyServerCount: 2, siteDhcpNetworkServerIds: ['srv-1', 'srv-2'] })
    await setAppearance(page, appearance)
    await page.goto('/platforms/deploy/slurm?site=site-a')
    await page.getByLabel('Platform name').fill('site-dhcp-slurm')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()
    await page.getByRole('checkbox', { name: 'Run slurmctld on gpu-node-01' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-01' }).locator('..').click()
    await page.getByRole('checkbox', { name: 'Run slurmd on gpu-node-02' }).locator('..').click()
    await next.click()
    await expect(page.getByRole('heading', { name: 'Slurm features' })).toBeVisible()
    await next.click()
    await page.getByRole('radio', { name: 'Ubuntu 24.04 LTS', exact: false }).locator('..').click()
    await expect(page.getByRole('heading', { name: 'OS installation' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'OS networking' })).toBeVisible()
    await expect(page.getByRole('radio', { name: 'Automatic', exact: true })).toBeChecked()
    await expect(page.getByText('Automatic addressing may conflict with the site DHCP pool', { exact: true })).toBeVisible()
    await prepareCapture(page, appearance)
    if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-wizard-site-dhcp-networking', false)
  })

  /**
   * The Networking step on a site-DHCP subnet: Automatic stays preselected and the site DHCP
   * warning sits below the addressing field (decision 058).
   */
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
    await expect(dialog.getByRole('radio', { name: 'Automatic', exact: true })).toBeChecked()
    await expect(dialog.getByText('Automatic addressing may conflict with the site DHCP pool', { exact: true })).toBeVisible()
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

  test('OS image upload dialog', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page)
    // Hold the content request so the dialog stays in its submitting state with the progress bar.
    await page.route('**/api/v1/provisioning/image-uploads/*/content', () => {})
    await setAppearance(page, appearance)
    await page.goto('/provisioning/images?site=site-a')
    await page.getByRole('button', { name: 'Upload image' }).click()
    const dialog = page.getByRole('dialog', { name: 'Upload OS image' })
    await chooseOption(page, 'Provisioner integration', 'MAAS Taipei')
    await dialog.locator('#upload-image-name').fill('ubuntu-24.04-rocm')
    await dialog.locator('#upload-image-file').setInputFiles({
      name: 'ubuntu-24.04-rocm.tar.gz',
      mimeType: 'application/gzip',
      buffer: Buffer.from('fake-image-bytes'),
    })
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'os-image-upload-dialog', false)
    await dialog.getByRole('button', { name: 'Upload', exact: true }).click()
    await expect(dialog.getByRole('status')).toContainText('Uploading…')
    await capture(page, testInfo, 'os-image-upload-dialog-uploading', false)
  })

  test('Platform operating system step', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { freePlatformCandidates: true, readyServerCount: 1 })
    await setAppearance(page, appearance)
    await page.goto('/platforms/deploy/kubernetes?site=site-a')
    await page.getByLabel('Platform name').fill('visual-k0s')
    const next = page.getByRole('button', { name: 'Next' })
    await next.click()
    await page.getByRole('radio', { name: /Standalone/ }).locator('xpath=following-sibling::*[1]').click()
    await chooseOption(page, 'Role for gpu-node-01', 'Standalone node')
    await next.click()
    await expect(page.getByRole('heading', { name: 'Kubernetes features' })).toBeVisible()
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
    await expect(page.getByRole('heading', { name: 'OS installation' })).toBeVisible()
    await capture(page, testInfo, 'platform-os-installation-step', false)

    await expect(page.getByRole('heading', { name: 'OS networking' })).toBeVisible()
    await capture(page, testInfo, 'platform-os-networking-step', false)

    // The Review cards, each with its named Edit button.
    await next.click()
    await expect(page.getByRole('heading', { name: 'Platform network' })).toBeVisible()
    await next.click()
    await expect(page.getByRole('heading', { name: 'Review deployment' })).toBeVisible()
    await prepareCapture(page, appearance)
    if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
    await capture(page, testInfo, 'platform-wizard-review')
  })
})

/** One Local repository capture: the fixture scenario, the route, and the text that marks it rendered. */
interface LocalRepositoryVisualCase {
  name: string
  scenario: 'ready' | 'syncing' | 'failed' | 'upstream' | 'cache'
  path: string
  rendered: string | RegExp
}

const localRepositoryVisualCases: readonly LocalRepositoryVisualCase[] = [
  // Every source and file ready, with the first page of OS packages in the content browser.
  { name: 'local-repository-ready', scenario: 'ready', path: '/infrastructure/local-repository?site=site-a', rendered: 'Showing 1–25 of 44 OS packages' },
  // Spinners on the syncing and pending rows, the API's not-ready message, and the polling cue.
  { name: 'local-repository-syncing', scenario: 'syncing', path: '/infrastructure/local-repository?site=site-a', rendered: 'Updating every 5 s while it converges' },
  // A failed archive and a checksum mismatch, each with the provider's reason as text.
  { name: 'local-repository-failed', scenario: 'failed', path: '/infrastructure/local-repository?site=site-a', rendered: /checksum mismatch/ },
  // Deployments fetch from the upstreams: the setting, a summary that is not a failure, and the
  // items' last observation without alarming badges or spinners.
  { name: 'local-repository-upstream', scenario: 'upstream', path: '/infrastructure/local-repository?site=site-a', rendered: 'Not synchronizing' },
  // Cache mode: deployments accepted while items still sync, with the count of what is not served
  // yet and a resyncing source that still serves its last sync.
  { name: 'local-repository-cache', scenario: 'cache', path: '/infrastructure/local-repository?site=site-a', rendered: /3 sources or files are not served yet/ },
  // site-b has no repository Integration: the explanation and the way to register one.
  { name: 'local-repository-empty', scenario: 'ready', path: '/infrastructure/local-repository?site=site-b', rendered: 'This Site has no Local Repository' },
  // The content browser's last page, reached through the URL-owned page.
  { name: 'local-repository-content-page-2', scenario: 'ready', path: '/infrastructure/local-repository?site=site-a&page=2', rendered: 'Showing 26–44 of 44 OS packages' },
]

/**
 * Captures Infrastructure → Local repository (decision 078) in each state a reviewer must read: a
 * ready repository with its content, a syncing one, a failed source and file with the provider's
 * reasons, a Site whose deployments fetch from the upstream sources, one that uses it as a cache, a
 * Site without one, the content browser's second page (also cropped to the browser at its real
 * scale), the confirmation of a switch of the mode, and the Manage user sources dialog with a draft source and a field
 * error. The syncing page polls every 5 s, so each capture waits for its own content rather than for
 * an idle network. Select them with `--grep local-repository`.
 */
test.describe('Local repository visual review', () => {
  for (const visualCase of localRepositoryVisualCases) {
    test(visualCase.name, async ({ page }, testInfo) => {
      const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
      await installApiFixtures(page, { localRepository: visualCase.scenario })
      await setAppearance(page, appearance)
      await page.goto(visualCase.path)
      await expect(page.getByRole('tab', { name: 'Local repository', selected: true })).toBeVisible()
      await prepareCapture(page, appearance, async () => {
        await expect(page.getByText(visualCase.rendered).filter({ visible: true }).first()).toBeVisible()
      })
      if (testInfo.project.name.startsWith('mobile-')) await expectNoHorizontalOverflow(page)
      await capture(page, testInfo, visualCase.name)
      if (visualCase.name === 'local-repository-content-page-2') {
        // A page clip rather than an element capture, so the sticky shell does not cover the browser.
        const box = await page.locator('.sw-local-repository-content').boundingBox()
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

  test('local-repository-user-sources-dialog', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { localRepository: 'ready' })
    await setAppearance(page, appearance)
    await page.goto('/infrastructure/local-repository?site=site-a')
    await page.getByRole('button', { name: 'Manage user sources' }).click()
    const dialog = page.getByRole('dialog', { name: 'Manage user sources' })
    await dialog.getByText('Sources declared by modules (5), not editable here').click()
    await dialog.getByRole('textbox', { name: /^Name/ }).fill('ubuntu-ports')
    await dialog.getByRole('textbox', { name: /^URL/ }).fill('http://ports.ubuntu.com/ubuntu-ports')
    await dialog.getByRole('textbox', { name: /^Suites/ }).fill('noble noble')
    await expect(dialog.getByText('List each suite once.')).toBeVisible()
    await prepareCapture(page, appearance, async () => {
      await expect(dialog.getByRole('list', { name: 'Sources declared by modules' })).toBeVisible()
    })
    await capture(page, testInfo, 'local-repository-user-sources-dialog', false)
  })

  test('local-repository-artifact-source-dialog', async ({ page }, testInfo) => {
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page, { localRepository: 'upstream' })
    await setAppearance(page, appearance)
    await page.goto('/infrastructure/local-repository?site=site-a')
    await page.getByRole('radiogroup', { name: 'Artifact source for deployments' }).getByText('Cache', { exact: true }).click()
    const dialog = page.getByRole('alertdialog', { name: 'Use this local repository as a cache?' })
    await prepareCapture(page, appearance, async () => {
      await expect(dialog.getByRole('button', { name: 'Use it as a cache' })).toBeVisible()
    })
    await capture(page, testInfo, 'local-repository-artifact-source-dialog', false)
  })
})

/**
 * The sidebar in the layouts the page cases do not show at a readable scale: the expanded rail with
 * OS Images current, the icon-only rail with its tooltip (desktop only, the rail is hidden on a
 * phone), and the mobile drawer (phone only).
 */
test.describe('Console navigation visual review', () => {
  test('sidebar-collapsed-rail @representative', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name.startsWith('mobile-'), 'The desktop rail is hidden on a phone.')
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page)
    await setAppearance(page, appearance)
    await page.goto('/provisioning/images?site=site-a')
    await expect(page.getByRole('heading', { name: 'OS Images', exact: true })).toBeVisible()
    const rail = page.getByTestId('desktop-navigation')
    await prepareCapture(page, appearance)
    await rail.screenshot({ path: testInfo.outputPath(`sidebar-expanded-rail-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide' })
    await page.getByRole('button', { name: 'Toggle navigation' }).click()
    await expect.poll(() => rail.evaluate((element) => Math.round(element.getBoundingClientRect().width))).toBe(64)
    await capture(page, testInfo, 'sidebar-collapsed-rail', false)
    await rail.screenshot({ path: testInfo.outputPath(`sidebar-collapsed-rail-crop-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide' })
    await rail.getByRole('link', { name: 'OS Images', exact: true }).hover()
    await expect(page.getByRole('tooltip', { name: 'OS Images', exact: true })).toBeVisible()
    await page.screenshot({
      path: testInfo.outputPath(`sidebar-collapsed-rail-tooltip-${testInfo.project.name}.png`), animations: 'disabled', caret: 'hide',
      clip: { x: 0, y: 0, width: 420, height: 520 },
    })
  })

  test('sidebar-mobile-drawer @representative', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name.startsWith('desktop-'), 'The drawer replaces the rail only on a phone.')
    const appearance = testInfo.project.name.endsWith('-dark') ? 'dark' : 'light'
    await installApiFixtures(page)
    await setAppearance(page, appearance)
    await page.goto('/change-plans?site=site-a')
    await expect(page.getByRole('heading', { name: 'Activity', exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Open navigation' }).click()
    const nav = page.getByRole('dialog').getByRole('navigation', { name: 'Primary navigation' })
    await expect(nav.getByRole('link', { name: 'Activity' })).toHaveAttribute('aria-current', 'page')
    await prepareCapture(page, appearance)
    await capture(page, testInfo, 'sidebar-mobile-drawer', false)
  })
})
