import type { Problem } from '../api/client'
import { problemInstance } from '../graph/flow'

export function ProblemList({ problems }: { problems: Problem[] }) {
  if (problems.length === 0) return <p className="problems problems--none">No problems.</p>
  return (
    <ul className="problems" aria-label="Problems">
      {problems.map((p, i) => (
        <li key={`${i}|${p.path}|${p.code}`}className={`problem problem--${p.severity}`}>
          <span className="problem__severity">{p.severity}</span>
          {problemInstance(p) !== undefined && <span className="problem__instance">{problemInstance(p)}</span>}
          <span>{p.message}</span>
        </li>
      ))}
    </ul>
  )
}
