import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import styles from './EmptyState.module.css'

/** Props of {@link EmptyState}. */
export interface EmptyStateProps {
  /** A decorative icon; hidden from screen readers. */
  icon?: LucideIcon
  title: string
  /** One or two sentences on why it is empty or what to do. */
  children?: ReactNode
  /** One or two buttons that get you out of the empty state. */
  actions?: ReactNode
}

/** What a list or area shows when it has nothing to show, and how to change that. */
export function EmptyState({ icon: Icon, title, children, actions }: EmptyStateProps) {
  return (
    <div className={styles.empty}>
      {Icon && <Icon aria-hidden="true" className={styles.icon} />}
      <p className={styles.title}>{title}</p>
      {children && <div className={styles.body}>{children}</div>}
      {actions && <div className={styles.actions}>{actions}</div>}
    </div>
  )
}
