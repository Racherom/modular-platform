import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { expect, type Page, test } from '@playwright/test'

// SMOKE_BASE is '' for the site root or '/platform' for compose.subpath.yaml.
const base = process.env.SMOKE_BASE ?? ''
const sample = `${base}/samples/whoami-postgres`

function recordRequests(page: Page): string[] {
  const paths: string[] = []
  page.on('request', (req) => paths.push(new URL(req.url()).pathname))
  return paths
}

// the web module's content policy must never block the editor's own scripts, styles or calls
let blocked: string[] = []
test.beforeEach(({ page }) => {
  blocked = []
  page.on('console', (msg) => {
    if (msg.type() === 'error' && /Content Security Policy/i.test(msg.text())) blocked.push(msg.text())
  })
})
test.afterEach(() => expect(blocked, 'blocked by the content policy').toEqual([]))

async function expectSampleGraph(page: Page) {
  await expect(page.getByTestId('node-whoami')).toBeVisible()
  await expect(page.getByTestId('node-postgres')).toBeVisible()
  const edges = page.locator('.react-flow__edge')
  await expect(edges).toHaveCount(1)
  // the marker is drawn: the edge path references a marker element that exists in the page
  const ref = await edges.locator('path.react-flow__edge-path').getAttribute('marker-end')
  const id = ref?.match(/url\(['"]?#([^'")]+)['"]?\)/)?.[1]
  expect(id, `marker-end is ${ref}`).toBeTruthy()
  await expect(page.locator(`marker[id="${id}"]`)).toHaveCount(1)
}

test('the editor shows the built in catalog (AC-2)', async ({ page }) => {
  const catalog = page.waitForResponse((r) => new URL(r.url()).pathname === `${base}/planner/v1/catalog`)
  const shell = await page.goto(`${base}/samples/whoami-postgres`)
  expect(shell?.headers()['content-security-policy']).toContain("default-src 'self'")
  expect(shell?.headers()['x-content-type-options']).toBe('nosniff')
  expect((await catalog).status()).toBe(200)
  await expect(page.getByRole('region', { name: 'Catalog' }).getByText('whoami', { exact: true })).toBeVisible()
})

test('downloading the sample saves its zip (AC-3)', async ({ page }) => {
  test.skip(base !== '', 'the zip is checked once, at the site root')
  await page.goto(sample)
  await expectSampleGraph(page)
  const render = page.waitForRequest((r) => r.method() === 'POST' && new URL(r.url()).pathname === '/planner/v1/render')
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Download zip' }).click()
  await render
  const file = await download
  expect(file.suggestedFilename()).toBe('whoami-postgres.zip')
  await file.saveAs(process.env.SMOKE_ZIP ?? 'results/whoami-postgres.zip')
})

test('a config value set in the inspector reaches the downloaded zip (spec 0025, AC-29)', async ({ page }) => {
  test.skip(base !== '', 'the zip is checked once, at the site root')
  await page.goto(`${base}/`)
  const graph = 'apiVersion: modular-platform/v1\nkind: Graph\nmodules:\n  planner:\n    module: planner\nname: config-smoke\n'
  await page
    .locator('input[type="file"]')
    .setInputFiles({ name: 'config-smoke.yaml', mimeType: 'application/yaml', buffer: Buffer.from(graph) })
  await page.getByTestId('node-planner').click()

  const field = page.getByRole('region', { name: 'Config' }).getByRole('textbox', { name: 'proxy-hops' })
  const plan = page.waitForRequest((r) => r.method() === 'POST' && new URL(r.url()).pathname === '/planner/v1/plan')
  await field.fill('2')
  await field.press('Enter')
  expect((await plan).postDataJSON().modules.planner.config).toEqual({ 'proxy-hops': '2' })
  await expect(page.getByText('Checking the graph…')).toHaveCount(0)

  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Download zip' }).click()
  const zip = await (await download).path()
  const env = execFileSync('unzip', ['-p', zip, 'config/planner.env'], { encoding: 'utf8' })
  expect(env).toContain('\nMP_PROXY_HOPS=2\n')
  expect(execFileSync('unzip', ['-p', zip, 'graph.yaml'], { encoding: 'utf8' })).toContain('    config:\n      proxy-hops: "2"\n')
})

test('everything stays under the base path (AC-7, AC-12)', async ({ page }) => {
  test.skip(base === '', 'only meaningful under a sub path')
  const paths = recordRequests(page)

  await page.goto(`${base}/`)
  await expect(page.getByRole('heading', { name: 'No graph open' })).toBeVisible()

  await page.goto(sample)
  await expectSampleGraph(page)
  await page.reload()
  await expectSampleGraph(page)

  expect(paths).toContain(`${base}/planner/v1/catalog`)
  const outside = paths.filter((p) => !p.startsWith(`${base}/`))
  expect(outside, 'requests outside the base path').toEqual([])

  const bare = await page.request.get(base, { maxRedirects: 0 })
  expect(bare.status()).toBe(308)
  expect(new URL(bare.headers().location ?? '', 'https://x').pathname).toBe(`${base}/`)
})

/** Drags from one canvas handle to another with the mouse. */
async function drag(page: Page, from: string, to: string) {
  const a = await page.locator(from).boundingBox()
  const b = await page.locator(to).boundingBox()
  if (!a || !b) throw new Error('a handle is not on screen')
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2)
  await page.mouse.down()
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 12 })
  await page.mouse.up()
}

test('unbind and bind again on a sample copy, each checked by the planner (spec 0010, AC-9, AC-10, AC-12, AC-18)', async ({ page }) => {
  test.skip(base !== '', 'the edit pass runs once, at the site root')
  const plans: number[] = []
  page.on('response', (r) => {
    if (r.request().method() === 'POST' && new URL(r.url()).pathname === '/planner/v1/plan') plans.push(r.status())
  })
  await page.goto(sample)
  await expectSampleGraph(page)
  const planned = plans.length // the sample's own plan

  // Delete on the selected binding makes the copy and unbinds; the editor moves to its root
  await page.locator('.react-flow__edge[data-id="whoami.postgresql"]').click()
  await page.keyboard.press('Delete')
  await expect(page).toHaveURL(`${base}/`)
  await expect(page.getByText('Edited', { exact: true })).toBeVisible()
  await expect(page.locator('.react-flow__edge')).toHaveCount(0)
  await expect(page.getByText('Checking the graph…')).toHaveCount(0)

  // drag the slot back to postgres, whose provide server makes the database (spec 0014)
  await drag(
    page,
    '.react-flow__handle[data-nodeid="whoami"][data-handleid="uses:postgresql"]',
    '.react-flow__handle[data-nodeid="postgres"][data-handleid="provides:server"]',
  )
  await expect(page.locator('.react-flow__edge[data-id="whoami.postgresql"]')).toHaveCount(1)
  await expect(page.getByText('Checking the graph…')).toHaveCount(0)
  expect(plans.slice(planned)).toEqual([200, 200])

  // the saved file is the sample as the planner resolved it, byte for byte: the default database
  // and login names written in (spec 0014, AC-7), as the render's golden graph.yaml pins them;
  // positions never reach the file
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Save as file' }).click()
  const saved = await (await download).path()
  const expected = readFileSync(new URL('../../libs/render/testdata/whoami-postgres/graph.yaml', import.meta.url), 'utf8')
  expect(readFileSync(saved, 'utf8')).toBe(expected)
})

test('a stored Dark theme is on <html> before the app module runs, under the content policy (spec 0010, AC-2)', async ({ page }) => {
  await page.goto(`${base}/`)
  await page.getByRole('radio', { name: 'Dark theme' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

  // readyState turns interactive after parsing and before deferred and module scripts run
  await page.addInitScript(() => {
    document.addEventListener('readystatechange', () => {
      if (document.readyState === 'interactive') {
        ;(window as unknown as { __themeAtParse: string | null }).__themeAtParse = document.documentElement.getAttribute('data-theme')
      }
    })
  })
  await page.reload()
  await expect(page.getByRole('heading', { name: 'No graph open' })).toBeVisible()
  expect(await page.evaluate(() => (window as unknown as { __themeAtParse: string | null }).__themeAtParse)).toBe('dark')
})
