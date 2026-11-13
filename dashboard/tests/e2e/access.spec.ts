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

test('An owner adds a member, overrides its sudo, and sees the effective values from the API', async ({ page }) => {
  await installApiFixtures(page, { identity: 'owner' })
  await page.goto('/pools?site=site-a')

  await expect(page.getByRole('table', { name: 'Pools' }).getByRole('row', { name: /research/ })).toContainText('Owner')
  await page.getByRole('link', { name: 'Members of research' }).first().click()
  await expect(page.getByRole('heading', { name: 'research', exact: true })).toBeVisible()

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

test('A member sees only its own areas, reads its Pool, and cannot act on Servers', async ({ page }) => {
  await installApiFixtures(page, { identity: 'member' })
  await page.goto('/?site=site-a')

  // The Overview needs the admin role, so a member lands on the Servers.
  await expect(page).toHaveURL(/\/servers\?site=site-a/)
  await expect(primaryNavigation(page).getByRole('link')).toHaveText(['Servers', 'Platforms', 'Software', 'Activity', 'Validation', 'Pools'])
  const actions = page.getByRole('button', { name: /^Actions for gpu-node-01/ }).first()
  await expect(actions).toBeDisabled()
  await expect(actions).toHaveAttribute('title', /Only an owner of the Server’s Pool or an admin/)

  await page.goto('/users?site=site-a')
  await expect(page.getByRole('heading', { name: 'Access denied' })).toBeVisible()

  await page.goto('/pools/pool-research?site=site-a')
  await expect(page.getByRole('heading', { name: 'research', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add member' })).toHaveCount(0)
  await expect(page.getByRole('combobox', { name: 'Sudo of bob' })).toHaveCount(0)
  await expect(page.getByText('Only the Pool\'s owners and admins can change these.')).toBeVisible()
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
