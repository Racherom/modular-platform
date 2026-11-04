import { readFileSync } from 'node:fs'
import type { Page, Route } from '@playwright/test'
import type { CatalogEntry, Graph, PlanResult, Problem } from '../src/api/client'

/** The catalog every test sees: the built in entries plus test entries. */
export const catalog: CatalogEntry[] = (
  JSON.parse(readFileSync(new URL('./fixtures/catalog.json', import.meta.url), 'utf8')) as { entries: CatalogEntry[] }
).entries

/** How the mock answers one plan call. */
export type PlanReply = { status: number } | { delayMs: number } | 'echo'

/**
 * A planner stand in. GET /v1/catalog answers the fixture. POST /v1/plan echoes the posted
 * graph, builds bindings from its uses (auto false), and reports unbound_required for each
 * required need set to null, so ok follows. `next` decides how each call is answered, in order.
 */
export async function mockPlanner(page: Page, next: () => PlanReply = () => 'echo') {
  const plans: Graph[] = []
  await page.route('**/planner/v1/catalog', (route) => route.fulfill({ json: { entries: catalog } }))
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
    await route.fulfill({ json: plan(graph) }).catch(() => undefined) // an aborted call has no one to answer
  })
  return { plans }
}

function plan(graph: Graph): PlanResult {
  const entries = new Map(catalog.map((e) => [e.name, e]))
  const bindings: PlanResult['bindings'] = []
  const problems: Problem[] = []
  for (const [consumer, m] of Object.entries(graph.modules)) {
    for (const [capability, provider] of Object.entries(m.uses ?? {})) {
      if (provider !== null) bindings.push({ consumer, capability, provider, auto: false })
    }
    for (const [capability, need] of Object.entries(entries.get(m.module)?.uses ?? {})) {
      if (!need.optional && m.uses?.[capability] === null) {
        problems.push({
          severity: 'error',
          code: 'unbound_required',
          path: `modules.${consumer}.uses.${capability}`,
          message: `${consumer} needs ${capability}, and it is not bound.`,
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
