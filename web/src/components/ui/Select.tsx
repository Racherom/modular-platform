import { ChevronDown } from 'lucide-react'
import type { ReactNode } from 'react'
import {
  Button as AriaButton,
  Select as AriaSelect,
  type Key,
  Label,
  ListBox,
  ListBoxItem,
  Popover,
  SelectValue,
  Text,
} from 'react-aria-components'
import styles from './Select.module.css'

/** One choice in a {@link Select}. */
export interface SelectOption {
  /** Unique within the select; the value passed to `onChange`. */
  id: string
  /** The visible text, also the option's accessible name. */
  label: string
  /** Shown but cannot be chosen. */
  isDisabled?: boolean
}

/** Props of {@link Select}. */
export interface SelectProps {
  /** The visible label, which also names the control. */
  label: ReactNode
  /** Hides the label visually; screen readers still hear it. */
  hideLabel?: boolean
  options: SelectOption[]
  /** The chosen option's id. */
  value: string | null
  onChange: (id: string) => void
  /** A short hint under the control. */
  description?: string
  isDisabled?: boolean
  size?: 'md' | 'sm'
}

/** A single choice from a list, on React Aria Components; keyboard and screen reader ready. */
export function Select({ label, hideLabel = false, options, value, onChange, description, isDisabled, size = 'md' }: SelectProps) {
  return (
    <AriaSelect
      className={styles.select}
      value={value}
      onChange={(key: Key | null) => {
        if (key !== null) onChange(String(key))
      }}
      isDisabled={isDisabled}
      data-size={size}
    >
      <Label className={hideLabel ? 'visually-hidden' : styles.label}>{label}</Label>
      <AriaButton className={styles.trigger}>
        <SelectValue className={styles.value} />
        <ChevronDown aria-hidden="true" className={styles.chevron} />
      </AriaButton>
      {description && (
        <Text slot="description" className={styles.description}>
          {description}
        </Text>
      )}
      <Popover className={styles.popover} offset={4}>
        <ListBox className={styles.list} items={options}>
          {(o) => (
            <ListBoxItem id={o.id} textValue={o.label} isDisabled={o.isDisabled} className={styles.option}>
              {o.label}
            </ListBoxItem>
          )}
        </ListBox>
      </Popover>
    </AriaSelect>
  )
}
