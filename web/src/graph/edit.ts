// Graph edits (spec 0010): pure functions, no React and no I/O. Each returns a new Graph and
// leaves the one it was given untouched. Only the exact capability name match lives here; the
// planner judges everything else (required needs, cycles, automatic binding).
import type { CatalogEntry, Graph } from '../api/client'
import { parseHandle } from './flow'

/** A drag from one handle to another, as React Flow reports it. */
export interface Drop {
  source: string | null
  sourceHandle?: string | null
  target: string | null
  targetHandle?: string | null
}

/** Sets the provider of one need, for connect and rebind. */
export function bind(graph: Graph, consumer: string, capability: string, provider: string): Graph {
  const m = Object.hasOwn(graph.modules, consumer) ? graph.modules[consumer] : undefined
  if (!m) return graph
  return {
    ...graph,
    modules: { ...graph.modules, [consumer]: { ...m, uses: { ...m.uses, [capability]: provider } } },
  }
}

/**
 * True when a drop binds a need: the source handle is uses:<C> on a module whose catalog entry
 * uses C, and the target handle is provides:<C> on another instance whose catalog entry
 * provides C.
 */
export function isValidDrop(graph: Graph, catalog: CatalogEntry[], drop: Drop): boolean {
  const from = parseHandle(drop.sourceHandle)
  const to = parseHandle(drop.targetHandle)
  if (from?.side !== 'uses' || to?.side !== 'provides' || from.capability !== to.capability) return false
  if (!drop.source || !drop.target || drop.source === drop.target) return false
  const consumer = Object.hasOwn(graph.modules, drop.source) ? graph.modules[drop.source] : undefined
  const provider = Object.hasOwn(graph.modules, drop.target) ? graph.modules[drop.target] : undefined
  if (!consumer || !provider) return false
  const entries = new Map(catalog.map((e) => [e.name, e]))
  const consumerEntry = entries.get(consumer.module)
  const providerEntry = entries.get(provider.module)
  return (
    consumerEntry?.uses !== undefined &&
    Object.hasOwn(consumerEntry.uses, from.capability) &&
    providerEntry?.provides !== undefined &&
    Object.hasOwn(providerEntry.provides, to.capability)
  )
}
