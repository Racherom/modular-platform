import {
  Background,
  BackgroundVariant,
  type Connection,
  ConnectionMode,
  Controls,
  type Edge,
  type EdgeChange,
  type NodeChange,
  type OnConnectStart,
  ReactFlow,
  useReactFlow,
} from '@xyflow/react'
import { type KeyboardEvent, useCallback, useEffect, useMemo, useRef } from 'react'
import type { Catalog, Graph, Problem } from '../api/client'
import type { ModuleState } from '../api/platform'
import { bind, type Drop, dropText, isValidDrop, unbind } from '../graph/edit'
import {
  emphasize,
  type ModuleFlowNode,
  NODE_WIDTH,
  type Positions,
  parseHandle,
  providesHandle,
  reuseNodes,
  toFlow,
  usesHandle,
} from '../graph/flow'
import { layout } from '../graph/layout'
import { isFormControl } from '../lib/keys'
import { useEditor } from '../lib/store'
import { useResolvedTheme } from '../lib/theme'
import styles from './GraphView.module.css'
import { ModuleNode } from './ModuleNode'

const nodeTypes = { module: ModuleNode }

const ariaLabelConfig = {
  'node.a11yDescription.default': 'Press Enter or Space to select this module.',
  'edge.a11yDescription.default': 'Press Enter or Space to select this binding. Press Delete to remove it.',
  'controls.ariaLabel': 'Canvas controls',
  'controls.zoomIn.ariaLabel': 'Zoom in',
  'controls.zoomOut.ariaLabel': 'Zoom out',
  'controls.fitView.ariaLabel': 'Fit the graph to the view',
}

const prefersReducedMotion = () => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false

/**
 * Centers the canvas on the node the store names (spec 0029, AC-8; the problems panel uses the
 * same path), at the current zoom (at least 1), without animation under reduced motion. It uses
 * the stored position and the node's known size, since a new node is not drawn yet. Renders
 * inside the canvas, where React Flow's own provider is.
 */
function CenterOnRequest({ nodes }: { nodes: ModuleFlowNode[] }) {
  const flow = useReactFlow()
  const request = useEditor((s) => s.centerRequest)
  const { centered } = useEditor.getState()
  const latest = useRef(nodes)
  latest.current = nodes
  useEffect(() => {
    if (!request) return
    const frame = requestAnimationFrame(() => {
      const node = latest.current.find((n) => n.id === request)
      if (node) {
        void flow.setCenter(node.position.x + NODE_WIDTH / 2, node.position.y + (node.height ?? 0) / 2, {
          zoom: Math.max(flow.getZoom(), 1),
          duration: prefersReducedMotion() ? 0 : 200,
        })
      }
      centered()
    })
    return () => cancelAnimationFrame(frame)
  }, [request, flow, centered])
  return null
}

/** A graph edit, as a pure function on the graph (spec 0010). */
export type GraphEdit = (graph: Graph) => Graph

interface Props {
  graph: Graph
  catalog: Catalog
  problems: Problem[]
  /** Stored positions; without them the graph gets the auto layout. */
  positions?: Positions
  /** Live module states, by instance, shown as a badge on each node. */
  states?: Record<string, ModuleState>
  /** Applies an edit. Without it the canvas only shows the graph (selection still works). */
  onEdit?: (edit: GraphEdit) => void
}

/**
 * Draws a graph. The graph is the only source of truth: nodes and edges are derived from it on
 * every render and never edited in place; React Flow only draws and reports gestures (spec 0010).
 */
export function GraphView({ graph, catalog, problems, positions, states, onEdit }: Props) {
  const theme = useResolvedTheme()
  const editable = onEdit !== undefined
  const dragFrom = useEditor((s) => (editable ? s.dragFrom : null))
  const selection = useEditor((s) => s.selection)
  const focusRequest = useEditor((s) => s.focusRequest)
  const { setDragFrom, select, focused } = useEditor.getState()
  const wrapper = useRef<HTMLElement>(null)

  const placed = useMemo(() => positions ?? layout(graph), [positions, graph])
  const base = useMemo(() => {
    const flow = toFlow(graph, catalog, problems, placed)
    if (!states) return flow
    return { ...flow, nodes: flow.nodes.map((n) => ({ ...n, data: { ...n.data, state: states[n.data.instance] } })) }
  }, [graph, catalog, problems, placed, states])
  const shown = useMemo(() => emphasize(base.nodes, base.edges, selection), [base, selection])

  // during a drag every node learns which of its ports accept the drop (AC-9)
  const last = useRef<ModuleFlowNode[]>([])
  const nodes = useMemo(() => {
    const next = dragFrom
      ? shown.nodes.map((n) => ({
          ...n,
          data: {
            ...n.data,
            dropTargets: n.data.provides
              .map((p) => p.name)
              .filter((provide) =>
                isValidDrop(graph, catalog, {
                  source: dragFrom.instance,
                  sourceHandle: usesHandle(dragFrom.slot),
                  target: n.id,
                  targetHandle: providesHandle(provide),
                }),
              ),
            dragSource: n.id === dragFrom.instance ? dragFrom.slot : undefined,
          },
        }))
      : shown.nodes
    const kept = reuseNodes(last.current, next)
    last.current = kept
    return kept
  }, [shown, dragFrom, graph, catalog])

  // stable callbacks that read the latest graph and catalog, so React Flow never re-subscribes
  const current = useRef({ graph, catalog, onEdit, selection, edges: shown.edges })
  current.current = { graph, catalog, onEdit, selection, edges: shown.edges }
  const isValidConnection = useCallback((c: Drop) => isValidDrop(current.current.graph, current.current.catalog, c), [])
  const onConnect = useCallback((c: Connection) => {
    const { graph, catalog, onEdit } = current.current
    const drop = dropText(graph, catalog, c)
    if (!onEdit || !drop) return
    onEdit((g) => bind(g, c.source, drop.slot, drop.provider))
  }, [])
  // only the provider end moves (reconnectable 'target'), so the new connection keeps the slot
  const onReconnect = useCallback((_: Edge, c: Connection) => {
    const { graph, catalog, onEdit } = current.current
    const drop = dropText(graph, catalog, c)
    if (!onEdit || !drop) return
    onEdit((g) => bind(g, c.source, drop.slot, drop.provider))
  }, [])
  const onConnectStart = useCallback<OnConnectStart>(
    (_, { nodeId, handleId, handleType }) => {
      const need = parseHandle(handleId)
      if (handleType !== 'source' || !nodeId || need?.side !== 'uses') return
      setDragFrom({ instance: nodeId, slot: need.name })
    },
    [setDragFrom],
  )
  const onConnectEnd = useCallback(() => setDragFrom(null), [setDragFrom])

  // click and Enter both reach React Flow as select changes; the store owns the selection
  const onNodesChange = useCallback(
    (changes: NodeChange<ModuleFlowNode>[]) => {
      for (const c of changes) if (c.type === 'select' && c.selected) select({ kind: 'module', instance: c.id })
    },
    [select],
  )
  const onEdgesChange = useCallback(
    (changes: EdgeChange[]) => {
      for (const c of changes) {
        if (c.type !== 'select' || !c.selected) continue
        const edge = current.current.edges.find((e) => e.id === c.id)
        const need = parseHandle(edge?.sourceHandle)
        if (edge && need) select({ kind: 'binding', consumer: edge.source, slot: need.name })
      }
    },
    [select],
  )
  const onPaneClick = useCallback(() => select(null), [select])

  // Delete or Backspace unbinds the selected binding; Escape clears the selection (AC-8, AC-10)
  const onKeyDown = (e: KeyboardEvent) => {
    if (isFormControl(e.target)) return
    const { selection, onEdit } = current.current
    if (e.key === 'Escape' && selection) {
      select(null)
      return
    }
    if ((e.key === 'Delete' || e.key === 'Backspace') && selection?.kind === 'binding' && onEdit) {
      e.preventDefault()
      onEdit((g) => unbind(g, selection.consumer, selection.slot))
    }
  }

  // the store asks for focus when the selection moved to a node (AC-8, AC-13)
  useEffect(() => {
    if (!focusRequest) return
    const frame = requestAnimationFrame(() => {
      const node = wrapper.current?.querySelector<HTMLElement>(`.react-flow__node[data-id="${CSS.escape(focusRequest)}"]`)
      node?.focus()
      focused()
    })
    return () => cancelAnimationFrame(frame)
  }, [focusRequest, focused])

  // a selection belongs to the graph on screen; leaving the screen drops it
  useEffect(() => () => select(null), [select])

  return (
    <section ref={wrapper} className={styles.view} aria-label={`Graph ${graph.name}`} tabIndex={-1} onKeyDown={onKeyDown}>
      <ReactFlow
        nodes={nodes}
        edges={shown.edges}
        nodeTypes={nodeTypes}
        colorMode={theme}
        nodesDraggable={false}
        nodesConnectable={editable}
        nodesFocusable
        edgesFocusable
        elementsSelectable
        edgesReconnectable={editable}
        connectionMode={ConnectionMode.Strict}
        isValidConnection={isValidConnection}
        onConnect={onConnect}
        onConnectStart={onConnectStart}
        onConnectEnd={onConnectEnd}
        onReconnect={editable ? onReconnect : undefined}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onPaneClick={onPaneClick}
        deleteKeyCode={null}
        multiSelectionKeyCode={null}
        selectionKeyCode={null}
        ariaLabelConfig={ariaLabelConfig}
        fitView
        proOptions={{ hideAttribution: false }}
      >
        <Background variant={BackgroundVariant.Dots} gap={16} size={1.5} />
        <Controls showInteractive={false} />
        <CenterOnRequest nodes={nodes} />
      </ReactFlow>
    </section>
  )
}
