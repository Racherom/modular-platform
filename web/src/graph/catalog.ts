// The catalog panel's filter (spec 0010, AC-19): pure, no React.
import type { CatalogEntry } from '../api/client'

/**
 * The entries whose name, description, or any provides or uses capability name contains the
 * text, ignoring case. Empty or blank text keeps every entry. Order is kept.
 */
export function filterCatalog(entries: CatalogEntry[], text: string): CatalogEntry[] {
  const needle = text.trim().toLowerCase()
  if (needle === '') return entries
  const has = (s: string) => s.toLowerCase().includes(needle)
  return entries.filter(
    (e) => has(e.name) || has(e.description) || Object.keys(e.provides ?? {}).some(has) || Object.keys(e.uses ?? {}).some(has),
  )
}
