import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CheckResult, Graph } from '../api/client'
import { CatalogPanel } from '../components/CatalogPanel'
import { downloadBlocked } from '../components/GraphScreen'
import { EMPTY_GRAPH, useEditor } from '../lib/store'
import { routeTree } from '../routeTree.gen'
import { testCatalog, testModule } from './fixtures'
import { fetchHandler } from './setup'

vi.mock('../lib/storage', async (actual) => ({
  ...(await actual<typeof import('../lib/storage')>()),
  loadWorking: () => Promise.resolve(undefined),
  saveWorking: () => Promise.resolve(),
}))

const catalog = testCatalog([
  testModule('postgres', { provides: { postgresql: { port: 5432 } } }),
  testModule('whoami', { provides: { http: { port: 80 } }, uses: { postgresql: { optional: true } } }),
])
const ok = (graph: Graph): CheckResult => ({ ok: true, graph, bindings: [], problems: [] })

function renderAt(path: string) {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

describe('Add button (AC-1)', () => {
  it('is named after the module and calls onAdd; without onAdd there is none', () => {
    const onAdd = vi.fn()
    const { unmount } = render(<CatalogPanel entries={catalog.modules} onAdd={onAdd} />)
    fireEvent.click(screen.getByRole('button', { name: 'Add postgres' }))
    expect(onAdd).toHaveBeenCalledWith('postgres')
    unmount()
    render(<CatalogPanel entries={catalog.modules} />)
    expect(screen.queryByRole('button', { name: /^Add / })).toBeNull()
  })
})

describe('downloadBlocked (AC-10)', () => {
  it('names the empty graph before every other reason', () => {
    expect(downloadBlocked('failed', ok(EMPTY_GRAPH), 0)).toBe('Add a module first')
    expect(downloadBlocked('current', ok(EMPTY_GRAPH), 1)).toBeNull()
  })
})

describe('start from nothing (AC-8, AC-9, AC-10)', () => {
  beforeEach(() => {
    useEditor.setState({ working: undefined, history: { past: [], future: [] }, selection: null, announcement: null })
    fetchHandler.current = async (req) => {
      const url = new URL(req.url)
      if (url.pathname === '/renderer/v1/catalog') return Response.json(catalog)
      if (url.pathname === '/renderer/v1/check') return Response.json(ok((await req.json()) as Graph))
      return new Response('not found', { status: 404 })
    }
  })

  it('shows the editor on an empty graph, then the first add keeps the canvas', async () => {
    renderAt('/')
    expect(await screen.findByText('No graph open')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Download zip' }).hasAttribute('disabled')).toBe(true)
    expect(screen.getByText('Add a module first')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Save as file' }).hasAttribute('disabled')).toBe(true)
    expect(screen.getByRole('link', { name: 'Start from the sample' })).toBeTruthy()

    const canvas = screen.getByRole('region', { name: 'Graph untitled' })
    fireEvent.click(await screen.findByRole('button', { name: 'Add postgres' }))
    expect(await screen.findByTestId('node-postgres')).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Graph untitled' })).toBe(canvas)
    expect(screen.queryByText('No graph open')).toBeNull()
    expect(screen.getByText('Added postgres')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Save as file' }).hasAttribute('disabled')).toBe(false)
  })
})

describe('Add on the sample (AC-11)', () => {
  beforeEach(() => {
    useEditor.setState({ working: undefined, history: { past: [], future: [] }, selection: null, announcement: null, handoff: null })
    fetchHandler.current = async (req) => {
      const url = new URL(req.url)
      if (url.pathname === '/renderer/v1/catalog') return Response.json(catalog)
      if (url.pathname === '/renderer/v1/check') return Response.json(ok((await req.json()) as Graph))
      return new Response('not found', { status: 404 })
    }
  })

  it('makes a copy with the module, one undo from the unedited copy, and lands in the editor', async () => {
    renderAt('/samples/whoami-postgres')
    fireEvent.click(await screen.findByRole('button', { name: 'Add postgres' }))
    await vi.waitFor(() =>
      expect(Object.keys(useEditor.getState().working?.graph.modules ?? {}).sort()).toEqual(['postgres', 'postgres-2', 'whoami']),
    )
    const s = useEditor.getState()
    expect(s.history.past).toHaveLength(1)
    expect(Object.keys(s.history.past[0]?.graph.modules ?? {}).sort()).toEqual(['postgres', 'whoami'])
    expect(await screen.findByRole('region', { name: /^Graph / })).toBeTruthy()
    expect(await screen.findByText('Added postgres-2')).toBeTruthy()
  })
})
