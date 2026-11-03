// Placement settings, written into index.html by the web image's Caddy templates at serve time
// (or by the dev server), so one bundle serves the site root and any sub path without a rebuild.

function meta(name: string, fallback: string): string {
  const value = document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)?.content
  // an untemplated page (tests) still holds the template text
  if (value === undefined || value.includes('{{')) return fallback
  return value
}

/** Empty for the site root, else like `/platform`. */
export const basePath = meta('mp-base-path', '')

/** Where the planning API lives, like `/planner` or `/platform/planner`. */
export const apiRoot = meta('mp-api-root', '/planner')

/** `online` for the online tool, `managed` inside the managed platform, with sign in and deploy. */
export const mode: 'online' | 'managed' = meta('mp-mode', 'online') === 'managed' ? 'managed' : 'online'

/** Managed mode, signed out: `index` shows the index with a sign in button, `sign-in` goes straight to the provider. */
export const signedOutView: 'index' | 'sign-in' = meta('mp-signed-out', 'index') === 'sign-in' ? 'sign-in' : 'index'
