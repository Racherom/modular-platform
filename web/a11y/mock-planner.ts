import { readFileSync } from 'node:fs'
import type { Page, Route } from '@playwright/test'
import type { Catalog, Graph, PlanResult, Problem } from '../src/api/client'
import { bindingText, splitText } from '../src/graph/model'

/** The catalog every test sees: built in modules plus test modules, each provide named like its capability. */
export const catalog: Catalog = JSON.parse(readFileSync(new URL('./fixtures/catalog.json', import.meta.url), 'utf8')) as Catalog

/** How the mock answers one plan call. */
export type PlanReply = { status: number } | { delayMs: number } | 'echo'

/**
 * A planner stand in. GET /v1/catalog answers the fixture. POST /v1/plan echoes the posted
 * graph, builds bindings from its uses (auto false), and reports unbound_required for each
 * required slot set to null, so ok follows. `next` decides how each call is answered, in order;
 * `entries` replaces the catalog, as the scale check does.
 */
export async function mockPlanner(page: Page, next: () => PlanReply = () => 'echo', entries: Catalog = catalog) {
  const plans: Graph[] = []
  await page.route('**/planner/v1/catalog', (route) => route.fulfill({ json: entries }))
  await page.route('**/planner/v1/plan', async (route: Route) => {
    const graph = route.request().postDataJSON() as Graph
    plans.push(graph)
    const reply = next()
    if (typeof reply === 'object' && 'status' in reply) {
      await route.fulfill({
        status: reply.status,
        json: { problems: [{ severity: 'error', code: 'mock', path: '', message: 'Mock failure.' }] },
      })
      return
    }
    if (typeof reply === 'object') await new Promise((r) => setTimeout(r, reply.delayMs))
    await route.fulfill({ json: plan(graph, entries) }).catch(() => undefined) // an aborted call has no one to answer
  })
  return { plans }
}

function plan(graph: Graph, catalog: Catalog): PlanResult {
  const modules = new Map(catalog.modules.map((e) => [e.name, e]))
  const bindings: PlanResult['bindings'] = []
  const problems: Problem[] = []
  for (const [consumer, m] of Object.entries(graph.modules)) {
    const slots = (m.module === undefined ? undefined : modules.get(m.module)?.uses) ?? {}
    for (const [slot, b] of Object.entries(m.uses ?? {})) {
      const text = bindingText(b)
      if (text === undefined) continue
      const { instance, provide } = splitText(text)
      const capability = slots[slot]?.capability ?? slot
      bindings.push({
        consumer,
        slot,
        capability,
        provider: instance,
        provide: provide || capability,
        auto: false,
        named: false,
        provisioned: false,
      })
    }
    for (const [slot, need] of Object.entries(slots)) {
      if (!need.optional && m.uses?.[slot] === null) {
        problems.push({
          severity: 'error',
          code: 'unbound_required',
          path: `modules.${consumer}.uses.${slot}`,
          message: `${consumer} needs ${slot}, and it is not bound.`,
        })
      }
    }
  }
  return { ok: problems.length === 0, graph, bindings, problems }
}

/** A graph file to open, as Playwright's setInputFiles takes it. */
export function graphFile(name: string, text: string) {
  return { name, mimeType: 'application/yaml', buffer: Buffer.from(text) }
}
