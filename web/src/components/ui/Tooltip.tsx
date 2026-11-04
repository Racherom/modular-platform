import type { ReactElement, ReactNode } from 'react'
import { Tooltip as AriaTooltip, TooltipTrigger } from 'react-aria-components'
import styles from './Tooltip.module.css'

/** Props of {@link Tooltip}. */
export interface TooltipProps {
  /** The text shown on hover and on keyboard focus; it adds to the control's name, never replaces it. */
  content: ReactNode
  /** One React Aria focusable control, such as a Button. */
  children: ReactElement
}

/** A short hint over a control, on hover after a moment and at once on keyboard focus. */
export function Tooltip({ content, children }: TooltipProps) {
  return (
    <TooltipTrigger delay={600} closeDelay={0}>
      {children}
      <AriaTooltip className={styles.tooltip} offset={6}>
        {content}
      </AriaTooltip>
    </TooltipTrigger>
  )
}
