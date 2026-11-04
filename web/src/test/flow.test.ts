import { describe, expect, it } from 'vitest'
import type { Graph, PlanResult, Problem } from '../api/client'
import { downloadBlocked } from '../components/GraphScreen'
import { emphasize, markOf, problemSlot, toFlow } from '../graph/flow'
import { testCatalog, testModule } from './fixtures'

const catalog = testCatalog([
  testModule('postgres', { provides: { postgresql: { port: 5432 } } }),
  testModule('app', { provides: { http: { port: 80 } }, uses: { postgresql: { optional: false }, cache: { optional: true } } }),
])
const graph: Graph = {
  apiVersion: 'modular-platform/v1',
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

describe('problemSlot', () => {
  it.each([
    ['modules.app.uses.postgresql', { instance: 'app', slot: 'postgresql' }],
    ['modules.app.uses.main-db.database', { instance: 'app', slot: 'main-db' }],
    ['modules.app', undefined],
    ['modules.app.module', undefined],
    ['name', undefined],
    ['', undefined],
  ])('%s', (path, want) => {
    expect(problemSlot(problem(path, 'error'))).toEqual(want)
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
    expect(node('app').uses.find((u) => u.slot === 'postgresql')?.mark).toEqual({ severity: 'error', messages: ['slow', 'broken'] })
    expect(node('app').uses.find((u) => u.slot === 'cache')?.mark).toBeUndefined()
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
    const out = emphasize(nodes, edges, { kind: 'binding', consumer: 'app', slot: 'postgresql' })
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

describe('toFlow on v1 (spec 0011, AC-19)', () => {
  const v1 = testCatalog(
    [
      testModule('reports', { uses: { 'main-db': { capability: 'postgresql' }, 'audit-db': { capability: 'postgresql' } } }),
      {
        ...testModule('pg'),
        provides: {
          primary: { capability: 'postgresql', version: '18.6', port: 5432 },
          replica: { capability: 'postgresql', version: '18.6', port: 5433 },
        },
      },
      { ...testModule('postgres'), provides: { server: { capability: 'postgresql-provisioner', version: '18.6', port: 5432 } } },
      testModule('svc', { runtime: 'sdk' }),
    ],
    [
      {
        apiVersion: 'modular-platform/v1',
        kind: 'Capability',
        name: 'postgresql-provisioner',
        description: 'x',
        fields: {},
        yields: { capability: 'postgresql', fields: {}, job: { image: 'x:1', provision: 'x' } },
      },
    ],
  )
  const g: Graph = {
    apiVersion: 'modular-platform/v1',
    kind: 'Graph',
    name: 'g',
    modules: {
      reports: { module: 'reports', uses: { 'main-db': { provider: 'postgres', database: 'r', user: 'r' }, 'audit-db': 'pg.replica' } },
      pg: { module: 'pg' },
      postgres: { module: 'postgres' },
      ext: { external: { provides: { db: { capability: 'postgresql', version: '16.4' } } } },
      svc: { module: 'svc' },
    },
  }
  const { nodes, edges } = toFlow(g, v1, [], {})
  const node = (id: string) => nodes.find((n) => n.id === id)!

  it('draws one handle per slot and per provide, with the provide version', () => {
    expect(node('reports').handles?.map((h) => h.id)).toEqual(['uses:audit-db', 'uses:main-db'])
    expect(node('pg').data.provides).toEqual([
      { name: 'primary', capability: 'postgresql', version: '18.6' },
      { name: 'replica', capability: 'postgresql', version: '18.6' },
    ])
  })

  it('ends an object form edge at the provisioner and a named one at its provide', () => {
    expect(edges.map((e) => [e.id, e.target, e.targetHandle])).toEqual([
      ['reports.audit-db', 'pg', 'provides:replica'],
      ['reports.main-db', 'postgres', 'provides:server'],
    ])
  })

  it('draws an external instance with its provides and no slots, marked external', () => {
    expect(node('ext').data).toMatchObject({
      external: true,
      known: true,
      uses: [],
      provides: [{ name: 'db', capability: 'postgresql', version: '16.4' }],
    })
    expect(node('ext').ariaLabel).toBe('ext, external service')
  })

  it('gives a runtime: sdk module its implicit acme slot', () => {
    expect(node('svc').data.uses.map((u) => u.slot)).toEqual(['acme'])
  })
})
