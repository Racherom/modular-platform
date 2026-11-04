// Graph edits (spec 0010, on the v1 graph of spec 0012): pure functions, no React and no I/O.
// Each returns a new Graph and leaves the one it was given untouched. Only the capability match
// (or a provisioner that yields it) lives here; the planner judges everything else (versions,
// required slots, cycles, automatic binding, named values).
import type { Catalog, Graph, GraphBinding } from '../api/client'
import { parseHandle } from './flow'
import { bindingText, moduleOf, offersOf, serves, slotsOf, textFor } from './model'

/** A drag from one handle to another, as React Flow reports it. */
export interface Drop {
  source: string | null
  sourceHandle?: string | null
  target: string | null
  targetHandle?: string | null
}

/**
 * Binds a slot to provider text (`instance` or `instance.provide`), for connect and rebind.
 * Binding the same text again changes nothing, so named values stay; another provider writes the
 * string form and the planner fills new names (editing names is #6).
 */
export function bind(graph: Graph, consumer: string, slot: string, provider: string): Graph {
  const m = instanceOf(graph, consumer)
  if (!m) return graph
  if (m.uses && Object.hasOwn(m.uses, slot) && bindingText(m.uses[slot]) === provider) return graph
  return setSlot(graph, consumer, slot, provider)
}

/** Marks a slot as deliberately not bound (`null`). */
export function unbind(graph: Graph, consumer: string, slot: string): Graph {
  const m = instanceOf(graph, consumer)
  if (!m) return graph
  if (m.uses && Object.hasOwn(m.uses, slot) && m.uses[slot] === null) return graph
  return setSlot(graph, consumer, slot, null)
}

/** Removes a slot's key, so the planner binds it automatically when it finds one candidate. */
export function letPlannerChoose(graph: Graph, consumer: string, slot: string): Graph {
  const m = instanceOf(graph, consumer)
  if (!m?.uses || !Object.hasOwn(m.uses, slot)) return graph
  const uses = { ...m.uses }
  delete uses[slot]
  return { ...graph, modules: { ...graph.modules, [consumer]: { ...m, uses } } }
}

/**
 * The provider text a slot can bind to: per other instance, every provide whose capability is
 * the slot's or yields it, written as the instance alone when it has one such provide, sorted.
 * Versions are the planner's to judge.
 */
export function candidates(graph: Graph, catalog: Catalog, consumer: string, slot: string): string[] {
  const capability = slotsOf(moduleOf(graph, catalog, consumer))[slot]?.capability
  if (capability === undefined) return []
  const out: string[] = []
  for (const name of Object.keys(graph.modules).sort()) {
    if (name === consumer) continue
    for (const o of offersOf(graph, catalog, name)) {
      if (serves(catalog, o.capability, capability)) out.push(textFor(graph, catalog, name, o.provide, capability))
    }
  }
  return out
}

function setSlot(graph: Graph, consumer: string, slot: string, value: GraphBinding): Graph {
  const m = instanceOf(graph, consumer)!
  return { ...graph, modules: { ...graph.modules, [consumer]: { ...m, uses: { ...m.uses, [slot]: value } } } }
}

function instanceOf(graph: Graph, instance: string) {
  return Object.hasOwn(graph.modules, instance) ? graph.modules[instance] : undefined
}

/**
 * The provider text a drop binds, or undefined when it binds nothing: the source handle is
 * uses:<slot> on a module that has the slot, and the target handle is provides:<provide> on
 * another instance whose provide serves the slot's capability.
 */
export function dropText(graph: Graph, catalog: Catalog, drop: Drop): { slot: string; provider: string } | undefined {
  const from = parseHandle(drop.sourceHandle)
  const to = parseHandle(drop.targetHandle)
  if (from?.side !== 'uses' || to?.side !== 'provides') return undefined
  if (!drop.source || !drop.target || drop.source === drop.target) return undefined
  if (!instanceOf(graph, drop.source) || !instanceOf(graph, drop.target)) return undefined
  const capability = slotsOf(moduleOf(graph, catalog, drop.source))[from.name]?.capability
  const offer = offersOf(graph, catalog, drop.target).find((o) => o.provide === to.name)
  if (capability === undefined || !offer || !serves(catalog, offer.capability, capability)) return undefined
  return { slot: from.name, provider: textFor(graph, catalog, drop.target, offer.provide, capability) }
}

/** True when a drop binds a slot (see dropText). */
export function isValidDrop(graph: Graph, catalog: Catalog, drop: Drop): boolean {
  return dropText(graph, catalog, drop) !== undefined
}
