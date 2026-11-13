import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// User journeys for accounts and Pool access (decision 082). Every request is served by
// deterministic fixtures: an admin, alice (owner of pool-research), and bob (its member).

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
  })
})

function primaryNavigation(page: Page) {
  return page.getByTestId('desktop-navigation').getByRole('navigation', { name: 'Primary navigation' })
}

test('An admin creates a user, changes its role, and deletes it', async ({ page }) => {
  let created: Record<string, unknown> | undefined
  await installApiFixtures(page)
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/users') created = request.postDataJSON() as Record<string, unknown>
  })
  await page.goto('/users?site=site-a')

  const table = page.getByRole('table', { name: 'Users' })
  await expect(table.getByRole('row', { name: /alice/ })).toContainText('10001')
  // The admin's own row offers no delete: the API refuses deleting oneself.
  await expect(table.getByRole('button', { name: 'Delete admin' })).toHaveCount(0)

  await page.getByRole('button', { name: 'Create user' }).first().click()
  await page.locator('#user-create-username').fill('Carol')
  await expect(page.getByText('Start with a lowercase letter or _')).toBeVisible()
  await page.locator('#user-create-username').fill('carol')
  await page.locator('#user-create-password').fill('short')
  await expect(page.getByText('Use at least 8 characters.')).toBeVisible()
  await page.locator('#user-create-password').fill('correct-horse')
  await page.getByRole('button', { name: 'Create user', exact: true }).last().click()

  await expect(page.getByText('User carol created')).toBeVisible()
  expect(created).toEqual({ username: 'carol', password: 'correct-horse', role: 'user' })
  await expect(table.getByRole('row', { name: /carol/ })).toContainText('10003')
  await expect(page.getByRole('table', { name: 'Recent access changes' })).toContainText('User created')

  await table.getByRole('button', { name: 'Edit carol' }).click()
  await page.getByRole('combobox', { name: 'Role', exact: true }).click()
  await page.getByRole('option', { name: /^Admin/ }).click()
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByText('User carol updated')).toBeVisible()
  await expect(table.getByRole('row', { name: /carol/ })).toContainText('Admin')

  await table.getByRole('button', { name: 'Delete carol' }).click()
  const dialog = page.getByRole('alertdialog')
  await expect(dialog).toContainText('loses their SSH and API keys')
  await dialog.getByRole('button', { name: 'Delete user' }).click()
  await expect(page.getByText('User carol deleted')).toBeVisible()
  await expect(table.getByRole('row', { name: /carol/ })).toHaveCount(0)
})

test('An admin sees the Directory and confirms the admin sudo setting before it changes', async ({ page }) => {
  await installApiFixtures(page)
  await page.goto('/users?site=site-a')

  await expect(page.getByText('ldaps://swallow.lab.example:636')).toBeVisible()
  await expect(page.getByText('3 users, 4 groups, 3 sudo rules, 5 hosts')).toBeVisible()
  await expect(page.getByText('First uid and gid')).toBeVisible()

  await page.getByText('Admins use sudo without a password: on').click()
  const dialog = page.getByRole('alertdialog')
  await expect(dialog).toContainText('must type their Swallow password')
  await dialog.getByRole('button', { name: 'Turn off' }).click()
  await expect(page.getByText('Access settings saved')).toBeVisible()
  await expect(page.getByText('Admins use sudo without a password: off')).toBeVisible()
})

test('An admin creates, renames, and deletes an installation-wide Pool on the Pools page', async ({ page }) => {
  let created: Record<string, unknown> | undefined
  await installApiFixtures(page)
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/infrastructure/pools') created = request.postDataJSON() as Record<string, unknown>
  })
  await page.goto('/pools?site=site-a')

  const table = page.getByRole('table', { name: 'Pools' })
  await expect(table.getByRole('row', { name: /research/ })).toContainText('Admin (not a member)')
  await expect(table.getByRole('columnheader', { name: 'Site' })).toHaveCount(0)

  await page.getByRole('button', { name: 'Create pool' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Create pool' })
  await expect(dialog.getByRole('combobox', { name: 'Site' })).toHaveCount(0)
  await dialog.getByRole('textbox', { name: 'Name' }).fill('research')
  await dialog.getByRole('button', { name: 'Create pool' }).click()
  await expect(dialog).toContainText('A pool with this name already exists.')
  await dialog.getByRole('textbox', { name: 'Name' }).fill('gpu')
  await dialog.getByRole('button', { name: 'Create pool' }).click()
  await expect(page.getByText('Pool created')).toBeVisible()
  expect(created).toEqual({ name: 'gpu', description: '' })

  await table.getByRole('button', { name: 'Edit gpu' }).click()
  await page.getByRole('dialog', { name: 'Edit pool' }).getByRole('textbox', { name: 'Name' }).fill('gpu-shared')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('Pool updated')).toBeVisible()
  await expect(table.getByRole('row', { name: /gpu-shared/ })).toBeVisible()

  await table.getByRole('button', { name: 'Delete gpu-shared' }).click()
  const confirm = page.getByRole('alertdialog')
  await expect(confirm).toContainText('with its members and sudo defaults; nothing changes in the provisioner')
  await confirm.getByRole('textbox', { name: 'Pool name confirmation' }).fill('gpu-shared')
  await confirm.getByRole('button', { name: 'Delete pool' }).click()
  await expect(page.getByText('Pool deleted')).toBeVisible()
  await expect(table.getByRole('row', { name: /gpu-shared/ })).toHaveCount(0)

  // The Infrastructure area no longer has a Pools tab; its old address opens this page.
  await page.goto('/infrastructure/pools?site=site-a')
  await expect(page).toHaveURL(/\/pools$/)
})

test('An owner adds a member, overrides its sudo, and sees the effective values from the API', async ({ page }) => {
  await installApiFixtures(page, { identity: 'owner' })
  await page.goto('/pools?site=site-a')

  await expect(page.getByRole('table', { name: 'Pools' }).getByRole('row', { name: /research/ })).toContainText('Owner')
  // The Pool's name opens its page, as a Server's or a Platform's name does; there is no separate
  // Members link.
  await expect(page.getByRole('link', { name: /^Members of / })).toHaveCount(0)
  await page.getByRole('table', { name: 'Pools' }).getByRole('link', { name: 'research', exact: true }).click()
  await expect(page).toHaveURL(/\/pools\/pool-research/)
  await expect(page.getByRole('heading', { name: 'research', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Pools', exact: true }).first()).toHaveAttribute('href', /\/pools/)

  const members = page.getByRole('table', { name: 'Pool members' })
  await expect(members.getByRole('row', { name: /alice/ })).toContainText('Yes, owner role')
  await expect(members.getByRole('row', { name: /bob/ })).toContainText('No, Pool default')

  await page.getByRole('button', { name: 'Add member' }).click()
  await page.locator('#member-add-username').fill('nobody')
  await page.getByRole('button', { name: 'Add member' }).last().click()
  await expect(page.getByText('No user is named nobody.')).toBeVisible()
  await page.locator('#member-add-username').fill('admin')
  await page.getByRole('button', { name: 'Add member' }).last().click()
  await expect(page.getByText('admin added to research')).toBeVisible()

  await members.getByRole('combobox', { name: 'Sudo of bob' }).click()
  await page.getByRole('option', { name: 'Yes', exact: true }).click()
  await expect(page.getByText('bob updated')).toBeVisible()
  await expect(members.getByRole('row', { name: /bob/ })).toContainText('Yes, member override')

  await page.getByText('Members have sudo: no').click()
  await page.getByRole('button', { name: 'Save defaults' }).click()
  await expect(page.getByText('Sudo defaults saved')).toBeVisible()
  await expect(members.getByRole('row', { name: /admin/ })).toContainText('Yes, Pool default')
})

test('An owner adds an account it picks from the Pool\'s candidates, right from the Pools page', async ({ page }) => {
  const candidateQueries: string[] = []
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (url.pathname.endsWith('/member-candidates')) candidateQueries.push(url.searchParams.get('query') ?? '')
  })
  await installApiFixtures(page, { identity: 'owner' })
  await page.goto('/pools?site=site-a')

  await page.getByRole('button', { name: 'Add a member to research' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Add a member to research' })
  // alice and bob are members already, so the only account left to add is admin.
  const matches = dialog.getByRole('group', { name: 'Matching accounts' })
  await expect(matches.getByRole('button')).toHaveText(['admin'])
  await dialog.locator('#member-add-username').fill('zz')
  await expect(dialog.getByText('No account that is not already a member matches “zz”.')).toBeVisible()
  await dialog.locator('#member-add-username').fill('ad')
  await matches.getByRole('button', { name: 'Choose admin' }).click()
  await expect(dialog.locator('#member-add-username')).toHaveValue('admin')
  await dialog.getByRole('button', { name: 'Add member' }).click()
  await expect(page.getByText('admin added to research')).toBeVisible()
  expect(candidateQueries).toContain('ad')
})

test('A member sees only its own areas, reads its Pool, and cannot act on Servers', async ({ page }) => {
  await installApiFixtures(page, { identity: 'member' })
  await page.goto('/?site=site-a')

  // The Overview needs the admin role, so a member lands on the Servers.
  await expect(page).toHaveURL(/\/servers\?site=site-a/)
  await expect(primaryNavigation(page).getByRole('link')).toHaveText(['Servers', 'Platforms', 'Software', 'Activity', 'Validation', 'OS Images', 'Pools'])
  const actions = page.getByRole('button', { name: /^Actions for gpu-node-01/ }).first()
  await expect(actions).toBeDisabled()
  await expect(actions).toHaveAttribute('title', /Only an owner of the Server’s Pool or an admin/)

  await page.goto('/users?site=site-a')
  await expect(page.getByRole('heading', { name: 'Access denied' })).toBeVisible()

  await page.goto('/pools?site=site-a')
  await expect(page.getByRole('table', { name: 'Pools' }).getByRole('link', { name: 'research', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add a member to research' })).toHaveCount(0)

  await page.goto('/pools/pool-research?site=site-a')
  await expect(page.getByRole('heading', { name: 'research', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add member' })).toHaveCount(0)
  await expect(page.getByRole('combobox', { name: 'Sudo of bob' })).toHaveCount(0)
  await expect(page.getByText('Only the Pool\'s owners and admins can change these.')).toBeVisible()
})

/** Records the paths of admin-only reads a screen must not make for a user who is not an admin. */
function recordAdminReads(page: import('playwright/test').Page) {
  const adminOnly = [
    /^\/api\/v1\/integrations/,
    /\/power-configuration$/,
    /\/boot-media$/,
    /\/virtual-machines$/,
    /^\/api\/v1\/change-plan-policy$/,
    /^\/api\/v1\/validation\/schedules/,
    /^\/api\/v1\/software\/docker-ce\/registry-credentials/,
    /^\/api\/v1\/overview/,
    /^\/api\/v1\/users/,
    /^\/api\/v1\/access\//,
    /^\/api\/v1\/monitoring\/alerts/,
    /^\/api\/v1\/provisioning\/(tasks|boot-isos|integrations)/,
    /^\/api\/v1\/sites\/[^/]+\/local-repository/,
  ]
  const reads: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (adminOnly.some((pattern) => pattern.test(path))) reads.push(path)
  })
  return reads
}

test('An owner operates its Server without the admin-only Power Configuration, Boot Media, or placement', async ({ page }) => {
  const adminReads = recordAdminReads(page)
  await installApiFixtures(page, { identity: 'owner', serverPlacements: { 'srv-1': { poolId: 'pool-research' } } })
  await page.goto('/servers/srv-1/summary?site=site-a')

  await expect(page.getByRole('button', { name: 'Take action' })).toBeEnabled()
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await expect(page.getByRole('menuitem', { name: 'Edit tags' })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: 'Set zone / pool' })).toHaveCount(0)
  await page.keyboard.press('Escape')
  await expect(page.getByRole('heading', { name: 'Boot media' })).toHaveCount(0)

  // The list's power dialog offers the actions without reading the Power Configuration.
  await page.goto('/servers?site=site-a')
  await page.getByRole('button', { name: /^Power actions for gpu-node-01/ }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Power actions' })
  await expect(dialog.getByRole('button', { name: 'Power on' })).toBeVisible()
  await expect(dialog.getByText('Power configuration unavailable')).toHaveCount(0)
  expect(adminReads).toEqual([])
})

test('A member reads its Pool\'s Server without the controls of an owner', async ({ page }) => {
  const detailReads: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.endsWith('/provisioner-detail')) detailReads.push(request.url())
  })
  const adminReads = recordAdminReads(page)
  await installApiFixtures(page, { identity: 'member', serverPlacements: { 'srv-1': { poolId: 'pool-research' } } })
  await page.goto('/servers?site=site-a')

  const row = page.getByRole('row', { name: /gpu-node-01/ }).first()
  await expect(row.getByRole('link', { name: 'gpu-node-01' })).toBeVisible()
  await expect(row.getByRole('button', { name: /^Power actions for/ })).toHaveCount(0)
  await expect(row.getByRole('button', { name: /^(Add|Edit) tags/ })).toHaveCount(0)
  await expect(row.getByRole('button', { name: 'Deploy OS for gpu-node-01' })).toHaveCount(0)

  await page.goto('/servers/srv-1/storage?site=site-a')
  await expect(page.getByText('Live hardware detail is shown to the Pool\'s owners and admins.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toHaveCount(0)
  await page.goto('/servers/srv-1/network?site=site-a')
  await expect(page.getByRole('table', { name: 'Server network interfaces' })).toBeVisible()
  await expect(page.getByRole('button', { name: /^(Configure|Add link to|Unbind) / })).toHaveCount(0)
  expect(detailReads).toEqual([])
  expect(adminReads).toEqual([])
})

test('A user who is not an admin is not offered admin-only settings', async ({ page }) => {
  const adminReads = recordAdminReads(page)
  await installApiFixtures(page, { identity: 'member' })

  await page.goto('/change-plans?site=site-a')
  await expect(page.getByRole('heading', { name: 'Waiting for approval' })).toBeVisible()
  await expect(page.getByText('Approval of confirmed changes')).toHaveCount(0)

  await page.goto('/validation/suites?site=site-a')
  await expect(page.getByRole('heading', { name: 'Server validation' })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Schedules' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'New script suite' })).toHaveCount(0)
  await page.goto('/validation/suites/failover-drill?site=site-a')
  await expect(page.getByRole('heading', { name: 'Schedules' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Create schedule' })).toHaveCount(0)

  await page.goto('/software/docker-ce?site=site-a')
  await expect(page.getByRole('heading', { name: 'Docker CE', level: 1 })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Settings' })).toHaveCount(0)

  await page.goto('/account/ssh-keys?site=site-a')
  await expect(page.getByRole('heading', { name: 'Deployment key' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sync to provisioners' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Regenerate' })).toHaveCount(0)
  await expect(page.getByText('Provisioners', { exact: true })).toHaveCount(0)

  for (const path of ['/validation/schedules', '/software/settings', '/platforms/settings']) {
    await page.goto(`${path}?site=site-a`)
    await expect(page.getByRole('heading', { name: 'Access denied' })).toBeVisible()
  }
  expect(adminReads).toEqual([])
})

/** pool-research holds production-k0s, research-slurm, and the Servers they run on. */
const researchPool = {
  platformPools: { 'platform-a': 'pool-research', 'platform-slurm': 'pool-research' },
  serverPlacements: Object.fromEntries(['srv-1', 'srv-2', 'srv-3', 'srv-4', 'srv-5'].map((id) => [id, { poolId: 'pool-research' }])),
}

test('A member reads its Pool\'s Platforms, plans, Workflows, runs, and software without an owner\'s controls', async ({ page }) => {
  const explorerReads: string[] = []
  page.on('request', (request) => {
    if (/^\/api\/v1\/platforms\/[^/]+\/kubernetes\//.test(new URL(request.url()).pathname)) explorerReads.push(request.url())
  })
  const adminReads = recordAdminReads(page)
  await installApiFixtures(page, { identity: 'member', ...researchPool })

  await page.goto('/platforms/platform-a/overview?site=site-a')
  await expect(page.getByRole('heading', { name: 'production-k0s' })).toBeVisible()
  await expect(page.getByText('Live cluster management is for the Pool’s owners and admins.', { exact: false })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Open cluster' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Sync' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /^Uninstall/ })).toHaveCount(0)
  await expect(page.getByRole('tab', { name: 'Overview' })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Applications' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Run a suite' })).toHaveCount(0)
  await page.goto('/platforms/platform-a/configuration?site=site-a')
  await expect(page.getByRole('region', { name: 'AMD GPU Operator' })).toBeVisible()
  await expect(page.getByRole('region', { name: 'AMD GPU Operator' }).getByRole('button', { name: /^(Configure|Uninstall|Install)$/ })).toHaveCount(0)
  await page.goto('/platforms/platform-a/applications?site=site-a')
  await expect(page.getByRole('tab', { name: 'Applications' })).toHaveCount(0)

  await page.goto('/change-plans?site=site-a')
  const plan = page.getByRole('article', { name: 'Change Plan for Platform research-slurm' })
  await expect(plan.getByText('Only an owner of the Platform’s Pool or an admin can approve or reject this plan.')).toBeVisible()
  await expect(plan.getByRole('button', { name: 'Approve plan' })).toHaveCount(0)

  await page.goto('/workflows/op-running?site=site-a')
  await expect(page.getByRole('table', { name: 'Workflow steps' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Cancel', exact: true })).toHaveCount(0)
  await page.goto('/workflows/op-failed?site=site-a')
  await expect(page.getByRole('table', { name: 'Workflow steps' })).toBeVisible()
  await expect(page.getByRole('table', { name: 'Workflow steps' }).getByRole('button', { name: /^Retry/ })).toHaveCount(0)

  await page.goto('/validation/runs?site=site-a')
  await expect(page.getByRole('heading', { name: 'Validation', level: 1 })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Run a suite' })).toHaveCount(0)

  await page.goto('/software/docker-ce?site=site-a')
  await expect(page.getByRole('heading', { name: 'Docker CE', level: 1 })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Install software' })).toHaveCount(0)
  expect(explorerReads).toEqual([])
  expect(adminReads).toEqual([])
})

test('An owner is offered the controls of its Pool\'s Platforms, plans, and Workflows', async ({ page }) => {
  await installApiFixtures(page, { identity: 'owner', ...researchPool })

  await page.goto('/platforms/platform-a/overview?site=site-a')
  await expect(page.getByRole('button', { name: 'Sync' })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Applications' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Open cluster' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Run a suite' })).toBeVisible()

  await page.goto('/change-plans?site=site-a')
  await expect(page.getByRole('article', { name: 'Change Plan for Platform research-slurm' }).getByRole('button', { name: 'Approve plan' })).toBeVisible()

  await page.goto('/workflows/op-running?site=site-a')
  await expect(page.getByRole('button', { name: 'Cancel', exact: true })).toBeVisible()
  await page.goto('/workflows/op-failed?site=site-a')
  await expect(page.getByRole('table', { name: 'Workflow steps' }).getByRole('button', { name: /^Retry/ })).toBeVisible()

  // A Platform outside the owner's Pool stays read-only.
  await page.goto('/platforms/platform-b/overview?site=site-a')
  await expect(page.getByRole('heading', { name: 'edge-staging' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sync' })).toHaveCount(0)
})

for (const [identity, title] of [['owner', 'An owner'], ['member', 'A member']] as const) {
  test(`${title}'s screens make no admin-only request`, async ({ page }) => {
    test.setTimeout(120_000)
    const adminReads = recordAdminReads(page)
    await installApiFixtures(page, { identity, ...researchPool })
    const routes = [
      '/servers', '/servers/srv-1/summary', '/servers/srv-1/activity', '/servers/srv-1/monitoring', '/servers/srv-1/network',
      '/servers/srv-1/storage', '/platforms', '/platforms/platform-a/overview', '/platforms/platform-a/monitoring',
      '/platforms/platform-a/configuration', '/platforms/platform-a/activity', '/platforms/platform-slurm/overview',
      '/software', '/software/docker-ce', '/workflows', '/workflows/op-running', '/change-plans', '/validation/runs',
      '/validation/suites', '/validation/suites/user-mounts', '/pools', '/pools/pool-research', '/account/ssh-keys',
      '/provisioning/images',
    ]
    // Some of these pages poll or stream, so each is given a moment after its heading renders
    // instead of waiting for the network to go idle.
    for (const route of routes) {
      await page.goto(`${route}?site=site-a`)
      await expect(page.locator('h1').first()).toBeVisible()
      await page.waitForTimeout(500)
    }
    expect(adminReads).toEqual([])
  })
}

test('An owner reads OS Images, refreshes them, and deploys one only to the Servers of its Pool', async ({ page }) => {
  const catalogReads: string[] = []
  await installApiFixtures(page, {
    identity: 'owner',
    readyServerCount: 2,
    untestedCustomImage: true,
    serverPlacements: { 'srv-1': { poolId: 'pool-research' } },
    onOSImageCatalogRequest: (integrationId) => catalogReads.push(integrationId),
  })
  await page.goto('/provisioning/images?site=site-a')

  await expect(page.getByRole('heading', { name: 'OS Images', level: 1 })).toBeVisible()
  await expect(page.getByRole('table', { name: 'OS images' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Manage integrations' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Upload image' })).toHaveCount(0)
  await expect(page.getByRole('checkbox', { name: 'Select all on this page' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /^More actions for / })).toHaveCount(0)
  // The provisioner is named, but its admin-only Infrastructure page is not linked.
  await expect(page.getByRole('table', { name: 'OS images' }).getByText('MAAS Taipei').first()).toBeVisible()
  await expect(page.getByRole('link', { name: 'MAAS Taipei' })).toHaveCount(0)
  expect(catalogReads).toContain('maas-a')

  const reads = catalogReads.length
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect.poll(() => catalogReads.length).toBeGreaterThan(reads)

  // A custom image no test has passed cannot be deployed yet; testing it is an admin's.
  const custom = page.getByRole('row', { name: 'Golden Custom catalog data', exact: true })
  await expect(custom.getByRole('button', { name: /^Deploy Golden Custom: / })).toHaveAttribute('aria-disabled', 'true')
  await expect(custom.getByRole('button', { name: 'Test deployment' })).toHaveCount(0)

  await page.getByRole('button', { name: 'Deploy Ubuntu 22.04 LTS' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Deploy OS' })
  const targets = dialog.getByRole('group', { name: 'Deployment target selection' })
  await expect(targets.getByText('gpu-node-01', { exact: true })).toBeVisible()
  await expect(targets.getByText('gpu-node-02', { exact: true })).toHaveCount(0)
  await expect(dialog.getByText('1 available')).toBeVisible()
})

test('A member reads OS Images without deploying them', async ({ page }) => {
  await installApiFixtures(page, { identity: 'member', readyServerCount: 1, serverPlacements: { 'srv-1': { poolId: 'pool-research' } } })
  await page.goto('/provisioning/images?site=site-a')

  await expect(page.getByRole('table', { name: 'OS images' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Refresh' })).toBeVisible()
  await expect(page.getByRole('button', { name: /^Deploy / })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /^More actions for / })).toHaveCount(0)
})

test('A user who sees no Server still reads every OS Image of the Site', async ({ page }) => {
  const catalogReads: string[] = []
  await installApiFixtures(page, { identity: 'member', fleetSize: 0, onOSImageCatalogRequest: (integrationId) => catalogReads.push(integrationId) })
  await page.goto('/provisioning/images?site=site-a')

  await expect(page.getByRole('row', { name: 'Ubuntu 22.04 LTS catalog data', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /^Deploy / })).toHaveCount(0)
  expect(catalogReads).toContain('maas-a')
})

test('A user who is not an admin is told when no provisioner is connected', async ({ page }) => {
  await installApiFixtures(page, { identity: 'member', noProvisioners: true })
  await page.goto('/provisioning/images?site=site-a')

  await expect(page.getByText('No provisioner connected')).toBeVisible()
  await expect(page.getByText('OS Images come from a provisioner, which an admin connects.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Manage integrations' })).toHaveCount(0)
})

test('A user who is not an admin runs a suite only on the Servers of the Pools it owns', async ({ page }) => {
  await installApiFixtures(page, { identity: 'owner', serverPlacements: { 'srv-3': { poolId: 'pool-research' } } })
  await page.goto('/validation/runs?site=site-a')
  await page.getByRole('button', { name: 'Run a suite' }).click()
  const dialog = page.getByRole('dialog', { name: 'Run a suite' })
  await dialog.getByRole('radio', { name: /Host baseline/ }).click()
  await expect(dialog.getByRole('checkbox', { name: 'gpu-node-03', exact: true })).toBeEnabled()
  await expect(dialog.getByRole('checkbox', { name: 'gpu-node-01 · Pool owners only' })).toBeDisabled()
})

test('A user changes its own password and learns when the current one is wrong', async ({ page }) => {
  let sent: Record<string, unknown> | undefined
  await installApiFixtures(page, { identity: 'member' })
  page.on('request', (request) => {
    if (request.method() === 'PUT' && new URL(request.url()).pathname === '/api/v1/auth/me/password') sent = request.postDataJSON() as Record<string, unknown>
  })
  await page.goto('/servers?site=site-a')
  await page.getByRole('button', { name: 'Account menu' }).click()
  await page.getByRole('menuitem', { name: 'Change password' }).click()
  await expect(page).toHaveURL(/\/account\/password/)

  await page.locator('#password-current').fill('wrong-password')
  await page.locator('#password-new').fill('a-new-password')
  await page.locator('#password-confirm').fill('a-new-passwor')
  await expect(page.getByText('The passwords do not match.')).toBeVisible()
  await page.locator('#password-confirm').fill('a-new-password')
  await page.getByRole('button', { name: 'Change password' }).click()
  await expect(page.getByText('The current password is wrong.')).toBeVisible()

  await page.locator('#password-current').fill('old-password')
  await page.getByRole('button', { name: 'Change password' }).click()
  await expect(page.getByText('Password changed')).toBeVisible()
  expect(sent).toEqual({ currentPassword: 'old-password', newPassword: 'a-new-password' })
  await expect(page.locator('#password-new')).toHaveValue('')
})
