import { parse, stringify } from 'yaml'
import type { Graph } from '../api/client'

export const API_VERSION = 'modular-platform/v1'
/** The provisional format of spec 0002; the planner converts such a graph to v1. */
export const API_VERSION_V0ALPHA1 = 'modular-platform/v0alpha1'
export const MAX_FILE_BYTES = 1 << 20

/**
 * Reads a graph file. Only apiVersion and kind are checked here; the planner is the one
 * validator, so the graph goes to POST /v1/plan right after (and a v0alpha1 graph comes back
 * converted to v1).
 */
export function parseGraph(text: string): Graph {
  if (new Blob([text]).size > MAX_FILE_BYTES) throw new Error('The file is over 1 MB.')
  const value: unknown = parse(text, { uniqueKeys: true })
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('The file is not a graph.')
  }
  const g = value as Partial<Graph>
  if (g.apiVersion !== API_VERSION && g.apiVersion !== API_VERSION_V0ALPHA1) {
    throw new Error(`The apiVersion must be ${API_VERSION}.`)
  }
  if (g.kind !== 'Graph') throw new Error('The kind must be Graph.')
  return value as Graph
}

/**
 * Writes the canonical text, byte identical to the Go writer (AC-19): keys sorted (so an object
 * binding writes database, provider, user), a binding as a string unless it has named values,
 * formatVersion left out at 1.0. Version 1.1 makes the writer quote YAML 1.1 words like yes and
 * on the way the Go side does.
 */
export function stringifyGraph(graph: Graph): string {
  const { formatVersion, ...rest } = graph
  const out = formatVersion === undefined || formatVersion === '1.0' ? rest : graph
  return stringify(out, { sortMapEntries: true, indent: 2, lineWidth: 0, version: '1.1' })
}
