import { Background, Controls, ReactFlow } from '@xyflow/react'
import { useMemo } from 'react'
import type { CatalogEntry, Graph, Problem } from '../api/client'
import type { ModuleState } from '../api/platform'
import { toFlow } from '../graph/flow'
import { ModuleNode } from './ModuleNode'

const nodeTypes = { module: ModuleNode }

interface Props {
  graph: Graph
  catalog: CatalogEntry[]
  problems: Problem[]
  /** Live module states, by instance, shown as a badge on each node. */
  states?: Record<string, ModuleState>
}

/** Shows a graph. Drawing and deleting nodes and edges come with features #5 and #6. */
export function GraphView({ graph, catalog, problems, states }: Props) {
  const { nodes, edges } = useMemo(() => {
    const flow = toFlow(graph, catalog, problems)
    if (!states) return flow
    return { ...flow, nodes: flow.nodes.map((n) => ({ ...n, data: { ...n.data, state: states[n.data.instance] } })) }
  }, [graph, catalog, problems, states])
  return (
    <section className="graph-view" aria-label={`Graph ${graph.name}`}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        nodesConnectable={false}
        edgesReconnectable={false}
        deleteKeyCode={null}
        fitView
        proOptions={{ hideAttribution: false }}
      >
        <Background />
        <Controls showInteractive={false} />
      </ReactFlow>
    </section>
  )
}
