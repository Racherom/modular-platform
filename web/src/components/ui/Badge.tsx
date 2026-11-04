import type { ReactNode } from 'react'
import styles from './Badge.module.css'

/** The meaning a badge carries; tone is never the only signal, the text always says it too. */
export type BadgeTone = 'neutral' | 'info' | 'success' | 'warning' | 'danger'

/** A short label beside a name, such as "not in catalog" or "optional". */
export function Badge({ tone = 'neutral', children }: { tone?: BadgeTone; children: ReactNode }) {
  return (
    <span className={styles.badge} data-tone={tone}>
      {children}
    </span>
  )
}
