import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CheckResult, Graph } from '../api/client'
import { nodeHeight } from '../graph/flow'
import { layout, ROW_GAP } from '../graph/layout'
import { samples } from '../graph/samples'
import { parseGraph } from '../graph/yaml'
import type { WorkingGraph } from '../lib/storage'
import { useEditor } from '../lib/store'
import { routeTree } from '../routeTree.gen'
import { testCatalog, testModule } from './fixtures'
import { fetchHandler } from './setup'

const stored = vi.hoisted(() => ({ current: undefined as unknown, loads: 0 }))
const saveGraphFile = vi.hoisted(() => vi.fn())
vi.mock('../lib/storage', async (actual) => ({
  ...(await actual<typeof import('../lib/storage')>()),
  loadWorking: () => {
    stored.loads++
    return Promise.resolve(stored.current)
  },
  saveWorking: () => Promise.resolve(),
}))
vi.mock('../lib/download', async (actual) => ({ ...(await actual<typeof import('../lib/download')>()), saveGraphFile }))

const catalog = testCatalog([
  testModule('postgres', { provides: { postgresql: { port: 5432 } } }),
  testModule('whoami', { provides: { http: { port: 80 } }, uses: { postgresql: { optional: true } } }),
])
const ok = (graph: Graph): CheckResult => ({ ok: true, graph, bindings: [], problems: [] })
const s = () => useEditor.getState()
const names = () => Object.keys(s().working?.graph.modules ?? {}).sort()

const mine: Graph = {
  apiVersion: 'modular-platform/v1',
  kind: 'Graph',
  name: 'mine',
  modules: { only: { module: 'postgres' } },
}
const edited = (): WorkingGraph => ({
  fileName: 'mine.yaml',
  graph: mine,
  check: ok(mine),
  positions: { only: { x: 7, y: 9 } },
  edited: true,
})

function renderAt(path: string) {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return router
}

const addButton = (name: string) => screen.findByRole('button', { name: `Add ${name}` })
const dialog = () => screen.queryByRole('alertdialog', { name: 'Replace the edited graph?' })

describe('Add on the sample route (AC-11)', () => {
  let checks: Graph[]
  const sample = parseGraph(samples['whoami-postgres'] as string)
  beforeEach(() => {
    checks = []
    stored.current = undefined
    stored.loads = 0
    saveGraphFile.mockClear()
    useEditor.setState({
      working: undefined,
      history: { past: [], future: [] },
      selection: null,
      announcement: null,
      handoff: null,
      centerRequest: null,
      focusRequest: null,
      catalogFilter: '',
    })
    fetchHandler.current = async (req) => {
      const url = new URL(req.url)
      if (url.pathname === '/renderer/v1/catalog') return Response.json(catalog)
      if (url.pathname === '/renderer/v1/check') {
        const g = (await req.json()) as Graph
        checks.push(g)
        return Response.json(ok(g))
      }
      return new Response('not found', { status: 404 })
    }
  })

  it('reads browser storage only when Add is pressed, and makes the copy at once when nothing is edited', async () => {
    const router = renderAt('/samples/whoami-postgres')
    const button = await addButton('postgres')
    expect(stored.loads).toBe(0)
    expect(s().working).toBeUndefined()
    fireEvent.click(button)
    await waitFor(() => expect(router.state.location.pathname).toBe('/'))
    expect(stored.loads).toBe(1)
    expect(dialog()).toBeNull()
    expect(names()).toEqual(['postgres', 'postgres-2', 'whoami'])
    expect(s().working?.fileName).toBe('whoami-postgres.yaml')
    expect(s().working?.edited).toBe(true)
  })

  it('places the new node at x 0 below the sample layout, and moves no sample node', async () => {
    renderAt('/samples/whoami-postgres')
    fireEvent.click(await addButton('postgres'))
    await waitFor(() => expect(names()).toContain('postgres-2'))
    const base = layout(sample)
    const at = s().working?.positions ?? {}
    expect(at.whoami).toEqual(base.whoami)
    expect(at.postgres).toEqual(base.postgres)
    // whoami sits at x 0 with one provide and one slot, so the row below it is free
    expect(at['postgres-2']).toEqual({ x: 0, y: (base.whoami?.y ?? 0) + nodeHeight(1, 1) + ROW_GAP })
  })

  it('one undo returns to the unedited copy, with the sample positions', async () => {
    renderAt('/samples/whoami-postgres')
    fireEvent.click(await addButton('postgres'))
    await waitFor(() => expect(names()).toContain('postgres-2'))
    expect(s().history.past).toHaveLength(1)
    expect(s().history.future).toHaveLength(0)
    s().undo()
    expect(names()).toEqual(['postgres', 'whoami'])
    expect(s().working?.positions).toEqual(layout(sample))
    expect(s().history.past).toHaveLength(0)
  })

  it('sends one check call with the new instance and the sample never changes', async () => {
    renderAt('/samples/whoami-postgres')
    await screen.findByTestId('node-whoami')
    const before = checks.length
    fireEvent.click(await addButton('postgres'))
    await waitFor(() => expect(checks.length).toBe(before + 1))
    expect(Object.keys(checks.at(-1)?.modules ?? {}).sort()).toEqual(['postgres', 'postgres-2', 'whoami'])
    expect(Object.keys(sample.modules).sort()).toEqual(['postgres', 'whoami'])
  })

  it('lands in the editor with the new node selected, announced and centered, handoff used up', async () => {
    const router = renderAt('/samples/whoami-postgres')
    fireEvent.click(await addButton('postgres'))
    await waitFor(() => expect(router.state.location.pathname).toBe('/'))
    expect(await screen.findByText('Added postgres-2')).toBeTruthy()
    expect(s().selection).toEqual({ kind: 'module', instance: 'postgres-2' })
    await waitFor(() => expect(document.querySelector('.react-flow__node[data-id="postgres-2"]')).not.toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(document.querySelector('.react-flow__node[data-id="postgres-2"]')))
    expect(s().handoff).toBeNull()
    await waitFor(() => expect(s().centerRequest).toBeNull())
  })

  describe('with an edited working graph', () => {
    beforeEach(() => {
      stored.current = edited()
    })

    it('holds the add behind the replace Dialog: nothing changes until it is answered', async () => {
      const router = renderAt('/samples/whoami-postgres')
      fireEvent.click(await addButton('postgres'))
      expect(await screen.findByRole('alertdialog', { name: 'Replace the edited graph?' })).toBeTruthy()
      expect(screen.getByText('mine.yaml')).toBeTruthy()
      expect(names()).toEqual(['only'])
      expect(s().history.past).toHaveLength(0)
      expect(router.state.location.pathname).toBe('/samples/whoami-postgres')
      expect(screen.queryByTestId('node-postgres-2')).toBeNull()
    })

    it('drops a second Add while the Dialog is open', async () => {
      renderAt('/samples/whoami-postgres')
      fireEvent.click(await addButton('postgres'))
      await screen.findByRole('alertdialog')
      fireEvent.click(screen.getByRole('button', { name: 'Add whoami', hidden: true }))
      fireEvent.click(screen.getByRole('button', { name: 'Add postgres', hidden: true }))
      fireEvent.click(screen.getByRole('button', { name: 'Replace' }))
      await waitFor(() => expect(names()).toContain('postgres-2'))
      expect(names()).toEqual(['postgres', 'postgres-2', 'whoami'])
      expect(s().history.past).toHaveLength(1)
    })

    it('Replace drops the edited graph and makes the copy with the module', async () => {
      const router = renderAt('/samples/whoami-postgres')
      fireEvent.click(await addButton('whoami'))
      fireEvent.click(await screen.findByRole('button', { name: 'Replace' }))
      await waitFor(() => expect(router.state.location.pathname).toBe('/'))
      expect(names()).toEqual(['postgres', 'whoami', 'whoami-2'])
      expect(s().working?.fileName).toBe('whoami-postgres.yaml')
      expect(saveGraphFile).not.toHaveBeenCalled()
      expect(s().selection).toEqual({ kind: 'module', instance: 'whoami-2' })
      expect(await screen.findByText('Added whoami-2')).toBeTruthy()
      expect(dialog()).toBeNull()
    })

    it('Save as file saves the edited graph first, then makes the copy', async () => {
      renderAt('/samples/whoami-postgres')
      fireEvent.click(await addButton('postgres'))
      const ask = await screen.findByRole('alertdialog')
      fireEvent.click(within(ask).getByRole('button', { name: 'Save as file' }))
      expect(saveGraphFile).toHaveBeenCalledWith(mine, 'mine.yaml')
      await waitFor(() => expect(names()).toContain('postgres-2'))
      expect(s().working?.fileName).toBe('whoami-postgres.yaml')
    })

    it('Cancel keeps the edited graph and the sample, and releases the hold so Add asks again', async () => {
      const router = renderAt('/samples/whoami-postgres')
      fireEvent.click(await addButton('postgres'))
      fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }))
      await waitFor(() => expect(dialog()).toBeNull())
      expect(names()).toEqual(['only'])
      expect(s().working?.edited).toBe(true)
      expect(router.state.location.pathname).toBe('/samples/whoami-postgres')
      expect(s().history.past).toHaveLength(0)
      expect(saveGraphFile).not.toHaveBeenCalled()

      fireEvent.click(await addButton('whoami'))
      expect(await screen.findByRole('alertdialog')).toBeTruthy()
      fireEvent.click(screen.getByRole('button', { name: 'Replace' }))
      await waitFor(() => expect(names()).toContain('whoami-2'))
      expect(names()).not.toContain('postgres-2')
    })

    it('asks only once storage was read: a second Add after Cancel does not read it again', async () => {
      renderAt('/samples/whoami-postgres')
      fireEvent.click(await addButton('postgres'))
      fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }))
      await waitFor(() => expect(dialog()).toBeNull())
      fireEvent.click(await addButton('postgres'))
      await screen.findByRole('alertdialog')
      expect(stored.loads).toBe(1)
    })
  })

  it('an edited working copy that is already in the store asks without reading storage', async () => {
    useEditor.setState({ working: edited() })
    renderAt('/samples/whoami-postgres')
    fireEvent.click(await addButton('postgres'))
    expect(await screen.findByRole('alertdialog')).toBeTruthy()
    expect(stored.loads).toBe(0)
  })

  it('an unedited working copy is replaced without a Dialog', async () => {
    useEditor.setState({ working: { ...edited(), edited: false } })
    renderAt('/samples/whoami-postgres')
    fireEvent.click(await addButton('postgres'))
    await waitFor(() => expect(names()).toContain('postgres-2'))
    expect(dialog()).toBeNull()
  })
})
