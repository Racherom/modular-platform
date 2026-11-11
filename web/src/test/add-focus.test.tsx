import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useMemo } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CheckResult, Graph } from '../api/client'
import { NODE_WIDTH, nodeHeight } from '../graph/flow'
import { ROW_GAP } from '../graph/layout'
import { EMPTY_GRAPH, useEditor } from '../lib/store'
import { routeTree } from '../routeTree.gen'
import { testCatalog, testModule } from './fixtures'
import { fetchHandler } from './setup'

const flow = vi.hoisted(() => ({ setCenter: vi.fn(() => Promise.resolve(true)), zoom: 1 }))
vi.mock('@xyflow/react', async (actual) => {
  const real = await actual<typeof import('@xyflow/react')>()
  return {
    ...real,
    // the same instance with setCenter and getZoom swapped; memoized so effects that depend on it do not rerun
    useReactFlow: (...args: Parameters<typeof real.useReactFlow>) => {
      const instance = real.useReactFlow(...args)
      return useMemo(() => ({ ...instance, setCenter: flow.setCenter, getZoom: () => flow.zoom }), [instance])
    },
  }
})
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
const s = () => useEditor.getState()

function renderEditor() {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ['/'] }) })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

const add = async (name: string) => fireEvent.click(await screen.findByRole('button', { name: `Add ${name}` }))
const nodeEl = (instance: string) => document.querySelector<HTMLElement>(`.react-flow__node[data-id="${instance}"]`)
// the header status note, not the catalog count or other live regions
const status = () => document.querySelector('[role="status"][class*="_note_"]')?.textContent

function reduceMotion(on: boolean) {
  window.matchMedia = ((query: string) => ({
    matches: on && query.includes('prefers-reduced-motion'),
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    addListener: () => undefined,
    removeListener: () => undefined,
    dispatchEvent: () => false,
    onchange: null,
  })) as unknown as typeof window.matchMedia
}

beforeEach(() => {
  flow.setCenter.mockClear()
  flow.zoom = 1
  useEditor.setState({
    working: undefined,
    history: { past: [], future: [] },
    selection: null,
    announcement: null,
    centerRequest: null,
    focusRequest: null,
    catalogFilter: '',
  })
  fetchHandler.current = async (req) => {
    const url = new URL(req.url)
    if (url.pathname === '/renderer/v1/catalog') return Response.json(catalog)
    if (url.pathname === '/renderer/v1/check') return Response.json(ok((await req.json()) as Graph))
    return new Response('not found', { status: 404 })
  }
})
afterEach(() => {
  reduceMotion(false)
})

describe('focus and the status line after an add (AC-8)', () => {
  it('focuses the new node, also the very first one, and says what was added', async () => {
    renderEditor()
    await add('postgres')
    await waitFor(() => expect(document.activeElement).toBe(nodeEl('postgres')))
    expect(nodeEl('postgres')?.getAttribute('aria-selected') ?? 'true').not.toBe('false')
    expect(status()).toBe('Added postgres')
    await add('postgres')
    await waitFor(() => expect(document.activeElement).toBe(nodeEl('postgres-2')))
    expect(status()).toBe('Added postgres-2')
    expect(s().focusRequest).toBeNull()
  })

  it('works from the keyboard: Tab order reaches Add and Enter on the button adds', async () => {
    renderEditor()
    const button = await screen.findByRole('button', { name: 'Add whoami' })
    button.focus()
    expect(document.activeElement).toBe(button)
    fireEvent.keyDown(button, { key: 'Enter' })
    fireEvent.keyUp(button, { key: 'Enter' })
    await waitFor(() => expect(nodeEl('whoami')).not.toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(nodeEl('whoami')))
    expect(status()).toBe('Added whoami')
  })

  it('keeps the filter text and Add keeps working for the next module', async () => {
    renderEditor()
    const filter = await screen.findByRole('searchbox', { name: 'Filter the catalog' })
    fireEvent.change(filter, { target: { value: 'postgres' } })
    await add('postgres')
    await waitFor(() => expect(nodeEl('postgres')).not.toBeNull())
    expect((screen.getByRole('searchbox', { name: 'Filter the catalog' }) as HTMLInputElement).value).toBe('postgres')
    expect(s().catalogFilter).toBe('postgres')
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filter the catalog' }), { target: { value: '' } })
    await add('whoami')
    await waitFor(() => expect(nodeEl('whoami')).not.toBeNull())
  })

  it('says it again after an undo and an add, and the line is empty in between', async () => {
    renderEditor()
    await add('postgres')
    expect(status()).toBe('Added postgres')
    await add('whoami')
    fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await waitFor(() => expect(status()).toBe(''))
    await add('whoami')
    await waitFor(() => expect(status()).toBe('Added whoami'))
  })

  it('a second press on the same module is heard as the new name, not left as the old line', async () => {
    renderEditor()
    await add('postgres')
    await add('postgres')
    expect(status()).toBe('Added postgres-2')
  })
})

describe('centering on the new node (AC-8)', () => {
  it('centers on the stored position and the known size of the node, once, at zoom 1 or more', async () => {
    renderEditor()
    await add('postgres')
    await waitFor(() => expect(flow.setCenter).toHaveBeenCalledTimes(1))
    expect(flow.setCenter).toHaveBeenCalledWith(NODE_WIDTH / 2, nodeHeight(1, 0) / 2, { zoom: 1, duration: 200 })
    await waitFor(() => expect(s().centerRequest).toBeNull())
    expect(flow.setCenter).toHaveBeenCalledTimes(1)
  })

  it('uses the second node position, one row below the first', async () => {
    renderEditor()
    await add('postgres')
    await waitFor(() => expect(flow.setCenter).toHaveBeenCalledTimes(1))
    await add('postgres')
    await waitFor(() => expect(flow.setCenter).toHaveBeenCalledTimes(2))
    const y = nodeHeight(1, 0) + ROW_GAP
    expect(flow.setCenter).toHaveBeenLastCalledWith(NODE_WIDTH / 2, y + nodeHeight(1, 0) / 2, { zoom: 1, duration: 200 })
  })

  it('keeps a zoom above 1 and lifts a zoom below 1 to 1', async () => {
    renderEditor()
    flow.zoom = 1.75
    await add('postgres')
    await waitFor(() => expect(flow.setCenter).toHaveBeenCalledTimes(1))
    expect(flow.setCenter).toHaveBeenLastCalledWith(expect.any(Number), expect.any(Number), expect.objectContaining({ zoom: 1.75 }))
    flow.zoom = 0.4
    await add('postgres')
    await waitFor(() => expect(flow.setCenter).toHaveBeenCalledTimes(2))
    expect(flow.setCenter).toHaveBeenLastCalledWith(expect.any(Number), expect.any(Number), expect.objectContaining({ zoom: 1 }))
  })

  it('does not animate under prefers-reduced-motion', async () => {
    reduceMotion(true)
    renderEditor()
    await add('postgres')
    await waitFor(() => expect(flow.setCenter).toHaveBeenCalledTimes(1))
    expect(flow.setCenter).toHaveBeenCalledWith(expect.any(Number), expect.any(Number), { zoom: 1, duration: 0 })
  })

  it('undo and redo do not center', async () => {
    renderEditor()
    await add('postgres')
    await waitFor(() => expect(s().centerRequest).toBeNull())
    flow.setCenter.mockClear()
    act(() => s().undo())
    act(() => s().redo())
    await new Promise((r) => setTimeout(r, 50))
    expect(flow.setCenter).not.toHaveBeenCalled()
  })

  it('a request for a node that is gone is cleared and centers nothing', async () => {
    renderEditor()
    await add('postgres')
    await waitFor(() => expect(s().centerRequest).toBeNull())
    flow.setCenter.mockClear()
    act(() => useEditor.setState({ centerRequest: 'nobody' }))
    await waitFor(() => expect(s().centerRequest).toBeNull())
    expect(flow.setCenter).not.toHaveBeenCalled()
  })
})

describe('undo and redo move focus (AC-5)', () => {
  const press = (name: string) => {
    const button = screen.getByRole('button', { name })
    button.focus()
    fireEvent.click(button)
    return button
  }

  it('Undo pressed with the button: focus stays on the Undo button, the selection is gone', async () => {
    renderEditor()
    await add('postgres')
    await add('whoami')
    await waitFor(() => expect(document.activeElement).toBe(nodeEl('whoami')))
    const undo = press('Undo')
    await waitFor(() => expect(nodeEl('whoami')).toBeNull())
    expect(s().selection).toBeNull()
    await new Promise((r) => setTimeout(r, 50))
    expect(document.activeElement).toBe(undo)
  })

  it('Undo by the shortcut: focus moves to the canvas region', async () => {
    renderEditor()
    await add('postgres')
    await waitFor(() => expect(document.activeElement).toBe(nodeEl('postgres')))
    fireEvent.keyDown(document.activeElement as Element, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(nodeEl('postgres')).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('region', { name: `Graph ${EMPTY_GRAPH.name}` })))
    expect(s().selection).toBeNull()
  })

  it('Redo selects the node again and moves focus to it', async () => {
    renderEditor()
    await add('postgres')
    await add('whoami')
    press('Undo')
    await waitFor(() => expect(nodeEl('whoami')).toBeNull())
    const redo = press('Redo')
    await waitFor(() => expect(nodeEl('whoami')).not.toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(nodeEl('whoami')))
    expect(document.activeElement).not.toBe(redo)
    expect(s().selection).toEqual({ kind: 'module', instance: 'whoami' })
  })

  it('Redo by the shortcut also lands on the node', async () => {
    renderEditor()
    await add('postgres')
    await add('whoami')
    fireEvent.keyDown(document.body, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(nodeEl('whoami')).toBeNull())
    fireEvent.keyDown(document.body, { key: 'z', ctrlKey: true, shiftKey: true })
    await waitFor(() => expect(document.activeElement).toBe(nodeEl('whoami')))
  })

  it('the shortcut does nothing while focus is in the filter field', async () => {
    renderEditor()
    await add('postgres')
    await waitFor(() => expect(document.activeElement).toBe(nodeEl('postgres')))
    const filter = screen.getByRole('searchbox', { name: 'Filter the catalog' })
    filter.focus()
    fireEvent.keyDown(filter, { key: 'z', ctrlKey: true })
    await new Promise((r) => setTimeout(r, 50))
    expect(nodeEl('postgres')).not.toBeNull()
    expect(document.activeElement).toBe(filter)
  })
})
