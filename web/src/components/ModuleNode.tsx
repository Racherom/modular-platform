import { Handle, type NodeProps, Position } from '@xyflow/react'
import { type CSSProperties, memo } from 'react'
import { HANDLE_SIZE, type ModuleFlowNode, NODE_HEADER, NODE_WIDTH, PORT_ROW, providesHandle, usesHandle } from '../graph/flow'
import styles from './ModuleNode.module.css'
import { StateBadge } from './StateBadge'

// the geometry React Flow uses for the handles, handed to CSS so both agree
const geometry = {
  '--node-width': `${NODE_WIDTH}px`,
  '--node-header': `${NODE_HEADER}px`,
  '--port-row': `${PORT_ROW}px`,
  '--handle-size': `${HANDLE_SIZE}px`,
} as CSSProperties

/**
 * One instance on the canvas: its name and module, provides ports on the left, uses ports on
 * the right. A drag can start only at a uses port and end only at a provides port (AC-9).
 * Handles are pointer targets only (React Flow gives them no role); the port names beside them
 * are the text screen readers hear, and the inspector offers every edit from the keyboard.
 */
export const ModuleNode = memo(function ModuleNode({ data, isConnectable }: NodeProps<ModuleFlowNode>) {
  const rows = Math.max(data.provides.length, data.uses.length)
  const dragging = data.dropTargets !== undefined
  return (
    <div
      className={styles.node}
      style={geometry}
      data-error={data.hasError || undefined}
      data-dragging={dragging || undefined}
      data-testid={`node-${data.instance}`}
    >
      <div className={styles.head}>
        <span className={styles.instance}>{data.instance}</span>
        <span className={styles.module}>{data.known ? data.module : `${data.module} (not in catalog)`}</span>
        {data.state && <StateBadge module={data.state} />}
      </div>
      {Array.from({ length: rows }, (_, i) => {
        const provides = data.provides[i]
        const uses = data.uses[i]
        return (
          <div key={`${provides ?? ''}|${uses?.capability ?? ''}`} className={styles.row} style={{ top: NODE_HEADER + i * PORT_ROW }}>
            <span className={styles.provides}>
              {provides !== undefined && (
                <>
                  <Handle
                    id={providesHandle(provides)}
                    type="target"
                    position={Position.Left}
                    isConnectableStart={false}
                    isConnectableEnd={isConnectable}
                    className={styles.handle}
                    data-drop={dragging ? (data.dropTargets?.includes(provides) ? 'valid' : 'invalid') : undefined}
                    aria-hidden="true"
                  />
                  <span className={styles.label}>{provides}</span>
                </>
              )}
            </span>
            <span className={styles.uses}>
              {uses !== undefined && (
                <>
                  <span className={styles.label}>
                    {uses.capability}
                    <span className="visually-hidden">{uses.optional ? ', optional' : ', required'}</span>
                  </span>
                  <Handle
                    id={usesHandle(uses.capability)}
                    type="source"
                    position={Position.Right}
                    isConnectableStart={isConnectable}
                    isConnectableEnd={false}
                    className={styles.handle}
                    data-need={uses.optional ? 'optional' : 'required'}
                    data-drop={dragging ? (data.dragSource === uses.capability ? 'source' : 'invalid') : undefined}
                    aria-hidden="true"
                  />
                </>
              )}
            </span>
          </div>
        )
      })}
    </div>
  )
})
