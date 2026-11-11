import { describe, expect, it } from 'vitest'
import type { Graph } from '../api/client'
import { addModule } from '../graph/edit'
import { nodeHeight, type Positions } from '../graph/flow'
import { placeNew, ROW_GAP } from '../graph/layout'
import { testCatalog, testModule } from './fixtures'

const graph = (modules: Graph['modules']): Graph => ({ apiVersion: 'modular-platform/v1', kind: 'Graph', name: 'g', modules })
const entries = (names: string[], module: string): Graph['modules'] => Object.fromEntries(names.map((n) => [n, { module }]))

describe('instance names (AC-2)', () => {
  it('numbers from 2 up, and skips a number that is taken', () => {
    const g = graph(entries(['postgres', 'postgres-2', 'postgres-3'], 'postgres'))
    expect(addModule(g, 'postgres').instance).toBe('postgres-4')
    // a gap below the highest number is filled first
    expect(addModule(graph(entries(['postgres', 'postgres-2', 'postgres-4'], 'postgres')), 'postgres').instance).toBe('postgres-3')
  })

  it('counts a key as taken whatever module it runs', () => {
    expect(addModule(graph(entries(['postgres'], 'whoami')), 'postgres').instance).toBe('postgres-2')
  })

  it('a module whose own name looks numbered still gets the next number after it', () => {
    expect(addModule(graph(entries(['db-2'], 'db-2')), 'db-2').instance).toBe('db-2-2')
  })

  it('writes only the module, also for a numbered name', () => {
    const { graph: next } = addModule(graph(entries(['postgres'], 'postgres')), 'postgres')
    expect(next.modules['postgres-2']).toStrictEqual({ module: 'postgres' })
  })

  // the module part is cut so the whole name is 63; the cut depends on the digits of N
  it.each([63, 62, 61, 60])('keeps the whole name within 63 characters for a %i character module', (length) => {
    const module = 'a'.repeat(length)
    let g = graph({})
    for (let i = 1; i <= 120; i++) {
      const added = addModule(g, module)
      g = added.graph
      const suffix = `-${i}`
      expect(added.instance).toBe(i === 1 ? module : `${module.slice(0, Math.min(length, 63 - suffix.length))}${suffix}`)
      expect(added.instance.length).toBeLessThanOrEqual(63)
      expect(Object.keys(g.modules)).toHaveLength(i)
    }
  })

  it('cuts one more character when N gets another digit', () => {
    const module = 'a'.repeat(63)
    let g = graph({})
    const names: string[] = []
    for (let i = 1; i <= 100; i++) {
      const added = addModule(g, module)
      g = added.graph
      names.push(added.instance)
    }
    expect(names[1]).toBe(`${'a'.repeat(61)}-2`)
    expect(names[8]).toBe(`${'a'.repeat(61)}-9`)
    expect(names[9]).toBe(`${'a'.repeat(60)}-10`)
    expect(names[98]).toBe(`${'a'.repeat(60)}-99`)
    expect(names[99]).toBe(`${'a'.repeat(59)}-100`)
    expect(new Set(names).size).toBe(100)
    expect(names.every((n) => n.length <= 63)).toBe(true)
    expect(names.slice(1).every((n) => n.length === 63)).toBe(true)
  })

  it('may leave a double hyphen where the cut lands on a hyphen', () => {
    const module = `${'a'.repeat(60)}-bc`
    expect(module).toHaveLength(63)
    const first = addModule(graph({}), module)
    expect(addModule(first.graph, module).instance).toBe(`${'a'.repeat(60)}--2`)
  })

  it('does not take a cut name that is already in the graph', () => {
    const module = 'a'.repeat(63)
    const cut = `${'a'.repeat(61)}-2`
    const g = graph({ [module]: { module }, [cut]: { module: 'other' } })
    expect(addModule(g, module).instance).toBe(`${'a'.repeat(61)}-3`)
  })
})

describe('adding the same module again and again (AC-3)', () => {
  it('has no limit per module and changes nothing that was there', () => {
    let g = graph({ keep: { module: 'whoami', uses: { postgresql: 'postgres' }, config: { a: '1' } } })
    const before = g.modules.keep
    const names: string[] = []
    for (let i = 0; i < 40; i++) {
      const added = addModule(g, 'postgres')
      g = added.graph
      names.push(added.instance)
    }
    expect(names[0]).toBe('postgres')
    expect(names.slice(1, 5)).toEqual(['postgres-2', 'postgres-3', 'postgres-4', 'postgres-5'])
    expect(names.at(-1)).toBe('postgres-40')
    expect(new Set(names).size).toBe(40)
    expect(g.modules.keep).toBe(before)
    expect(Object.values(g.modules).filter((m) => m.module === 'postgres')).toHaveLength(40)
    expect(Object.keys(g.modules)).toHaveLength(41)
  })

  it('leaves the graph it was given as it was', () => {
    const before = graph(entries(['postgres'], 'postgres'))
    const snapshot = JSON.stringify(before)
    const added = addModule(before, 'postgres')
    expect(JSON.stringify(before)).toBe(snapshot)
    expect(added.graph).not.toBe(before)
    expect(added.graph.modules).not.toBe(before.modules)
  })

  it('keeps the other graph fields', () => {
    const before: Graph = { ...graph({}), name: 'mine' }
    expect(addModule(before, 'postgres').graph).toMatchObject({ apiVersion: 'modular-platform/v1', kind: 'Graph', name: 'mine' })
  })
})

describe('where the new node goes (AC-4)', () => {
  const catalog = testCatalog([
    testModule('short', { provides: { a: {} } }),
    testModule('tall', { provides: { a: {} }, uses: { b: {}, c: {}, d: {}, e: {} } }),
    testModule('wide', { provides: { a: {}, b: {}, c: {}, d: {}, e: {}, f: {} } }),
  ])
  const heightOf = (module: string) => (module === 'short' ? nodeHeight(1, 0) : module === 'tall' ? nodeHeight(1, 4) : nodeHeight(6, 0))
  const bottom = (y: number, module: string) => y + heightOf(module)

  it('is below the lowest bottom edge even when that node is not the last or the lowest y', () => {
    const g = graph({ a: { module: 'tall' }, b: { module: 'short' }, c: { module: 'short' } })
    const positions: Positions = { a: { x: 0, y: 40 }, b: { x: 0, y: 0 }, c: { x: 0, y: 20 } }
    expect(placeNew(g, catalog, positions)).toEqual({ x: 0, y: bottom(40, 'tall') + ROW_GAP })
  })

  it.each(['short', 'tall', 'wide'])('does not overlap a %s node already at the first row', (module) => {
    const g = graph({ a: { module } })
    const at = placeNew(g, catalog, { a: { x: 0, y: 0 } })
    expect(at.x).toBe(0)
    expect(at.y).toBeGreaterThanOrEqual(bottom(0, module))
    expect(at.y).toBe(bottom(0, module) + ROW_GAP)
  })

  it('stacks several adds without overlap, whatever their height', () => {
    let g = graph({})
    let positions: Positions = {}
    for (const module of ['tall', 'short', 'wide', 'tall', 'short']) {
      const { graph: next, instance } = addModule(g, module)
      positions = { ...positions, [instance]: placeNew(g, catalog, positions) }
      g = next
    }
    const boxes = Object.entries(positions).map(([name, p]) => ({ top: p.y, bottom: bottom(p.y, g.modules[name]?.module ?? 'short') }))
    boxes.sort((a, b) => a.top - b.top)
    expect(boxes).toHaveLength(5)
    for (let i = 1; i < boxes.length; i++) expect(boxes[i]?.top).toBeGreaterThanOrEqual((boxes[i - 1]?.bottom ?? 0) + ROW_GAP)
    expect(Object.values(positions).every((p) => p.x === 0)).toBe(true)
  })

  it('counts an external with its provides and no uses', () => {
    const g = graph({
      ext: { external: { provides: { a: { capability: 'a', version: '1.0' }, b: { capability: 'b', version: '1.0' } } } } as never,
    })
    expect(placeNew(g, catalog, { ext: { x: 0, y: 10 } })).toEqual({ x: 0, y: 10 + nodeHeight(2, 0) + ROW_GAP })
  })

  it('counts a module the catalog does not know as the smallest node', () => {
    const g = graph({ gone: { module: 'removed' } })
    expect(placeNew(g, catalog, { gone: { x: 0, y: 0 } })).toEqual({ x: 0, y: nodeHeight(0, 0) + ROW_GAP })
  })

  it('ignores nodes in another column, nodes with no position and positions of names not in the graph', () => {
    const g = graph({ a: { module: 'tall' }, b: { module: 'tall' }, c: { module: 'short' } })
    const positions: Positions = { a: { x: 320, y: 900 }, c: { x: 0, y: 5 }, ghost: { x: 0, y: 5000 } }
    expect(placeNew(g, catalog, positions)).toEqual({ x: 0, y: 5 + nodeHeight(1, 0) + ROW_GAP })
  })

  it('goes to y 0 when nothing sits at x 0, however many sit elsewhere', () => {
    const g = graph({ a: { module: 'tall' }, b: { module: 'tall' } })
    expect(placeNew(g, catalog, { a: { x: 320, y: 0 }, b: { x: 640, y: 0 } })).toEqual({ x: 0, y: 0 })
  })

  it('reads positions by own key, so a prototype name is no position', () => {
    const g = graph({ constructor: { module: 'tall' } })
    expect(placeNew(g, catalog, {})).toEqual({ x: 0, y: 0 })
  })

  it('moves nothing it was given', () => {
    const g = graph({ a: { module: 'tall' } })
    const positions: Positions = { a: { x: 0, y: 0 } }
    placeNew(g, catalog, positions)
    expect(positions).toEqual({ a: { x: 0, y: 0 } })
  })
})
