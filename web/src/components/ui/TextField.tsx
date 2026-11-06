import type { ReactNode } from 'react'
import { TextField as AriaTextField, Input, Label, Text } from 'react-aria-components'
import styles from './TextField.module.css'

/** Props of {@link TextField}. */
export interface TextFieldProps {
  /** The visible label, which also names the field. */
  label: ReactNode
  /** Hides the label visually; screen readers still hear it. */
  hideLabel?: boolean
  value: string
  onChange: (value: string) => void
  /** Called with the text on Enter and when focus leaves the field, for a value that is saved once. */
  onCommit?: (value: string) => void
  /** A short hint under the control. */
  description?: ReactNode
  placeholder?: string
  isDisabled?: boolean
  /** Shows the field as invalid; the reason is told beside it. */
  isInvalid?: boolean
  size?: 'md' | 'sm'
}

/** One line of text, on React Aria Components; keyboard and screen reader ready. */
export function TextField({
  label,
  hideLabel = false,
  value,
  onChange,
  onCommit,
  description,
  placeholder,
  isDisabled,
  isInvalid,
  size = 'md',
}: TextFieldProps) {
  return (
    <AriaTextField
      className={styles.field}
      value={value}
      onChange={onChange}
      onBlur={() => onCommit?.(value)}
      onKeyDown={(e) => {
        if (e.key === 'Enter') onCommit?.(value)
      }}
      isDisabled={isDisabled}
      isInvalid={isInvalid}
      data-size={size}
    >
      <Label className={hideLabel ? 'visually-hidden' : styles.label}>{label}</Label>
      <Input className={styles.input} placeholder={placeholder} />
      {description && (
        <Text slot="description" className={styles.description}>
          {description}
        </Text>
      )}
    </AriaTextField>
  )
}
