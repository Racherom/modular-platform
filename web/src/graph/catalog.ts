// The catalog panel's filter (spec 0010, AC-19): pure, no React.
import type { Module } from '../api/client'

/**
 * The modules whose name, description, or any provide or slot name or capability contains the
 * text, ignoring case. Empty or blank text keeps every module. Order is kept.
 */
export function filterCatalog(modules: Module[], text: string): Module[] {
  const needle = text.trim().toLowerCase()
  if (needle === '') return modules
  const has = (s: string) => s.toLowerCase().includes(needle)
  const named = (m: Record<string, { capability: string }> | undefined) =>
    Object.entries(m ?? {}).some(([name, v]) => has(name) || has(v.capability))
  return modules.filter((e) => has(e.name) || has(e.description) || named(e.provides) || named(e.uses))
}
