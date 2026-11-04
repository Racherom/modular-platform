import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import styles from './EmptyState.module.css'

/** Props of {@link EmptyState}. */
export interface EmptyStateProps {
  /** A decorative icon; hidden from screen readers. */
  icon?: LucideIcon
  title: string
  /** Makes the title a heading of this level, for an empty state that fills a whole screen. */
  level?: 1 | 2 | 3
  /** One or two sentences on why it is empty or what to do. */
  children?: ReactNode
  /** One or two buttons that get you out of the empty state. */
  actions?: ReactNode
}

/** What a list or area shows when it has nothing to show, and how to change that. */
export function EmptyState({ icon: Icon, title, level, children, actions }: EmptyStateProps) {
  const Title = level ? (`h${level}` as const) : 'p'
  return (
    <div className={styles.empty} data-page={level === 1 || undefined}>
      {Icon && <Icon aria-hidden="true" className={styles.icon} />}
      <Title className={styles.title}>{title}</Title>
      {children && <div className={styles.body}>{children}</div>}
      {actions && <div className={styles.actions}>{actions}</div>}
    </div>
  )
}
