// The editor store (spec 0010). The graph in here is the only source of truth: the canvas is
// derived from it, every edit is a pure function on it, and the planner checks each edit.
import { create } from 'zustand'
import { type Graph, PlannerError, planGraph } from '../api/client'
import { saveWorking, type WorkingGraph, withPositions } from './storage'

/** Whether the shown plan belongs to the shown graph. */
export type PlanState = 'current' | 'checking' | 'failed'

/** The need a connect drag started from. */
export interface DragFrom {
  instance: string
  capability: string
}

/** The editor's state and the actions that change it. */
export interface EditorState {
  /** undefined until IndexedDB answered; null when nothing was opened yet */
  working: WorkingGraph | null | undefined
  planState: PlanState
  /** Counts plan calls; only the reply to the latest one is used. */
  planSeq: number
  planError: PlannerError | null
  dragFrom: DragFrom | null
  /** The last save to IndexedDB failed, so changes are not kept after a reload. */
  storageFailed: boolean

  /** Shows the working copy read from IndexedDB, unless a graph was opened meanwhile. */
  restore: (working: WorkingGraph | null) => void
  /** Shows a graph that was just opened and planned, and keeps it. */
  open: (working: WorkingGraph) => void
  /** Applies a user edit at once, then plans the edited graph. */
  edit: (change: (graph: Graph) => Graph) => void
  /** Plans the current graph again after a failed call. */
  retry: () => void
  /** Clears the edited flag after Save as file. */
  saved: () => void
  setDragFrom: (from: DragFrom | null) => void
}

// the call in flight; a newer call aborts it
let inflight: AbortController | undefined

export const useEditor = create<EditorState>()((set, get) => {
  const persist = (working: WorkingGraph) => {
    saveWorking(working).then(
      () => set({ storageFailed: false }),
      () => set({ storageFailed: true }),
    )
  }

  // AC-12: one POST /v1/plan per edit; a newer call aborts the older one, and a reply whose
  // sequence is no longer the latest is dropped
  const plan = () => {
    const working = get().working
    if (!working) return
    inflight?.abort()
    const controller = new AbortController()
    inflight = controller
    const seq = get().planSeq + 1
    set({ planSeq: seq, planState: 'checking', planError: null })
    planGraph(working.graph, controller.signal).then(
      (result) => {
        const now = get().working
        if (get().planSeq !== seq || !now) return
        // the planner's graph carries the automatic bindings; adopting it adds no history step
        const next = withPositions({ ...now, graph: result.graph, plan: result })
        set({ working: next, planState: 'current' })
        persist(next)
      },
      (error: unknown) => {
        if (get().planSeq !== seq || controller.signal.aborted) return
        set({ planState: 'failed', planError: toPlannerError(error) })
      },
    )
  }

  return {
    working: undefined,
    planState: 'current',
    planSeq: 0,
    planError: null,
    dragFrom: null,
    storageFailed: false,

    restore: (working) => {
      if (get().working !== undefined) return
      set({ working: working ? withPositions(working) : null })
    },
    open: (working) => {
      inflight?.abort()
      // positions of an earlier graph never carry over to a newly opened one
      const next = withPositions({ ...working, positions: undefined, edited: false })
      set((s) => ({ working: next, planSeq: s.planSeq + 1, planState: 'current', planError: null, dragFrom: null }))
      persist(next)
    },
    edit: (change) => {
      const working = get().working
      if (!working) return
      const graph = change(working.graph)
      if (graph === working.graph) return
      set({ working: { ...working, graph, edited: true } })
      plan()
    },
    retry: plan,
    saved: () => {
      const working = get().working
      if (!working?.edited) return
      const next = { ...working, edited: false }
      set({ working: next })
      // the copy in storage must hold a plan for exactly its graph
      if (get().planState === 'current') persist(next)
    },
    setDragFrom: (dragFrom) => set({ dragFrom }),
  }
})

function toPlannerError(error: unknown): PlannerError {
  if (error instanceof PlannerError) return error
  return new PlannerError('The planner could not be reached. Check your connection and retry.', 0)
}
