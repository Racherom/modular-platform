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
