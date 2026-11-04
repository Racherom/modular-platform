// Applies a stored Light or Dark theme before the first paint (spec 0010, AC-2). A classic
// script, loaded in <head> before the app bundle. Anything but exactly light or dark sets
// nothing, and tokens.css follows the OS; src/lib/theme.ts owns the rest of the logic.
try {
  const stored = window.localStorage.getItem('mp-theme')
  if (stored === 'light' || stored === 'dark') document.documentElement.setAttribute('data-theme', stored)
} catch {
  // storage is blocked: follow the OS
}
