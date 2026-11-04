// The working graph at <base>/ is the last opened file, kept in IndexedDB with its last plan so
// a refresh loses nothing and needs no new plan call. It is saved only with a plan for exactly
// its graph (spec 0010, AC-12).
import type { Graph, PlanResult } from '../api/client'
import type { Positions } from '../graph/flow'
import { layout } from '../graph/layout'

/** The working copy: the graph, its plan, and what only this browser keeps about it. */
export interface WorkingGraph {
  fileName: string
  graph: Graph
  plan: PlanResult
  /** Where each instance sits on the canvas; never written into a graph file. */
  positions?: Positions
  /** Changed since it was opened or saved as a file; missing means false. */
  edited?: boolean
}

const DB = 'modular-platform'
const STORE = 'working'
const KEY = 'graph'

/**
 * Fills in positions: when any instance has none, the whole graph gets the auto layout (AC-16).
 * Keys for instances the graph no longer has are dropped.
 */
export function withPositions(working: WorkingGraph): WorkingGraph {
  const names = Object.keys(working.graph.modules)
  const stored = working.positions ?? {}
  if (names.every((n) => Object.hasOwn(stored, n))) {
    if (Object.keys(stored).length === names.length) return working
    return { ...working, positions: Object.fromEntries(names.map((n) => [n, stored[n]!])) }
  }
  return { ...working, positions: layout(working.graph) }
}

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB, 1)
    req.onupgradeneeded = () => req.result.createObjectStore(STORE)
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error ?? new Error('IndexedDB is not available.'))
  })
}

function run<T>(mode: IDBTransactionMode, op: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  return open().then((db) =>
    new Promise<T>((resolve, reject) => {
      const req = op(db.transaction(STORE, mode).objectStore(STORE))
      req.onsuccess = () => resolve(req.result)
      req.onerror = () => reject(req.error ?? new Error('IndexedDB failed.'))
    }).finally(() => db.close()),
  )
}

/** Reads the working copy, if this browser kept one. */
export function loadWorking(): Promise<WorkingGraph | undefined> {
  return run<WorkingGraph | undefined>('readonly', (s) => s.get(KEY) as IDBRequest<WorkingGraph | undefined>)
}

/** Keeps the working copy; callers pass only a graph together with its own plan. */
export async function saveWorking(working: WorkingGraph): Promise<void> {
  await run('readwrite', (s) => s.put(working, KEY))
}
