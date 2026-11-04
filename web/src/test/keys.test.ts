import { describe, expect, it } from 'vitest'
import { historyKey, isFormControl, type ShortcutEvent } from '../lib/keys'

const press = (key: string, mods: Partial<ShortcutEvent> = {}): ShortcutEvent => ({
  key,
  ctrlKey: false,
  metaKey: false,
  shiftKey: false,
  altKey: false,
  ...mods,
})

describe('historyKey (AC-15)', () => {
  it.each([
    ['Ctrl+Z undoes', press('z', { ctrlKey: true }), false, 'undo'],
    ['Ctrl+Shift+Z redoes', press('Z', { ctrlKey: true, shiftKey: true }), false, 'redo'],
    ['Ctrl+Y redoes', press('y', { ctrlKey: true }), false, 'redo'],
    ['Cmd+Z undoes on Apple', press('z', { metaKey: true }), true, 'undo'],
    ['Cmd+Shift+Z redoes on Apple', press('z', { metaKey: true, shiftKey: true }), true, 'redo'],
    ['Cmd+Y is nothing on Apple', press('y', { metaKey: true }), true, null],
    ['Ctrl+Z is nothing on Apple', press('z', { ctrlKey: true }), true, null],
    ['Cmd+Z is nothing elsewhere', press('z', { metaKey: true }), false, null],
    ['Z alone is nothing', press('z'), false, null],
    ['Ctrl+Alt+Z is nothing', press('z', { ctrlKey: true, altKey: true }), false, null],
    ['Ctrl+X is nothing', press('x', { ctrlKey: true }), false, null],
  ] as const)('%s', (_, event, apple, want) => {
    expect(historyKey(event, apple)).toBe(want)
  })
})

describe('isFormControl', () => {
  it('knows text fields, selects and the React Aria select trigger', () => {
    document.body.innerHTML = `
      <input id="text" />
      <textarea id="area"></textarea>
      <button id="trigger" aria-haspopup="listbox"><span id="inner">a</span></button>
      <button id="plain">b</button>
      <div id="div" tabindex="0"></div>`
    const el = (id: string) => document.getElementById(id)
    expect(isFormControl(el('text'))).toBe(true)
    expect(isFormControl(el('area'))).toBe(true)
    expect(isFormControl(el('inner'))).toBe(true)
    expect(isFormControl(el('plain'))).toBe(false)
    expect(isFormControl(el('div'))).toBe(false)
    expect(isFormControl(null)).toBe(false)
  })
})
