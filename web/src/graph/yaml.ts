import { parse, stringify } from 'yaml'
import type { Graph } from '../api/client'

export const API_VERSION = 'modular-platform/v0alpha1'
export const MAX_FILE_BYTES = 1 << 20

/**
 * Reads a graph file. Only apiVersion and kind are checked here; the planner is the one
 * validator, so the graph goes to POST /v1/plan right after.
 */
export function parseGraph(text: string): Graph {
  if (new Blob([text]).size > MAX_FILE_BYTES) throw new Error('The file is over 1 MB.')
  const value: unknown = parse(text, { uniqueKeys: true })
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('The file is not a graph.')
  }
  const g = value as Partial<Graph>
  if (g.apiVersion !== API_VERSION) throw new Error(`The apiVersion must be ${API_VERSION}.`)
  if (g.kind !== 'Graph') throw new Error('The kind must be Graph.')
  return value as Graph
}

/**
 * Writes the canonical text, byte identical to the Go writer (AC-9). Version 1.1 makes the
 * writer quote YAML 1.1 words like yes and on the way the Go side does.
 */
export function stringifyGraph(graph: Graph): string {
  return stringify(graph, { sortMapEntries: true, indent: 2, lineWidth: 0, version: '1.1' })
}
