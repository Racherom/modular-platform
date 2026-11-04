import { type ReactNode, useId } from 'react'
import styles from './Panel.module.css'

/** Props of {@link Panel}. */
export interface PanelProps {
  /** The heading, which also names the region. */
  title: string
  /** Controls beside the heading. */
  actions?: ReactNode
  /** Hides the body; the heading and actions stay. */
  collapsed?: boolean
  /** Extra class for placing the panel in a layout. */
  className?: string
  /** The id of the body, for a control that collapses it. */
  bodyId?: string
  children?: ReactNode
}

/** A named region of the screen with a heading bar and a scrolling body. */
export function Panel({ title, actions, collapsed = false, className, bodyId, children }: PanelProps) {
  const headingId = useId()
  return (
    <section className={className ? `${styles.panel} ${className}` : styles.panel} aria-labelledby={headingId}>
      <div className={styles.head}>
        <h2 id={headingId} className={styles.title}>
          {title}
        </h2>
        {actions && <div className={styles.actions}>{actions}</div>}
      </div>
      <div id={bodyId} className={styles.body} hidden={collapsed}>
        {children}
      </div>
    </section>
  )
}
