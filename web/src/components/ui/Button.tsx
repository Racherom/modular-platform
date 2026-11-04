import type { ReactNode } from 'react'
import { Button as AriaButton, type ButtonProps as AriaButtonProps } from 'react-aria-components'
import styles from './Button.module.css'

/** How much a button stands out: primary for the one main action, danger for destructive ones. */
export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'

/** Props of {@link Button}. */
export interface ButtonProps extends Omit<AriaButtonProps, 'className' | 'children'> {
  variant?: ButtonVariant
  size?: 'md' | 'sm'
  children: ReactNode
}

/** A button on React Aria Components; `onPress` fires for mouse, touch and keyboard alike. */
export function Button({ variant = 'secondary', size = 'md', children, ...props }: ButtonProps) {
  return (
    <AriaButton {...props} className={styles.button} data-variant={variant} data-size={size}>
      {children}
    </AriaButton>
  )
}
