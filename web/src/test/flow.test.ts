import { describe, expect, it } from 'vitest'
import type { CatalogEntry, Graph, PlanResult, Problem } from '../api/client'
import { downloadBlocked } from '../components/GraphScreen'
import { emphasize, markOf, problemNeed, toFlow } from '../graph/flow'

const entry = (name: string, more: Partial<CatalogEntry>): CatalogEntry => ({
  apiVersion: 'modular-platform/v0alpha1',
  kind: 'Module',
  name,
  description: name,
  image: `${name}:1`,
  ...more,
})
const catalog = [
  entry('postgres', { provides: { postgresql: { port: 5432 } } }),
  entry('app', { provides: { http: { port: 80 } }, uses: { postgresql: { optional: false }, cache: { optional: true } } }),
]
const graph: Graph = {
  apiVersion: 'modular-platform/v0alpha1',
  kind: 'Graph',
  name: 'g',
  modules: {
    app: { module: 'app', uses: { postgresql: 'db', cache: null } },
    db: { module: 'postgres' },
    other: { module: 'postgres' },
    ghost: { module: 'missing', uses: { postgresql: null } },
  },
}
const problem = (path: string, severity: Problem['severity'], message = path): Problem => ({ severity, code: 'x', path, message })

describe('problemNeed', () => {
  it.each([
    ['modules.app.uses.postgresql', { instance: 'app', capability: 'postgresql' }],
    ['modules.app', undefined],
    ['modules.app.module', undefined],
    ['name', undefined],
    ['', undefined],
  ])('%s', (path, want) => {
    expect(problemNeed(problem(path, 'error'))).toEqual(want)
  })
})

describe('markOf', () => {
  it('lets an error win and keeps every message', () => {
    expect(markOf([problem('a', 'warning', 'w'), problem('a', 'error', 'e')])).toEqual({ severity: 'error', messages: ['w', 'e'] })
    expect(markOf([problem('a', 'warning', 'w')])?.severity).toBe('warning')
    expect(markOf([])).toBeUndefined()
  })
})

describe('toFlow problem marks (AC-13)', () => {
  const problems = [
    problem('modules.app.uses.postgresql', 'warning', 'slow'),
    problem('modules.app.uses.postgresql', 'error', 'broken'),
    problem('modules.app.module', 'warning', 'old'),
    problem('modules.app.uses.nope', 'error', 'not a port'),
    problem('modules.ghost.uses.postgresql', 'error', 'unknown module'),
    problem('name', 'error', 'graph level'),
  ]
  const { nodes } = toFlow(graph, catalog, problems, {})
  const node = (id: string) => nodes.find((n) => n.id === id)!.data

  it('marks the port of a need problem, error first, with every message', () => {
    expect(node('app').uses.find((u) => u.capability === 'postgresql')?.mark).toEqual({ severity: 'error', messages: ['slow', 'broken'] })
    expect(node('app').uses.find((u) => u.capability === 'cache')?.mark).toBeUndefined()
  })

  it('marks the node for other module problems and for ports that are not drawn', () => {
    expect(node('app').mark).toEqual({ severity: 'error', messages: ['old', 'not a port'] })
    expect(node('ghost').mark).toEqual({ severity: 'error', messages: ['unknown module'] })
    expect(node('db').mark).toBeUndefined()
  })
})

describe('emphasize (AC-8)', () => {
  const { nodes, edges } = toFlow(graph, catalog, [], {})

  it('changes nothing without a selection', () => {
    expect(emphasize(nodes, edges, null)).toEqual({ nodes, edges })
  })

  it('highlights a module, its edges and the ports at both ends, and dims the rest', () => {
    const out = emphasize(nodes, edges, { kind: 'module', instance: 'db' })
    const n = (id: string) => out.nodes.find((x) => x.id === id)!
    expect(n('db').selected).toBe(true)
    expect(n('db').data.dimmed).toBeUndefined()
    expect(n('db').data.highlight).toEqual(['provides:postgresql'])
    expect(n('app').data.dimmed).toBeUndefined()
    expect(n('app').data.highlight).toEqual(['uses:postgresql'])
    expect(n('other').data.dimmed).toBe(true)
    expect(n('ghost').data.dimmed).toBe(true)
    expect(out.edges.map((e) => [e.id, e.className])).toEqual([['app.postgresql', 'is-related']])
  })

  it('selects a binding and lights its two ports', () => {
    const out = emphasize(nodes, edges, { kind: 'binding', consumer: 'app', capability: 'postgresql' })
    expect(out.edges[0]).toMatchObject({ selected: true, className: 'is-related' })
    expect(out.nodes.every((n) => !n.selected)).toBe(true)
    expect(out.nodes.find((n) => n.id === 'app')?.data.highlight).toEqual(['uses:postgresql'])
    expect(out.nodes.find((n) => n.id === 'other')?.data.dimmed).toBe(true)
  })

  it('lets only the provider end of an edge move (AC-9)', () => {
    expect(edges.every((e) => e.reconnectable === 'target')).toBe(true)
  })
})

describe('downloadBlocked (AC-14)', () => {
  const plan = (problems: Problem[]): PlanResult => ({
    ok: !problems.some((p) => p.severity === 'error'),
    graph,
    bindings: [],
    problems,
  })
  it.each([
    ['current and ok', 'current', plan([]), null],
    ['warnings never block', 'current', plan([problem('a', 'warning')]), null],
    ['checking', 'checking', plan([]), 'Checking the graph'],
    ['failed', 'failed', plan([]), 'Not checked, retry first'],
    ['one error', 'current', plan([problem('a', 'error'), problem('b', 'warning')]), 'Fix 1 error to download'],
    ['two errors', 'current', plan([problem('a', 'error'), problem('b', 'error')]), 'Fix 2 errors to download'],
  ] as const)('%s', (_, state, p, want) => {
    expect(downloadBlocked(state, p)).toBe(want)
  })
})
