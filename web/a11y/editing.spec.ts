import { expect, type Page, test } from '@playwright/test'
import { drag, handle, openGraph } from './helpers'
import { mockRenderer } from './mock-renderer'

const edges = '.react-flow__edge'

/** whoami bound to a, with b as a second postgres. */
const TWO = `apiVersion: modular-platform/v1
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
      postgresql: a
`

/** test-app needs postgresql (required) and it is not bound. */
const REQUIRED = `apiVersion: modular-platform/v1
kind: Graph
name: required
modules:
  postgres:
    module: postgres
  whoami:
    module: whoami
    uses:
      postgresql: postgres
  app:
    module: test-app
    uses:
      postgresql: null
`

const edge = (page: Page, id: string) => page.locator(`${edges}[data-id="${id}"]`)
const node = (page: Page, id: string) => page.locator(`.react-flow__node[data-id="${id}"]`)
const inspector = (page: Page) => page.getByRole('region', { name: 'Inspector' })

test('dragging the provider end of an edge rebinds it; dropping elsewhere keeps it (AC-9)', async ({ page }) => {
  const { plans } = await mockRenderer(page)
  await openGraph(page, TWO)
  await expect(edge(page, 'whoami.postgresql')).toHaveAttribute('aria-label', 'whoami uses postgresql from a')

  const end = edge(page, 'whoami.postgresql').locator('.react-flow__edgeupdater-target')
  await drag(page, end, handle(page, 'b', 'provides:postgresql'), async () => {
    await expect(handle(page, 'b', 'provides:postgresql')).toHaveAttribute('data-drop', 'valid')
  })
  await expect(edge(page, 'whoami.postgresql')).toHaveAttribute('aria-label', 'whoami uses postgresql from b')
  await expect.poll(() => plans.length).toBe(2)
  expect(plans[1]?.modules.whoami?.uses).toEqual({ postgresql: 'b' })

  // a reconnect dropped on empty canvas changes nothing
  const pane = await page.locator('.react-flow__pane').boundingBox()
  const a = await edge(page, 'whoami.postgresql').locator('.react-flow__edgeupdater-target').boundingBox()
  await page.mouse.move(a!.x + a!.width / 2, a!.y + a!.height / 2)
  await page.mouse.down()
  await page.mouse.move(pane!.x + 20, pane!.y + pane!.height - 20, { steps: 8 })
  await page.mouse.up()
  await expect(edge(page, 'whoami.postgresql')).toHaveAttribute('aria-label', 'whoami uses postgresql from b')
  expect(plans).toHaveLength(2)

  // the consumer end cannot be dragged
  await expect(edge(page, 'whoami.postgresql').locator('.react-flow__edgeupdater-source')).toHaveCount(0)
})

test('Delete unbinds the selected binding, then the consumer is selected and focused (AC-8, AC-10)', async ({ page }) => {
  const { plans } = await mockRenderer(page)
  await openGraph(page, TWO)
  await edge(page, 'whoami.postgresql').click()
  await expect(inspector(page).getByRole('heading', { name: 'Binding' })).toBeVisible()
  await expect(edge(page, 'whoami.postgresql')).toHaveClass(/selected/)

  await page.keyboard.press('Delete')
  await expect(page.locator(edges)).toHaveCount(0)
  await expect(node(page, 'whoami')).toBeFocused()
  await expect(node(page, 'whoami')).toHaveClass(/selected/)
  await expect(inspector(page).getByRole('heading', { name: 'whoami' })).toBeVisible()
  await expect.poll(() => plans.length).toBe(2)
  expect(plans[1]?.modules.whoami?.uses).toEqual({ postgresql: null })
  // nothing else changed: every module is still there
  expect(Object.keys(plans[1]?.modules ?? {}).sort()).toEqual(['a', 'b', 'whoami'])

  // with the module selected, Delete never removes it
  await page.keyboard.press('Delete')
  await expect(page.locator('.react-flow__node')).toHaveCount(3)
  expect(plans).toHaveLength(2)
})

test('the Remove binding button unbinds and selects the consumer (AC-10)', async ({ page }) => {
  await mockRenderer(page)
  await openGraph(page, TWO)
  await edge(page, 'whoami.postgresql').focus()
  await page.keyboard.press('Enter')
  await inspector(page).getByRole('button', { name: 'Remove binding' }).click()
  await expect(page.locator(edges)).toHaveCount(0)
  await expect(node(page, 'whoami')).toBeFocused()
})

test('keyboard only: select a module, bind and unbind from the inspector (AC-8, AC-11)', async ({ page }) => {
  const { plans } = await mockRenderer(page)
  await openGraph(page, TWO)

  // nodes and edges are reached with Tab
  await page.locator('.react-flow__pane').click()
  await node(page, 'a').focus()
  await page.keyboard.press('Tab')
  await page.keyboard.press('Tab')
  await expect(node(page, 'whoami')).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(inspector(page).getByRole('heading', { name: 'whoami' })).toBeVisible()
  await expect(inspector(page).getByText('optional', { exact: true })).toBeVisible()
  await expect(inspector(page).getByText('Used by', { exact: false })).toHaveCount(0)

  const select = inspector(page).getByRole('button', { name: /postgresql Bound to/ })
  await expect(select).toContainText('a')
  await select.focus()
  await page.keyboard.press('Enter')
  const list = page.getByRole('listbox')
  await expect(list.getByRole('option')).toHaveText(['a', 'b', 'Not bound', 'Let the planner choose'])
  await list.getByRole('option', { name: 'Not bound', exact: true }).press('Enter')
  await expect(page.locator(edges)).toHaveCount(0)
  await expect.poll(() => plans.length).toBe(2)
  expect(plans[1]?.modules.whoami?.uses).toEqual({ postgresql: null })

  await select.focus()
  await page.keyboard.press('Enter')
  await page.getByRole('listbox').getByRole('option', { name: 'b', exact: true }).press('Enter')
  await expect(edge(page, 'whoami.postgresql')).toHaveAttribute('aria-label', 'whoami uses postgresql from b')

  // a provider lists who uses it
  await node(page, 'b').focus()
  await page.keyboard.press('Enter')
  await expect(inspector(page).getByText('Used by whoami')).toBeVisible()
})

test('Let the planner choose removes the key; the renderer fills it and the note shows (AC-11)', async ({ page }) => {
  await mockRenderer(page)
  // the mock echoes the graph, so fake the automatic binding the renderer would write back
  await page.route('**/renderer/v1/plan', async (route) => {
    const graph = route.request().postDataJSON()
    const uses = graph.modules.whoami.uses ?? {}
    const auto = !Object.hasOwn(uses, 'postgresql')
    if (auto) graph.modules.whoami.uses = { ...uses, postgresql: 'a' }
    await route.fulfill({
      json: {
        ok: true,
        graph,
        bindings: [
          {
            consumer: 'whoami',
            slot: 'postgresql',
            capability: 'postgresql',
            provider: graph.modules.whoami.uses.postgresql ?? 'a',
            provide: 'postgresql',
            auto,
            named: false,
            provisioned: false,
          },
        ],
        problems: [],
      },
    })
  })
  await openGraph(page, TWO)
  await node(page, 'whoami').click()
  const select = inspector(page).getByRole('button', { name: /postgresql Bound to/ })
  await select.click()
  await page.getByRole('option', { name: 'Let the planner choose' }).click()
  await expect(inspector(page).getByText('Bound by the renderer')).toBeVisible()
  await expect(select).toContainText('a')
})

test('a binding to an instance that is not a candidate shows as invalid (AC-11)', async ({ page }) => {
  await mockRenderer(page)
  await openGraph(page, TWO.replace('postgresql: a', 'postgresql: whoami'))
  await node(page, 'whoami').click()
  await expect(inspector(page).getByRole('button', { name: /postgresql Bound to/ })).toContainText('whoami (invalid)')
})

test('Escape and a click on empty canvas clear the selection (AC-8)', async ({ page }) => {
  await mockRenderer(page)
  await openGraph(page, TWO)
  await node(page, 'whoami').click()
  await expect(page.getByTestId('node-b')).toHaveAttribute('data-dimmed', 'true')
  await expect(page.getByTestId('node-a')).not.toHaveAttribute('data-dimmed')
  await expect(edge(page, 'whoami.postgresql')).toHaveClass(/is-related/)

  await page.keyboard.press('Escape')
  await expect(inspector(page).getByText('Select a module or a binding')).toBeVisible()
  await expect(page.getByTestId('node-b')).not.toHaveAttribute('data-dimmed')

  await edge(page, 'whoami.postgresql').click()
  // top left: the zoom controls sit bottom left and the attribution bottom right
  const pane = await page.locator('.react-flow__pane').boundingBox()
  await page.mouse.click(pane!.x + 10, pane!.y + 10)
  await expect(inspector(page).getByText('Select a module or a binding')).toBeVisible()
})

test('a required need set to null shows unbound_required on its port and blocks download (AC-10, AC-13, AC-14)', async ({ page }) => {
  await mockRenderer(page)
  await openGraph(page, REQUIRED)
  const mark = page.getByTestId('node-app').getByRole('img', { name: /app needs postgresql/ })
  await expect(mark).toBeVisible()
  await expect(page.getByRole('button', { name: 'Download zip' })).toBeDisabled()
  await expect(page.getByText('Fix 1 error to download')).toBeVisible()

  // a click in the problems panel selects, centers and focuses the module
  await page
    .getByRole('region', { name: 'Problems' })
    .getByRole('button', { name: /app needs postgresql/ })
    .click()
  await expect(node(page, 'app')).toBeFocused()
  await expect(node(page, 'app')).toHaveClass(/selected/)
  await expect(inspector(page).getByRole('heading', { name: 'app' })).toBeVisible()
  await expect(inspector(page).getByText('required', { exact: true })).toBeVisible()
  const view = await page.locator('.react-flow').boundingBox()
  // the canvas animates there unless reduced motion is asked for
  await expect
    .poll(async () => {
      const box = await node(page, 'app').boundingBox()
      return Math.max(
        Math.abs(box!.x + box!.width / 2 - (view!.x + view!.width / 2)),
        Math.abs(box!.y + box!.height / 2 - (view!.y + view!.height / 2)),
      )
    })
    .toBeLessThan(4)

  // binding it fixes the problem and allows download
  await drag(page, handle(page, 'app', 'uses:postgresql'), handle(page, 'postgres', 'provides:postgresql'))
  await expect(mark).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Download zip' })).toBeEnabled()
})

test('a 400 shows the renderer message without Retry; a 429 says the renderer is busy (AC-14)', async ({ page }) => {
  let call = 0
  await mockRenderer(page, () => {
    call++
    if (call === 2) return { status: 400 }
    if (call === 3) return { status: 429 }
    return 'echo'
  })
  await openGraph(page, TWO)
  await edge(page, 'whoami.postgresql').click()
  await page.keyboard.press('Delete')
  const alert = page.getByRole('alert')
  await expect(alert).toContainText('Mock failure.')
  await expect(alert.getByRole('button', { name: 'Retry' })).toHaveCount(0)
  await expect(page.locator(edges)).toHaveCount(0) // the edit stays

  await drag(page, handle(page, 'whoami', 'uses:postgresql'), handle(page, 'b', 'provides:postgresql'))
  await expect(alert).toContainText('The renderer is busy, try again in a minute.')
  await expect(alert.getByRole('button', { name: 'Retry' })).toBeVisible()
  await alert.getByRole('button', { name: 'Retry' }).click()
  await expect(alert).toHaveCount(0)
})
