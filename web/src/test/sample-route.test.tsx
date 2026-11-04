import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Graph, PlanResult } from '../api/client'
import { routeTree } from '../routeTree.gen'
import { testCatalog, testModule } from './fixtures'
import { fetchHandler } from './setup'

// the editor only needs names and ports from the catalog here
const entry = (name: string, image: string, more: Parameters<typeof testModule>[1]) => testModule(name, { ...more, image })
const catalog = testCatalog([
  entry('postgres', 'postgres:18.6', { provides: { postgresql: { port: 5432 } } }),
  entry('whoami', 'traefik/whoami:v1.12.0', { provides: { http: { port: 80 } }, uses: { postgresql: { optional: true } } }),
])

function renderAt(path: string) {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

describe('sample route', () => {
  let calls: string[]
  beforeEach(() => {
    calls = []
    fetchHandler.current = async (req) => {
      const url = new URL(req.url)
      calls.push(`${req.method} ${url.pathname}`)
      if (url.pathname === '/planner/v1/catalog') return Response.json(catalog)
      if (url.pathname === '/planner/v1/plan') {
        const graph = (await req.json()) as Graph
        const plan: PlanResult = {
          ok: true,
          graph,
          bindings: [
            {
              consumer: 'whoami',
              slot: 'postgresql',
              capability: 'postgresql',
              provider: 'postgres',
              provide: 'postgresql',
              auto: false,
              named: false,
              provisioned: false,
            },
          ],
          problems: [],
        }
        return Response.json(plan)
      }
      return new Response('not found', { status: 404 })
    }
  })

  // AC-12: two modules, whoami left of postgres, one edge with its marker, no browser storage
  it('shows the sample with one edge', async () => {
    const storage = vi.fn(() => undefined)
    Object.defineProperty(globalThis, 'indexedDB', { get: storage, configurable: true })
    const { container } = renderAt('/samples/whoami-postgres')

    const whoami = await screen.findByTestId('node-whoami')
    const postgres = screen.getByTestId('node-postgres')
    const x = (el: HTMLElement) =>
      Number(el.closest<HTMLElement>('.react-flow__node')!.style.transform.match(/translate\((-?[\d.]+)px/)![1])
    expect(x(whoami)).toBeLessThan(x(postgres))

    await vi.waitFor(() => expect(container.querySelectorAll('.react-flow__edge')).toHaveLength(1))
    const edge = container.querySelector('.react-flow__edge')!
    expect(edge.getAttribute('data-id') ?? edge.getAttribute('data-testid')).toContain('whoami.postgresql')
    expect(edge.querySelector('path[marker-end]')).not.toBeNull()

    expect(calls.sort()).toEqual(['GET /planner/v1/catalog', 'POST /planner/v1/plan'])
    expect(storage).not.toHaveBeenCalled()
  })

  it('exposes the graph canvas as a region named after the graph', async () => {
    renderAt('/samples/whoami-postgres')
    expect(await screen.findByRole('region', { name: 'Graph whoami-postgres' })).toBeTruthy()
  })

  it('names an unknown sample', async () => {
    renderAt('/samples/nope')
    expect(await screen.findByRole('heading', { name: 'No sample named nope' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Go to the editor' })).toBeTruthy()
  })
})
