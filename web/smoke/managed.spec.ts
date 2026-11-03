import { expect, test } from '@playwright/test'

// The managed done line (spec 0006, AC-19): sign in as the admin through the bundled identity
// provider, deploy the sample from the editor after the review, and watch every module reach
// running. task smoke:managed installs the platform first and passes the admin's credentials.
const user = process.env.SMOKE_ADMIN_USER ?? 'admin'
const password = process.env.SMOKE_ADMIN_PASSWORD ?? ''

test('install, sign in, deploy the sample, see it running', async ({ page }) => {
  test.setTimeout(8 * 60_000)
  expect(password, 'SMOKE_ADMIN_PASSWORD').not.toBe('')

  await page.goto('/')
  await page.getByRole('link', { name: 'Sign in' }).click()
  await page.locator('#username-textfield').fill(user)
  await page.locator('#password-textfield').fill(password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()

  await page.getByRole('link', { name: 'Sample' }).click()
  await expect(page.getByTestId('node-whoami')).toBeVisible()
  await page.getByRole('button', { name: 'Deploy' }).click()
  const review = page.getByRole('dialog', { name: 'Deploy whoami-postgres' })
  await expect(review.getByText('traefik/whoami:v1.12.0')).toBeVisible()
  await review.getByRole('button', { name: 'Deploy' }).click()

  await expect(page).toHaveURL(/\/deployments\/whoami-postgres$/)
  for (const instance of ['postgres', 'whoami']) {
    await expect(page.getByTestId(`badge-${instance}`).first()).toHaveText(/^running/, { timeout: 5 * 60_000 })
  }
  await expect(page.getByRole('heading', { name: 'whoami-postgres' })).toBeVisible()
})
