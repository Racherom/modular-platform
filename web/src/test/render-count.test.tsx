import { render } from '@testing-library/react'
import { ReactFlowProvider } from '@xyflow/react'
import { describe, expect, it, vi } from 'vitest'
import type { Graph, Problem } from '../api/client'
import { GraphView } from '../components/GraphView'
import { bind } from '../graph/edit'
import { toFlow } from '../graph/flow'
import { layout } from '../graph/layout'
import { testCatalog, testModule } from './fixtures'

// Every render of a node, by instance. The stand in is memoized like the real ModuleNode, so it
// renders again only when React Flow hands it a new node object.
const renders: string[] = vi.hoisted(() => [])

vi.mock('../components/ModuleNode', async () => {
  const { memo } = await import('react')
  return {
    ModuleNode: memo(function CountingNode({ data }: { data: { instance: string } }) {
      renders.push(data.instance)
      return <div data-testid={`node-${data.instance}`} />
    }),
  }
})

const entry = (name: string, provides: string[], uses: string[]) =>
  testModule(name, {
    provides: Object.fromEntries(provides.map((c) => [c, { port: 80 }])),
    uses: Object.fromEntries(uses.map((c) => [c, {}])),
  })

const catalog = testCatalog([entry('db', ['postgresql'], []), entry('app', ['http'], ['postgresql']), entry('proxy', [], ['http'])])

/** Ten apps with their own database each, and a proxy in front of the first. */
function graph(): Graph {
  const modules: Graph['modules'] = { proxy: { module: 'proxy', uses: { http: 'app0' } } }
  for (let i = 0; i < 10; i++) {
    modules[`db${i}`] = { module: 'db' }
    modules[`app${i}`] = { module: 'app', uses: { postgresql: i === 3 ? null : `db${i}` } }
  }
  return { apiVersion: 'modular-platform/v1', kind: 'Graph', name: 'scale', modules }
}

function view(g: Graph, positions: ReturnType<typeof layout>, problems: Problem[] = []) {
  return (
    <ReactFlowProvider>
      <div style={{ width: 1200, height: 800 }}>
        <GraphView graph={g} catalog={catalog} problems={problems} positions={positions} onEdit={() => undefined} />
      </div>
    </ReactFlowProvider>
  )
}

/** The instances whose node data differs between two flows, as toFlow builds them. */
function changed(a: Graph, b: Graph, positions: ReturnType<typeof layout>, problems: Problem[] = []): string[] {
  const before = new Map(toFlow(a, catalog, [], positions).nodes.map((n) => [n.id, JSON.stringify(n.data)]))
  return toFlow(b, catalog, problems, positions)
    .nodes.filter((n) => before.get(n.id) !== JSON.stringify(n.data))
    .map((n) => n.id)
    .sort()
}

describe('render count (AC-7)', () => {
  it('a connect renders again only the nodes whose data changed', () => {
    const before = graph()
    const after = bind(before, 'app3', 'postgresql', 'db3')
    const positions = layout(before)
    const { rerender, getAllByTestId } = render(view(before, positions))
    expect(getAllByTestId(/^node-/)).toHaveLength(21)

    renders.length = 0
    rerender(view(after, positions))
    expect([...new Set(renders)].sort()).toEqual(changed(before, after, positions))
  })

  it('a problem on one module renders only that node again', () => {
    const g = graph()
    const positions = layout(g)
    const problems: Problem[] = [
      { severity: 'error', code: 'unbound_required', path: 'modules.app3.uses.postgresql', message: 'app3 needs postgresql.' },
    ]
    const { rerender } = render(view(g, positions))
    renders.length = 0
    rerender(view(g, positions, problems))
    expect(changed(g, g, positions, problems)).toEqual(['app3'])
    expect([...new Set(renders)]).toEqual(['app3'])
  })

  it('a render with the same graph renders no node again', () => {
    const g = graph()
    const positions = layout(g)
    const { rerender } = render(view(g, positions))
    renders.length = 0
    rerender(view(g, positions))
    expect(renders).toEqual([])
  })
})
