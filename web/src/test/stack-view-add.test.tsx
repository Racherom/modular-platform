import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CheckResult, Graph } from '../api/client'
import type { Session, Stack } from '../api/platform'
import { useEditor } from '../lib/store'
import { routeTree } from '../routeTree.gen'
import { testCatalog, testModule } from './fixtures'
import { fetchHandler } from './setup'

// Spec 0029 AC-1: "The managed stack view has no Add." Only managed mode has a stack view, so the
// editor runs here as the managed platform serves it.
vi.mock('../lib/config', async (actual) => ({
  ...(await actual<typeof import('../lib/config')>()),
  mode: 'managed',
}))
vi.mock('../lib/storage', async (actual) => ({
  ...(await actual<typeof import('../lib/storage')>()),
  loadWorking: () => Promise.resolve(undefined),
  saveWorking: () => Promise.resolve(),
}))

// jsdom has no EventSource; the stack view follows the event stream with one (useEvents)
class EventSourceStub {
  addEventListener() {}
  close() {}
}
globalThis.EventSource ??= EventSourceStub as unknown as typeof EventSource

const catalog = testCatalog([
  testModule('postgres', { provides: { postgresql: { port: 5432 } } }),
  testModule('whoami', { provides: { http: { port: 80 } }, uses: { postgresql: { optional: true } } }),
])
const graph: Graph = {
  apiVersion: 'modular-platform/v1',
  kind: 'Graph',
  name: 'demo',
  modules: { whoami: { module: 'whoami', uses: { postgresql: 'postgres' } }, postgres: { module: 'postgres' } },
}
const session: Session = {
  email: 'admin@example.test',
  expiresAt: '2099-01-01T00:00:00Z',
  idleExpiresAt: '2099-01-01T00:00:00Z',
}
const stack: Stack = {
  name: 'demo',
  state: 'running',
  composeProject: 'mp-demo',
  createdAt: '2026-11-11T00:00:00Z',
  graph,
  modules: [
    { instance: 'whoami', module: 'whoami', state: 'running', restartCount: 0 },
    { instance: 'postgres', module: 'postgres', state: 'running', restartCount: 0 },
  ],
}
const ok = (g: Graph): CheckResult => ({ ok: true, graph: g, bindings: [], problems: [] })

function renderAt(path: string) {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

describe('managed mode Add (spec 0029, AC-1)', () => {
  let calls: string[]
  beforeEach(() => {
    calls = []
    useEditor.setState({ working: undefined, history: { past: [], future: [] }, selection: null, announcement: null })
    fetchHandler.current = async (req) => {
      const url = new URL(req.url)
      calls.push(`${req.method} ${url.pathname}`)
      if (url.pathname === '/api/v1/session') return Response.json(session)
      if (url.pathname === '/api/v1/stacks/demo') return Response.json(stack)
      if (url.pathname === '/api/v1/stacks/demo/operations') return Response.json({ items: [] })
      if (url.pathname === '/renderer/v1/catalog') return Response.json(catalog)
      if (url.pathname === '/renderer/v1/check') return Response.json(ok((await req.json()) as Graph))
      return new Response('not found', { status: 404 })
    }
  })

  it('the stack view draws the graph of the stack with the catalog loaded, and has no Add', async () => {
    renderAt('/stacks/demo')
    await screen.findByTestId('node-whoami')
    expect(screen.getByTestId('node-postgres')).toBeTruthy()
    expect(screen.getByRole('heading', { level: 1, name: 'demo' })).toBeTruthy()
    // the catalog was there to draw the nodes, so a missing Add is the view's rule, not a load gap
    expect(calls).toContain('GET /renderer/v1/catalog')
    expect(screen.queryByRole('button', { name: /^Add / })).toBeNull()
    expect(screen.queryByRole('searchbox', { name: 'Filter the catalog' })).toBeNull()
  })

  it('the editor in managed mode still has Add on every catalog entry', async () => {
    renderAt('/')
    expect(await screen.findByRole('button', { name: 'Add postgres' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Add whoami' })).toBeTruthy()
  })
})
