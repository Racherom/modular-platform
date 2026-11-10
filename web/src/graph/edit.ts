// Graph edits (spec 0010, on the v1 graph of spec 0012): pure functions, no React and no I/O.
// Each returns a new Graph and leaves the one it was given untouched. Only the capability match
// (or a provisioner that yields it) lives here; the checks judge everything else (versions,
// required slots, cycles, recommendations, named values).
import type { Catalog, Graph, GraphBinding, Problem } from '../api/client'
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
 * string form and the renderer fills new names (editing names is #6).
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

/** The problem code of a required use the graph leaves out that exactly one provide serves (spec 0028). */
export const RECOMMENDED = 'binding_recommended'

/**
 * Accepts the provider the checks recommend for a slot the graph leaves out (spec 0028): the same
 * edit as picking that provider from the list.
 */
export function acceptRecommendation(graph: Graph, consumer: string, slot: string, candidate: string): Graph {
  return bind(graph, consumer, slot, candidate)
}

/** What {@link acceptAllRecommendations} did: the new graph and how many it had to skip. */
export interface AcceptedAll {
  graph: Graph
  skipped: number
}

/**
 * Accepts every `binding_recommended` problem in one edit (spec 0028). Each is bound only while
 * its consumer exists, the use is still left out and its candidate's instance is still in the
 * graph; any other is skipped and counted. Optional uses are never accepted in bulk.
 */
export function acceptAllRecommendations(graph: Graph, problems: readonly Problem[]): AcceptedAll {
  let next = graph
  let skipped = 0
  for (const p of problems) {
    if (p.code !== RECOMMENDED) continue
    const [, consumer, , slot] = p.path.split('.')
    const candidate = p.candidates?.length === 1 ? p.candidates[0] : undefined
    const m = consumer === undefined ? undefined : instanceOf(next, consumer)
    const left = m !== undefined && slot !== undefined && !(m.uses && Object.hasOwn(m.uses, slot))
    const provider = candidate?.split('.')[0]
    if (!left || candidate === undefined || provider === undefined || !instanceOf(next, provider)) {
      skipped++
      continue
    }
    next = bind(next, consumer as string, slot as string, candidate)
  }
  return { graph: next, skipped }
}

/**
 * The provider text a slot can bind to: per other instance, every provide whose capability is
 * the slot's or yields it, written as the instance alone when it has one such provide, sorted.
 * Versions are the renderer's to judge.
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

/**
 * Sets config value `value` of an instance to `text` (spec 0025, AC-29). The text is the renderer's
 * to check; setting the same text again changes nothing. An empty text is kept: on an optional
 * value it means empty on purpose.
 */
export function setConfig(graph: Graph, instance: string, value: string, text: string): Graph {
  const m = instanceOf(graph, instance)
  if (!m) return graph
  if (m.config && Object.hasOwn(m.config, value) && m.config[value] === text) return graph
  return { ...graph, modules: { ...graph.modules, [instance]: { ...m, config: { ...m.config, [value]: text } } } }
}

/** Removes config value `value` of an instance, and the config map once it is empty (AC-29). */
export function clearConfig(graph: Graph, instance: string, value: string): Graph {
  const m = instanceOf(graph, instance)
  if (!m?.config || !Object.hasOwn(m.config, value)) return graph
  const { [value]: _, ...config } = m.config
  const { config: __, ...rest } = m
  const next = Object.keys(config).length > 0 ? { ...rest, config } : rest
  return { ...graph, modules: { ...graph.modules, [instance]: next } }
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
