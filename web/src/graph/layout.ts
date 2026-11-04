import type { Graph } from '../api/client'
import { bindingText, splitText } from './model'

export const COLUMN_GAP = 320
export const ROW_GAP = 160

/**
 * Lays out instances in columns: an instance's column is the length of the longest chain of
 * consumers leading to it (0 when nobody uses it), so every provider sits right of all its
 * consumers. Rows are sorted by instance name. A visited set keeps a cycle from looping.
 */
export function layout(graph: Graph): Record<string, { x: number; y: number }> {
  const names = Object.keys(graph.modules).sort()
  // Maps, not plain objects: a broken file may name an instance or provider like constructor
  const consumers = new Map<string, string[]>(names.map((n) => [n, []]))
  for (const name of names) {
    for (const b of Object.values(graph.modules[name]?.uses ?? {})) {
      const text = bindingText(b)
      const provider = text === undefined ? undefined : splitText(text).instance
      if (provider !== undefined && provider !== name) consumers.get(provider)?.push(name)
    }
  }

  const column = new Map<string, number>()
  const depth = (name: string, visiting: Set<string>): number => {
    const known = column.get(name)
    if (known !== undefined) return known
    if (visiting.has(name)) return 0
    visiting.add(name)
    let deepest = 0
    for (const consumer of consumers.get(name) ?? []) deepest = Math.max(deepest, depth(consumer, visiting) + 1)
    visiting.delete(name)
    column.set(name, deepest)
    return deepest
  }

  const rows = new Map<number, number>()
  // fromEntries defines own keys, so even __proto__ lands as a plain entry
  return Object.fromEntries(
    names.map((name) => {
      const col = depth(name, new Set())
      const row = rows.get(col) ?? 0
      rows.set(col, row + 1)
      return [name, { x: col * COLUMN_GAP, y: row * ROW_GAP }]
    }),
  )
}
