import createClient from 'openapi-fetch'
import { apiRoot } from '../lib/config'
import type { components, paths } from './planner.gen'

export type Graph = components['schemas']['Graph']
export type GraphModule = components['schemas']['GraphModule']
export type CatalogEntry = components['schemas']['CatalogEntry']
export type PlanResult = components['schemas']['PlanResult']
export type Problem = components['schemas']['Problem']

// absolute on the page's own origin, so every call stays same origin under any base path
const client = createClient<paths>({ baseUrl: new URL(apiRoot, window.location.origin).href })

/** A failed planner call, with the message the editor shows as is. */
export class PlannerError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
  }
}

function failure(status: number, problems?: Problem[]): PlannerError {
  if (status === 429) return new PlannerError('The planner is busy, try again in a minute.', status)
  const message = problems?.[0]?.message ?? `The planner answered ${status}.`
  return new PlannerError(message, status)
}

export async function fetchCatalog(): Promise<CatalogEntry[]> {
  const { data, error, response } = await client.GET('/v1/catalog')
  if (!data) throw failure(response.status, error?.problems)
  return data.entries
}

export async function planGraph(graph: Graph): Promise<PlanResult> {
  const { data, error, response } = await client.POST('/v1/plan', { body: graph })
  if (!data) throw failure(response.status, error?.problems)
  return data
}

/** Renders the graph; a plan with an error comes back as the PlanResult instead of a zip. */
export async function renderGraph(graph: Graph): Promise<{ zip: Blob } | { plan: PlanResult }> {
  const { data, error, response } = await client.POST('/v1/render', { body: graph, parseAs: 'blob' })
  if (data) return { zip: data }
  if (response.status === 422) return { plan: error as PlanResult }
  throw failure(response.status, (error as { problems?: Problem[] } | undefined)?.problems)
}
