import { MarkerType, Position, type Edge, type Node, type NodeHandle } from '@xyflow/react'
import type { CatalogEntry, Graph, Problem } from '../api/client'
import { layout } from './layout'

export const NODE_WIDTH = 220
const HEADER = 52
const PORT = 26
const HANDLE = 10

export interface ModuleNodeData extends Record<string, unknown> {
  instance: string
  module: string
  known: boolean
  provides: string[]
  uses: { capability: string; optional: boolean }[]
  hasError: boolean
}

export type ModuleFlowNode = Node<ModuleNodeData, 'module'>

/** The vertical centre of the i-th port row inside a node. */
export function portY(i: number): number {
  return HEADER + i * PORT + PORT / 2
}

/** The instance a problem belongs to, from its path like modules.whoami.uses.postgresql. */
export function problemInstance(p: Problem): string | undefined {
  const [root, instance] = p.path.split('.')
  return root === 'modules' ? instance : undefined
}

/**
 * Builds the canvas: one node per instance with a handle per provides capability on the left
 * (provides:<c>) and per uses capability on the right (uses:<c>), and one edge per non null uses
 * value from the consumer's uses handle to the provider's provides handle, marker at the provider.
 * Nodes carry their size and handles, so edges draw before the browser measures anything.
 */
export function toFlow(graph: Graph, catalog: CatalogEntry[], problems: Problem[]) {
  const entries = new Map(catalog.map((e) => [e.name, e]))
  const failing = new Set(problems.filter((p) => p.severity === 'error').map(problemInstance))
  const positions = layout(graph)
  const names = Object.keys(graph.modules).sort()

  const nodes: ModuleFlowNode[] = names.map((instance) => {
    const m = graph.modules[instance]!
    const entry = entries.get(m.module)
    const provides = Object.keys(entry?.provides ?? {}).sort()
    const uses = Object.entries(entry?.uses ?? {})
      .map(([capability, u]) => ({ capability, optional: u.optional ?? false }))
      .sort((a, b) => (a.capability < b.capability ? -1 : 1))
    const height = HEADER + Math.max(provides.length, uses.length, 1) * PORT + 8
    const handles: NodeHandle[] = [
      ...provides.map((c, i) => handle(`provides:${c}`, 'target', Position.Left, -HANDLE / 2, portY(i))),
      ...uses.map((u, i) => handle(`uses:${u.capability}`, 'source', Position.Right, NODE_WIDTH - HANDLE / 2, portY(i))),
    ]
    return {
      id: instance,
      type: 'module',
      position: positions[instance] ?? { x: 0, y: 0 },
      width: NODE_WIDTH,
      height,
      handles,
      data: { instance, module: m.module, known: entry !== undefined, provides, uses, hasError: failing.has(instance) },
    }
  })

  const byId = new Map(nodes.map((n) => [n.id, n]))
  const edges: Edge[] = []
  for (const consumer of names) {
    for (const [capability, provider] of Object.entries(graph.modules[consumer]?.uses ?? {})) {
      if (provider === null) continue
      const from = byId.get(consumer)
      const to = byId.get(provider)
      // a broken file can bind to a missing instance or port; its problem is listed instead
      if (!from?.handles?.some((h) => h.id === `uses:${capability}`)) continue
      if (!to?.handles?.some((h) => h.id === `provides:${capability}`)) continue
      edges.push({
        id: `${consumer}.${capability}`,
        source: consumer,
        sourceHandle: `uses:${capability}`,
        target: provider,
        targetHandle: `provides:${capability}`,
        markerEnd: { type: MarkerType.ArrowClosed, width: 18, height: 18 },
      })
    }
  }
  return { nodes, edges }
}

function handle(id: string, type: 'source' | 'target', position: Position, x: number, y: number): NodeHandle {
  return { id, type, position, x, y: y - HANDLE / 2, width: HANDLE, height: HANDLE }
}
