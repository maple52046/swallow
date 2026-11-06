import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// User journeys for Infrastructure → Local repository (decision 078, contract local-repository.md):
// a Site's sync state, where its deployments fetch artifacts, its content, the user's sources, and a
// convergence request. site-a has the `repo-a` Pulp Integration when a scenario is set; site-b never
// has one. Every request is served by deterministic fixtures; no backend or Pulp is contacted.

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
  })
})

/** Records the JSON body of every spec write, in order. */
function recordSpecWrites(page: Page): Array<Record<string, unknown>> {
  const bodies: Array<Record<string, unknown>> = []
  page.on('request', (request) => {
    if (request.method() === 'PUT' && new URL(request.url()).pathname === '/api/v1/sites/site-a/local-repository/spec') {
      bodies.push(request.postDataJSON() as Record<string, unknown>)
    }
  })
  return bodies
}

const ubuntuSource = {
  name: 'ubuntu', format: 'deb', url: 'http://archive.ubuntu.com/ubuntu',
  suites: ['noble', 'noble-updates', 'noble-security'], components: ['main', 'universe'], architectures: ['amd64'],
}

test('the Local repository tab shows the scoped Site\'s readiness, sources, files, and declarations', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'ready' })
  await page.goto('/infrastructure/sites?site=site-a')
  await page.getByRole('tab', { name: 'Local repository' }).click()

  await expect(page).toHaveURL('/infrastructure/local-repository?site=site-a')
  await expect(page.getByRole('tab', { name: 'Local repository' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByText('Deployments that fetch from the Local Repository are accepted.')).toBeVisible()
  await expect(page.getByText('http://10.170.168.20:8080', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Copy content origin' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Pulp Taipei' })).toHaveAttribute('href', '/infrastructure/integrations/repo-a/general?site=site-a')
  await expect(page.getByText('Generation 1, converged by the worker')).toBeVisible()

  // Who declared each source: a module by its id, the user's spec as "User".
  const sources = page.getByRole('table', { name: 'Repository sources' })
  await expect(sources.getByRole('row')).toHaveCount(7)
  await expect(sources.getByRole('row', { name: /^docker\.io/ })).toContainText('grafana, amd')
  await expect(sources.getByRole('row', { name: /^ubuntu/ })).toContainText('User')
  await expect(sources.getByRole('row', { name: /^ubuntu/ })).toContainText('64,012')
  await expect(sources.getByRole('row', { name: /^ubuntu/ })).toContainText('Suites: noble, noble-updates, noble-security')

  // A file's digest is shortened, with the full value one click away.
  const k0s = page.getByRole('table', { name: 'Declared files' }).getByRole('row', { name: /k0s-v1\.36\.3/ })
  await expect(k0s).toContainText('f8b278ba5a99…')
  await expect(k0s.getByRole('button', { name: 'Copy sha256 of k0s/v1.36.3+k0s.2/k0s-v1.36.3+k0s.2-amd64' })).toBeVisible()
  await expect(k0s).toContainText('237 MiB')

  // A declared image reads like a file: its observed state and, once every layer is stored, its size.
  const pause = page.getByRole('table', { name: 'Declared images' }).getByRole('row', { name: /pause:3\.10\.2-0/ })
  await expect(pause).toContainText('Ready')
  await expect(pause).toContainText('313 KiB')
  await expect(pause).toContainText('kubernetes')
  await expect(page.getByRole('list', { name: 'Declared Python packages' })).toContainText('Flask==3.1.3')
  await expect(page.getByRole('list', { name: 'User sources' })).toContainText('http://archive.ubuntu.com/ubuntu')
})

test('a syncing repository says why deployments wait and settles in place', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'syncing', localRepositorySettleAfterReads: 1 })
  await page.goto('/infrastructure/local-repository?site=site-a')

  const sources = page.getByRole('table', { name: 'Repository sources' })
  await expect(sources.getByRole('row', { name: /^grafana/ })).toContainText('Syncing')
  await expect(sources.getByRole('row', { name: /^grafana/ })).toContainText('Downloading package indexes')
  await expect(sources.getByRole('row', { name: /^docker\.io/ })).toContainText('Pending')
  // Images wait for their registry's cache, then for their layers; neither has a size yet.
  const images = page.getByRole('table', { name: 'Declared images' })
  await expect(images.getByRole('row', { name: /grafana\/grafana/ })).toContainText('Pending')
  const kubeProxy = images.getByRole('row', { name: /kube-proxy/ })
  await expect(kubeProxy).toContainText('Syncing')
  await expect(kubeProxy).toContainText('—')
  await expect(page.getByText('Not ready', { exact: true })).toBeVisible()
  // The API's own explanation of what is in the way, not one the dashboard works out.
  await expect(page.getByText(/source grafana is syncing/)).toBeVisible()
  await expect(page.getByText(/image quay\.io\/k0sproject\/kube-proxy:v1\.36\.3-1 is syncing/)).toBeVisible()
  await expect(page.getByText('Updating every 5 s while it converges')).toBeVisible()

  // The next read, five seconds later, finds everything ready.
  await expect(sources.getByRole('row', { name: /^grafana/ })).toContainText('Ready', { timeout: 10_000 })
  await expect(kubeProxy).toContainText('Ready')
  await expect(kubeProxy).toContainText('29 MiB')
  await expect(page.getByText('Deployments that fetch from the Local Repository are accepted.')).toBeVisible()
  await expect(page.getByText('Updating every 5 s while it converges')).toHaveCount(0)
})

test('a failed source, file, and image show the provider\'s reason as text', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'failed' })
  await page.goto('/infrastructure/local-repository?site=site-a')

  const dockerCe = page.getByRole('table', { name: 'Repository sources' }).getByRole('row', { name: /^docker-ce/ })
  await expect(dockerCe).toContainText('Failed')
  await expect(dockerCe).toContainText('sync failed: 404 Not Found fetching https://download.docker.com/linux/ubuntu/dists/noble/Release')
  const k0s = page.getByRole('table', { name: 'Declared files' }).getByRole('row', { name: /k0s-v1\.36\.3/ })
  await expect(k0s).toContainText('Failed')
  await expect(k0s).toContainText('checksum mismatch')
  const kubeProxy = page.getByRole('table', { name: 'Declared images' }).getByRole('row', { name: /kube-proxy/ })
  await expect(kubeProxy).toContainText('Failed')
  await expect(kubeProxy).toContainText('could not fetch the image quay.io/k0sproject/kube-proxy:v1.36.3-1 from its upstream registry (HTTP 502)')
  await expect(page.getByText('Sync failed', { exact: true })).toBeVisible()
  await expect(page.getByText(/source docker-ce failed: 404 Not Found; file k0s/)).toBeVisible()
})

test('a rejected credential sends the user to the Integration\'s Credential tab', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'attention' })
  await page.goto('/infrastructure/local-repository?site=site-a')

  await expect(page.getByText('The repository Integration needs attention')).toBeVisible()
  await expect(page.getByText(/Pulp refused the Integration's credential \(401 Unauthorized\)/).first()).toBeVisible()
  await page.getByRole('link', { name: 'Open the Integration settings' }).click()
  await expect(page).toHaveURL('/infrastructure/integrations/repo-a/credential?site=site-a')
  await expect(page.getByText('Pulp admin password').first()).toBeVisible()
})

test('Sync now asks the worker to converge and says the result follows', async ({ page }) => {
  const writes: string[] = []
  await installApiFixtures(page, { localRepository: 'ready', onLocalRepositoryRequest: (method, path) => writes.push(`${method} ${path}`) })
  await page.goto('/infrastructure/local-repository?site=site-a')

  await page.getByRole('button', { name: 'Sync now' }).click()
  await expect(page.getByText('Sync requested')).toBeVisible()
  expect(writes).toEqual(['POST /api/v1/sites/site-a/local-repository/sync'])
})

test('a Site without a Local Repository explains why and registers one from Integrations', async ({ page }) => {
  const created: Array<Record<string, unknown>> = []
  await installApiFixtures(page, { localRepository: 'ready' })
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/integrations') {
      created.push(request.postDataJSON() as Record<string, unknown>)
    }
  })
  await page.goto('/infrastructure/local-repository?site=site-b')

  await expect(page.getByText('This Site has no Local Repository')).toBeVisible()
  await expect(page.getByText(/This Site's deployments fetch each package, image, and file from the upstream source its module declares/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sync now' })).toHaveCount(0)

  await page.getByRole('button', { name: 'Register a repository' }).click()
  await expect(page).toHaveURL('/infrastructure/integrations?site=site-b')
  const dialog = page.getByRole('dialog', { name: 'Create integration' })
  await expect(dialog.getByRole('combobox', { name: 'Role', exact: true })).toContainText('Repository')
  await expect(dialog.getByRole('combobox', { name: 'Provider', exact: true })).toContainText('Pulp')

  await dialog.getByRole('textbox', { name: /^Name/ }).fill('Pulp Edge')
  await dialog.getByRole('textbox', { name: /^Endpoint/ }).fill('http://10.9.0.5:24817')
  await dialog.getByLabel(/^Pulp admin password/).fill('fixture-password')
  const origin = dialog.getByRole('textbox', { name: /^Content origin/ })
  await origin.fill('http://10.9.0.5')
  await expect(dialog.getByText('Use a scheme, host, and port without a path, for example http://10.0.0.5:8080.')).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Create integration' })).toBeDisabled()
  await origin.fill('http://10.9.0.5:8080')
  await dialog.getByRole('button', { name: 'Create integration' }).click()

  await expect(page).toHaveURL('/infrastructure/local-repository?site=site-b')
  expect(created).toEqual([expect.objectContaining({
    siteId: 'site-b', kind: 'repository', providerKind: 'pulp', credential: 'fixture-password', settings: { contentOrigin: 'http://10.9.0.5:8080' },
  })])
})

test('without a scoped Site the page asks for one and keeps the choice in the URL', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'ready' })
  await page.goto('/infrastructure/local-repository')

  await expect(page.getByText(/^Each Site has its own Local Repository/)).toBeVisible()
  await page.getByRole('combobox', { name: 'Site', exact: true }).click()
  await page.getByRole('option', { name: 'Taipei Lab', exact: true }).click()
  await expect(page).toHaveURL('/infrastructure/local-repository?site=site-a')
  await expect(page.getByRole('table', { name: 'Repository sources' })).toBeVisible()
})

test('the content browser pages, filters, and switches formats through the URL', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'ready' })
  await page.goto('/infrastructure/local-repository?site=site-a')

  const packages = page.getByRole('table', { name: 'OS packages (deb) in the Local Repository' })
  const summary = page.getByText(/^Showing \d+–\d+ of \d+/)
  await expect(summary).toHaveText('Showing 1–25 of 44 OS packages')
  await page.getByRole('button', { name: 'Next content page' }).click()
  await expect(page).toHaveURL('/infrastructure/local-repository?site=site-a&page=2')
  await expect(summary).toHaveText('Showing 26–44 of 44 OS packages')
  await expect(packages.getByRole('row', { name: /^zstd/ })).toContainText('ubuntu')

  // A source narrows the list and starts again at its first page.
  await page.getByRole('combobox', { name: 'Source', exact: true }).click()
  await page.getByRole('option', { name: 'docker-ce', exact: true }).click()
  await expect(page).toHaveURL('/infrastructure/local-repository?site=site-a&source=docker-ce')
  await expect(summary).toHaveText('Showing 1–3 of 3 OS packages')
  await page.getByRole('textbox', { name: 'Search content by name' }).fill('cli')
  await expect(page).toHaveURL('/infrastructure/local-repository?site=site-a&source=docker-ce&q=cli')
  await expect(packages.getByRole('row')).toHaveCount(2)
  await expect(packages).toContainText('docker-ce-cli')

  // Another format drops the source of the previous one; columns follow the fields present.
  await page.getByRole('combobox', { name: 'Content format', exact: true }).click()
  await page.getByRole('option', { name: 'Container images', exact: true }).click()
  await expect(page).toHaveURL('/infrastructure/local-repository?site=site-a&q=cli&format=oci')
  await page.getByRole('button', { name: 'Clear search' }).click()
  const images = page.getByRole('table', { name: 'Container images in the Local Repository' })
  await expect(images).toContainText('quay.io/k0sproject/pause')
  await expect(images.getByRole('columnheader', { name: 'Version' })).toBeVisible()
  await expect(images.getByRole('columnheader', { name: 'Path' })).toHaveCount(0)
})

test('a shared link with a source the format does not have is canonicalized away', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'ready' })
  await page.goto('/infrastructure/local-repository?site=site-a&format=python&source=docker-ce&page=1')
  await expect(page).toHaveURL('/infrastructure/local-repository?site=site-a&format=python')
  await expect(page.getByRole('table', { name: 'Python packages in the Local Repository' })).toContainText('Flask')
})

test('the content browser shows the provider\'s reason when it cannot be read', async ({ page }) => {
  await installApiFixtures(page, {
    localRepository: 'ready',
    localRepositoryContentError: { status: 503, code: 'provider_unavailable', message: 'The local repository provider could not be reached.' },
  })
  await page.goto('/infrastructure/local-repository?site=site-a')
  await expect(page.getByText('The local repository provider could not be reached.')).toBeVisible()
  await expect(page.getByText('Request ID: req-local-repository-content')).toBeVisible()
  await expect(page.getByText('Content unavailable')).toBeVisible()
})

test('Manage user sources adds a deb source and saves the whole list with its generation', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'ready' })
  const writes = recordSpecWrites(page)
  await page.goto('/infrastructure/local-repository?site=site-a')
  await page.getByRole('button', { name: 'Manage user sources' }).click()

  const dialog = page.getByRole('dialog', { name: 'Manage user sources' })
  await expect(dialog.getByRole('list', { name: 'Your sources' })).toContainText('ubuntu')
  await expect(dialog.getByText('Sources declared by modules (5), not editable here')).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Save sources' })).toBeDisabled()

  // A module's source name with another URL would contradict the module, which the API refuses.
  const name = dialog.getByRole('textbox', { name: /^Name/ })
  await name.fill('docker-ce')
  await expect(dialog.getByText('A module already declares docker-ce with another format or URL; choose another name.')).toBeVisible()

  await name.fill('ubuntu-ports')
  await dialog.getByRole('textbox', { name: /^URL/ }).fill('http://ports.ubuntu.com/ubuntu-ports')
  await dialog.getByRole('textbox', { name: /^Suites/ }).fill('noble noble-updates')
  await dialog.getByRole('textbox', { name: /^Components/ }).fill('main')
  await dialog.getByRole('textbox', { name: /^Architectures/ }).fill('arm64')
  await dialog.getByRole('button', { name: 'Add to list' }).click()
  await expect(dialog.getByRole('list', { name: 'Your sources' })).toContainText('ubuntu-ports')
  await dialog.getByRole('button', { name: 'Save sources' }).click()

  await expect(page.getByText('User sources saved')).toBeVisible()
  expect(writes).toEqual([{
    generation: 1,
    serveDeployments: true,
    sources: [
      ubuntuSource,
      { name: 'ubuntu-ports', format: 'deb', url: 'http://ports.ubuntu.com/ubuntu-ports', suites: ['noble', 'noble-updates'], components: ['main'], architectures: ['arm64'] },
    ],
  }])
  // The new source reads pending until the worker syncs it, then settles in place.
  const row = page.getByRole('table', { name: 'Repository sources' }).getByRole('row', { name: /^ubuntu-ports/ })
  await expect(row).toContainText('Pending')
  await expect(row).toContainText('User')
  await expect(page.getByText('Generation 2 saved, not converged by the worker yet')).toBeVisible()
  await expect(row).toContainText('Ready', { timeout: 10_000 })
})

test('a stale generation reloads the sources and asks for the change again instead of overwriting', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'ready', localRepositorySpecConflict: true })
  const writes = recordSpecWrites(page)
  await page.goto('/infrastructure/local-repository?site=site-a')
  await page.getByRole('button', { name: 'Manage user sources' }).click()
  const dialog = page.getByRole('dialog', { name: 'Manage user sources' })

  await dialog.getByRole('button', { name: 'Remove source ubuntu' }).click()
  await dialog.getByRole('button', { name: 'Save sources' }).click()
  await expect(dialog.getByText("This Site's Local Repository changed since you opened this dialog")).toBeVisible()
  await expect(dialog.getByText('Nothing was saved. The list now shows the current sources; make your change again.')).toBeVisible()
  // Another admin's `ports` source arrived with the newer generation; the removal was not applied.
  const current = dialog.getByRole('list', { name: 'Your sources' })
  await expect(current).toContainText('ubuntu')
  await expect(current).toContainText('ports')
  await expect(dialog.getByRole('button', { name: 'Save sources' })).toBeDisabled()

  await dialog.getByRole('button', { name: 'Remove source ubuntu' }).click()
  await dialog.getByRole('button', { name: 'Save sources' }).click()
  await expect(page.getByText('User sources saved')).toBeVisible()
  expect(writes.map((body) => body.generation)).toEqual([1, 2])
  expect(writes[1].sources).toEqual([
    { name: 'ports', format: 'deb', url: 'http://ports.ubuntu.com/ubuntu-ports', suites: ['noble'], components: ['main'], architectures: ['arm64'] },
  ])
})

test('switching deployments to the upstream sources asks first and writes the whole spec', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'ready' })
  const writes = recordSpecWrites(page)
  await page.goto('/infrastructure/local-repository?site=site-a')

  const setting = page.getByRole('radiogroup', { name: 'Artifact source for deployments' })
  const upstream = setting.getByRole('radio', { name: /^Upstream sources/ })
  const local = setting.getByRole('radio', { name: /^This local repository/ })
  await expect(local).toBeChecked()

  // Cancelling leaves the stored choice and writes nothing.
  await setting.getByText('Upstream sources', { exact: true }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Fetch deployment artifacts from the upstream sources?' })
  await expect(confirm).toContainText('Swallow stops synchronizing this repository and keeps what it already holds.')
  await expect(confirm.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await confirm.getByRole('button', { name: 'Cancel' }).click()
  await expect(confirm).toHaveCount(0)
  await expect(local).toBeChecked()
  expect(writes).toEqual([])

  await setting.getByText('Upstream sources', { exact: true }).click()
  await confirm.getByRole('button', { name: 'Use the upstream sources' }).click()
  await expect(page.getByText('Deployments use the upstream sources', { exact: true })).toBeVisible()
  expect(writes).toEqual([{ generation: 1, serveDeployments: false, sources: [ubuntuSource] }])

  // The page now reads as a Site that fetches from the upstreams, not as a failed repository.
  await expect(upstream).toBeChecked()
  await expect(page.getByText('Deployments fetch each artifact from the upstream source its module declares.')).toBeVisible()
  await expect(page.getByText('Not synchronizing', { exact: true })).toBeVisible()
  await expect(page.getByText('Generation 2 saved, synchronized once deployments use this repository')).toBeVisible()
  await expect(page.getByText('Not ready', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Sync now' })).toHaveCount(0)
})

test('a Site that fetches from the upstream sources reads the last observation without alarms', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'not-serving' })
  await page.goto('/infrastructure/local-repository?site=site-a')

  await expect(page.getByRole('radio', { name: /^Upstream sources/ })).toBeChecked()
  await expect(page.getByText('Deployments fetch each artifact from the upstream source its module declares.')).toBeVisible()
  await expect(page.getByText('The repository is not synchronized while deployments use the upstream sources; what it already holds is kept.')).toBeVisible()
  await expect(page.getByText('Not ready', { exact: true })).toHaveCount(0)
  await expect(page.getByText('Sync failed', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Sync now' })).toHaveCount(0)
  // Nothing converges, so the page does not poll.
  await expect(page.getByText('Updating every 5 s while it converges')).toHaveCount(0)

  // Items keep their last observed state, as facts rather than as work in progress or failures.
  const sources = page.getByRole('table', { name: 'Repository sources' })
  const dockerCe = sources.getByRole('row', { name: /^docker-ce/ })
  await expect(dockerCe).toContainText('Last sync failed')
  await expect(dockerCe).toContainText('sync failed: 404 Not Found')
  const grafana = sources.getByRole('row', { name: /^grafana/ })
  await expect(grafana).toContainText('Not fetched')
  // The progress message of a fetch that no longer runs is left out.
  await expect(grafana).not.toContainText('Downloading package indexes')
  await expect(sources.getByRole('row', { name: /^ubuntu/ })).toContainText('Ready')
  await expect(sources.getByText('Failed', { exact: true })).toHaveCount(0)
  await expect(sources.getByText('Syncing', { exact: true })).toHaveCount(0)
  const kubeProxy = page.getByRole('table', { name: 'Declared images' }).getByRole('row', { name: /kube-proxy/ })
  await expect(kubeProxy).toContainText('Not fetched')
  await expect(page.getByRole('table', { name: 'Declared files' }).getByRole('row', { name: /k0s-v1\.36\.3/ })).toContainText('Not fetched')
  await expect(page.locator('.sw-local-repository .sw-progress-spinner')).toHaveCount(0)
})

test('switching deployments to the local repository starts its synchronization', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'not-serving' })
  const writes = recordSpecWrites(page)
  await page.goto('/infrastructure/local-repository?site=site-a')

  const setting = page.getByRole('radiogroup', { name: 'Artifact source for deployments' })
  await setting.getByText('This local repository', { exact: true }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Fetch deployment artifacts from this local repository?' })
  await expect(confirm).toContainText('Swallow starts synchronizing it now, and deployments are refused until it is ready.')
  await expect(confirm).toContainText('Deployments already accepted keep the source they were accepted with.')
  await confirm.getByRole('button', { name: 'Use this local repository' }).click()

  await expect(page.getByText('Deployments use the local repository', { exact: true })).toBeVisible()
  expect(writes).toEqual([{ generation: 1, serveDeployments: true, sources: [ubuntuSource] }])
  await expect(setting.getByRole('radio', { name: /^This local repository/ })).toBeChecked()
  // Readiness reads as today again: not ready yet, the polling cue, and the items in progress.
  await expect(page.getByText('Not ready', { exact: true })).toBeVisible()
  await expect(page.getByText('Updating every 5 s while it converges')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sync now' })).toBeVisible()
  const sources = page.getByRole('table', { name: 'Repository sources' })
  await expect(sources.getByRole('row', { name: /^grafana/ })).toContainText('Syncing')
  await expect(sources.getByRole('row', { name: /^docker-ce/ })).toContainText('Failed')
  // Until the worker judges the new generation, its earlier `not_serving` judgement reads as waiting.
  await expect(page.getByText('The worker has not checked the repository since deployments started using it; it synchronizes it shortly.')).toBeVisible()
  await expect(page.getByText('the spec does not serve deployments')).toHaveCount(0)
  // Then the API's own explanation of what is in the way.
  await expect(page.getByText(/source docker-ce failed: 404 Not Found; source docker\.io is pending/)).toBeVisible({ timeout: 10_000 })
})

test('the setting refuses to overwrite a spec someone else changed and shows the current one', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'ready', localRepositorySpecConflict: true })
  const writes = recordSpecWrites(page)
  await page.goto('/infrastructure/local-repository?site=site-a')

  const setting = page.getByRole('radiogroup', { name: 'Artifact source for deployments' })
  await setting.getByText('Upstream sources', { exact: true }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Use the upstream sources' }).click()
  await expect(page.getByRole('alertdialog')).toHaveCount(0)
  await expect(page.getByText("This Site's Local Repository changed since the page read it")).toBeVisible()
  await expect(page.getByText('Nothing was saved. The choice below is the current one; change it again if you still want to.')).toBeVisible()
  await expect(setting.getByRole('radio', { name: /^This local repository/ })).toBeChecked()

  // The next attempt is made on the newer generation, with the other admin's sources kept.
  await setting.getByText('Upstream sources', { exact: true }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Use the upstream sources' }).click()
  await expect(page.getByText('Deployments use the upstream sources', { exact: true })).toBeVisible()
  expect(writes.map((body) => [body.generation, body.serveDeployments])).toEqual([[1, false], [2, false]])
  expect(writes[1].sources).toEqual([
    ubuntuSource,
    { name: 'ports', format: 'deb', url: 'http://ports.ubuntu.com/ubuntu-ports', suites: ['noble'], components: ['main'], architectures: ['arm64'] },
  ])
})

test('Manage user sources keeps where deployments fetch artifacts', async ({ page }) => {
  await installApiFixtures(page, { localRepository: 'not-serving' })
  const writes = recordSpecWrites(page)
  await page.goto('/infrastructure/local-repository?site=site-a')
  await page.getByRole('button', { name: 'Manage user sources' }).click()

  const dialog = page.getByRole('dialog', { name: 'Manage user sources' })
  await dialog.getByRole('button', { name: 'Remove source ubuntu' }).click()
  await dialog.getByRole('button', { name: 'Save sources' }).click()
  await expect(page.getByText('User sources saved')).toBeVisible()
  await expect(page.getByText("The worker syncs new sources once this Site's deployments use the local repository.")).toBeVisible()
  expect(writes).toEqual([{ generation: 1, serveDeployments: false, sources: [] }])
  await expect(page.getByRole('radio', { name: /^Upstream sources/ })).toBeChecked()
})
