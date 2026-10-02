import { expect, test } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// The Server detail "Containers" tab (decision 043): offered only where swallow installed Docker CE,
// a live Docker Host Explorer when the Engine API is enabled, and an explained opt-in otherwise.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
  })
})

/** The Server-detail Containers tab (the explorer also has a "Containers" sub-section tab). */
function containersTab(page: import('playwright/test').Page) {
  return page.getByRole('tab', { name: 'Containers', exact: true }).first()
}

/** The explorer's Images section tab; the explorer opens on its Containers section. */
function imagesTab(page: import('playwright/test').Page) {
  return page.getByRole('tab', { name: 'Images', exact: true })
}

test('the Containers tab is offered only for a Server where swallow installed Docker CE', async ({ page }) => {
  await installApiFixtures(page, { dockerAssignments: { 'srv-1': 'enabled' } })

  await page.goto('/servers/srv-2/summary?site=site-a')
  await expect(page.getByRole('tab', { name: 'Summary', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Containers', exact: true })).toHaveCount(0)

  await page.goto('/servers/srv-2/containers?site=site-a')
  await expect(page.getByText('Docker CE is not installed by swallow')).toBeVisible()

  await page.goto('/servers/srv-1/summary?site=site-a')
  await expect(containersTab(page)).toBeVisible()
})

test('images are listed live and can be pulled and removed', async ({ page }) => {
  const writes: Array<{ method: string; path: string; body: Record<string, unknown> | null }> = []
  await installApiFixtures(page, {
    dockerAssignments: { 'srv-1': 'enabled' },
    onDockerRequest: (method, path, body) => writes.push({ method, path, body }),
  })
  await page.goto('/servers/srv-1/containers?site=site-a')

  await expect(page.getByRole('heading', { name: 'Docker Engine' })).toBeVisible()
  await expect(page.getByText('tcp://192.168.40.21:2375')).toBeVisible()
  await imagesTab(page).click()
  const images = page.getByRole('table', { name: 'Docker images' })
  await expect(images.getByRole('row').filter({ hasText: 'nginx:1.27' })).toBeVisible()
  await expect(images.getByText('dangling')).toBeVisible()

  await page.getByRole('button', { name: 'Pull image' }).click()
  const pull = page.getByRole('dialog', { name: 'Pull image' })
  await pull.getByRole('textbox', { name: /Image reference/ }).fill('hello-world')
  await pull.getByRole('button', { name: 'Pull', exact: true }).click()
  await expect(pull).toHaveCount(0)
  await expect(images.getByRole('row').filter({ hasText: 'hello-world:latest' })).toBeVisible()
  expect(writes).toContainEqual({ method: 'POST', path: '/images/pull', body: { reference: 'hello-world' } })

  await images.getByRole('button', { name: 'Remove image nginx:1.27' }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Remove image' })
  await expect(confirm).toContainText('An image used by a running container cannot be removed.')
  await confirm.getByRole('button', { name: 'Remove image' }).click()
  await expect(images.getByRole('row').filter({ hasText: 'nginx:1.27' })).toHaveCount(0)
  expect(writes.some((write) => write.method === 'DELETE' && write.path === `/images/${encodeURIComponent(`sha256:${'1'.repeat(64)}`)}`)).toBe(true)
})

test('a long pull continues in the background after the dialog is closed', async ({ page }) => {
  let releasePull = () => {}
  const dockerPullGate = new Promise<void>((resolve) => {
    releasePull = resolve
  })
  await installApiFixtures(page, { dockerAssignments: { 'srv-1': 'enabled' }, dockerPullGate })
  await page.goto('/servers/srv-1/containers?site=site-a')
  await imagesTab(page).click()

  await page.getByRole('button', { name: 'Pull image' }).click()
  const pull = page.getByRole('dialog', { name: 'Pull image' })
  await pull.getByRole('textbox', { name: /Image reference/ }).fill('rocm/pytorch:latest')
  await pull.getByRole('button', { name: 'Pull', exact: true }).click()
  await pull.getByRole('button', { name: 'Continue in background' }).click()
  await expect(pull).toHaveCount(0)

  const inProgress = page.getByRole('list', { name: 'Image pulls in progress' })
  await expect(inProgress).toContainText('rocm/pytorch:latest')
  // Another pull can be started while the first one runs.
  await expect(page.getByRole('button', { name: 'Pull image' })).toBeEnabled()

  releasePull()
  await expect(page.getByText('Image pulled', { exact: true })).toBeVisible()
  await expect(inProgress).toHaveCount(0)
  await expect(page.getByRole('table', { name: 'Docker images' }).getByRole('row').filter({ hasText: 'rocm/pytorch:latest' })).toBeVisible()
})

test('containers show the Engine state and offer only the lifecycle actions that apply', async ({ page }) => {
  const writes: string[] = []
  await installApiFixtures(page, {
    dockerAssignments: { 'srv-1': 'enabled' },
    onDockerRequest: (method, path) => writes.push(`${method} ${path}`),
  })
  await page.goto('/servers/srv-1/containers?site=site-a')
  // The explorer opens on its Containers section (the second "Containers" tab, after the Server tab).
  const sections = page.getByRole('tablist', { name: 'Docker objects' }).getByRole('tab')
  await expect(sections).toHaveText(['Containers', 'Images', 'Volumes', 'Networks'])
  await expect(sections.first()).toHaveAttribute('aria-selected', 'true')

  const containers = page.getByRole('table', { name: 'Docker containers' })
  const web = containers.getByRole('row').filter({ hasText: 'web' })
  await expect(web).toContainText('running')
  await expect(web).toContainText('0.0.0.0:8080->80/tcp')
  await expect(web.getByRole('button', { name: 'Start container web', exact: true })).toHaveCount(0)

  await web.getByRole('button', { name: 'Stop container web' }).click()
  await expect(web).toContainText('exited')
  await expect(web.getByRole('button', { name: 'Start container web', exact: true })).toBeVisible()
  expect(writes).toContain(`POST /containers/c0ffee000000${'a'.repeat(52)}/stop`)

  await web.getByRole('button', { name: 'Logs of container web' }).click()
  await expect(page.getByRole('dialog', { name: 'Logs — web' })).toContainText('nginx: ready')
})

test('without the Engine API the tab explains the risk and re-applies Docker CE to enable it', async ({ page }) => {
  const installs: Array<Record<string, unknown>> = []
  await installApiFixtures(page, {
    dockerAssignments: { 'srv-1': 'disabled' },
    onSoftwareInstallRequest: (body) => installs.push(body),
  })
  await page.goto('/servers/srv-1/containers?site=site-a')

  await expect(page.getByRole('heading', { name: 'Docker management needs the Docker Engine API' })).toBeVisible()
  await expect(page.getByText('Anyone who can reach that port controls the host as root.')).toBeVisible()
  await expect(page.getByText('If the Server can be reached from the Internet, keep this disabled.')).toBeVisible()

  await page.getByRole('button', { name: 'Enable Docker API' }).click()
  await expect(page.getByText('Applying Docker CE', { exact: true })).toBeVisible()
  // The tab stays listed while the re-apply runs, because Docker CE was applied before.
  await expect(containersTab(page)).toBeVisible()
  expect(installs).toEqual([{ kind: 'docker-ce', assignments: [{ serverId: 'srv-1', roles: [] }], spec: { enableApi: true } }])
})

test('a locked Server keeps the explorer readable but disables every write', async ({ page }) => {
  await installApiFixtures(page, { dockerAssignments: { 'srv-1': 'enabled' }, lockedServerIds: ['srv-1'] })
  await page.goto('/servers/srv-1/containers?site=site-a')

  await expect(page.getByText('Read-only while locked')).toBeVisible()
  await imagesTab(page).click()
  await expect(page.getByRole('table', { name: 'Docker images' }).getByRole('row').filter({ hasText: 'nginx:1.27' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Pull image' })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Remove image nginx:1.27' })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Disable Docker API' })).toBeDisabled()
})
