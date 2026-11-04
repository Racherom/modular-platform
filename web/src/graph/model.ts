// The v1 graph as the editor reads it (spec 0012): slots, provides and binding text. Pure, no
// React. The planner is the one judge of a graph; these helpers only find what to draw.
import type { Catalog, Graph, GraphBinding, Module, Slot } from '../api/client'

/** One provide an instance offers: a module's provide or an external's. */
export interface Offer {
  provide: string
  capability: string
  version: string
}

/** The slot every runtime: sdk module has without declaring it (spec 0013). */
export const ACME_SLOT = 'acme'

/** A module's slots, with the implicit acme slot of a runtime: sdk module. */
export function slotsOf(entry: Module | undefined): Record<string, Slot> {
  const slots: Record<string, Slot> = { ...(entry?.uses ?? {}) }
  if (entry?.runtime === 'sdk' && !Object.hasOwn(slots, ACME_SLOT)) {
    slots[ACME_SLOT] = { capability: 'acme', optional: true, per_caller: false }
  }
  return slots
}

/** The catalog module an instance runs, when it is a module in the catalog. */
export function moduleOf(graph: Graph, catalog: Catalog, instance: string): Module | undefined {
  const name = Object.hasOwn(graph.modules, instance) ? graph.modules[instance]?.module : undefined
  return name === undefined ? undefined : catalog.modules.find((m) => m.name === name)
}

/** Whether an instance is a service outside the stack. */
export function isExternal(graph: Graph, instance: string): boolean {
  return Object.hasOwn(graph.modules, instance) && graph.modules[instance]?.external !== undefined
}

/** The provides of an instance, sorted by name. */
export function offersOf(graph: Graph, catalog: Catalog, instance: string): Offer[] {
  const m = Object.hasOwn(graph.modules, instance) ? graph.modules[instance] : undefined
  const provides = m?.external ? m.external.provides : (moduleOf(graph, catalog, instance)?.provides ?? {})
  return Object.entries(provides)
    .map(([provide, p]) => ({ provide, capability: p.capability, version: p.version }))
    .sort((a, b) => (a.provide < b.provide ? -1 : a.provide > b.provide ? 1 : 0))
}

/** Whether a provide of capability `offered` serves a slot of `wanted`: the same, or a provisioner yielding it. */
export function serves(catalog: Catalog, offered: string, wanted: string): boolean {
  if (offered === wanted) return true
  return catalog.capabilities.find((c) => c.name === offered)?.yields?.capability === wanted
}

/** The provider text of a binding (`instance` or `instance.provide`), or undefined for null. */
export function bindingText(b: GraphBinding | null | undefined): string | undefined {
  if (b === null || b === undefined) return undefined
  return typeof b === 'string' ? b : b.provider
}

/** Splits provider text into its instance and its provide ('' when it names only the instance). */
export function splitText(text: string): { instance: string; provide: string } {
  const at = text.indexOf('.')
  return at < 0 ? { instance: text, provide: '' } : { instance: text.slice(0, at), provide: text.slice(at + 1) }
}

/**
 * The provide a slot's binding reaches: the named one, or the one provide of the instance that
 * serves the slot's capability. Undefined when the binding is null, broken or ambiguous; the
 * planner lists the problem then.
 */
export function boundProvide(
  graph: Graph,
  catalog: Catalog,
  consumer: string,
  slot: string,
): { provider: string; provide: string } | undefined {
  const text = bindingText(graph.modules[consumer]?.uses?.[slot])
  if (text === undefined) return undefined
  const { instance, provide } = splitText(text)
  const capability = slotsOf(moduleOf(graph, catalog, consumer))[slot]?.capability
  const offers = offersOf(graph, catalog, instance)
  if (provide !== '') return offers.some((o) => o.provide === provide) ? { provider: instance, provide } : undefined
  const serving = offers.filter((o) => capability !== undefined && serves(catalog, o.capability, capability))
  return serving.length === 1 ? { provider: instance, provide: serving[0]!.provide } : undefined
}

/** The provider text for a binding to a provide: the instance alone when it has one provide serving the slot. */
export function textFor(graph: Graph, catalog: Catalog, provider: string, provide: string, capability: string): string {
  const serving = offersOf(graph, catalog, provider).filter((o) => serves(catalog, o.capability, capability))
  return serving.length === 1 ? provider : `${provider}.${provide}`
}
