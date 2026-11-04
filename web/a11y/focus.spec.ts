import { expect, type Locator, type Page, test } from '@playwright/test'
import { openGraph, useTheme } from './helpers'
import { mockPlanner } from './mock-planner'

// AC-3: a canvas node shows the focus ring and a canvas edge a thicker stroke in the focus
// color when it has keyboard focus. React Flow's own stylesheet removes the outline from
// focused nodes and paints focused edges in the selected color, so the editor's rules must win.

const BOUND = `apiVersion: modular-platform/v0alpha1
kind: Graph
name: bound
modules:
  postgres:
    module: postgres
  whoami:
    module: whoami
    uses:
      postgresql: postgres
`

/** whoami bound to a, with b as a second postgres. */
const TWO = `apiVersion: modular-platform/v0alpha1
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

/** The resolved --color-focus of the canvas, as the browser writes a computed color. */
async function focusColor(page: Page): Promise<string> {
  return page.evaluate(() => {
    const probe = document.createElement('div')
    probe.style.color = 'var(--color-focus)'
    document.querySelector('.react-flow')?.appendChild(probe)
    const color = getComputedStyle(probe).color
    probe.remove()
    return color
  })
}

/** Moves keyboard focus onto the target with Tab, so :focus-visible applies as for a person. */
async function tabTo(page: Page, target: Locator) {
  for (let i = 0; i < 60; i++) {
    if (await target.evaluate((el) => el === document.activeElement)) return
    await page.keyboard.press('Tab')
  }
  throw new Error('Tab never reached the target')
}

for (const theme of ['light', 'dark'] as const) {
  test(`a focused node and edge show the focus color in ${theme} (AC-3)`, async ({ page }) => {
    await useTheme(page, theme)
    await mockPlanner(page)
    await openGraph(page, BOUND)
    const ring = await focusColor(page)

    const node = page.locator('.react-flow__node[data-id="whoami"]')
    await tabTo(page, node)
    await expect(node).toHaveCSS('outline-style', 'solid')
    await expect(node).toHaveCSS('outline-width', '2px')
    await expect(node).toHaveCSS('outline-color', ring)

    const edge = page.locator('.react-flow__edge[data-id="whoami.postgresql"]')
    await tabTo(page, edge)
    const path = edge.locator('.react-flow__edge-path')
    await expect(path).toHaveCSS('stroke', ring)
    await expect(path).toHaveCSS('stroke-width', '3px')
  })

  test(`a focused edge dimmed by the selection still shows the focus stroke in ${theme} (AC-3)`, async ({ page }) => {
    await useTheme(page, theme)
    await mockPlanner(page)
    await openGraph(page, TWO)
    const ring = await focusColor(page)

    // selecting b dims the edge whoami to a, which is not b's
    await page.locator('.react-flow__node[data-id="b"]').click()
    const edge = page.locator('.react-flow__edge[data-id="whoami.postgresql"]')
    await expect(edge).toHaveClass(/is-dimmed/)
    await tabTo(page, edge)
    const path = edge.locator('.react-flow__edge-path')
    await expect(path).toHaveCSS('stroke', ring)
    await expect(path).toHaveCSS('stroke-width', '3px')
  })
}
