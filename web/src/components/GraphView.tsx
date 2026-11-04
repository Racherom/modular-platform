import { Background, BackgroundVariant, type Connection, ConnectionMode, Controls, type OnConnectStart, ReactFlow } from '@xyflow/react'
import { useCallback, useMemo, useRef } from 'react'
import type { CatalogEntry, Graph, Problem } from '../api/client'
import type { ModuleState } from '../api/platform'
import { type Drop, isValidDrop } from '../graph/edit'
import { type ModuleFlowNode, type Positions, parseHandle, providesHandle, reuseNodes, toFlow, usesHandle } from '../graph/flow'
import { layout } from '../graph/layout'
import { useEditor } from '../lib/store'
import { useResolvedTheme } from '../lib/theme'
import styles from './GraphView.module.css'
import { ModuleNode } from './ModuleNode'

const nodeTypes = { module: ModuleNode }

const ariaLabelConfig = {
  'node.a11yDescription.default': 'Press Enter or Space to select this module.',
  'edge.a11yDescription.default': 'Press Enter or Space to select this binding.',
  'controls.ariaLabel': 'Canvas controls',
  'controls.zoomIn.ariaLabel': 'Zoom in',
  'controls.zoomOut.ariaLabel': 'Zoom out',
  'controls.fitView.ariaLabel': 'Fit the graph to the view',
}

interface Props {
  graph: Graph
  catalog: CatalogEntry[]
  problems: Problem[]
  /** Stored positions; without them the graph gets the auto layout. */
  positions?: Positions
  /** Live module states, by instance, shown as a badge on each node. */
  states?: Record<string, ModuleState>
  /** Binds a need after a valid drop. Without it the canvas only shows the graph. */
  onBind?: (consumer: string, capability: string, provider: string) => void
}

/**
 * Draws a graph. The graph is the only source of truth: nodes and edges are derived from it on
 * every render and never edited in place; React Flow only draws and reports gestures (spec 0010).
 */
export function GraphView({ graph, catalog, problems, positions, states, onBind }: Props) {
  const theme = useResolvedTheme()
  const dragFrom = useEditor((s) => (onBind ? s.dragFrom : null))
  const setDragFrom = useEditor((s) => s.setDragFrom)

  const placed = useMemo(() => positions ?? layout(graph), [positions, graph])
  const base = useMemo(() => {
    const flow = toFlow(graph, catalog, problems, placed)
    if (!states) return flow
    return { ...flow, nodes: flow.nodes.map((n) => ({ ...n, data: { ...n.data, state: states[n.data.instance] } })) }
  }, [graph, catalog, problems, placed, states])

  // during a drag every node learns which of its ports accept the drop (AC-9)
  const last = useRef<ModuleFlowNode[]>([])
  const nodes = useMemo(() => {
    const next = dragFrom
      ? base.nodes.map((n) => ({
          ...n,
          data: {
            ...n.data,
            dropTargets: n.data.provides.filter((c) =>
              isValidDrop(graph, catalog, {
                source: dragFrom.instance,
                sourceHandle: usesHandle(dragFrom.capability),
                target: n.id,
                targetHandle: providesHandle(c),
              }),
            ),
            dragSource: n.id === dragFrom.instance ? dragFrom.capability : undefined,
          },
        }))
      : base.nodes
    const kept = reuseNodes(last.current, next)
    last.current = kept
    return kept
  }, [base, dragFrom, graph, catalog])

  // stable callbacks that read the latest graph and catalog, so React Flow never re-subscribes
  const current = useRef({ graph, catalog, onBind })
  current.current = { graph, catalog, onBind }
  const isValidConnection = useCallback((c: Drop) => isValidDrop(current.current.graph, current.current.catalog, c), [])
  const onConnect = useCallback((c: Connection) => {
    const { graph, catalog, onBind } = current.current
    const need = parseHandle(c.sourceHandle)
    if (!onBind || !need || !isValidDrop(graph, catalog, c)) return
    onBind(c.source, need.capability, c.target)
  }, [])
  const onConnectStart = useCallback<OnConnectStart>(
    (_, { nodeId, handleId, handleType }) => {
      const need = parseHandle(handleId)
      if (handleType !== 'source' || !nodeId || need?.side !== 'uses') return
      setDragFrom({ instance: nodeId, capability: need.capability })
    },
    [setDragFrom],
  )
  const onConnectEnd = useCallback(() => setDragFrom(null), [setDragFrom])

  return (
    <section className={styles.view} aria-label={`Graph ${graph.name}`}>
      <ReactFlow
        nodes={nodes}
        edges={base.edges}
        nodeTypes={nodeTypes}
        colorMode={theme}
        nodesDraggable={false}
        nodesConnectable={onBind !== undefined}
        nodesFocusable
        edgesFocusable
        elementsSelectable
        edgesReconnectable={false}
        connectionMode={ConnectionMode.Strict}
        isValidConnection={isValidConnection}
        onConnect={onConnect}
        onConnectStart={onConnectStart}
        onConnectEnd={onConnectEnd}
        deleteKeyCode={null}
        ariaLabelConfig={ariaLabelConfig}
        fitView
        proOptions={{ hideAttribution: false }}
      >
        <Background variant={BackgroundVariant.Dots} gap={16} size={1.5} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </section>
  )
}
