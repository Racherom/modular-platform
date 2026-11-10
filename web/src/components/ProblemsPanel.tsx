import { Button as AriaButton } from 'react-aria-components'
import type { Graph, Problem } from '../api/client'
import { problemInstance } from '../graph/flow'
import { useEditor } from '../lib/store'
import { ProblemMark } from './ModuleNode'
import styles from './ProblemsPanel.module.css'

/**
 * The problems of the shown check result. A problem that belongs to a module is a button: it selects
 * the module, centers the canvas on it at the current zoom (at least 1) and moves focus to it
 * (AC-13); the canvas does the centering when the store asks it to (spec 0029, AC-8).
 */
export function ProblemsPanel({ problems, graph }: { problems: Problem[]; graph: Graph }) {
  const show = useEditor((s) => s.showModule)

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
