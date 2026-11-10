import type { Problem } from '../api/client'
import { problemInstance } from '../graph/flow'
import styles from './ProblemList.module.css'

/** The renderer's problems as a plain list, for review (spec 0006). */
export function ProblemList({ problems }: { problems: Problem[] }) {
  if (problems.length === 0) return <p className={styles.none}>No problems.</p>
  return (
    <ul className={styles.problems} aria-label="Problems">
      {problems.map((p, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: the list is never reordered, and two problems may share a path and code
        <li key={`${i}|${p.path}|${p.code}`} className={styles.problem} data-severity={p.severity}>
          <span className={styles.severity}>{p.severity}</span>
          {problemInstance(p) !== undefined && <span className={styles.instance}>{problemInstance(p)}</span>}
          <span>{p.message}</span>
        </li>
      ))}
    </ul>
  )
}
