import { expect, type Page, test } from '@playwright/test'
import { drag, handle, openGraph } from './helpers'
import { type CheckReply, mockRenderer } from './mock-renderer'

const edges = '.react-flow__edge'

/** A node's position in the graph (React Flow's transform on the node), not on the page, so a
 * header that wraps to another height or a canvas that resizes does not count as a move. */
function graphPosition(page: Page, instance: string) {
  return page.locator(`.react-flow__node[data-id="${instance}"]`).evaluate((n) => (n as HTMLElement).style.transform)
}

test('dragging a need to an offer binds it through one check call, and it survives a reload (AC-9, AC-12, AC-16)', async ({ page }) => {
  const { checks } = await mockRenderer(page)
  await openGraph(page)
  await expect(page.locator(edges)).toHaveCount(0)
  expect(checks).toHaveLength(1) // the open
  const position = await graphPosition(page, 'postgres')

  await drag(page, handle(page, 'whoami', 'uses:postgresql'), handle(page, 'postgres', 'provides:postgresql'), async () => {
    // valid targets highlighted, the rest dimmed
    await expect(handle(page, 'postgres', 'provides:postgresql')).toHaveAttribute('data-drop', 'valid')
    await expect(handle(page, 'whoami', 'provides:http')).toHaveAttribute('data-drop', 'invalid')
  })

  await expect(page.locator(edges)).toHaveCount(1)
  await expect(page.locator(`${edges}[data-id="whoami.postgresql"]`)).toBeVisible()
  await expect(page.getByText('Edited')).toBeVisible()
  await expect.poll(() => checks.length).toBe(2)
  expect(checks[1]?.modules.whoami?.uses).toEqual({ postgresql: 'postgres' })
  // positions stay where they are
  expect(await graphPosition(page, 'postgres')).toBe(position)
  await expect(page.getByRole('status').filter({ hasText: 'Checking' })).toHaveCount(0)

  await page.reload()
  await expect(page.locator(edges)).toHaveCount(1)
  await expect(page.getByText('Edited')).toBeVisible()
  expect(checks).toHaveLength(2) // the working copy came back from IndexedDB, no new check call
})

test('a drop anywhere else changes nothing (AC-9)', async ({ page }) => {
  const { checks } = await mockRenderer(page)
  await openGraph(page)
  // onto whoami's own offer: not a valid target
  await drag(page, handle(page, 'whoami', 'uses:postgresql'), handle(page, 'whoami', 'provides:http'))
  // onto empty canvas
  const pane = await page.locator('.react-flow__pane').boundingBox()
  const from = handle(page, 'whoami', 'uses:postgresql')
  const a = await from.boundingBox()
  await page.mouse.move(a!.x + 6, a!.y + 6)
  await page.mouse.down()
  await page.mouse.move(pane!.x + 20, pane!.y + pane!.height - 20, { steps: 8 })
  await page.mouse.up()

  await expect(page.locator(edges)).toHaveCount(0)
  await expect(page.getByText('Edited')).toHaveCount(0)
  expect(checks).toHaveLength(1)
})

test('a drag cannot start at a provides port (AC-9)', async ({ page }) => {
  const { checks } = await mockRenderer(page)
  await openGraph(page)
  await drag(page, handle(page, 'postgres', 'provides:postgresql'), handle(page, 'whoami', 'uses:postgresql'))
  await expect(page.locator(edges)).toHaveCount(0)
  expect(checks).toHaveLength(1)
})

test('only the reply for the latest edit is used (AC-12)', async ({ page }) => {
  let call = 0
  // the open answers at once, the first edit's reply is held back past the second one's
  const { checks } = await mockRenderer(page, () => (++call === 2 ? { delayMs: 1500 } : 'echo'))
  const two = `apiVersion: modular-platform/v1
kind: Graph
name: two
modules:
  a:
    module: postgres
  b:
    module: postgres
  whoami:
    module: whoami
    uses:
      postgresql: null
`
  await openGraph(page, two)
  await drag(page, handle(page, 'whoami', 'uses:postgresql'), handle(page, 'a', 'provides:postgresql'))
  await drag(page, handle(page, 'whoami', 'uses:postgresql'), handle(page, 'b', 'provides:postgresql'))
  await expect.poll(() => checks.length).toBe(3)
  await page.waitForTimeout(1800)
  await expect(page.locator(edges)).toHaveCount(1)
  await expect(page.locator(`${edges}[data-id="whoami.postgresql"]`)).toBeVisible()
  const target = await page.locator(`${edges}[data-id="whoami.postgresql"]`).getAttribute('aria-label')
  expect(target).toBe('whoami uses postgresql from b')
})

test('a failed check keeps the edit and offers Retry: 503, then 429, then it recovers (AC-12, AC-14)', async ({ page }) => {
  let call = 0
  const replies: CheckReply[] = ['echo', { status: 503 }, { status: 429 }, 'echo']
  const { checks } = await mockRenderer(page, () => replies[call++] ?? 'echo')
  await openGraph(page)
  await drag(page, handle(page, 'whoami', 'uses:postgresql'), handle(page, 'postgres', 'provides:postgresql'))
  await expect(page.getByRole('alert')).toBeVisible()
  await expect(page.getByText('Not checked', { exact: true })).toBeVisible()
  await expect(page.locator(edges)).toHaveCount(1)
  await expect(page.getByRole('button', { name: 'Download zip' })).toBeDisabled()
  await expect(page.getByText('Not checked, retry first')).toBeVisible()

  // the retry meets a busy renderer: still not checked, the busy message, Retry offered again
  await page.getByRole('button', { name: 'Retry' }).click()
  await expect(page.getByRole('alert')).toContainText(/busy/i)
  await expect(page.getByText('Not checked', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Download zip' })).toBeDisabled()
  await expect(page.locator(edges)).toHaveCount(1)

  await page.getByRole('button', { name: 'Retry' }).click()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Download zip' })).toBeEnabled()
  expect(checks).toHaveLength(4)
})
