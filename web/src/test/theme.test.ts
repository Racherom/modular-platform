import { afterEach, describe, expect, it, vi } from 'vitest'
import { parsePreference, resolveTheme, setThemePreference, THEME_KEY } from '../lib/theme'

describe('theme preference', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    setThemePreference('system')
  })

  it.each([
    ['light', 'light'],
    ['dark', 'dark'],
    ['system', 'system'],
    [null, 'system'],
    ['Dark', 'system'],
    ['<script>', 'system'],
  ])('reads %s as %s', (stored, want) => {
    expect(parsePreference(stored)).toBe(want)
  })

  it('resolves System from the OS setting', () => {
    expect(resolveTheme('system', true)).toBe('dark')
    expect(resolveTheme('system', false)).toBe('light')
    expect(resolveTheme('light', true)).toBe('light')
  })

  it('stores Light and Dark and sets data-theme; System removes both', () => {
    setThemePreference('dark')
    expect(localStorage.getItem(THEME_KEY)).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    setThemePreference('system')
    expect(localStorage.getItem(THEME_KEY)).toBeNull()
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
  })

  it('still applies the choice when storage throws', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    setThemePreference('light')
    expect(document.documentElement.dataset.theme).toBe('light')
  })
})
