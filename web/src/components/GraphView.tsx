import { Background, Controls, ReactFlow } from '@xyflow/react'
import { useMemo } from 'react'
import type { CatalogEntry, Graph, Problem } from '../api/client'
import { toFlow } from '../graph/flow'
import { ModuleNode } from './ModuleNode'

const nodeTypes = { module: ModuleNode }

interface Props {
  graph: Graph
  catalog: CatalogEntry[]
  problems: Problem[]
}

/** Shows a graph. Drawing and deleting nodes and edges come with features #5 and #6. */
export function GraphView({ graph, catalog, problems }: Props) {
  const { nodes, edges } = useMemo(() => toFlow(graph, catalog, problems), [graph, catalog, problems])
  return (
    <div className="graph-view" aria-label={`Graph ${graph.name}`}>
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
    </div>
  )
}
