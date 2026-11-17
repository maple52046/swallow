import { expect, test, type Page } from 'playwright/test'
import { installApiFixtures, type FixtureOptions } from './fixtures'

// User-defined Validation Suites shared between Users (validation.md, Suites and Start): any User
// writes one, only its author edits it, the author or an admin retires it, it runs as the starter's
// account, and a changed suite is reviewed before it runs. Identities are the access fixtures':
// the admin, alice (owner of pool-research and author of "Data mounts"), and bob (its member and
// author of "Kernel modules").

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Date.now = () => Date.parse('2026-08-27T03:05:00Z')
    if (!localStorage.getItem('swallow.appearance')) localStorage.setItem('swallow.appearance', 'light')
  })
})

/** Every fixture Server is in pool-research, so alice owns them and bob is a member of their Pool. */
const researchPool: FixtureOptions = {
  serverPlacements: Object.fromEntries(['srv-1', 'srv-2', 'srv-3', 'srv-4', 'srv-5'].map((id) => [id, { poolId: 'pool-research' }])),
}

/** Collects the JSON bodies the page sends with `method` to `path`. */
function recordBodies(page: Page, method: string, path: string): unknown[] {
  const bodies: unknown[] = []
  page.on('request', (request) => {
    if (request.method() === method && new URL(request.url()).pathname === path) bodies.push(request.postDataJSON())
  })
  return bodies
}

/** Opens the run dialog from a suite's page and chooses one Server. */
async function openRunOn(page: Page, suiteId: string, server: string) {
  await page.goto(`/validation/suites/${suiteId}?site=site-a`)
  await page.getByRole('button', { name: 'Run suite' }).click()
  const dialog = page.getByRole('dialog', { name: 'Run a suite' })
  await dialog.getByRole('checkbox', { name: server, exact: true }).locator('..').click()
  await dialog.getByRole('button', { name: 'Continue' }).click()
  return dialog
}

test('A member writes a script suite that runs with sudo and that members may start', async ({ page }) => {
  const created = recordBodies(page, 'POST', '/api/v1/validation/suites')
  await installApiFixtures(page, { identity: 'member', ...researchPool })
  await page.goto('/validation/suites?site=site-a')
  await page.getByRole('button', { name: 'New script suite' }).click()
  const editor = page.getByRole('dialog', { name: 'New script suite' })
  await expect(editor).toContainText('everyone who can read the run reads')
  await editor.getByLabel('Name').fill('GPU visible')
  await editor.getByRole('textbox', { name: 'Script', exact: true }).fill('#!/bin/sh\nrocm-smi\n')
  await editor.getByRole('checkbox', { name: 'Run with sudo' }).locator('..').click()
  await expect(editor).toContainText('only on targets where the Sudo Policy gives them passwordless sudo')
  await editor.getByRole('radiogroup', { name: 'Who can start' }).getByText('Pool members too').click()
  await editor.getByRole('button', { name: 'Create suite' }).click()

  await expect(page.getByRole('link', { name: /GPU visible/ })).toContainText('Created by bob (you)')
  expect(created).toEqual([{
    name: 'GPU visible', description: '', script: '#!/bin/sh\nrocm-smi\n', sudo: true, starterLevel: 'member', timeoutSeconds: 600, impact: 'workload',
  }])
})

test('Only the author edits a script suite; the author or an admin retires it', async ({ page }) => {
  await installApiFixtures(page, { identity: 'member', ...researchPool })
  // bob reads alice's suite but may neither edit nor retire it.
  await page.goto('/validation/suites/user-mounts?site=site-a')
  await expect(page.getByText('Created by alice', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Edit' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Retire' })).toHaveCount(0)
  // bob's own suite.
  await page.goto('/validation/suites/user-kernel-modules?site=site-a')
  await expect(page.getByText('Created by bob (you)')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Edit' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Retire' })).toBeVisible()
  // A module suite is read-only for everyone.
  await page.goto('/validation/suites/host-baseline?site=site-a')
  await expect(page.getByRole('heading', { name: 'Host baseline', level: 1 })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Edit' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Retire' })).toHaveCount(0)
})

test('An admin who is not the author retires but does not edit a script suite', async ({ page }) => {
  await installApiFixtures(page, researchPool)
  await page.goto('/validation/suites/user-kernel-modules?site=site-a')
  await expect(page.getByRole('heading', { name: 'Kernel modules', level: 1 })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Edit' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Retire' }).click()
  const confirm = page.getByRole('alertdialog', { name: 'Retire suite' })
  await expect(confirm).toContainText('You are retiring a suite written by bob.')
  await confirm.getByRole('button', { name: 'Retire suite' }).click()
  await expect(page.getByText('Retired', { exact: true })).toBeVisible()

  // A suite whose author was deleted says so, and nobody edits it.
  await page.goto('/validation/suites/user-legacy-disk?site=site-a')
  await expect(page.getByText('Created by carol (author deleted)')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Edit' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Retire' })).toBeVisible()
})

test('Hiding other users\' suites keeps module suites and survives a reload', async ({ page }) => {
  await installApiFixtures(page, { identity: 'owner', ...researchPool })
  await page.goto('/validation/suites?site=site-a')
  const hide = page.getByRole('checkbox', { name: /^Hide other users’ suites/ })
  await expect(hide).not.toBeChecked()
  await expect(page.getByRole('link', { name: /Kernel modules/ })).toBeVisible()
  await hide.locator('..').click()
  await expect(page.getByRole('link', { name: /Kernel modules/ })).toHaveCount(0)
  await expect(page.getByRole('link', { name: /Legacy disk check/ })).toHaveCount(0)
  await expect(page.getByRole('link', { name: /Data mounts/ })).toBeVisible()
  await expect(page.getByRole('link', { name: /Host baseline/ })).toBeVisible()
  // Like the experimental switch's count, the label keeps only the number: bob's and carol's suites.
  await expect(hide).toHaveAccessibleName('Hide other users’ suites (2)')

  // The two switches share one left-aligned column, so their toggles' left edges line up. The
  // control has no role of its own; Ark UI marks it with data-part="control" inside the switch.
  const toggleOf = (name: RegExp) => page.getByRole('checkbox', { name }).locator('..').locator('[data-part="control"]')
  const experimentalBox = await toggleOf(/^Show experimental suites/).boundingBox()
  const hideBox = await toggleOf(/^Hide other users’ suites/).boundingBox()
  expect(experimentalBox).not.toBeNull()
  expect(hideBox).not.toBeNull()
  expect(Math.abs((experimentalBox?.x ?? 0) - (hideBox?.x ?? 0))).toBeLessThanOrEqual(1)

  await page.reload()
  await expect(page.getByRole('checkbox', { name: /^Hide other users’ suites/ })).toBeChecked()
  await expect(page.getByRole('link', { name: /Data mounts/ })).toBeVisible()
  await expect(page.getByRole('link', { name: /Kernel modules/ })).toHaveCount(0)
})

test('The author saves a revision at the revision the editor read', async ({ page }) => {
  const replaced = recordBodies(page, 'PUT', '/api/v1/validation/suites/user-mounts')
  await installApiFixtures(page, { identity: 'owner', ...researchPool })
  await page.goto('/validation/suites/user-mounts?site=site-a')
  await page.getByRole('button', { name: 'Edit' }).click()
  const editor = page.getByRole('dialog', { name: 'Edit Data mounts' })
  await editor.getByLabel('Timeout (seconds)').fill('900')
  await editor.getByRole('button', { name: 'Save revision' }).click()
  await expect(editor).toBeHidden()
  expect(replaced).toEqual([{
    name: 'Data mounts', description: 'Checks /data is mounted.', script: '#!/bin/sh\nmountpoint -q /data\nmountpoint -q /scratch\n',
    sudo: false, starterLevel: 'member', timeoutSeconds: 900, impact: 'workload', revision: 3,
  }])
})

test('A save refused because the suite changed offers a reload, never an overwrite', async ({ page }) => {
  const replaced = recordBodies(page, 'PUT', '/api/v1/validation/suites/user-mounts')
  await installApiFixtures(page, { identity: 'owner', validationConcurrentSave: 'user-mounts', ...researchPool })
  await page.goto('/validation/suites/user-mounts?site=site-a')
  await page.getByRole('button', { name: 'Edit' }).click()
  const editor = page.getByRole('dialog', { name: 'Edit Data mounts' })
  await editor.getByLabel('Description').fill('Checks /data and /scratch.')
  await editor.getByRole('button', { name: 'Save revision' }).click()
  await expect(editor).toContainText('The suite changed since you opened it')
  await expect(editor).toContainText('Your change was not saved and nothing was overwritten.')
  await expect(editor.getByRole('button', { name: 'Save revision' })).toHaveCount(0)
  await editor.getByRole('button', { name: 'Reload the suite' }).click()

  await expect(editor).toBeHidden()
  await expect(page.getByText('Checks /data is mounted.').first()).toBeVisible()
  await expect(page.getByText('Script, revision 4')).toBeVisible()
  expect(replaced).toHaveLength(1)
})

test('A suite changed since the User\'s last run is reviewed with a diff before it runs', async ({ page }) => {
  const started = recordBodies(page, 'POST', '/api/v1/validation/runs')
  await installApiFixtures(page, { identity: 'owner', ...researchPool })
  const dialog = await openRunOn(page, 'user-mounts', 'gpu-node-03')

  const review = dialog.getByRole('region', { name: 'Suite changes' })
  await expect(review.getByRole('heading', { name: 'Changed since your last run: r2 → r3' })).toBeVisible()
  await expect(review).toContainText('Who can start: Pool owners → Pool members too')
  await expect(review).toContainText('Timeout: 300 → 600 seconds')
  const diff = review.getByRole('group', { name: 'Script changes' })
  await expect(diff).toContainText('1 line added and 0 removed from revision 2 to revision 3.')
  await expect(diff).toContainText('Added: mountpoint -q /scratch')
  await expect(dialog.getByRole('button', { name: 'Start run' })).toHaveCount(0)

  await dialog.getByRole('button', { name: 'Use the new version' }).click()
  await expect(dialog).toContainText('Your account alice')
  await expect(dialog.getByLabel('Script of Data mounts, revision 3')).toContainText('mountpoint -q /scratch')
  await expect(dialog).toContainText('Everyone who can read a run reads its output')
  await dialog.getByRole('button', { name: 'Start run' }).click()

  await expect(page).toHaveURL(/\/validation\/runs\/run-6\?site=site-a$/)
  expect(started).toEqual([{ suiteId: 'user-mounts', serverIds: ['srv-3'], confirm: false, suiteRevision: 3 }])
  await expect(page.getByText('alice (uid 10001)')).toBeVisible()
})

test('A review between revisions with the same script and settings says they changed and changed back', async ({ page }) => {
  await installApiFixtures(page, { identity: 'owner', ...researchPool })
  // Here revision 2 (alice's last run) holds what revision 3 holds again.
  await page.route('**/api/v1/validation/suites/user-mounts/revisions/2', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({
      suiteId: 'user-mounts', revision: 2, script: '#!/bin/sh\nmountpoint -q /data\nmountpoint -q /scratch\n', scriptSha256: 'ab13',
      sudo: false, starterLevel: 'member', timeoutSeconds: 600, impact: 'workload', createdAt: '2026-08-21T00:00:00Z',
    }),
  }))
  const dialog = await openRunOn(page, 'user-mounts', 'gpu-node-03')
  const review = dialog.getByRole('region', { name: 'Suite changes' })
  await expect(review.getByRole('heading', { name: 'Changed since your last run: r2 → r3' })).toBeVisible()
  await expect(review).toContainText(
    'Revision 2 and revision 3 run the same script with the same settings: the revisions in between changed them and changed them back.',
  )
  await expect(review).not.toContainText('only the script did')
  await expect(review.getByRole('group', { name: 'Script changes' })).toContainText('No line changed between revision 2 and revision 3.')
  await dialog.getByRole('button', { name: 'Use the new version' }).click()
  await expect(dialog.getByRole('button', { name: 'Start run' })).toBeEnabled()
})

test('A sudo suite\'s run dialog states passwordless sudo as a requirement, not a promise', async ({ page }) => {
  await installApiFixtures(page, { identity: 'owner', ...researchPool })
  const dialog = await openRunOn(page, 'user-kernel-modules', 'gpu-node-03')
  await expect(dialog).toContainText(
    'Your account alice, through sudo: this needs your passwordless sudo on every target, under the Pool’s Sudo Policy',
  )
  await expect(dialog).not.toContainText('through your passwordless sudo')
})

test('A revision that was never kept is reviewed through the whole current script', async ({ page }) => {
  await installApiFixtures(page, { identity: 'member', ...researchPool })
  const dialog = await openRunOn(page, 'user-mounts', 'gpu-node-04')
  const review = dialog.getByRole('region', { name: 'Suite changes' })
  await expect(review.getByRole('heading', { name: 'Changed since your last run: r1 → r3' })).toBeVisible()
  await expect(review).toContainText('The earlier revision 1 is not available')
  await expect(review.getByLabel('Script of revision 3')).toContainText('mountpoint -q /scratch')
  await dialog.getByRole('button', { name: 'Use the new version' }).click()
  await expect(dialog).toContainText('Your account bob')
  await expect(dialog.getByRole('button', { name: 'Start run' })).toBeEnabled()
})

test('A start refused because the suite changed while the dialog was open opens the same review', async ({ page }) => {
  const started = recordBodies(page, 'POST', '/api/v1/validation/runs')
  await installApiFixtures(page, { identity: 'owner', validationConcurrentSave: 'user-mounts', ...researchPool })
  const dialog = await openRunOn(page, 'user-mounts', 'gpu-node-03')
  await dialog.getByRole('button', { name: 'Use the new version' }).click()
  await dialog.getByRole('button', { name: 'Start run' }).click()

  const review = dialog.getByRole('region', { name: 'Suite changes' })
  await expect(review.getByRole('heading', { name: 'Changed while you were reviewing it: r3 → r4' })).toBeVisible()
  await expect(review.getByRole('group', { name: 'Script changes' })).toContainText('Added: echo checked by revision 4')
  // The suite page behind the dialog reads the suite again and shows the new revision.
  await expect(page.getByText('Script, revision 4')).toBeVisible()
  await expect(page.getByText('Script, revision 3')).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Use the new version' }).click()
  await dialog.getByRole('button', { name: 'Start run' }).click()

  await expect(page).toHaveURL(/\/validation\/runs\/run-6\?site=site-a$/)
  expect(started.map((body) => (body as { suiteRevision: number }).suiteRevision)).toEqual([3, 4])
})

test('A member starts member-level suites only and cancels only its own runs', async ({ page }) => {
  await installApiFixtures(page, { identity: 'member', ...researchPool })
  await page.goto('/validation/runs?site=site-a')
  await page.getByRole('button', { name: 'Run a suite' }).click()
  const dialog = page.getByRole('dialog', { name: 'Run a suite' })
  const hostBaseline = dialog.getByRole('radio', { name: /Host baseline/ })
  await expect(hostBaseline).toBeDisabled()
  await expect(hostBaseline).toContainText('Only a Pool owner can start this suite, and you own no Pool.')
  await expect(dialog.getByRole('radio', { name: /Kernel modules/ })).toBeDisabled()
  await dialog.getByRole('radio', { name: /Data mounts/ }).click()
  await expect(dialog.getByRole('checkbox', { name: 'gpu-node-03', exact: true })).toBeEnabled()
  await dialog.getByRole('button', { name: 'Cancel' }).click()

  await page.goto('/validation/suites/host-baseline?site=site-a')
  await expect(page.getByRole('button', { name: 'Run suite' })).toBeDisabled()
  await expect(page.getByText('Only a Pool owner can start this suite, and you own no Pool.')).toBeVisible()

  await page.goto('/validation/runs/run-running?site=site-a')
  await expect(page.getByText('admin (uid 10000)')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Cancel run' })).toHaveCount(0)
  await page.goto('/validation/runs/run-bob-mounts?site=site-b')
  await expect(page.getByText('bob (uid 10002)')).toBeVisible()
  await page.getByRole('button', { name: 'Cancel run' }).click()
  await page.getByRole('alertdialog', { name: 'Cancel Validation Run' }).getByRole('button', { name: 'Cancel run' }).click()
  await expect(page.getByText('Cancel requested')).toBeVisible()
})
