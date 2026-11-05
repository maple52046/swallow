import { expect, test, type Locator, type Page } from 'playwright/test'
import { installApiFixtures } from './fixtures'

// A Kubernetes Platform last deployed before swallow split its cluster credential still uses one
// cluster-admin token for reads and writes (platforms-kubernetes.md "Cluster Credentials"). Its
// Overview says so from the Integration's `hasOperatorCredential`, and says nothing for a Platform
// with both credentials, without an Integration, or of another type.

const NOTICE_TITLE = 'Cluster credentials are not separated yet'

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('swallow.appearance', 'light')
    localStorage.setItem('access_token', 'e2e-token')
  })
})

/** The Overview's Cluster access section, which the Kubernetes module contributes. */
function clusterAccess(page: Page): Locator {
  return page.locator('section').filter({ has: page.getByRole('heading', { name: 'Cluster access' }) })
}

/**
 * Opens a Platform's Overview and waits until the Cluster access section has rendered its
 * Integration read, so an absent notice means "not applicable" rather than "still loading".
 */
async function openOverviewAfterIntegrationRead(page: Page, platformId: string, integrationId: string) {
  const read = page.waitForResponse((response) => new URL(response.url()).pathname === `/api/v1/integrations/${integrationId}`)
  await page.goto(`/platforms/${platformId}/overview?site=site-a`)
  await read
  // The response arrives before React renders the state it sets; two frames later it has.
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))))
}

test('a Kubernetes Platform with its single cluster-admin token says a successful deployment narrows it', async ({ page }) => {
  await installApiFixtures(page, { singleClusterCredential: true })
  await page.goto('/platforms/platform-a/overview?site=site-a')

  const access = clusterAccess(page)
  await expect(access.getByText(NOTICE_TITLE)).toBeVisible()
  await expect(access).toContainText('deployed before Swallow separated read and write cluster credentials')
  await expect(access).toContainText('one cluster-admin token for both')
  await expect(access).toContainText('The next deployment Workflow on this Platform that succeeds narrows that token to read-only')
  await expect(access).not.toContainText('The latest deployment failed')
  await expect(access.getByRole('link', { name: 'Open cluster' })).toBeVisible()
})

test('a failed latest deployment adds that changes may be refused until a deployment succeeds', async ({ page }) => {
  await installApiFixtures(page, { singleClusterCredential: true, platformLifecycleStates: { 'platform-a': 'deploy_failed' } })
  await page.goto('/platforms/platform-a/overview?site=site-a')

  const access = clusterAccess(page)
  await expect(access.getByText(NOTICE_TITLE)).toBeVisible()
  await expect(access).toContainText('If it failed after narrowing the token, cluster changes are refused until a deployment succeeds')
})

test('no notice for a Platform with both credentials, without an Integration, or of another type', async ({ page }) => {
  await installApiFixtures(page, { slurmDeployed: true })

  await openOverviewAfterIntegrationRead(page, 'platform-a', 'k8s-a')
  await expect(clusterAccess(page).getByRole('link', { name: 'Open cluster' })).toBeVisible()
  // The endpoint is the one the deployment recorded on the Integration.
  await expect(clusterAccess(page)).toContainText('https://192.168.40.200:6443')
  await expect(page.getByText(NOTICE_TITLE)).toHaveCount(0)

  // edge-staging's deployment never recorded an Integration, so there is nothing to read.
  await page.goto('/platforms/platform-b/overview?site=site-a')
  await expect(page.getByRole('heading', { name: 'Cluster access' })).toBeVisible()
  await expect(page.getByText(NOTICE_TITLE)).toHaveCount(0)

  // lab-slurm-ha's Integration has no operator credential either, but the notice is Kubernetes-only.
  await page.goto('/platforms/platform-slurm-ha/overview?site=site-a')
  await expect(page.getByRole('heading', { name: 'Submission access' })).toBeVisible()
  await expect(page.getByText(NOTICE_TITLE)).toHaveCount(0)
})
