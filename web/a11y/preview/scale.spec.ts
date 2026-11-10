import { expect, type Page, test } from '@playwright/test'
import { stringify } from 'yaml'
import type { Catalog, Graph } from '../../src/api/client'
import { drag, handle } from '../helpers'
import { graphFile, mockRenderer } from '../mock-renderer'

// AC-7: on the production build, with a 50 module, 100 binding graph, a selection and a connect
// each update the screen within 100 ms (median of 5 runs). The check call is not counted: its
// reply is held back until after the measurement.
const BUDGET_MS = 100
const RUNS = 5

/** Ten entries: scale-k provides cap-k and uses the next two capabilities, cap-(k+1) and cap-(k+2). */
const modules = Array.from({ length: 10 }, (_, k) => ({
  apiVersion: 'modular-platform/v1',
  kind: 'Module',
  name: `scale-${k}`,
  description: `Scale test entry ${k}.`,
  image: `example.test/scale-${k}:1`,
  provides: { [`cap-${k}`]: { capability: `cap-${k}`, version: '1.0', port: 8000 + k } },
  uses: { [`cap-${(k + 1) % 10}`]: { capability: `cap-${(k + 1) % 10}` }, [`cap-${(k + 2) % 10}`]: { capability: `cap-${(k + 2) % 10}` } },
}))
const entries = { modules, capabilities: [] } as unknown as Catalog

/** Five instances per entry (s<k>-<j>), each binding both needs to the instances of the same j: 50 modules, 100 bindings. */
function scaleGraph(): Graph {
  const modules: Graph['modules'] = {}
  for (let k = 0; k < 10; k++) {
    for (let j = 0; j < 5; j++) {
      const a = (k + 1) % 10
      const b = (k + 2) % 10
      modules[`s${k}-${j}`] = { module: `scale-${k}`, uses: { [`cap-${a}`]: `s${a}-${j}`, [`cap-${b}`]: `s${b}-${j}` } }
    }
  }
  return { apiVersion: 'modular-platform/v1', kind: 'Graph', name: 'scale', modules }
}

/**
 * Runs `act` and measures, inside the page, the time from the pointer release to the first DOM
 * change after which `done` holds. Measuring in the page keeps Playwright's round trips out.
 */
async function measure(
  page: Page,
  done: { kind: 'selected'; instance: string } | { kind: 'edge'; id: string; from: string },
  act: () => Promise<void>,
) {
  await page.evaluate((d) => {
    const w = window as unknown as { __measure: Promise<number> }
    const holds = () => {
      if (d.kind === 'edge')
        return document.querySelector(`.react-flow__edge[data-id="${d.id}"]`)?.getAttribute('aria-label')?.endsWith(`from ${d.from}`)
      const inspector = [...document.querySelectorAll('section')].find((s) => s.querySelector('h2')?.textContent === 'Inspector')
      return (
        inspector?.querySelector('h3')?.textContent === d.instance &&
        document.querySelector(`.react-flow__node[data-id="${d.instance}"]`)?.classList.contains('selected')
      )
    }
    w.__measure = new Promise((resolve) => {
      // a miss fails the budget clearly instead of hanging the test
      setTimeout(() => resolve(Number.POSITIVE_INFINITY), 2000)
      let start = 0
      window.addEventListener('pointerup', () => (start = performance.now()), { capture: true, once: true })
      const observer = new MutationObserver(() => {
        if (start === 0 || !holds()) return
        observer.disconnect()
        resolve(performance.now() - start)
      })
      observer.observe(document.body, { subtree: true, childList: true, attributes: true, characterData: true })
    })
  }, done)
  await act()
  return page.evaluate(() => (window as unknown as { __measure: Promise<number> }).__measure)
}

/** The instances whose node lies fully inside the canvas, so a click or a drag can reach it. */
async function visibleNodes(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const pane = document.querySelector('.react-flow')?.getBoundingClientRect()
    if (!pane) return []
    return [...document.querySelectorAll<HTMLElement>('.react-flow__node')]
      .filter((n) => {
        const r = n.getBoundingClientRect()
        return r.left >= pane.left && r.top >= pane.top && r.right <= pane.right - 60 && r.bottom <= pane.bottom
      })
      .map((n) => n.dataset.id ?? '')
      .sort()
  })
}

function median(values: number[]): number {
  const sorted = [...values].sort((a, b) => a - b)
  return sorted[Math.floor(sorted.length / 2)] ?? Number.NaN
}

test('a selection and a connect each update a 50 module graph within 100 ms (AC-7)', async ({ page }) => {
  test.slow()
  await mockRenderer(page, () => ({ delayMs: 3000 }), entries)
  await page.goto('/')
  await page.getByRole('button', { name: 'Open file' }).waitFor()
  await page.locator('input[type=file]').setInputFiles(graphFile('scale.yaml', stringify(scaleGraph())))
  await expect(page.locator('.react-flow__node')).toHaveCount(50)
  await expect(page.locator('.react-flow__edge')).toHaveCount(100)

  // at fit view zoom not every node of 50 is inside the canvas; use the ones that are
  const visible = await visibleNodes(page)
  expect(visible.length, `visible nodes: ${visible}`).toBeGreaterThanOrEqual(RUNS)

  const selections: number[] = []
  for (const instance of visible.slice(0, RUNS)) {
    const node = page.locator(`.react-flow__node[data-id="${instance}"]`)
    selections.push(await measure(page, { kind: 'selected', instance }, () => node.click({ position: { x: 20, y: 12 } })))
  }

  // connect: a visible consumer s<k>-<j> drags its cap-(k+1) need to a visible provider of
  // cap-(k+1) other than the current one, alternating between two providers
  const pair = visible
    .map((consumer) => {
      const k = Number(consumer.slice(1, consumer.indexOf('-')))
      const cap = `cap-${(k + 1) % 10}`
      const providers = visible.filter((v) => v.startsWith(`s${(k + 1) % 10}-`))
      return { consumer, cap, providers }
    })
    .find((c) => c.providers.length >= 2)
  if (!pair) throw new Error(`no visible consumer with two visible providers among ${visible}`)
  const connects: number[] = []
  for (let i = 0; i < RUNS; i++) {
    const provider = pair.providers[i % 2] ?? ''
    connects.push(
      await measure(page, { kind: 'edge', id: `${pair.consumer}.${pair.cap}`, from: provider }, () =>
        drag(page, handle(page, pair.consumer, `uses:${pair.cap}`), handle(page, provider, `provides:${pair.cap}`)),
      ),
    )
  }

  test.info().annotations.push({
    type: 'timings',
    description: `selection ${selections.map(Math.round)} ms, connect ${connects.map(Math.round)} ms`,
  })
  expect(median(selections), `selection medians of ${selections.map(Math.round)}`).toBeLessThan(BUDGET_MS)
  expect(median(connects), `connect medians of ${connects.map(Math.round)}`).toBeLessThan(BUDGET_MS)
})
