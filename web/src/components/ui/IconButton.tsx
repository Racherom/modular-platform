import type { ReactNode } from 'react'
import { Button as AriaButton, type ButtonProps as AriaButtonProps } from 'react-aria-components'
import type { ButtonVariant } from './Button'
import styles from './IconButton.module.css'
import { Tooltip } from './Tooltip'

/** Props of {@link IconButton}. */
export interface IconButtonProps extends Omit<AriaButtonProps, 'className' | 'children' | 'aria-label'> {
  /** The accessible name, required since the button shows only an icon; also the tooltip text. */
  label: string
  /** Extra tooltip text after the label, such as a keyboard shortcut. */
  hint?: string
  variant?: Exclude<ButtonVariant, 'primary'>
  size?: 'md' | 'sm'
  /** The icon, hidden from screen readers (the label names the button). */
  children: ReactNode
}

/** A square button with only an icon; its label is its accessible name and shows as a Tooltip. */
export function IconButton({ label, hint, variant = 'ghost', size = 'md', children, ...props }: IconButtonProps) {
  return (
    <Tooltip content={hint ? `${label} (${hint})` : label}>
      <AriaButton {...props} aria-label={label} className={styles.button} data-variant={variant} data-size={size}>
        {children}
      </AriaButton>
    </Tooltip>
  )
}
