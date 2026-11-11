import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CheckResult, Graph, Problem } from '../api/client'
import { Inspector } from '../components/Inspector'
import { useEditor } from '../lib/store'
import { routeTree } from '../routeTree.gen'
import { testCatalog, testModule } from './fixtures'
import { fetchHandler } from './setup'

// Spec 0027 verify, UI steps: the toolbar and inspector of a recommended binding (spec 0028).
const working = vi.hoisted(() => ({ current: undefined as unknown }))
vi.mock('../lib/storage', async (actual) => ({
  ...(await actual<typeof import('../lib/storage')>()),
  loadWorking: () => Promise.resolve(working.current),
  saveWorking: () => Promise.resolve(),
}))

const catalog = testCatalog([
  testModule('postgres', { provides: { postgresql: { port: 5432 } } }),
  testModule('test-app', { uses: { postgresql: { optional: false } } }),
])

const graph: Graph = {
  apiVersion: 'modular-platform/v1',
  kind: 'Graph',
  name: 'g',
  modules: { 'app-1': { module: 'test-app' }, 'app-2': { module: 'test-app' }, db: { module: 'postgres' } },
}

/** The renderer's rule in short: a left out required use with one provider is recommended. */
function checkOf(g: Graph): CheckResult {
  const problems: Problem[] = Object.entries(g.modules).flatMap(([name, m]) =>
    m.module === 'test-app' && !(m.uses && Object.hasOwn(m.uses, 'postgresql'))
      ? [
          {
            severity: 'error' as const,
            code: 'binding_recommended',
            path: `modules.${name}.uses.postgresql`,
            message: `Instance ${name} needs postgresql; db provides it.`,
            candidates: ['db'],
          },
        ]
      : [],
  )
  return { ok: problems.length === 0, graph: g, bindings: [], problems }
}

function renderAt(path: string) {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

describe('Accept all in the toolbar (spec 0028, AC-9)', () => {
  beforeEach(() => {
    working.current = { fileName: 'g.yaml', graph, check: checkOf(graph), positions: {} }
    useEditor.setState({ working: undefined, history: { past: [], future: [] }, selection: null, announcement: null, handoff: null })
    fetchHandler.current = async (req) => {
      const url = new URL(req.url)
      if (url.pathname === '/renderer/v1/catalog') return Response.json(catalog)
      if (url.pathname === '/renderer/v1/check') return Response.json(checkOf((await req.json()) as Graph))
      return new Response('not found', { status: 404 })
    }
  })

  it('blocks Download with its reason, accepts every recommendation in one undo step, and moves focus', async () => {
    renderAt('/')
    const accept = await screen.findByRole('button', { name: 'Accept all recommendations (2)' })
    const download = screen.getByRole('button', { name: 'Download zip' })
    expect(download.hasAttribute('disabled')).toBe(true)
    const reason = screen.getByText('Accept or change 2 recommended bindings first')
    expect(download.getAttribute('aria-describedby')).toBe(reason.id)

    fireEvent.click(accept)
    await waitFor(() => expect(screen.queryByRole('button', { name: /^Accept all/ })).toBeNull())
    await waitFor(() => expect(screen.getByRole('button', { name: 'Download zip' }).hasAttribute('disabled')).toBe(false))
    const uses = (name: string) => useEditor.getState().working?.graph.modules[name]?.uses
    expect(uses('app-1')).toEqual({ postgresql: 'db' })
    expect(uses('app-2')).toEqual({ postgresql: 'db' })
    expect(useEditor.getState().history.past).toHaveLength(1)
    await waitFor(() => expect(document.activeElement?.closest('[role="toolbar"]')).not.toBeNull())

    fireEvent.click(screen.getByRole('button', { name: /^Undo/ }))
    expect(await screen.findByRole('button', { name: 'Accept all recommendations (2)' })).toBeTruthy()
    expect(uses('app-1')).toBeUndefined()
  })
})

describe('the binding list of a left out use (spec 0028, AC-9, AC-10)', () => {
  const left: Graph = { ...graph, modules: { 'app-1': { module: 'test-app' }, db: { module: 'postgres' }, db2: { module: 'postgres' } } }
  const ambiguous: Problem = {
    severity: 'error',
    code: 'ambiguous_provider',
    path: 'modules.app-1.uses.postgresql',
    message: 'Two providers serve postgresql.',
    candidates: ['db', 'db2'],
  }
  const result = (problems: Problem[]): CheckResult => ({ ok: false, graph: left, bindings: [], problems })

  beforeEach(() => useEditor.getState().select({ kind: 'module', instance: 'app-1' }))

  it('offers "Choose a provider" (disabled), the candidates and "Not bound", and no Accept for an ambiguous use', async () => {
    render(<Inspector graph={left} catalog={catalog} check={result([ambiguous])} checkCurrent onEdit={() => undefined} />)
    expect(screen.queryByRole('button', { name: /^Accept/ })).toBeNull()
    expect(screen.queryByText(/Recommended:/)).toBeNull()
    fireEvent.click(await screen.findByRole('button', { name: /Choose a provider/ }))
    const options = screen.getAllByRole('option').map((o) => o.textContent)
    expect(options).toEqual(['Choose a provider', 'db', 'db2', 'Not bound'])
    expect(screen.getByRole('option', { name: 'Choose a provider' }).getAttribute('aria-disabled')).toBe('true')
    expect(options.join(' ')).not.toMatch(/planner|checks choose/i) // vocabulary: allow
  })
})

describe('the optional use with one provider (spec 0028, AC-10)', () => {
  it('stays a warning that does not block Download, and binds nothing until you press Accept', async () => {
    const optional = testCatalog([
      testModule('postgres', { provides: { postgresql: { port: 5432 } } }),
      testModule('test-app', { uses: { postgresql: { optional: true } } }),
    ])
    const g: Graph = { ...graph, modules: { app: { module: 'test-app' }, db: { module: 'postgres' } } }
    const warning: Problem = {
      severity: 'warning',
      code: 'optional_unbound',
      path: 'modules.app.uses.postgresql',
      message: 'db provides postgresql.',
      candidates: ['db'],
    }
    working.current = { fileName: 'g.yaml', graph: g, check: { ok: true, graph: g, bindings: [], problems: [warning] }, positions: {} }
    useEditor.setState({ working: undefined, history: { past: [], future: [] }, selection: null, announcement: null, handoff: null })
    fetchHandler.current = async (req) => {
      const url = new URL(req.url)
      if (url.pathname === '/renderer/v1/catalog') return Response.json(optional)
      if (url.pathname === '/renderer/v1/check') {
        const checked = (await req.json()) as Graph
        const bound = checked.modules.app?.uses?.postgresql !== undefined
        return Response.json({ ok: true, graph: checked, bindings: [], problems: bound ? [] : [warning] })
      }
      return new Response('not found', { status: 404 })
    }
    renderAt('/')
    const download = await screen.findByRole('button', { name: 'Download zip' })
    await waitFor(() => expect(download.hasAttribute('disabled')).toBe(false))
    // the toolbar offers no bulk accept for an optional use
    expect(screen.queryByRole('button', { name: /^Accept all/ })).toBeNull()
    expect(useEditor.getState().working?.graph.modules.app?.uses).toBeUndefined()
  })
})

describe('the stack words (spec 0027, AC-2, AC-4)', () => {
  it('the online editor says it stores nothing on the stacks page', async () => {
    renderAt('/stacks')
    expect(await screen.findByText(/The online editor stores nothing/)).toBeTruthy()
  })
})
