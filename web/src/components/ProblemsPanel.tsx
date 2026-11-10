import { useReactFlow } from '@xyflow/react'
import { Button as AriaButton } from 'react-aria-components'
import type { Graph, Problem } from '../api/client'
import { problemInstance } from '../graph/flow'
import { useEditor } from '../lib/store'
import { ProblemMark } from './ModuleNode'
import styles from './ProblemsPanel.module.css'

const prefersReducedMotion = () => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false

/**
 * The problems of the shown check result. A problem that belongs to a module is a button: it selects
 * the module, centers the canvas on it at the current zoom (at least 1) and moves focus to it
 * (AC-13). Must render inside the canvas's ReactFlowProvider.
 */
export function ProblemsPanel({ problems, graph }: { problems: Problem[]; graph: Graph }) {
  const flow = useReactFlow()
  const focusModule = useEditor((s) => s.focusModule)

  const show = (instance: string) => {
    focusModule(instance)
    const node = flow.getInternalNode(instance)
    if (!node) return
    const { x, y } = node.internals.positionAbsolute
    const width = node.measured.width ?? node.width ?? 0
    const height = node.measured.height ?? node.height ?? 0
    void flow.setCenter(x + width / 2, y + height / 2, {
      zoom: Math.max(flow.getZoom(), 1),
      duration: prefersReducedMotion() ? 0 : 200,
    })
  }

  if (problems.length === 0) return <p className={styles.none}>No problems.</p>
  return (
    <ul className={styles.list} aria-label="Problems">
      {problems.map((p, i) => {
        const instance = problemInstance(p)
        const content = (
          <>
            <ProblemMark mark={{ severity: p.severity, messages: [p.message] }} decorative />
            {instance !== undefined && <span className={styles.instance}>{instance}</span>}
            <span className={styles.message}>{p.message}</span>
          </>
        )
        return (
          // biome-ignore lint/suspicious/noArrayIndexKey: never reordered, and two problems may share a path and code
          <li key={`${i}|${p.path}|${p.code}`} data-severity={p.severity}>
            {instance !== undefined && Object.hasOwn(graph.modules, instance) ? (
              <AriaButton className={styles.problem} onPress={() => show(instance)}>
                {content}
              </AriaButton>
            ) : (
              <div className={styles.problem}>{content}</div>
            )}
          </li>
        )
      })}
    </ul>
  )
}
