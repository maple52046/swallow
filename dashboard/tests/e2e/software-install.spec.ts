import { expect, test } from 'playwright/test'
import type { SoftwareAssignment } from '@/domain/software/types'
import { installApiFixtures } from './fixtures'

const timestamp = '2026-08-20T00:00:00Z'

function assignment(
  serverId: string,
  kind: SoftwareAssignment['kind'],
  state: SoftwareAssignment['state'],
  overrides: Partial<SoftwareAssignment> = {},
): SoftwareAssignment {
  return {
    serverId,
    kind,
    roles: [],
    spec: null,
    state,
    lastWorkflowId: `op-${kind}-${serverId}`,
    lastAppliedAt: state === 'installed' ? timestamp : null,
    createdAt: timestamp,
    updatedAt: timestamp,
    ...overrides,
  }
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

test('Software is a grouped catalog with Site-scoped deployment footprints', async ({ page }) => {
  await installApiFixtures(page, {
    fleetSize: 5,
    softwareAssignments: [
      assignment('srv-5', 'docker-ce', 'installed', { spec: { enableApi: true } }),
      assignment('srv-1', 'nfs', 'pending', { roles: ['client'], spec: { source: '10.0.0.9:/data', mountPath: '/data' } }),
      assignment('srv-2', 'podman', 'failed'),
    ],
  })
  await page.goto('/software?site=site-a')

  await expect(page.getByRole('heading', { name: 'Container runtimes', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Storage & file sharing', exact: true })).toBeVisible()
  const docker = page.getByRole('link', { name: 'Open Docker CE' })
  await expect(docker).toContainText('Installed on 1 Server')
  await expect(docker).toHaveAttribute('href', '/software/docker-ce?site=site-a')
  await expect(page.getByRole('link', { name: 'Open NFS' })).toContainText('1 changing')
  await expect(page.getByRole('link', { name: 'Open Podman' })).toContainText('1 failed')
  await expect(page.getByRole('table')).toHaveCount(0)
  await docker.focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL('/software/docker-ce?site=site-a')
  await expect(page.getByRole('link', { name: 'Settings', exact: true })).toHaveAttribute('href', '/software/docker-ce/settings?site=site-a')

  await page.goto('/software/podman?site=site-a')
  await expect(page.getByRole('link', { name: 'Settings', exact: true })).toHaveCount(0)
  await page.goto('/software/nfs?site=site-a')
  await expect(page.getByRole('link', { name: 'Settings', exact: true })).toHaveCount(0)
})

test('catalog identity stays available when deployment footprint loading fails', async ({ page }) => {
  await installApiFixtures(page, { softwareAssignmentFailureRequestNumbers: [1, 2] })
  await page.goto('/software?site=site-a')

  await expect(page.getByRole('heading', { name: 'Software', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Open Docker CE' })).toBeVisible()
  await expect(page.getByText('Deployment context is temporarily unavailable')).toBeVisible()
  await expect(page.getByText('Deployment count unavailable').first()).toBeVisible()
})

test('Software detail filters deployments and preserves Site scope in resource links', async ({ page }) => {
  await installApiFixtures(page, {
    fleetSize: 5,
    softwareAssignments: [
      assignment('srv-5', 'docker-ce', 'installed', { spec: { version: '27.3.1', enableApi: true } }),
      assignment('srv-1', 'docker-ce', 'pending'),
      assignment('srv-2', 'docker-ce', 'failed'),
    ],
  })
  await page.goto('/software/docker-ce?site=site-a')

  const table = page.getByRole('table', { name: 'Docker CE deployments' })
  await expect(table).toContainText('gpu-node-01')
  await expect(table).toContainText('gpu-node-02')
  await expect(table).toContainText('compute-node-005')
  await expect(table.getByRole('link', { name: 'compute-node-005' })).toHaveAttribute('href', '/servers/srv-5/summary?site=site-a')

  await page.getByRole('button', { name: 'Failed', exact: true }).click()
  await expect(page).toHaveURL(/state=failed/)
  await expect(table).toContainText('gpu-node-02')
  await expect(table).not.toContainText('compute-node-005')
  await expect(table.getByRole('link', { name: 'View workflow' })).toHaveAttribute('href', '/workflows/op-docker-ce-srv-2?site=site-a')

  await page.getByRole('textbox', { name: 'Search software deployments' }).fill('no-such-server')
  await expect(page.getByRole('heading', { name: 'No deployments match these filters' })).toBeVisible()
  await page.getByRole('button', { name: 'Clear filters' }).click()
  await expect(page).toHaveURL('/software/docker-ce?site=site-a')
})

test('Software detail paginates a large deployment set without server-side reordering', async ({ page }) => {
  await installApiFixtures(page, {
    fleetSize: 12,
    softwareAssignments: Array.from({ length: 12 }, (_, index) =>
      assignment(`srv-${index + 1}`, 'nfs', 'installed', { roles: ['client'], spec: { source: 'storage:/data', mountPath: '/data' } }),
    ),
  })
  await page.goto('/software/nfs?site=site-a')

  await expect(page.getByText('Showing 1–10 of 12 deployments')).toBeVisible()
  await page.getByRole('button', { name: 'Next page' }).click()
  await expect(page).toHaveURL(/page=2/)
  await expect(page.getByText('Showing 11–12 of 12 deployments')).toBeVisible()
})

test('Software detail installs on multiple new Servers and excludes existing assignments', async ({ page }) => {
  const installs: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    softwareAssignments: [
      assignment('srv-2', 'nfs', 'installed', { roles: ['client'], spec: { source: 'old:/data', mountPath: '/old' } }),
    ],
    onSoftwareInstallRequest: (body) => installs.push(body),
  })
  await page.goto('/software/nfs?site=site-a')
  await page.getByRole('button', { name: 'Install software', exact: true }).click()

  const dialog = page.getByRole('dialog', { name: 'Install software' })
  await expect(dialog.getByText('Already managed; use Reconfigure from its deployment.')).toBeVisible()
  await expect(dialog.getByRole('checkbox', { name: 'Select gpu-node-02' })).toBeDisabled()
  await expect(dialog.getByRole('checkbox', { name: 'Select gpu-node-04' })).toBeDisabled()
  await dialog.getByRole('textbox', { name: 'Search installation targets' }).fill('gpu-node-01')
  await dialog.getByText('Select all available results', { exact: true }).click()
  await expect(dialog.getByText('1 selected')).toBeVisible()
  await expect(dialog.getByRole('checkbox', { name: 'Select gpu-node-01' })).toBeChecked()
  await dialog.getByRole('button', { name: 'Next', exact: true }).click()

  await dialog.getByRole('checkbox', { name: 'client' }).click({ force: true })
  await dialog.getByRole('textbox', { name: 'Source (client)' }).fill('10.0.0.9:/export/data')
  await dialog.getByRole('textbox', { name: 'Mount path (client)' }).fill('/shared')
  await dialog.getByRole('button', { name: 'Install', exact: true }).click()

  await expect(page).toHaveURL(/\/workflows\/op-docker-reapply\?site=site-a/)
  expect(installs).toEqual([
    {
      kind: 'nfs',
      assignments: [{ serverId: 'srv-1', roles: ['client'] }],
      spec: { source: '10.0.0.9:/export/data', mountPath: '/shared' },
    },
  ])
})

test('an installed deployment reconfigures one Server with its complete existing spec', async ({ page }) => {
  const installs: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    fleetSize: 5,
    softwareAssignments: [
      assignment('srv-5', 'docker-ce', 'installed', {
        spec: { version: '5:27.3.1-1~ubuntu.24.04~noble', enableApi: false },
      }),
    ],
    onSoftwareInstallRequest: (body) => installs.push(body),
  })
  await page.goto('/software/docker-ce?site=site-a')

  const row = page.getByRole('table', { name: 'Docker CE deployments' }).getByRole('row', { name: /compute-node-005/ })
  await row.getByRole('button', { name: 'More' }).click()
  await page.getByRole('menuitem', { name: 'Reconfigure' }).click()

  const dialog = page.getByRole('dialog', { name: 'Reconfigure Docker CE' })
  await expect(dialog.getByRole('textbox', { name: 'Version' })).toHaveValue('5:27.3.1-1~ubuntu.24.04~noble')
  await expect(dialog.getByRole('checkbox', { name: /Enable the Docker Engine API/ })).not.toBeChecked()
  await dialog.getByRole('button', { name: 'Reconfigure', exact: true }).click()

  expect(installs).toEqual([
    {
      kind: 'docker-ce',
      assignments: [{ serverId: 'srv-5', roles: [] }],
      spec: { version: '5:27.3.1-1~ubuntu.24.04~noble', enableApi: false },
    },
  ])
})

test('Server detail chooses software first, then configures its fixed target', async ({ page }) => {
  const installs: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    fleetSize: 5,
    onSoftwareInstallRequest: (body) => installs.push(body),
  })
  await page.goto('/servers/srv-5/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: /Install software/ }).click()

  const dialog = page.getByRole('dialog', { name: 'Install software' })
  await expect(dialog).toContainText('Choose and configure software for compute-node-005.')
  await dialog.getByRole('radio', { name: /Docker CE/ }).locator('..').click()
  await dialog.getByRole('button', { name: 'Next', exact: true }).click()
  await dialog.getByRole('button', { name: 'Install', exact: true }).click()

  await expect(page).toHaveURL(/\/workflows\/op-docker-reapply\?site=site-a/)
  expect(installs).toEqual([
    {
      kind: 'docker-ce',
      assignments: [{ serverId: 'srv-5', roles: [] }],
      spec: { enableApi: true },
    },
  ])
})

test('Server detail software choices stay equal height when an assignment failed', async ({ page }) => {
  await installApiFixtures(page, {
    fleetSize: 5,
    softwareAssignments: [assignment('srv-5', 'docker-ce', 'failed')],
  })
  await page.goto('/servers/srv-5/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: /Install software/ }).click()

  const dialog = page.getByRole('dialog', { name: 'Install software' })
  await expect(dialog.getByText('Install failed')).toBeVisible()
  const cardHeights = await dialog
    .locator('.sw-software-picker-item')
    .evaluateAll((cards) => cards.slice(0, 2).map((card) => card.getBoundingClientRect().height))

  expect(cardHeights).toHaveLength(2)
  expect(cardHeights[0]).toBeGreaterThan(0)
  expect(Math.abs(cardHeights[0] - cardHeights[1])).toBeLessThanOrEqual(1)

  await page.setViewportSize({ width: 390, height: 844 })
  const mobileCardHeights = await dialog
    .locator('.sw-software-picker-item')
    .evaluateAll((cards) => cards.slice(0, 2).map((card) => card.getBoundingClientRect().height))
  expect(Math.abs(mobileCardHeights[0] - mobileCardHeights[1])).toBeLessThanOrEqual(1)
})

test('a Server-fixed NFS install still requires its role-specific configuration', async ({ page }) => {
  const installs: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    fleetSize: 5,
    onSoftwareInstallRequest: (body) => installs.push(body),
  })
  await page.goto('/servers/srv-5/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  await page.getByRole('menuitem', { name: /Install software/ }).click()

  const dialog = page.getByRole('dialog', { name: 'Install software' })
  await dialog.getByRole('radio', { name: /NFS/ }).locator('..').click()
  await dialog.getByRole('button', { name: 'Next', exact: true }).click()
  const install = dialog.getByRole('button', { name: 'Install', exact: true })
  await expect(install).toBeDisabled()
  await dialog.getByRole('checkbox', { name: 'client' }).click({ force: true })
  await dialog.getByRole('textbox', { name: 'Source (client)' }).fill('10.0.0.9:/export/data')
  await dialog.getByRole('textbox', { name: 'Mount path (client)' }).fill('/shared')
  await install.click()

  expect(installs).toEqual([
    {
      kind: 'nfs',
      assignments: [{ serverId: 'srv-5', roles: ['client'] }],
      spec: { source: '10.0.0.9:/export/data', mountPath: '/shared' },
    },
  ])
})

test('Install software is disabled with the reason on a Server that cannot take it', async ({ page }) => {
  await installApiFixtures(page, { lockedServerIds: ['srv-3'] })

  await page.goto('/servers/srv-4/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  let item = page.getByRole('menuitem', { name: /Install software/ })
  await expect(item).toHaveAttribute('aria-disabled', 'true')
  await expect(item).toContainText('The last OS deployment did not succeed.')
  await page.keyboard.press('Escape')

  await page.goto('/servers/srv-3/summary?site=site-a')
  await page.getByRole('button', { name: 'Take action' }).click()
  item = page.getByRole('menuitem', { name: /Install software/ })
  await expect(item).toHaveAttribute('aria-disabled', 'true')
  await expect(item).toContainText('Unlock the Server before installing software')
})

test('changing deployments keep last-good rows through a failed refresh and recover', async ({ page }) => {
  let assignmentRequests = 0
  await page.clock.install({ time: new Date('2026-08-27T03:05:00Z') })
  await installApiFixtures(page, {
    softwareAssignments: [assignment('srv-1', 'nfs', 'pending', { roles: ['client'] })],
  })
  await page.route('**/api/v1/software/assignments*', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    assignmentRequests += 1
    if (assignmentRequests === 3) {
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'provider_unavailable', message: 'Software refresh unavailable' } }),
      })
      return
    }
    if (assignmentRequests >= 4) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [assignment('srv-1', 'nfs', 'installed', { roles: ['client'] })] }),
      })
      return
    }
    await route.fallback()
  })
  await page.goto('/software/nfs?site=site-a')

  const row = page.getByRole('table', { name: 'NFS deployments' }).getByRole('row', { name: /gpu-node-01/ })
  await expect(row).toContainText('Installing')
  await expect.poll(() => assignmentRequests).toBe(2)
  await page.clock.fastForward(5_000)
  await expect.poll(() => assignmentRequests).toBe(3)
  await expect(page.getByText('Deployment data may be out of date')).toBeVisible()
  await expect(row).toContainText('Installing')

  await page.clock.fastForward(5_000)
  await expect.poll(() => assignmentRequests).toBe(4)
  await expect(row).toContainText('Installed')
  await expect(page.getByText('Deployment data may be out of date')).toHaveCount(0)

  await page.clock.fastForward(6_000)
  expect(assignmentRequests).toBe(4)
})

test('an installed deployment uninstalls from its row and opens the Workflow', async ({ page }) => {
  const uninstalls: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    fleetSize: 5,
    softwareAssignments: [assignment('srv-5', 'docker-ce', 'installed')],
    onSoftwareUninstallRequest: (body) => uninstalls.push(body),
  })
  await page.goto('/software/docker-ce?site=site-a')
  const row = page.getByRole('table', { name: 'Docker CE deployments' }).getByRole('row', { name: /compute-node-005/ })
  await row.getByRole('button', { name: 'More' }).click()
  await page.getByRole('menuitem', { name: 'Uninstall' }).click()
  const dialog = page.getByRole('dialog', { name: 'Uninstall Docker CE' })
  await dialog.getByRole('button', { name: 'Uninstall', exact: true }).click()
  await expect(page).toHaveURL('/workflows/op-software-uninstall?site=site-a')
  expect(uninstalls).toEqual([{ kind: 'docker-ce', serverIds: ['srv-5'] }])
})

test('the target picker explains Kubernetes and mutually exclusive software restrictions', async ({ page }) => {
  await installApiFixtures(page, {
    fleetSize: 5,
    softwareAssignments: [assignment('srv-5', 'podman', 'installed')],
  })
  await page.goto('/software/docker-ce?site=site-a')
  await page.locator('header').getByRole('button', { name: 'Install software', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Install software' })
  await dialog.getByRole('textbox', { name: 'Search installation targets' }).fill('gpu-node-01')
  await expect(dialog.getByRole('checkbox', { name: 'Select gpu-node-01' })).toBeDisabled()
  await expect(dialog.getByText('This software cannot be installed on a Kubernetes member.')).toBeVisible()
  await dialog.getByRole('textbox', { name: 'Search installation targets' }).fill('compute-node-005')
  await expect(dialog.getByRole('checkbox', { name: 'Select compute-node-005' })).toBeDisabled()
  await expect(dialog.getByText('A mutually exclusive software runtime is already assigned.')).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Next', exact: true })).toBeDisabled()
})

test('a failed deployment retries with its complete spec and keeps backend errors inline', async ({ page }) => {
  const installs: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    fleetSize: 5,
    softwareAssignments: [assignment('srv-5', 'nfs', 'failed', {
      roles: ['client'],
      spec: { source: 'storage:/training', mountPath: '/training', mountOptions: 'rw,_netdev' },
    })],
    softwareInstallError: { status: 409, code: 'server_busy', message: 'Another Workflow owns this Server.' },
    onSoftwareInstallRequest: (body) => installs.push(body),
  })
  await page.goto('/software/nfs?site=site-a')
  const row = page.getByRole('table', { name: 'NFS deployments' }).getByRole('row', { name: /compute-node-005/ })
  await row.getByRole('button', { name: 'More' }).click()
  await page.getByRole('menuitem', { name: 'Retry install' }).click()
  const dialog = page.getByRole('dialog', { name: 'Retry NFS installation' })
  await expect(dialog.getByRole('textbox', { name: 'Source (client)' })).toHaveValue('storage:/training')
  await expect(dialog.getByRole('textbox', { name: 'Mount path (client)' })).toHaveValue('/training')
  await expect(dialog.getByRole('checkbox', { name: 'client' })).toBeChecked()
  await dialog.getByRole('button', { name: 'Retry install', exact: true }).click()
  await expect(dialog.getByText('Another Workflow owns this Server.')).toBeVisible()
  expect(installs).toEqual([{
    kind: 'nfs',
    assignments: [{ serverId: 'srv-5', roles: ['client'] }],
    spec: { source: 'storage:/training', mountPath: '/training', mountOptions: 'rw,_netdev' },
  }])
})

test('an unknown software kind returns to the catalog safely', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/software/not-real?site=site-a')
  await expect(page.getByRole('heading', { name: 'Software not found', exact: true }).first()).toBeVisible()
  await page.getByRole('button', { name: 'Back to Software' }).click()
  await expect(page).toHaveURL('/software?site=site-a')
})
