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

/** What the inspector shows: nothing, one module, or one binding (a need of a consumer). */
export type Selection = null | { kind: 'module'; instance: string } | { kind: 'binding'; consumer: string; capability: string }

/** True when the graph still binds this need to an instance. */
export function bindingExists(graph: Graph, consumer: string, capability: string): boolean {
  const uses = Object.hasOwn(graph.modules, consumer) ? graph.modules[consumer]?.uses : undefined
  return uses !== undefined && Object.hasOwn(uses, capability) && typeof uses[capability] === 'string'
}

/**
 * The selection after the graph changed (AC-8): a binding that no longer exists hands the
 * selection to its consumer module; a module that no longer exists clears it. `moved` says the
 * selection moved, so focus follows it.
 */
export function followSelection(graph: Graph, selection: Selection): { selection: Selection; moved: boolean } {
  if (selection?.kind === 'binding' && !bindingExists(graph, selection.consumer, selection.capability)) {
    if (!Object.hasOwn(graph.modules, selection.consumer)) return { selection: null, moved: true }
    return { selection: { kind: 'module', instance: selection.consumer }, moved: true }
  }
  if (selection?.kind === 'module' && !Object.hasOwn(graph.modules, selection.instance)) return { selection: null, moved: true }
  return { selection, moved: false }
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
  selection: Selection
  /** A node that should take keyboard focus once drawn; the canvas clears it after moving focus. */
  focusRequest: string | null
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
  /** Selects one item, or nothing. */
  select: (selection: Selection) => void
  /** Selects a module and asks the canvas to move keyboard focus to it. */
  focusModule: (instance: string) => void
  /** Called by the canvas once it moved focus. */
  focused: () => void
}

// the call in flight; a newer call aborts it
let inflight: AbortController | undefined

export const useEditor = create<EditorState>()((set, get) => {
  // every change of the working graph goes through here, so the selection follows it (AC-8)
  const setWorking = (working: WorkingGraph, more: Partial<EditorState> = {}) => {
    const { selection, moved } = followSelection(working.graph, get().selection)
    set({
      working,
      selection,
      ...(moved && selection?.kind === 'module' ? { focusRequest: selection.instance } : {}),
      ...more,
    })
  }

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
        setWorking(next, { planState: 'current' })
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
    selection: null,
    focusRequest: null,
    storageFailed: false,

    restore: (working) => {
      if (get().working !== undefined) return
      set({ working: working ? withPositions(working) : null })
    },
    open: (working) => {
      inflight?.abort()
      // positions of an earlier graph never carry over to a newly opened one
      const next = withPositions({ ...working, positions: undefined, edited: false })
      set((s) => ({
        working: next,
        planSeq: s.planSeq + 1,
        planState: 'current',
        planError: null,
        dragFrom: null,
        selection: null,
        focusRequest: null,
      }))
      persist(next)
    },
    edit: (change) => {
      const working = get().working
      if (!working) return
      const graph = change(working.graph)
      if (graph === working.graph) return
      setWorking({ ...working, graph, edited: true })
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
    select: (selection) => set({ selection }),
    focusModule: (instance) => set({ selection: { kind: 'module', instance }, focusRequest: instance }),
    focused: () => set({ focusRequest: null }),
  }
})

function toPlannerError(error: unknown): PlannerError {
  if (error instanceof PlannerError) return error
  return new PlannerError('The planner could not be reached. Check your connection and retry.', 0)
}
