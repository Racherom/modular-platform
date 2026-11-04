import type { ReactNode } from 'react'
import { Toolbar as AriaToolbar } from 'react-aria-components'
import styles from './Toolbar.module.css'

/** Props of {@link Toolbar}. */
export interface ToolbarProps {
  /** Names the group for screen readers, such as "History" or "File". */
  label: string
  /** Draws a line after the group, to set it apart from the next one. */
  divider?: boolean
  children: ReactNode
}

/**
 * A row of related controls on React Aria Components. The toolbar is one Tab stop: Tab enters
 * it, arrow keys move focus between its controls, and the next Tab leaves it.
 */
export function Toolbar({ label, divider = false, children }: ToolbarProps) {
  return (
    <AriaToolbar aria-label={label} className={styles.toolbar} data-divider={divider || undefined}>
      {children}
    </AriaToolbar>
  )
}
