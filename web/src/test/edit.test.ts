import { describe, expect, it } from 'vitest'
import type { Graph } from '../api/client'
import { bind, candidates, type Drop, isValidDrop, letPlannerChoose, unbind } from '../graph/edit'
import { testCatalog, testModule } from './fixtures'

const catalog = testCatalog([
  testModule('postgres', { provides: { postgresql: { port: 5432 } } }),
  testModule('mysql', { provides: { mysql: { port: 3306 } } }),
  testModule('whoami', { provides: { http: { port: 80 } }, uses: { postgresql: { optional: true } } }),
])

const graph: Graph = {
  apiVersion: 'modular-platform/v1',
  kind: 'Graph',
  name: 'g',
  modules: {
    db: { module: 'postgres' },
    db2: { module: 'postgres' },
    other: { module: 'mysql' },
    ghost: { module: 'not-in-catalog' },
    web: { module: 'whoami', uses: { postgresql: null } },
  },
}

const drop = (source: string, need: string, target: string, offer: string): Drop => ({
  source,
  sourceHandle: `uses:${need}`,
  target,
  targetHandle: `provides:${offer}`,
})

describe('bind', () => {
  it('sets the provider of a need and leaves the input untouched', () => {
    const next = bind(graph, 'web', 'postgresql', 'db')
    expect(next.modules.web?.uses).toEqual({ postgresql: 'db' })
    expect(graph.modules.web?.uses).toEqual({ postgresql: null })
    expect(next.modules.db).toBe(graph.modules.db)
  })

  it('replaces a previous provider (rebind)', () => {
    const next = bind(bind(graph, 'web', 'postgresql', 'db'), 'web', 'postgresql', 'db2')
    expect(next.modules.web?.uses).toEqual({ postgresql: 'db2' })
  })

  it('keeps other needs of the consumer', () => {
    const g: Graph = { ...graph, modules: { ...graph.modules, web: { module: 'whoami', uses: { cache: 'x', postgresql: null } } } }
    expect(bind(g, 'web', 'postgresql', 'db').modules.web?.uses).toEqual({ cache: 'x', postgresql: 'db' })
  })

  it('returns the same graph for an unknown consumer, even one named like a prototype key', () => {
    expect(bind(graph, 'nope', 'postgresql', 'db')).toBe(graph)
    expect(bind(graph, 'constructor', 'postgresql', 'db')).toBe(graph)
  })

  it('stores a capability named __proto__ as a plain key', () => {
    const next = bind(graph, 'web', '__proto__', 'db')
    expect(Object.hasOwn(next.modules.web?.uses ?? {}, '__proto__')).toBe(true)
  })

  it('returns the same graph when the need already has that provider', () => {
    const once = bind(graph, 'web', 'postgresql', 'db')
    expect(bind(once, 'web', 'postgresql', 'db')).toBe(once)
  })

  it('adds a uses section to a module without one', () => {
    expect(bind(graph, 'db', 'postgresql', 'db2').modules.db?.uses).toEqual({ postgresql: 'db2' })
  })
})

describe('unbind', () => {
  const bound = bind(graph, 'web', 'postgresql', 'db')

  it('sets the need to null and never removes a module', () => {
    const next = unbind(bound, 'web', 'postgresql')
    expect(next.modules.web?.uses).toEqual({ postgresql: null })
    expect(Object.keys(next.modules)).toEqual(Object.keys(bound.modules))
    expect(bound.modules.web?.uses).toEqual({ postgresql: 'db' })
  })

  it('sets a missing need to null', () => {
    const g = letPlannerChoose(bound, 'web', 'postgresql')
    expect(unbind(g, 'web', 'postgresql').modules.web?.uses).toEqual({ postgresql: null })
  })

  it('returns the same graph when the need is already null, or the consumer is unknown', () => {
    expect(unbind(graph, 'web', 'postgresql')).toBe(graph)
    expect(unbind(graph, 'nope', 'postgresql')).toBe(graph)
    expect(unbind(graph, 'constructor', 'postgresql')).toBe(graph)
  })
})

describe('letPlannerChoose', () => {
  it('removes the key and keeps the other needs', () => {
    const g: Graph = { ...graph, modules: { ...graph.modules, web: { module: 'whoami', uses: { cache: 'x', postgresql: 'db' } } } }
    const next = letPlannerChoose(g, 'web', 'postgresql')
    expect(next.modules.web?.uses).toEqual({ cache: 'x' })
    expect(Object.hasOwn(next.modules.web?.uses ?? {}, 'postgresql')).toBe(false)
    expect(g.modules.web?.uses).toEqual({ cache: 'x', postgresql: 'db' })
  })

  it('returns the same graph when the key is already missing, or the consumer is unknown', () => {
    const g = letPlannerChoose(graph, 'web', 'postgresql')
    expect(letPlannerChoose(g, 'web', 'postgresql')).toBe(g)
    expect(letPlannerChoose(graph, 'db', 'postgresql')).toBe(graph)
    expect(letPlannerChoose(graph, 'constructor', 'postgresql')).toBe(graph)
  })
})

describe('candidates', () => {
  it('lists other known instances that provide the capability, sorted by name', () => {
    expect(candidates(graph, catalog, 'web', 'postgresql')).toEqual(['db', 'db2'])
  })

  it('lists nothing for a slot the consumer module does not have', () => {
    expect(candidates(graph, catalog, 'db', 'postgresql')).toEqual([])
    expect(candidates(graph, catalog, 'web', 'mysql')).toEqual([])
    expect(candidates(graph, catalog, 'ghost', 'postgresql')).toEqual([])
  })

  it('ignores a capability named like a prototype key', () => {
    expect(candidates(graph, catalog, 'web', 'constructor')).toEqual([])
  })
})

describe('isValidDrop', () => {
  it.each([
    ['a matching offer on another known instance', drop('web', 'postgresql', 'db', 'postgresql'), true],
    ['a second provider of the same capability', drop('web', 'postgresql', 'db2', 'postgresql'), true],
    ['a different capability', drop('web', 'postgresql', 'other', 'mysql'), false],
    ['a capability the target does not provide', drop('web', 'postgresql', 'other', 'postgresql'), false],
    ['the same instance', drop('web', 'postgresql', 'web', 'postgresql'), false],
    ['an instance not in the catalog', drop('web', 'postgresql', 'ghost', 'postgresql'), false],
    ['an instance not in the graph', drop('web', 'postgresql', 'missing', 'postgresql'), false],
    ['a need the consumer does not have', drop('db', 'postgresql', 'db2', 'postgresql'), false],
    ['a prototype key as target', drop('web', 'postgresql', 'constructor', 'postgresql'), false],
  ])('%s → %s', (_, d, want) => {
    expect(isValidDrop(graph, catalog, d)).toBe(want)
  })

  it('refuses a drag that starts at a provides port', () => {
    expect(
      isValidDrop(graph, catalog, { source: 'db', sourceHandle: 'provides:postgresql', target: 'web', targetHandle: 'uses:postgresql' }),
    ).toBe(false)
  })

  it('refuses missing handles', () => {
    expect(isValidDrop(graph, catalog, { source: 'web', sourceHandle: null, target: 'db', targetHandle: null })).toBe(false)
  })
})
