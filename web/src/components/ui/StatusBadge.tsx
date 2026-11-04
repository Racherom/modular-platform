import type { ReactNode } from 'react'
import styles from './StatusBadge.module.css'

/** The meaning of a state: neutral, in progress (info), fine (success), needs a look (warning), broken (danger). */
export type StatusTone = 'neutral' | 'info' | 'success' | 'warning' | 'danger'

/** Props of {@link StatusBadge}. */
export interface StatusBadgeProps {
  tone: StatusTone
  /** The state in words; the dot only repeats it, so color is never the only signal. */
  children: ReactNode
  /** Longer details, shown on hover. */
  title?: string
  'data-testid'?: string
}

/** The state of something that runs, such as a module or a deployment: a colored dot plus a text label. */
export function StatusBadge({ tone, children, title, 'data-testid': testId }: StatusBadgeProps) {
  return (
    <span className={styles.badge} data-tone={tone} title={title} data-testid={testId}>
      <span className={styles.dot} aria-hidden="true" />
      {children}
    </span>
  )
}
