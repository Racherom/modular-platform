import createClient from 'openapi-fetch'
import { basePath } from '../lib/config'
import type { components, paths } from './api.gen'
import type { Graph } from './client'

export type Session = components['schemas']['Session']
export type Deployment = components['schemas']['Deployment']
export type DeploymentSummary = components['schemas']['DeploymentSummary']
export type DeploymentState = components['schemas']['DeploymentState']
export type Operation = components['schemas']['Operation']
export type ModuleState = components['schemas']['ModuleState']
export type Review = components['schemas']['Review']
export type PlacedInput = components['schemas']['PlacedInput']
type Problem = { code: string; message: string }

// the managed platform's api, on the page's own origin under the base path; its paths start with /api/v1
const client = createClient<paths>({ baseUrl: new URL(basePath || '/', window.location.origin).href.replace(/\/$/, '') })

/** A failed api call, with the problem code the editor branches on and the message it shows. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
  ) {
    super(message)
  }
}

function failure(status: number, error: unknown): ApiError {
  const problem = (error as { problems?: Problem[] } | undefined)?.problems?.[0]
  return new ApiError(problem?.message ?? `The platform answered ${status}.`, status, problem?.code)
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

export async function fetchDeployments(): Promise<DeploymentSummary[]> {
  const { data, error, response } = await client.GET('/api/v1/deployments', { params: { query: { limit: 200 } } })
  if (!data) throw failure(response.status, error)
  return data.items
}

export async function fetchDeployment(name: string): Promise<Deployment> {
  const { data, error, response } = await client.GET('/api/v1/deployments/{name}', { params: { path: { name } } })
  if (!data) throw failure(response.status, error)
  return data
}

export async function fetchOperations(name: string): Promise<Operation[]> {
  const { data, error, response } = await client.GET('/api/v1/deployments/{name}/operations', {
    params: { path: { name } },
  })
  if (!data) throw failure(response.status, error)
  return data.items
}

export async function reviewDeployment(graph: Graph): Promise<Review> {
  const { data, error, response } = await client.POST('/api/v1/deployments/{name}/review', {
    params: { path: { name: graph.name } },
    body: { graph },
  })
  if (!data) throw failure(response.status, error)
  return data
}

export async function applyDeployment(graph: Graph, planDigest: string, placed: PlacedInput[]): Promise<Operation> {
  const { data, error, response } = await client.POST('/api/v1/deployments/{name}/apply', {
    params: { path: { name: graph.name } },
    body: { graph, planDigest, placed },
  })
  if (!data) throw failure(response.status, error)
  return data
}

/** The event stream, for the whole list or one deployment. */
export function eventsURL(deployment?: string): string {
  const query = deployment ? `?deployment=${encodeURIComponent(deployment)}` : ''
  return `${basePath}/api/v1/events${query}`
}
