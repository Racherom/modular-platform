import { describe, expect, it } from 'vitest'
import type { Graph, Problem } from '../api/client'
import {
  acceptAllRecommendations,
  acceptRecommendation,
  bind,
  candidates,
  clearConfig,
  type Drop,
  isValidDrop,
  setConfig,
  unbind,
} from '../graph/edit'
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
    const g: Graph = { ...graph, modules: { ...graph.modules, web: { module: 'whoami' } } }
    expect(unbind(g, 'web', 'postgresql').modules.web?.uses).toEqual({ postgresql: null })
  })

  it('returns the same graph when the need is already null, or the consumer is unknown', () => {
    expect(unbind(graph, 'web', 'postgresql')).toBe(graph)
    expect(unbind(graph, 'nope', 'postgresql')).toBe(graph)
    expect(unbind(graph, 'constructor', 'postgresql')).toBe(graph)
  })
})

const recommended = (consumer: string, slot: string, candidate: string): Problem => ({
  severity: 'error',
  code: 'binding_recommended',
  path: `modules.${consumer}.uses.${slot}`,
  message: `Instance ${consumer} needs ${slot}; ${candidate} provides it.`,
  candidates: [candidate],
})

const leftOut: Graph = {
  ...graph,
  modules: { ...graph.modules, web: { module: 'whoami' }, app: { module: 'whoami' } },
}

describe('acceptRecommendation', () => {
  it('writes the same binding as picking the provider by hand (spec 0028)', () => {
    const next = acceptRecommendation(leftOut, 'web', 'postgresql', 'db')
    expect(next.modules.web?.uses).toEqual({ postgresql: 'db' })
    expect(next).toEqual(bind(leftOut, 'web', 'postgresql', 'db'))
    expect(leftOut.modules.web?.uses).toBeUndefined()
  })
})

describe('acceptAllRecommendations', () => {
  it('binds every open recommendation in one new graph', () => {
    const { graph: next, skipped } = acceptAllRecommendations(leftOut, [
      recommended('web', 'postgresql', 'db'),
      recommended('app', 'postgresql', 'db2'),
    ])
    expect(skipped).toBe(0)
    expect(next.modules.web?.uses).toEqual({ postgresql: 'db' })
    expect(next.modules.app?.uses).toEqual({ postgresql: 'db2' })
  })

  it('skips one whose candidate or consumer is gone, or whose use is no longer left out', () => {
    const { db2: _, ...rest } = leftOut.modules
    const g: Graph = { ...leftOut, modules: { ...rest, other: { module: 'mysql', uses: { postgresql: null } } } }
    const { graph: next, skipped } = acceptAllRecommendations(g, [
      recommended('web', 'postgresql', 'db'),
      recommended('app', 'postgresql', 'db2'),
      recommended('nope', 'postgresql', 'db'),
      recommended('other', 'postgresql', 'db'),
    ])
    expect(skipped).toBe(3)
    expect(next.modules.web?.uses).toEqual({ postgresql: 'db' })
    expect(next.modules.app?.uses).toBeUndefined()
    expect(next.modules.other?.uses).toEqual({ postgresql: null })
  })

  it('accepts no optional warning in bulk', () => {
    const optional: Problem = { ...recommended('web', 'postgresql', 'db'), severity: 'warning', code: 'optional_unbound' }
    const { graph: next, skipped } = acceptAllRecommendations(leftOut, [optional])
    expect(next).toBe(leftOut)
    expect(skipped).toBe(0)
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

describe('config values (spec 0025, AC-29)', () => {
  it('sets a value, keeps the others and leaves the input untouched', () => {
    const one = setConfig(graph, 'db', 'mode', 'fast')
    expect(one.modules.db?.config).toEqual({ mode: 'fast' })
    const two = setConfig(one, 'db', 'label', '')
    expect(two.modules.db?.config).toEqual({ label: '', mode: 'fast' })
    expect(graph.modules.db?.config).toBeUndefined()
    expect(setConfig(two, 'db', 'mode', 'fast')).toBe(two)
    expect(setConfig(graph, 'nobody', 'mode', 'x')).toBe(graph)
  })

  it('clears a value, and the config map once it is empty', () => {
    const g = setConfig(setConfig(graph, 'db', 'mode', 'fast'), 'db', 'level', '3')
    const one = clearConfig(g, 'db', 'mode')
    expect(one.modules.db?.config).toEqual({ level: '3' })
    const none = clearConfig(one, 'db', 'level')
    expect(none.modules.db).toEqual({ module: 'postgres' })
    expect(Object.hasOwn(none.modules.db ?? {}, 'config')).toBe(false)
    expect(clearConfig(none, 'db', 'level')).toBe(none)
  })
})
