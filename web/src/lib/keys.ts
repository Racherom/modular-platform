// Keyboard rules shared by the canvas and the editor screen (spec 0010, AC-10, AC-15).

/**
 * True for a text field, select or other form control, where keys like Delete, Escape and
 * Ctrl+Z belong to the control. A React Aria select trigger counts as a select.
 */
export function isFormControl(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  return (
    target.isContentEditable ||
    target.closest('input, textarea, select, [role="combobox"], [role="listbox"], [role="textbox"], [aria-haspopup="listbox"]') !== null
  )
}

/** True on macOS and iOS, where Cmd takes the place of Ctrl in shortcuts. */
export function isApple(): boolean {
  return typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
}

/** The keys of a keydown event that {@link historyKey} reads. */
export interface ShortcutEvent {
  key: string
  ctrlKey: boolean
  metaKey: boolean
  shiftKey: boolean
  altKey: boolean
}

/**
 * The history step a key press asks for (AC-15): Ctrl+Z undoes, Ctrl+Shift+Z or Ctrl+Y redoes;
 * on Apple systems Cmd+Z and Cmd+Shift+Z. Anything else gives null.
 */
export function historyKey(e: ShortcutEvent, apple: boolean): 'undo' | 'redo' | null {
  const mod = apple ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey
  if (!mod || e.altKey) return null
  const key = e.key.toLowerCase()
  if (key === 'z') return e.shiftKey ? 'redo' : 'undo'
  if (key === 'y' && !apple && !e.shiftKey) return 'redo'
  return null
}

/** The shortcut text for a history step, for `aria-keyshortcuts` and tooltips. */
export function historyShortcut(step: 'undo' | 'redo', apple: boolean): { aria: string; label: string } {
  if (apple) return step === 'undo' ? { aria: 'Meta+Z', label: '⌘Z' } : { aria: 'Meta+Shift+Z', label: '⇧⌘Z' }
  return step === 'undo' ? { aria: 'Control+Z', label: 'Ctrl+Z' } : { aria: 'Control+Shift+Z Control+Y', label: 'Ctrl+Shift+Z' }
}
