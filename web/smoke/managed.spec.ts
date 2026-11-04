import { expect, type Page, test } from '@playwright/test'

// The managed done line (spec 0006, AC-19), in two parts that scripts/smoke-managed.sh runs
// with a Docker restart between them (AC-12): first sign in as the admin through the bundled
// identity provider, download the sample's zip (AC-20), deploy it after the review and watch
// every module reach running; then, after the restart, see it running again without an apply
// and remove it with Delete data too. task smoke:managed installs the platform first and passes
// the admin's credentials.
const user = process.env.SMOKE_ADMIN_USER ?? 'admin'
const password = process.env.SMOKE_ADMIN_PASSWORD ?? ''
const deployment = 'whoami-postgres'
const instances = ['postgres', 'whoami']

async function signIn(page: Page) {
  expect(password, 'SMOKE_ADMIN_PASSWORD').not.toBe('')
  await page.goto('/')
  await page.getByRole('link', { name: 'Sign in' }).click()
  await page.locator('#username-textfield').fill(user)
  await page.locator('#password-textfield').fill(password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
}

async function expectRunning(page: Page, timeout: number) {
  for (const instance of instances) {
    await expect(page.getByTestId(`badge-${instance}`).first()).toHaveText(/^running/, { timeout })
  }
}

test('install, sign in, deploy the sample, see it running @deploy', async ({ page }) => {
  test.setTimeout(8 * 60_000)
  await signIn(page)

  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Sample' }).click()
  await expect(page.getByTestId('node-whoami')).toBeVisible()

  // Download zip still works in managed mode, through api (AC-20)
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Download zip' }).click()
  const zip = await download
  expect(zip.suggestedFilename()).toBe(`${deployment}.zip`)
  await zip.saveAs(process.env.SMOKE_ZIP ?? `results/${deployment}-managed.zip`)

  await page.getByRole('button', { name: 'Deploy' }).click()
  const review = page.getByRole('dialog', { name: `Deploy ${deployment}` })
  await expect(review.getByText('traefik/whoami:v1.12.0')).toBeVisible()
  await review.getByRole('button', { name: 'Deploy' }).click()

  await expect(page).toHaveURL(new RegExp(`/deployments/${deployment}$`))
  await expectRunning(page, 5 * 60_000)
  await expect(page.getByRole('heading', { name: deployment })).toBeVisible()
})

test('after a Docker restart it runs again without an apply, then remove it with its data @after-restart', async ({ page }) => {
  test.setTimeout(8 * 60_000)
  await signIn(page)

  await page.goto(`/deployments/${deployment}`)
  await expectRunning(page, 5 * 60_000)
  // the history still holds the one apply: nothing applied the stack again
  const history = page.getByRole('region', { name: 'History' }).getByRole('listitem')
  await expect(history).toHaveCount(1)
  await expect(history.first()).toContainText('apply')

  await page.getByRole('button', { name: 'Remove', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: `Remove ${deployment}` })
  await dialog.getByLabel('Delete data too').check()
  await dialog.getByLabel(/to confirm/).fill(deployment)
  await dialog.getByRole('button', { name: 'Remove and delete data' }).click()
  await expect(page.getByTestId('deployment-state')).toHaveText('removed', { timeout: 5 * 60_000 })
})
