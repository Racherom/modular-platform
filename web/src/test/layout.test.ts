import { describe, expect, it } from 'vitest'
import type { Graph } from '../api/client'
import { COLUMN_GAP, layout, ROW_GAP } from '../graph/layout'
import { withPositions } from '../lib/storage'

const graph = (modules: Graph['modules']): Graph => ({ apiVersion: 'modular-platform/v0alpha1', kind: 'Graph', name: 'g', modules })

describe('layout', () => {
  it('puts every provider right of its consumers', () => {
    const pos = layout(
      graph({
        web: { module: 'm', uses: { api: 'api' } },
        api: { module: 'm', uses: { db: 'db' } },
        admin: { module: 'm', uses: { db: 'db' } },
        db: { module: 'm' },
      }),
    )
    expect(pos.admin).toEqual({ x: 0, y: 0 })
    expect(pos.web).toEqual({ x: 0, y: ROW_GAP })
    expect(pos.api).toEqual({ x: COLUMN_GAP, y: 0 })
    expect(pos.db).toEqual({ x: 2 * COLUMN_GAP, y: 0 })
  })

  it('treats prototype names as plain names', () => {
    const pos = layout(
      graph({
        constructor: { module: 'm', uses: { x: '__proto__' } },
        ['__proto__']: { module: 'm', uses: { y: 'toString' } }, // computed, so an own key like parsed JSON
      }),
    )
    expect(pos.constructor).toEqual({ x: 0, y: 0 })
    expect(Object.getOwnPropertyDescriptor(pos, '__proto__')?.value).toEqual({ x: COLUMN_GAP, y: 0 })
  })

  it('does not loop on a cycle', () => {
    const pos = layout(graph({ a: { module: 'm', uses: { x: 'b' } }, b: { module: 'm', uses: { x: 'a' } } }))
    expect(Object.keys(pos)).toEqual(['a', 'b'])
  })
})

describe('withPositions (AC-16)', () => {
  const g: Graph = {
    apiVersion: 'modular-platform/v0alpha1',
    kind: 'Graph',
    name: 'g',
    modules: { a: { module: 'x' }, b: { module: 'x', uses: { c: 'a' } } },
  }
  const w = { fileName: 'g.yaml', graph: g, check: { ok: true, graph: g, bindings: [], problems: [] } }

  it('keeps stored positions when every instance has one', () => {
    const positions = { a: { x: 7, y: 7 }, b: { x: 9, y: 9 } }
    expect(withPositions({ ...w, positions }).positions).toBe(positions)
  })

  it('lays out the whole graph when one instance has none', () => {
    expect(withPositions({ ...w, positions: { a: { x: 7, y: 7 } } }).positions).toEqual(layout(g))
  })

  it('lays out a stored entry from before positions existed', () => {
    expect(withPositions(w).positions).toEqual(layout(g))
  })

  it('drops positions of instances the graph no longer has', () => {
    const positions = { a: { x: 1, y: 1 }, b: { x: 2, y: 2 }, gone: { x: 3, y: 3 } }
    expect(withPositions({ ...w, positions }).positions).toEqual({ a: { x: 1, y: 1 }, b: { x: 2, y: 2 } })
  })
})
