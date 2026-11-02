import { Handle, type NodeProps, Position } from '@xyflow/react'
import { type ModuleFlowNode, portY } from '../graph/flow'

export function ModuleNode({ data }: NodeProps<ModuleFlowNode>) {
  return (
    <div className={`module-node${data.hasError ? ' has-error' : ''}`} data-testid={`node-${data.instance}`}>
      <div className="module-node__head">
        <strong>{data.instance}</strong>
        <span>{data.known ? data.module : `${data.module} (not in catalog)`}</span>
      </div>
      {data.provides.map((c, i) => (
        <div key={`p-${c}`} className="port port--provides" style={{ top: portY(i) }}>
          <Handle id={`provides:${c}`} type="target" position={Position.Left} isConnectable={false} />
          <span>{c}</span>
        </div>
      ))}
      {data.uses.map((u, i) => (
        <div key={`u-${u.capability}`} className="port port--uses" style={{ top: portY(i) }}>
          <span>{u.capability}</span>
          <Handle
            id={`uses:${u.capability}`}
            type="source"
            position={Position.Right}
            isConnectable={false}
            className={u.optional ? 'handle--optional' : undefined}
            title={u.optional ? 'optional need' : 'required need'}
          />
        </div>
      ))}
    </div>
  )
}
