import type { Graph } from '../api/client'

export const COLUMN_GAP = 320
export const ROW_GAP = 160

/**
 * Lays out instances in columns: an instance's column is the length of the longest chain of
 * consumers leading to it (0 when nobody uses it), so every provider sits right of all its
 * consumers. Rows are sorted by instance name. A visited set keeps a cycle from looping.
 */
export function layout(graph: Graph): Record<string, { x: number; y: number }> {
  const names = Object.keys(graph.modules).sort()
  const consumers: Record<string, string[]> = Object.fromEntries(names.map((n) => [n, []]))
  for (const name of names) {
    for (const provider of Object.values(graph.modules[name]?.uses ?? {})) {
      if (provider !== null && provider !== name && provider in consumers) consumers[provider]?.push(name)
    }
  }

  const column: Record<string, number> = {}
  const depth = (name: string, visiting: Set<string>): number => {
    const known = column[name]
    if (known !== undefined) return known
    if (visiting.has(name)) return 0
    visiting.add(name)
    let deepest = 0
    for (const consumer of consumers[name] ?? []) deepest = Math.max(deepest, depth(consumer, visiting) + 1)
    visiting.delete(name)
    column[name] = deepest
    return deepest
  }

  const rows: Record<number, number> = {}
  const positions: Record<string, { x: number; y: number }> = {}
  for (const name of names) {
    const col = depth(name, new Set())
    const row = rows[col] ?? 0
    rows[col] = row + 1
    positions[name] = { x: col * COLUMN_GAP, y: row * ROW_GAP }
  }
  return positions
}
