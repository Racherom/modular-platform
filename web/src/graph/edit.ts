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

/** Sets the provider of one need, for connect and rebind. Binding the current provider again changes nothing. */
export function bind(graph: Graph, consumer: string, capability: string, provider: string): Graph {
  return setNeed(graph, consumer, capability, provider)
}

/** Marks a need as deliberately not bound (`null`). */
export function unbind(graph: Graph, consumer: string, capability: string): Graph {
  return setNeed(graph, consumer, capability, null)
}

/** Removes a need's key, so the planner binds it automatically when it finds one candidate (spec 0002). */
export function letPlannerChoose(graph: Graph, consumer: string, capability: string): Graph {
  const m = moduleOf(graph, consumer)
  if (!m?.uses || !Object.hasOwn(m.uses, capability)) return graph
  const uses = { ...m.uses }
  delete uses[capability]
  return { ...graph, modules: { ...graph.modules, [consumer]: { ...m, uses } } }
}

/**
 * The instances that can provide a need: every other instance whose module is in the catalog
 * and provides the capability, sorted by name (the candidate rule of spec 0002).
 */
export function candidates(graph: Graph, catalog: CatalogEntry[], consumer: string, capability: string): string[] {
  const entries = entryMap(catalog)
  return Object.keys(graph.modules)
    .filter((name) => {
      if (name === consumer) return false
      const provides = entries.get(graph.modules[name]!.module)?.provides
      return provides !== undefined && Object.hasOwn(provides, capability)
    })
    .sort()
}

function setNeed(graph: Graph, consumer: string, capability: string, value: string | null): Graph {
  const m = moduleOf(graph, consumer)
  if (!m) return graph
  if (m.uses && Object.hasOwn(m.uses, capability) && m.uses[capability] === value) return graph
  return {
    ...graph,
    modules: { ...graph.modules, [consumer]: { ...m, uses: { ...m.uses, [capability]: value } } },
  }
}

function moduleOf(graph: Graph, instance: string) {
  return Object.hasOwn(graph.modules, instance) ? graph.modules[instance] : undefined
}

function entryMap(catalog: CatalogEntry[]) {
  return new Map(catalog.map((e) => [e.name, e]))
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
  const consumer = moduleOf(graph, drop.source)
  const provider = moduleOf(graph, drop.target)
  if (!consumer || !provider) return false
  const entries = entryMap(catalog)
  const consumerEntry = entries.get(consumer.module)
  const providerEntry = entries.get(provider.module)
  return (
    consumerEntry?.uses !== undefined &&
    Object.hasOwn(consumerEntry.uses, from.capability) &&
    providerEntry?.provides !== undefined &&
    Object.hasOwn(providerEntry.provides, to.capability)
  )
}
