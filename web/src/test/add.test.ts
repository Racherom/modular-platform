import { describe, expect, it } from 'vitest'
import type { Graph } from '../api/client'
import { addModule } from '../graph/edit'
import { NODE_HEADER, nodeHeight, PORT_ROW } from '../graph/flow'
import { placeNew, ROW_GAP } from '../graph/layout'
import { testCatalog, testModule } from './fixtures'

const graph = (modules: Graph['modules']): Graph => ({ apiVersion: 'modular-platform/v1', kind: 'Graph', name: 'g', modules })

describe('addModule (AC-2, AC-3)', () => {
  it('names the instance after the module and writes only the module', () => {
    const before = graph({})
    const { graph: next, instance } = addModule(before, 'postgres')
    expect(instance).toBe('postgres')
    expect(next.modules).toEqual({ postgres: { module: 'postgres' } })
    expect(before.modules).toEqual({})
  })

  it('takes the lowest free number from 2', () => {
    let g = graph({})
    const names: string[] = []
    for (let i = 0; i < 3; i++) {
      const added = addModule(g, 'postgres')
      g = added.graph
      names.push(added.instance)
    }
    expect(names).toEqual(['postgres', 'postgres-2', 'postgres-3'])
    expect(addModule(graph({ 'postgres-2': { module: 'postgres' } }), 'postgres').instance).toBe('postgres')
    expect(addModule(graph({ postgres: { module: 'postgres' }, 'postgres-3': { module: 'postgres' } }), 'postgres').instance).toBe(
      'postgres-2',
    )
  })

  it('counts an external and any own key as taken, prototype names included', () => {
    expect(addModule(graph({ whoami: { external: { provides: {} } } as never }), 'whoami').instance).toBe('whoami-2')
    expect(addModule(graph({}), 'constructor').instance).toBe('constructor')
  })

  it('cuts a long module name so the whole name is 63 characters', () => {
    const long = 'a'.repeat(63)
    const first = addModule(graph({}), long)
    expect(first.instance).toBe(long)
    const second = addModule(first.graph, long)
    expect(second.instance).toBe(`${'a'.repeat(61)}-2`)
    expect(second.instance).toHaveLength(63)
    const many = graph(
      Object.fromEntries(Array.from({ length: 9 }, (_, i) => [i === 0 ? long : `${'a'.repeat(61)}-${i + 1}`, { module: long }])),
    )
    expect(addModule(many, long).instance).toBe(`${'a'.repeat(60)}-10`)
  })
})

describe('placeNew (AC-4)', () => {
  const catalog = testCatalog([
    testModule('db', { provides: { postgresql: {} } }),
    testModule('tall', { provides: { a: {}, b: {} }, uses: { c: {}, d: {}, e: {}, f: {} } }),
  ])

  it('starts at the origin on an empty graph', () => {
    expect(placeNew(graph({}), catalog, {})).toEqual({ x: 0, y: 0 })
  })

  it('goes one row gap below the lowest bottom edge at x 0, so a tall node is not overlapped', () => {
    const g = graph({ db: { module: 'db' }, tall: { module: 'tall' } })
    const positions = { db: { x: 0, y: 0 }, tall: { x: 0, y: 160 } }
    expect(nodeHeight(2, 4)).toBe(NODE_HEADER + 4 * PORT_ROW + 8)
    expect(placeNew(g, catalog, positions)).toEqual({ x: 0, y: 160 + nodeHeight(2, 4) + ROW_GAP })
  })

  it('ignores nodes in other columns and leaves positions alone', () => {
    const g = graph({ db: { module: 'db' } })
    const positions = { db: { x: 320, y: 900 } }
    expect(placeNew(g, catalog, positions)).toEqual({ x: 0, y: 0 })
    expect(positions).toEqual({ db: { x: 320, y: 900 } })
  })
})
