import { describe, expect, it } from 'vitest'
import type { Graph } from '../api/client'
import { COLUMN_GAP, layout, ROW_GAP } from '../graph/layout'

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

  it('does not loop on a cycle', () => {
    const pos = layout(graph({ a: { module: 'm', uses: { x: 'b' } }, b: { module: 'm', uses: { x: 'a' } } }))
    expect(Object.keys(pos)).toEqual(['a', 'b'])
  })
})
