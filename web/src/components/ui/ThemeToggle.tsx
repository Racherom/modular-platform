import { Monitor, Moon, Sun } from 'lucide-react'
import { ToggleButton, ToggleButtonGroup } from 'react-aria-components'
import { parsePreference, setThemePreference, type ThemePreference, useThemePreference } from '../../lib/theme'
import styles from './ThemeToggle.module.css'

const choices: { id: ThemePreference; label: string; Icon: typeof Sun }[] = [
  { id: 'system', label: 'System theme', Icon: Monitor },
  { id: 'light', label: 'Light theme', Icon: Sun },
  { id: 'dark', label: 'Dark theme', Icon: Moon },
]

/** System, Light or Dark (AC-2); the choice is stored and applied at once. */
export function ThemeToggle() {
  const preference = useThemePreference()
  return (
    <ToggleButtonGroup
      aria-label="Theme"
      className={styles.group}
      selectionMode="single"
      disallowEmptySelection
      selectedKeys={[preference]}
      onSelectionChange={(keys) => {
        const [key] = [...keys]
        if (key !== undefined) setThemePreference(parsePreference(key))
      }}
    >
      {choices.map(({ id, label, Icon }) => (
        <ToggleButton key={id} id={id} aria-label={label} className={styles.button}>
          <Icon aria-hidden="true" />
        </ToggleButton>
      ))}
    </ToggleButtonGroup>
  )
}
