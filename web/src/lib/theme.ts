// The theme preference (spec 0010, AC-2): System, Light or Dark, kept in localStorage under
// mp-theme. public/theme.js applies a stored Light or Dark before the first paint; this module
// keeps <html data-theme> in step afterwards and tells React which theme is showing.
import { useSyncExternalStore } from 'react'

/** What the person chose. */
export type ThemePreference = 'system' | 'light' | 'dark'
/** What is showing. */
export type Theme = 'light' | 'dark'

/** The localStorage key, shared with public/theme.js. */
export const THEME_KEY = 'mp-theme'

/** Reads a stored value through an allow list; anything else means System. */
export function parsePreference(value: unknown): ThemePreference {
  return value === 'light' || value === 'dark' ? value : 'system'
}

function readStored(): ThemePreference {
  try {
    return parsePreference(window.localStorage.getItem(THEME_KEY))
  } catch {
    return 'system'
  }
}

const darkQuery = (): MediaQueryList | undefined => window.matchMedia?.('(prefers-color-scheme: dark)')

let preference: ThemePreference = readStored()
const listeners = new Set<() => void>()

function emit() {
  for (const listener of listeners) listener()
}

/** Resolves System to the OS setting. */
export function resolveTheme(pref: ThemePreference, osDark: boolean): Theme {
  if (pref === 'system') return osDark ? 'dark' : 'light'
  return pref
}

function apply(pref: ThemePreference) {
  // System sets nothing, so tokens.css follows the OS on its own
  if (pref === 'system') document.documentElement.removeAttribute('data-theme')
  else document.documentElement.setAttribute('data-theme', pref)
}

/** Stores and applies a new preference. A storage that throws keeps it for this session only. */
export function setThemePreference(pref: ThemePreference): void {
  preference = pref
  try {
    if (pref === 'system') window.localStorage.removeItem(THEME_KEY)
    else window.localStorage.setItem(THEME_KEY, pref)
  } catch {
    // blocked storage: the choice still holds until the page closes
  }
  apply(pref)
  emit()
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  const query = darkQuery()
  query?.addEventListener('change', listener)
  return () => {
    listeners.delete(listener)
    query?.removeEventListener('change', listener)
  }
}

/** The current preference, for the toggle. */
export function useThemePreference(): ThemePreference {
  return useSyncExternalStore(subscribe, () => preference)
}

/** The theme that is showing, for React Flow's colorMode. */
export function useResolvedTheme(): Theme {
  return useSyncExternalStore(subscribe, () => resolveTheme(preference, darkQuery()?.matches ?? false))
}
