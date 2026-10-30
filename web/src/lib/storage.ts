// The working graph at <base>/ is the last opened file, kept in IndexedDB with its last plan so
// a refresh loses nothing and needs no new plan call.
import type { Graph, PlanResult } from '../api/client'

export interface WorkingGraph {
  fileName: string
  graph: Graph
  plan: PlanResult
}

const DB = 'modular-platform'
const STORE = 'working'
const KEY = 'graph'

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB, 1)
    req.onupgradeneeded = () => req.result.createObjectStore(STORE)
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error ?? new Error('IndexedDB is not available.'))
  })
}

function run<T>(mode: IDBTransactionMode, op: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  return open().then(
    (db) =>
      new Promise<T>((resolve, reject) => {
        const req = op(db.transaction(STORE, mode).objectStore(STORE))
        req.onsuccess = () => resolve(req.result)
        req.onerror = () => reject(req.error ?? new Error('IndexedDB failed.'))
      }).finally(() => db.close()),
  )
}

export function loadWorking(): Promise<WorkingGraph | undefined> {
  return run<WorkingGraph | undefined>('readonly', (s) => s.get(KEY) as IDBRequest<WorkingGraph | undefined>)
}

export async function saveWorking(working: WorkingGraph): Promise<void> {
  await run('readwrite', (s) => s.put(working, KEY))
}
