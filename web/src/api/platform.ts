import createClient from 'openapi-fetch'
import { basePath } from '../lib/config'
import type { components, paths } from './api.gen'
import type { Graph, Problem } from './client'

export type Session = components['schemas']['Session']
export type Stack = components['schemas']['Stack']
export type StackSummary = components['schemas']['StackSummary']
export type StackState = components['schemas']['StackState']
export type Operation = components['schemas']['Operation']
export type ModuleState = components['schemas']['ModuleState']
export type Review = components['schemas']['Review']
export type PlacedInput = components['schemas']['PlacedInput']

// the managed platform's api, on the page's own origin under the base path; its paths start with /api/v1
const client = createClient<paths>({ baseUrl: new URL(basePath || '/', window.location.origin).href.replace(/\/$/, '') })

/**
 * A failed api call, with the first problem's code the editor branches on, its message, and every
 * problem of a refusal (a 422 can name several, such as one host_access per instance and key).
 */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
    readonly problems: Problem[] = [],
  ) {
    super(message)
  }
}

function failure(status: number, error: unknown): ApiError {
  const problems = (error as { problems?: Problem[] } | undefined)?.problems ?? []
  const first = problems[0]
  return new ApiError(first?.message ?? `The platform answered ${status}.`, status, first?.code, problems)
}

/** True for a refusal (422) that lists its problems, which the editor shows in full. */
export function isRefusal(error: unknown): error is ApiError {
  return error instanceof ApiError && error.status === 422 && error.problems.length > 0
}

/** True for the answer of a call made without a valid session. */
export function isSignedOut(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401
}

/** Where the browser goes to sign in, coming back to returnTo (a path under the base). */
export function loginURL(returnTo?: string): string {
  const query = returnTo ? `?return_to=${encodeURIComponent(returnTo)}` : ''
  return `${basePath}/api/v1/auth/login${query}`
}

export async function fetchSession(): Promise<Session> {
  const { data, error, response } = await client.GET('/api/v1/session')
  if (!data) throw failure(response.status, error)
  return data
}

/** Signs out and answers where the browser goes next. */
export async function logout(): Promise<string> {
  const response = await fetch(`${basePath}/api/v1/auth/logout`, { method: 'POST', credentials: 'same-origin' })
  if (!response.ok) throw failure(response.status, await response.json().catch(() => undefined))
  return ((await response.json()) as { redirect: string }).redirect
}

/** One page of a list, and the cursor of the next one when there is more. */
export type Page<T> = { items: T[]; next?: string }

/** How many rows one page of the stacks list or a history holds. */
export const pageSize = 50

/** One page of stacks sorted by name, after the name cursor. */
export async function fetchStacks(cursor?: string): Promise<Page<StackSummary>> {
  const { data, error, response } = await client.GET('/api/v1/stacks', {
    params: { query: { limit: pageSize, ...(cursor ? { cursor } : {}) } },
  })
  if (!data) throw failure(response.status, error)
  return { items: data.items, ...(data.next ? { next: data.next } : {}) }
}

export async function fetchStack(name: string): Promise<Stack> {
  const { data, error, response } = await client.GET('/api/v1/stacks/{name}', { params: { path: { name } } })
  if (!data) throw failure(response.status, error)
  return data
}

/** One page of a stack's history, newest first, older than the operation cursor. */
export async function fetchOperations(name: string, cursor?: string): Promise<Page<Operation>> {
  const { data, error, response } = await client.GET('/api/v1/stacks/{name}/operations', {
    params: { path: { name }, query: { limit: pageSize, ...(cursor ? { cursor } : {}) } },
  })
  if (!data) throw failure(response.status, error)
  return { items: data.items, ...(data.next ? { next: data.next } : {}) }
}

export async function reviewStack(graph: Graph): Promise<Review> {
  const { data, error, response } = await client.POST('/api/v1/stacks/{name}/review', {
    params: { path: { name: graph.name } },
    body: { graph },
  })
  if (!data) throw failure(response.status, error)
  return data
}

export async function applyStack(graph: Graph, digest: string, placed: PlacedInput[]): Promise<Operation> {
  const { data, error, response } = await client.POST('/api/v1/stacks/{name}/apply', {
    params: { path: { name: graph.name } },
    body: { graph, digest, placed },
  })
  if (!data) throw failure(response.status, error)
  return data
}

/** Removes a stack's containers; with deleteData its volumes and secrets too, confirmed by its name. */
export async function removeStack(name: string, deleteData: boolean, confirmName?: string): Promise<Operation> {
  const { data, error, response } = await client.POST('/api/v1/stacks/{name}/remove', {
    params: { path: { name } },
    body: deleteData ? { deleteData, confirmName } : { deleteData },
  })
  if (!data) throw failure(response.status, error)
  return data
}

/** The event stream, for the whole list or one stack. */
export function eventsURL(stack?: string): string {
  const query = stack ? `?stack=${encodeURIComponent(stack)}` : ''
  return `${basePath}/api/v1/events${query}`
}
