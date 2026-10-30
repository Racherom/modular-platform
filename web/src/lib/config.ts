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
