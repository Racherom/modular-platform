import { readFileSync } from 'node:fs'
import { expect, type Page, test } from '@playwright/test'
import { drag, handle, openGraph, UNBOUND } from './helpers'
import { graphFile, mockRenderer } from './mock-renderer'

// The session strand of spec 0010: undo and redo, Tidy layout, the edited flag, the sample
// copy and its replace prompt, the catalog filter, and unknown modules (AC-15 to AC-20).

const edges = '.react-flow__edge'
const SAMPLE = '/samples/whoami-postgres'
const sampleFile = readFileSync(new URL('../../samples/whoami-postgres/graph.yaml', import.meta.url), 'utf8')

const edge = (page: Page, id: string) => page.locator(`${edges}[data-id="${id}"]`)
const node = (page: Page, id: string) => page.locator(`.react-flow__node[data-id="${id}"]`)
const fileName = (page: Page) => page.getByRole('heading', { level: 1 })
const button = (page: Page, name: string) => page.getByRole('button', { name, exact: true })
// the node's x in canvas coordinates, from React Flow's transform (screen pixels change with the zoom)
const x = async (page: Page, id: string) => Number((await node(page, id).getAttribute('style'))?.match(/translate\((-?[\d.]+)px/)?.[1])

/** Opens UNBOUND and binds whoami to postgres, so the working graph carries the edited flag. */
async function editedWorkingGraph(page: Page) {
  await openGraph(page)
  await drag(page, handle(page, 'whoami', 'uses:postgresql'), handle(page, 'postgres', 'provides:postgresql'))
  await expect(page.locator(edges)).toHaveCount(1)
  await expect(page.getByText('Edited')).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'Checking' })).toHaveCount(0)
}

test('undo and redo with the buttons and the keyboard, one step per edit (AC-15, AC-17)', async ({ page }) => {
  const { plans } = await mockRenderer(page)
  await openGraph(page)
  await expect(button(page, 'Undo')).toBeDisabled()
  await expect(button(page, 'Redo')).toBeDisabled()

  await drag(page, handle(page, 'whoami', 'uses:postgresql'), handle(page, 'postgres', 'provides:postgresql'))
  await expect(page.locator(edges)).toHaveCount(1)
  await expect.poll(() => plans.length).toBe(2)

  await button(page, 'Undo').click()
  await expect(page.locator(edges)).toHaveCount(0)
  await expect.poll(() => plans.length).toBe(3) // undo plans the restored graph
  expect(plans[2]?.modules.whoami?.uses).toEqual({ postgresql: null })
  await expect(page.getByText('Edited')).toBeVisible() // undo is an edit too

  await page.locator('.react-flow__pane').click()
  await page.keyboard.press('Control+Shift+Z')
  await expect(page.locator(edges)).toHaveCount(1)
  await page.keyboard.press('Control+Z')
  await expect(page.locator(edges)).toHaveCount(0)
  await page.keyboard.press('Control+Y')
  await expect(page.locator(edges)).toHaveCount(1)
  await expect(button(page, 'Redo')).toBeDisabled()

  // the shortcuts do nothing while focus is in a text field
  await page.getByRole('searchbox', { name: 'Filter the catalog' }).focus()
  await page.keyboard.press('Control+Z')
  await expect(page.locator(edges)).toHaveCount(1)
})

test('Tidy layout moves nodes back without a plan call, and undo puts them back (AC-15, AC-16)', async ({ page }) => {
  const { plans } = await mockRenderer(page)
  // a working copy whose positions are not the auto layout: open, then move a node in storage
  await openGraph(page)
  const tidyX = await x(page, 'postgres')
  await page.evaluate(
    () =>
      new Promise<void>((resolve, reject) => {
        const req = indexedDB.open('modular-platform', 1)
        req.onsuccess = () => {
          const store = req.result.transaction('working', 'readwrite').objectStore('working')
          const get = store.get('graph')
          get.onsuccess = () => {
            const w = get.result
            w.positions.postgres = { x: w.positions.postgres.x + 400, y: w.positions.postgres.y }
            store.put(w, 'graph').onsuccess = () => resolve()
          }
        }
        req.onerror = () => reject(req.error)
      }),
  )
  await page.reload()
  await expect(node(page, 'postgres')).toBeVisible()
  const movedX = await x(page, 'postgres')
  expect(movedX).not.toBe(tidyX)
  const before = plans.length

  await button(page, 'Tidy layout').click()
  await expect.poll(() => x(page, 'postgres')).toBe(tidyX)
  await expect(page.getByText('Edited')).toBeVisible()
  expect(plans).toHaveLength(before)

  await button(page, 'Undo').click()
  await expect.poll(() => x(page, 'postgres')).toBe(movedX)
})

test('the sample: hint, history disabled, first edit makes an edited copy and moves to the editor (AC-18)', async ({ page }) => {
  const { plans } = await mockRenderer(page)
  await page.goto(SAMPLE)
  await expect(page.getByText('This is a sample.')).toBeVisible()
  await expect(button(page, 'Undo')).toBeDisabled()
  await expect(button(page, 'Redo')).toBeDisabled()
  await expect(button(page, 'Tidy layout')).toBeDisabled()

  await edge(page, 'whoami.postgresql').click()
  await page.keyboard.press('Delete')
  await expect(page).toHaveURL(/\/$/)
  await expect(fileName(page)).toHaveText('whoami-postgres.yaml')
  await expect(page.getByText('Edited')).toBeVisible()
  await expect(page.locator(edges)).toHaveCount(0)
  await expect(node(page, 'whoami')).toBeFocused()
  await expect.poll(() => plans.at(-1)?.modules.whoami?.uses).toEqual({ postgresql: null })

  // one undo returns to the unedited copy
  await button(page, 'Undo').click()
  await expect(page.locator(edges)).toHaveCount(1)
  await expect(button(page, 'Undo')).toBeDisabled()

  // bound by hand again, Save as file writes the sample's bytes (positions never go in)
  await button(page, 'Redo').click()
  await expect(page.locator(edges)).toHaveCount(0)
  await drag(page, handle(page, 'whoami', 'uses:postgresql'), handle(page, 'postgres', 'provides:postgresql'))
  await expect(page.locator(edges)).toHaveCount(1)
  const download = page.waitForEvent('download')
  await button(page, 'Save as file').click()
  const file = await download
  expect(file.suggestedFilename()).toBe('whoami-postgres.yaml')
  expect(readFileSync(await file.path(), 'utf8')).toBe(sampleFile)
  await expect(page.getByText('Edited')).toHaveCount(0)

  // the sample itself never changed
  await page.goto(SAMPLE)
  await expect(page.locator(edges)).toHaveCount(1)
})

test('an edit on the sample asks first when the working graph is edited; Cancel keeps both (AC-18)', async ({ page }) => {
  const { plans } = await mockRenderer(page)
  await editedWorkingGraph(page)
  // a full load: the sample page reads the edited flag from IndexedDB at the first edit
  await page.goto(SAMPLE)
  await expect(page.locator(edges)).toHaveCount(1)
  const calls = plans.length

  await edge(page, 'whoami.postgresql').click()
  await page.keyboard.press('Delete')
  const dialog = page.getByRole('alertdialog', { name: 'Replace the edited graph?' })
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('graph.yaml')
  await expect(page.locator(edges)).toHaveCount(1) // held: nothing changed on screen

  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toBeHidden()
  await expect(page).toHaveURL(/samples/)
  await expect(page.locator(edges)).toHaveCount(1)
  expect(plans).toHaveLength(calls)

  // the inspector select reverts after Cancel
  await node(page, 'whoami').click()
  const select = page.getByRole('region', { name: 'Inspector' }).getByRole('button', { name: /postgresql Bound to/ })
  await select.click()
  await page.getByRole('option', { name: 'Not bound' }).click()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(select).toContainText('postgres')

  // Replace makes the copy
  await edge(page, 'whoami.postgresql').click()
  await page.keyboard.press('Delete')
  await dialog.getByRole('button', { name: 'Replace' }).click()
  await expect(page).toHaveURL(/\/$/)
  await expect(fileName(page)).toHaveText('whoami-postgres.yaml')
  await expect(page.locator(edges)).toHaveCount(0)
})

test('Open file asks first when the working graph is edited, before any file is read (AC-18)', async ({ page }) => {
  await mockRenderer(page)
  await editedWorkingGraph(page)
  let choosers = 0
  page.on('filechooser', () => {
    choosers += 1
  })

  await button(page, 'Open file').click()
  const dialog = page.getByRole('alertdialog', { name: 'Replace the edited graph?' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(button(page, 'Open file')).toBeFocused() // focus returns to the opener
  expect(choosers).toBe(0)

  await button(page, 'Open file').click()
  const chooser = page.waitForEvent('filechooser')
  await dialog.getByRole('button', { name: 'Replace' }).click()
  await (await chooser).setFiles(graphFile('other.yaml', UNBOUND.replace('name: whoami-postgres', 'name: other')))
  await expect(fileName(page)).toHaveText('other.yaml')
  await expect(page.getByText('Edited')).toHaveCount(0)
  await expect(button(page, 'Undo')).toBeDisabled() // history cleared on open
})

test('Find providers filters the catalog and moves focus to the filter; no match shows an empty state (AC-19)', async ({ page }) => {
  await mockRenderer(page)
  await openGraph(page)
  await node(page, 'whoami').click()
  await page.getByRole('region', { name: 'Inspector' }).getByRole('button', { name: 'Find providers of postgresql' }).click()

  const filter = page.getByRole('searchbox', { name: 'Filter the catalog' })
  await expect(filter).toBeFocused()
  await expect(filter).toHaveValue('postgresql')
  const catalog = page.getByRole('region', { name: 'Catalog' })
  await expect(catalog.getByRole('heading', { name: 'postgres', exact: true })).toBeVisible()
  await expect(catalog.getByRole('heading', { name: 'test-app' })).toBeVisible() // uses postgresql
  await expect(catalog.getByRole('status')).toHaveText('4 of 4 modules')

  await filter.fill('HTTP')
  await expect(catalog.getByRole('status')).toHaveText('2 of 4 modules')
  await expect(catalog.getByRole('heading', { name: 'postgres', exact: true })).toHaveCount(0)

  await filter.fill('NO-SUCH-THING')
  await expect(catalog.getByText('No module matches')).toBeVisible()
  await catalog.getByRole('button', { name: 'Clear the filter' }).click()
  await expect(filter).toHaveValue('')
})

test('an instance whose module is not in the catalog has a badge and no ports; text stays text (AC-20)', async ({ page }) => {
  await mockRenderer(page)
  let alerted = false
  page.on('dialog', (d) => {
    alerted = true
    void d.dismiss()
  })
  await openGraph(page, `${UNBOUND}  ghost:\n    module: '<b>nope</b>'\n`)
  const ghost = page.getByTestId('node-ghost')
  await expect(ghost.getByText('not in catalog')).toBeVisible()
  await expect(ghost.getByText('<b>nope</b>')).toBeVisible()
  await expect(page.locator('.react-flow__handle[data-nodeid="ghost"]')).toHaveCount(0)

  // never a drop target or a candidate
  await node(page, 'whoami').click()
  await page
    .getByRole('region', { name: 'Inspector' })
    .getByRole('button', { name: /postgresql Bound to/ })
    .click()
  await expect(page.getByRole('listbox').getByRole('option')).toHaveText(['postgres', 'Not bound', 'Let the planner choose'])
  await page.keyboard.press('Escape')

  // the hostile catalog description shows as text and runs nothing
  const catalog = page.getByRole('region', { name: 'Catalog' })
  await expect(catalog.getByText(/<img src=x onerror=alert\(1\)>/)).toBeVisible()
  await expect(catalog.locator('img')).toHaveCount(0)
  expect(alerted).toBe(false)
})

test('a quiet note says changes are not kept when browser storage fails (AC-17)', async ({ page }) => {
  await page.addInitScript(() => {
    indexedDB.open = () => {
      throw new Error('storage blocked')
    }
  })
  await mockRenderer(page)
  await openGraph(page)
  await expect(page.getByRole('status').filter({ hasText: 'Changes are not kept after reload in this browser.' })).toBeVisible()
})
