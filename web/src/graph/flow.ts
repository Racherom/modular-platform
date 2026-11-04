import { type Edge, MarkerType, type Node, type NodeHandle, Position } from '@xyflow/react'
import type { CatalogEntry, Graph, Problem } from '../api/client'
import type { ModuleState } from '../api/platform'

/**
 * Node geometry (spec 0010). React Flow needs handle positions before it measures anything, so
 * these live here; ModuleNode writes them as CSS custom properties, so CSS and TS share them.
 */
export const NODE_WIDTH = 240
export const NODE_HEADER = 56
export const PORT_ROW = 32
export const HANDLE_SIZE = 12

/** An instance's place on the canvas. */
export type Positions = Record<string, { x: number; y: number }>

/** A problem mark on a port or a node: the worst severity, and every message for its name (AC-13). */
export interface Mark {
  severity: 'error' | 'warning'
  messages: string[]
}

/** The selection, as the canvas needs it (mirrors the store's Selection). */
export type CanvasSelection = null | { kind: 'module'; instance: string } | { kind: 'binding'; consumer: string; capability: string }

/** Everything a module node draws. */
export interface ModuleNodeData extends Record<string, unknown> {
  instance: string
  module: string
  known: boolean
  provides: string[]
  uses: { capability: string; optional: boolean; mark?: Mark }[]
  hasError: boolean
  /** Problems of the module that no drawn port takes. */
  mark?: Mark
  /** Dimmed because something unrelated is selected (AC-8). */
  dimmed?: boolean
  /** Handle ids highlighted because they belong to the selection. */
  highlight?: string[]
  /** The module's live state on a deployment screen. */
  state?: ModuleState
  /** Only during a connect drag: the provides capabilities of this node that accept the drop. */
  dropTargets?: string[]
  /** Only during a connect drag: the uses capability the drag started from, on this node. */
  dragSource?: string
}

/** A module node as React Flow sees it. */
export type ModuleFlowNode = Node<ModuleNodeData, 'module'>

/** The handle id of a capability a module uses (a connection source). */
export const usesHandle = (capability: string) => `uses:${capability}`
/** The handle id of a capability a module provides (a connection target). */
export const providesHandle = (capability: string) => `provides:${capability}`

/** Splits a handle id like uses:postgresql; anything else gives undefined. */
export function parseHandle(id: string | null | undefined): { side: 'uses' | 'provides'; capability: string } | undefined {
  if (!id) return undefined
  const at = id.indexOf(':')
  const side = id.slice(0, at)
  if (at < 1 || (side !== 'uses' && side !== 'provides')) return undefined
  return { side, capability: id.slice(at + 1) }
}

/** The vertical centre of the i-th port row inside a node. */
export function portY(i: number): number {
  return NODE_HEADER + i * PORT_ROW + PORT_ROW / 2
}

/** The instance a problem belongs to, from its path like modules.whoami.uses.postgresql. */
export function problemInstance(p: Problem): string | undefined {
  const [root, instance] = p.path.split('.')
  return root === 'modules' ? instance : undefined
}

/** The need a problem is about, when its path is exactly modules.<i>.uses.<C>. */
export function problemNeed(p: Problem): { instance: string; capability: string } | undefined {
  const parts = p.path.split('.')
  if (parts.length !== 4 || parts[0] !== 'modules' || parts[2] !== 'uses') return undefined
  return { instance: parts[1]!, capability: parts[3]! }
}

/** Folds problems into one mark; an error wins over warnings, and every message is kept. */
export function markOf(problems: Problem[]): Mark | undefined {
  if (problems.length === 0) return undefined
  return {
    severity: problems.some((p) => p.severity === 'error') ? 'error' : 'warning',
    messages: problems.map((p) => p.message),
  }
}

/**
 * Builds the canvas: one node per instance with a handle per provides capability on the left
 * (provides:<c>) and per uses capability on the right (uses:<c>), and one smooth step edge per
 * non null uses value from the consumer's uses handle to the provider's provides handle, marker
 * at the provider. Nodes carry their size and handles, so edges draw before the browser
 * measures anything. An instance without a position goes to the origin; callers lay out first.
 */
export function toFlow(graph: Graph, catalog: CatalogEntry[], problems: Problem[], positions: Positions) {
  const entries = new Map(catalog.map((e) => [e.name, e]))
  const failing = new Set(problems.filter((p) => p.severity === 'error').map(problemInstance))
  const names = Object.keys(graph.modules).sort()
  // problems by instance; Maps, since a broken file may name an instance like constructor
  const byInstance = new Map<string, Problem[]>()
  for (const p of problems) {
    const i = problemInstance(p)
    if (i === undefined) continue
    byInstance.set(i, [...(byInstance.get(i) ?? []), p])
  }

  const nodes: ModuleFlowNode[] = names.map((instance) => {
    const m = graph.modules[instance]!
    const entry = entries.get(m.module)
    const provides = Object.keys(entry?.provides ?? {}).sort()
    const own = byInstance.get(instance) ?? []
    const drawn = new Set(Object.keys(entry?.uses ?? {}))
    // a need problem marks its port when the port is drawn, else the node (AC-13)
    const onPort = (p: Problem) => {
      const need = problemNeed(p)
      return need !== undefined && drawn.has(need.capability) ? need.capability : undefined
    }
    const uses = Object.entries(entry?.uses ?? {})
      .map(([capability, u]) => {
        const mark = markOf(own.filter((p) => onPort(p) === capability))
        return { capability, optional: u.optional ?? false, ...(mark ? { mark } : {}) }
      })
      .sort((a, b) => (a.capability < b.capability ? -1 : a.capability > b.capability ? 1 : 0))
    const mark = markOf(own.filter((p) => onPort(p) === undefined))
    const height = NODE_HEADER + Math.max(provides.length, uses.length, 1) * PORT_ROW + 8
    const handles: NodeHandle[] = [
      ...provides.map((c, i) => handle(providesHandle(c), 'target', Position.Left, -HANDLE_SIZE / 2, portY(i))),
      ...uses.map((u, i) => handle(usesHandle(u.capability), 'source', Position.Right, NODE_WIDTH - HANDLE_SIZE / 2, portY(i))),
    ]
    return {
      id: instance,
      type: 'module',
      position: (Object.hasOwn(positions, instance) ? positions[instance] : undefined) ?? { x: 0, y: 0 },
      width: NODE_WIDTH,
      height,
      handles,
      ariaLabel: `${instance}, module ${m.module}`,
      data: {
        instance,
        module: m.module,
        known: entry !== undefined,
        provides,
        uses,
        hasError: failing.has(instance),
        ...(mark ? { mark } : {}),
      },
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
      if (!from?.handles?.some((h) => h.id === usesHandle(capability))) continue
      if (!to?.handles?.some((h) => h.id === providesHandle(capability))) continue
      edges.push({
        id: `${consumer}.${capability}`,
        type: 'smoothstep',
        source: consumer,
        sourceHandle: usesHandle(capability),
        target: provider,
        targetHandle: providesHandle(capability),
        ariaLabel: `${consumer} uses ${capability} from ${provider}`,
        markerEnd: { type: MarkerType.ArrowClosed, width: 18, height: 18 },
        // only the provider end moves; the consumer end belongs to the need (AC-9)
        reconnectable: 'target',
      })
    }
  }
  return { nodes, edges }
}

/**
 * Applies the selection to a derived canvas (AC-8): the selected node or edge is marked
 * selected, the edges and ports that belong to the selection are highlighted, and every other
 * edge and node border is dimmed. Without a selection nothing changes.
 */
export function emphasize(nodes: ModuleFlowNode[], edges: Edge[], selection: CanvasSelection) {
  if (!selection) return { nodes, edges }
  const related =
    selection.kind === 'module'
      ? edges.filter((e) => e.source === selection.instance || e.target === selection.instance)
      : edges.filter((e) => e.id === `${selection.consumer}.${selection.capability}`)
  const relatedIds = new Set(related.map((e) => e.id))
  const lit = new Map<string, string[]>()
  const light = (node: string, handleId: string | null | undefined) => {
    if (handleId) lit.set(node, [...(lit.get(node) ?? []), handleId])
  }
  for (const e of related) {
    light(e.source, e.sourceHandle)
    light(e.target, e.targetHandle)
  }
  const near = new Set<string>(related.flatMap((e) => [e.source, e.target]))
  if (selection.kind === 'module') near.add(selection.instance)

  return {
    nodes: nodes.map((n) => {
      const selected = selection.kind === 'module' && n.id === selection.instance
      const highlight = lit.get(n.id)
      return {
        ...n,
        selected,
        data: {
          ...n.data,
          ...(near.has(n.id) ? {} : { dimmed: true }),
          ...(highlight ? { highlight: [...new Set(highlight)].sort() } : {}),
        },
      }
    }),
    edges: edges.map((e) => {
      const isRelated = relatedIds.has(e.id)
      return {
        ...e,
        selected: selection.kind === 'binding' && isRelated,
        className: isRelated ? 'is-related' : 'is-dimmed',
      }
    }),
  }
}

/**
 * Keeps the node objects of the last render whose content did not change, so React Flow draws
 * again only the nodes an edit touched (AC-7).
 */
export function reuseNodes(prev: ModuleFlowNode[], next: ModuleFlowNode[]): ModuleFlowNode[] {
  const old = new Map(prev.map((n) => [n.id, n]))
  return next.map((n) => {
    const p = old.get(n.id)
    return p && sameNode(p, n) ? p : n
  })
}

function sameNode(a: ModuleFlowNode, b: ModuleFlowNode): boolean {
  return (
    a.position.x === b.position.x &&
    a.position.y === b.position.y &&
    a.height === b.height &&
    a.selected === b.selected &&
    a.ariaLabel === b.ariaLabel &&
    JSON.stringify(a.data) === JSON.stringify(b.data)
  )
}

function handle(id: string, type: 'source' | 'target', position: Position, x: number, y: number): NodeHandle {
  return { id, type, position, x, y: y - HANDLE_SIZE / 2, width: HANDLE_SIZE, height: HANDLE_SIZE }
}
