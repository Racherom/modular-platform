import AxeBuilder from '@axe-core/playwright'
import { expect, type Locator, type Page } from '@playwright/test'
import { graphFile } from './mock-planner'

/** The WCAG tags AC-3 names; axe core 4.14 accepts wcag22aa. */
export const WCAG_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']

/** Shows the page in one theme: the OS setting and the stored choice agree. */
export async function useTheme(page: Page, theme: 'light' | 'dark') {
  await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' })
  await page.addInitScript((t) => window.localStorage.setItem('mp-theme', t), theme)
}

/** Runs axe on the whole page and expects no violations, listing any it finds. */
export async function expectNoAxeViolations(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withTags(WCAG_TAGS).analyze()
  const found = violations.map((v) => `${v.id}: ${v.help} (${v.nodes.map((n) => n.target.join(' ')).join(', ')})`)
  expect(found).toEqual([])
}

/** A handle on the canvas, like uses:postgresql on node whoami. */
export function handle(page: Page, instance: string, id: string): Locator {
  return page.locator(`.react-flow__handle[data-nodeid="${instance}"][data-handleid="${id}"]`)
}

/** Drags from one handle to another with the mouse; `during` runs while the button is held. */
export async function drag(page: Page, from: Locator, to: Locator, during?: () => Promise<void>) {
  const a = await from.boundingBox()
  const b = await to.boundingBox()
  if (!a || !b) throw new Error('a handle is not on screen')
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2)
  await page.mouse.down()
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 12 })
  await during?.()
  await page.mouse.up()
}

/** whoami with its postgresql need unbound, next to postgres. */
export const UNBOUND = `apiVersion: modular-platform/v0alpha1
kind: Graph
name: whoami-postgres
modules:
  postgres:
    module: postgres
  whoami:
    module: whoami
    uses:
      postgresql: null
`

/** Opens a graph file in the editor and waits for its nodes. */
export async function openGraph(page: Page, text = UNBOUND, name = 'graph.yaml') {
  await page.goto('/')
  await page.getByRole('button', { name: 'Open file' }).waitFor()
  await page.locator('input[type=file]').setInputFiles(graphFile(name, text))
  await expect(page.getByTestId('node-whoami')).toBeVisible()
}
