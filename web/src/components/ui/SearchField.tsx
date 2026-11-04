import { Search, X } from 'lucide-react'
import type { Ref } from 'react'
import { Button as AriaButton, SearchField as AriaSearchField, Input, Label } from 'react-aria-components'
import styles from './SearchField.module.css'

/** Props of {@link SearchField}. */
export interface SearchFieldProps {
  /** The visible label, which also names the field. */
  label: string
  /** Hides the label visually; screen readers still hear it. */
  hideLabel?: boolean
  value: string
  onChange: (value: string) => void
  placeholder?: string
  /** The text input, for moving focus to it. */
  inputRef?: Ref<HTMLInputElement>
}

/** A text field for filtering, with a clear button; Escape clears it too (React Aria). */
export function SearchField({ label, hideLabel = false, value, onChange, placeholder, inputRef }: SearchFieldProps) {
  return (
    <AriaSearchField className={styles.field} value={value} onChange={onChange} aria-label={hideLabel ? label : undefined}>
      {!hideLabel && <Label className={styles.label}>{label}</Label>}
      <div className={styles.control}>
        <Search aria-hidden="true" className={styles.icon} />
        <Input ref={inputRef} className={styles.input} placeholder={placeholder} />
        <AriaButton className={styles.clear} aria-label="Clear">
          <X aria-hidden="true" />
        </AriaButton>
      </div>
    </AriaSearchField>
  )
}
