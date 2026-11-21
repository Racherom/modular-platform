import { cpSync, readFileSync } from 'node:fs'
import { basename, dirname } from 'node:path'
import { expect, type Page, test } from '@playwright/test'

// The managed done line (spec 0006, AC-19), in three parts that scripts/smoke-managed.sh runs
// one by one, with a Docker restart before the last (AC-12). @setup is the real first admin path
// of spec 0037 (AC-17): the code from the file, the setup page, Pocket ID's first admin page with
// a virtual passkey, and the first sign in that binds the admin. It saves the browser state, and
// @deploy and @after-restart load it: a virtual authenticator lives in the context that made the
// passkey, so a fresh context could not sign in again. @deploy downloads the sample's zip (AC-20),
// deploys it after the review and watches every module reach running; @after-restart sees it
// running again without an apply and removes it with Delete data too. task smoke:managed installs
// the platform first and passes the files and addresses below.
const stack = 'whoami-postgres'
const instances = ['postgres', 'whoami']
const stateFile = process.env.SMOKE_STATE_FILE ?? 'results/managed-state.json'

async function expectSignedIn(page: Page) {
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible({ timeout: 30_000 })
}

// signIn reuses the saved state. When the session of api is gone (after a restart of Docker, say)
// the sign in goes through Pocket ID, whose own cookie is in the state too, so no passkey is needed.
async function signIn(page: Page) {
  await page.goto('/')
  const signOut = page.getByRole('button', { name: 'Sign out' })
  const signInLink = page.getByRole('link', { name: 'Sign in' })
  await expect(signOut.or(signInLink)).toBeVisible()
  if (await signOut.isVisible()) return
  await signInLink.click()
  await finishSignIn(page)
}

// finishSignIn follows the sign in until the editor shows, pressing the consent button when
// Pocket ID shows one (the soft gate on skipConsent of spec 0037, AC-17: the log of the run says
// whether the screen came).
async function finishSignIn(page: Page) {
  const signOut = page.getByRole('button', { name: 'Sign out' })
  const consent = page.getByRole('button', { name: /^(sign in|authorize|continue)$/i })
  for (let i = 0; i < 4 && !(await signOut.isVisible()); i++) {
    await expect(signOut.or(consent)).toBeVisible({ timeout: 30_000 })
    if (await signOut.isVisible()) break
    console.log(`consent screen: pressing "${await consent.first().textContent()}" at ${new URL(page.url()).host}`)
    await consent.first().click()
  }
  await expectSignedIn(page)
}

async function expectRunning(page: Page, timeout: number) {
  for (const instance of instances) {
    await expect(page.getByTestId(`badge-${instance}`).first()).toHaveText(/^running/, { timeout })
  }
}

// The auth host of the platform: https://auth.<domain>, the host of the issuer.
function authOrigin(page: Page) {
  const url = new URL(page.url())
  return `${url.protocol}//auth.${url.host}`
}

test('first admin: code, passkey, bind @setup', async ({ page, context }) => {
  test.setTimeout(3 * 60_000)
  const codeFile = process.env.SMOKE_SETUP_CODE_FILE ?? ''
  expect(codeFile, 'SMOKE_SETUP_CODE_FILE').not.toBe('')
  const code = readFileSync(codeFile, 'utf8').trim()

  // a virtual authenticator with a resident key and user verification, on this page
  const cdp = await context.newCDPSession(page)
  await cdp.send('WebAuthn.enable')
  await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: {
      protocol: 'ctap2',
      transport: 'internal',
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
      automaticPresenceSimulation: true,
    },
  })

  // with no setup session the editor sends every page to /setup, and the API is closed
  await page.goto('/')
  await expect(page).toHaveURL(/\/setup$/)
  await page.getByRole('textbox', { name: 'Setup code' }).fill(code)
  await page.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByRole('heading', { name: '1. Create your admin account' })).toBeVisible({ timeout: 2 * 60_000 })
  const origin = new URL(page.url()).origin
  const auth = authOrigin(page)

  // the code is single use: a second browser has no session and gets 401 code_refused
  const other = await context.browser()?.newContext({ ignoreHTTPSErrors: true })
  if (other) {
    const response = await other.request.post(`${origin}/api/v1/setup/code`, { data: { code }, headers: { Origin: origin } })
    expect(response.status()).toBe(401)
    await other.close()
  }

  // the handoff leaves for Pocket ID's first admin page
  await page.getByRole('button', { name: 'Create your admin account' }).click()
  await expect(page).toHaveURL(new RegExp(`^${auth.replace(/\./g, '\\.')}/(setup|signup/setup)`), { timeout: 30_000 })
  const gate = (await context.cookies(auth)).find((c) => c.name === '__Host-mp_gate')
  expect(gate, 'the gate cookie of the auth host').toBeTruthy()

  // the account and its passkey, on Pocket ID's own page
  await page.getByLabel(/first name/i).fill('Smoke')
  await page.getByLabel(/last name/i).fill('Admin')
  await page.getByLabel(/username/i).fill('smoke-admin')
  await page.getByLabel(/email/i).fill('admin@example.com')
  await page.getByRole('button', { name: /sign up|create/i }).click()
  await expect(page).not.toHaveURL(/\/(signup\/)?setup/, { timeout: 30_000 })

  // keep the setup files for the log check, which runs after the bind deleted them
  const copy = process.env.SMOKE_SETUP_COPY
  if (copy) cpSync(dirname(codeFile), copy, { recursive: true })

  // Pocket ID has no return address: go back to the setup page, which says to sign in
  await page.goto(`${origin}/setup`)
  await page.getByRole('link', { name: 'Sign in to finish the setup' }).first().click()
  await finishSignIn(page)
  expect(basename(page.url())).not.toBe('setup')

  // the old gate cookie opens nothing now that an admin is bound
  const replay = await context.request.post(`${auth}/api/signup/setup`, {
    headers: { Cookie: `${gate?.name}=${gate?.value}` },
    data: { username: 'second', email: 'second@example.com', firstName: 'Second', lastName: 'Admin' },
  })
  expect(replay.status()).toBe(403)

  await context.storageState({ path: stateFile })
})

test.describe('with the saved admin session', () => {
  test.use({ storageState: stateFile })

  test('deploy the sample and see it running @deploy', async ({ page }) => {
    test.setTimeout(8 * 60_000)
    await signIn(page)

    await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Sample' }).click()
    await expect(page.getByTestId('node-whoami')).toBeVisible()

    // Download zip still works in managed mode, through api (AC-20)
    const download = page.waitForEvent('download')
    await page.getByRole('button', { name: 'Download zip' }).click()
    const zip = await download
    expect(zip.suggestedFilename()).toBe(`${stack}.zip`)
    await zip.saveAs(process.env.SMOKE_ZIP ?? `results/${stack}-managed.zip`)

    await page.getByRole('button', { name: 'Deploy', exact: true }).click()
    const review = page.getByRole('dialog', { name: `Deploy ${stack}` })
    // spec 0031: each image row shows its reference, a new one frozen at review time
    const images = review.getByRole('list', { name: 'Images' })
    await expect(images.getByText('traefik/whoami:v1.12.0', { exact: true })).toBeVisible()
    await expect(images.getByTestId('image-traefik/whoami:v1.12.0').getByText('new', { exact: true })).toBeVisible()
    // the full @sha256: digests wrap inside the dialog instead of running past its right edge
    expect(await review.evaluate((d) => d.scrollWidth - d.clientWidth)).toBe(0)
    await review.getByRole('button', { name: 'Deploy', exact: true }).click()

    await expect(page).toHaveURL(new RegExp(`/stacks/${stack}$`))
    await expectRunning(page, 5 * 60_000)
    await expect(page.getByRole('heading', { name: stack })).toBeVisible()
    // the running stack is shown, not edited: no Add on this view (spec 0029, AC-1)
    await expect(page.getByRole('button', { name: /^Add / })).toHaveCount(0)
  })

  test('after a Docker restart it runs again without an apply, then remove it with its data @after-restart', async ({ page }) => {
    test.setTimeout(8 * 60_000)
    await signIn(page)

    await page.goto(`/stacks/${stack}`)
    await expectRunning(page, 5 * 60_000)
    // the history still holds the one apply: nothing applied the stack again
    const history = page.getByRole('region', { name: 'History' }).getByRole('listitem')
    await expect(history).toHaveCount(1)
    await expect(history.first()).toContainText('apply')

    await page.getByRole('button', { name: 'Remove', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: `Remove ${stack}` })
    await dialog.getByLabel('Delete data too').check()
    await dialog.getByLabel(/to confirm/).fill(stack)
    await dialog.getByRole('button', { name: 'Remove and delete data' }).click()
    await expect(page.getByTestId('stack-state')).toHaveText('removed', { timeout: 5 * 60_000 })
  })
})
