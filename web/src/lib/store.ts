// The editor store (spec 0010). The graph in here is the only source of truth: the canvas is
// derived from it, every edit is a pure function on it, and the renderer checks each edit.
import { create } from 'zustand'
import { checkGraph, type Graph, RendererError } from '../api/client'
import type { Positions } from '../graph/flow'
import { layout } from '../graph/layout'
import { saveWorking, type WorkingGraph, withPositions } from './storage'

/** Whether the shown check result belongs to the shown graph. */
export type CheckState = 'current' | 'checking' | 'failed'

/** The slot a connect drag started from. */
export interface DragFrom {
  instance: string
  slot: string
}

/** What the inspector shows: nothing, one module, or one binding (a slot of a consumer). */
export type Selection = null | { kind: 'module'; instance: string } | { kind: 'binding'; consumer: string; slot: string }

/** The working graph and positions just before a user edit; one undo step (AC-15). */
export interface Snapshot {
  graph: Graph
  positions: Positions
}

/** Undo steps (oldest first) and redo steps (the next redo last). Kept in memory only. */
export interface History {
  past: Snapshot[]
  future: Snapshot[]
}

const emptyHistory: History = { past: [], future: [] }

/** True when the graph still binds this slot to an instance (string or object form). */
export function bindingExists(graph: Graph, consumer: string, slot: string): boolean {
  const uses = Object.hasOwn(graph.modules, consumer) ? graph.modules[consumer]?.uses : undefined
  return uses !== undefined && Object.hasOwn(uses, slot) && uses[slot] !== null && uses[slot] !== undefined
}

/**
 * The selection after the graph changed (AC-8): a binding that no longer exists hands the
 * selection to its consumer module; a module that no longer exists clears it. `moved` says the
 * selection moved, so focus follows it.
 */
export function followSelection(graph: Graph, selection: Selection): { selection: Selection; moved: boolean } {
  if (selection?.kind === 'binding' && !bindingExists(graph, selection.consumer, selection.slot)) {
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
  checkState: CheckState
  /** Counts check calls; only the reply to the latest one is used. */
  checkSeq: number
  checkError: RendererError | null
  dragFrom: DragFrom | null
  selection: Selection
  /** A node that should take keyboard focus once drawn; the canvas clears it after moving focus. */
  focusRequest: string | null
  /** The last save to IndexedDB failed, so changes are not kept after a reload. */
  storageFailed: boolean
  /** Undo and redo steps; cleared when a graph is opened. */
  history: History
  /** The catalog panel's filter text (AC-19). */
  catalogFilter: string
  /** Counts Find providers requests; the catalog panel moves focus to its filter on each. */
  filterFocus: number
  /** A selection carried from the sample screen to the editor root after the sample copy. */
  handoff: Selection

  /** Shows the working copy read from IndexedDB, unless a graph was opened meanwhile. */
  restore: (working: WorkingGraph | null) => void
  /** Shows a graph that was just opened and checked, and keeps it. */
  open: (working: WorkingGraph) => void
  /** Applies a user edit at once, then checks the edited graph. */
  edit: (change: (graph: Graph) => Graph) => void
  /** Steps back to the graph and positions before the last edit, then checks it (AC-15). */
  undo: () => void
  /** Steps forward again after an undo, then checks it (AC-15). */
  redo: () => void
  /** Lays out the whole graph again; changes positions only and checks nothing (AC-16). */
  tidy: () => void
  /**
   * Makes the working copy from a sample and applies the first edit to it (AC-18): history holds
   * the unedited copy, the edited flag is set and a check call starts. Returns false when the edit
   * changes nothing, so no copy is made.
   */
  copySample: (base: WorkingGraph, change: (graph: Graph) => Graph) => boolean
  /** Takes the selection carried over by the sample copy, once. */
  takeHandoff: () => Selection
  setCatalogFilter: (text: string) => void
  /** Filters the catalog by a capability and asks the catalog panel to take focus (AC-19). */
  findProviders: (capability: string) => void
  /** Checks the current graph again after a failed call. */
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

  // AC-12: one POST /v1/check per edit; a newer call aborts the older one, and a reply whose
  // sequence is no longer the latest is dropped
  const check = () => {
    const working = get().working
    if (!working) return
    inflight?.abort()
    const controller = new AbortController()
    inflight = controller
    const seq = get().checkSeq + 1
    set({ checkSeq: seq, checkState: 'checking', checkError: null })
    checkGraph(working.graph, controller.signal).then(
      (result) => {
        const now = get().working
        if (get().checkSeq !== seq || !now) return
        // the renderer's graph carries the automatic bindings; adopting it adds no history step
        const next = withPositions({ ...now, graph: result.graph, check: result })
        setWorking(next, { checkState: 'current' })
        persist(next)
      },
      (error: unknown) => {
        if (get().checkSeq !== seq || controller.signal.aborted) return
        set({ checkState: 'failed', checkError: toRendererError(error) })
      },
    )
  }

  // a user edit pushes the state before it and drops the redo steps (AC-15)
  const record = (working: WorkingGraph) => {
    set((s) => ({ history: { past: [...s.history.past, snapshot(working)], future: [] } }))
  }

  // undo takes from past and keeps the current state for redo; redo is the mirror image. Either
  // way the restored graph is checked again, since no check is kept in history
  const step = (direction: 'undo' | 'redo') => {
    const { working, history } = get()
    const from = direction === 'undo' ? history.past : history.future
    const to = direction === 'undo' ? history.future : history.past
    const target = from.at(-1)
    if (!working || !target) return
    const rest = { from: from.slice(0, -1), to: [...to, snapshot(working)] }
    set({ history: direction === 'undo' ? { past: rest.from, future: rest.to } : { past: rest.to, future: rest.from } })
    setWorking(withPositions({ ...working, graph: target.graph, positions: target.positions, edited: true }))
    check()
  }

  return {
    working: undefined,
    checkState: 'current',
    checkSeq: 0,
    checkError: null,
    dragFrom: null,
    selection: null,
    focusRequest: null,
    storageFailed: false,
    history: emptyHistory,
    catalogFilter: '',
    filterFocus: 0,
    handoff: null,

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
        checkSeq: s.checkSeq + 1,
        checkState: 'current',
        checkError: null,
        dragFrom: null,
        selection: null,
        focusRequest: null,
        history: emptyHistory,
        handoff: null,
      }))
      persist(next)
    },
    edit: (change) => {
      const working = get().working
      if (!working) return
      const graph = change(working.graph)
      if (graph === working.graph) return
      record(working)
      setWorking({ ...working, graph, edited: true })
      check()
    },
    undo: () => step('undo'),
    redo: () => step('redo'),
    tidy: () => {
      const working = get().working
      if (!working) return
      const positions = layout(working.graph)
      if (samePositions(positions, working.positions)) return
      record(working)
      const next = { ...working, positions, edited: true }
      set({ working: next })
      // a check still running saves the new positions with its reply; a failed one saves nothing
      if (get().checkState === 'current') persist(next)
    },
    copySample: (base, change) => {
      const graph = change(base.graph)
      if (graph === base.graph) return false
      inflight?.abort()
      const start = withPositions({ ...base, edited: false })
      set({ history: { past: [snapshot(start)], future: [] }, checkError: null, dragFrom: null })
      setWorking({ ...start, graph, edited: true })
      set({ handoff: get().selection })
      check()
      return true
    },
    takeHandoff: () => {
      const handoff = get().handoff
      if (handoff) set({ handoff: null })
      return handoff
    },
    setCatalogFilter: (catalogFilter) => set({ catalogFilter }),
    findProviders: (capability) => set((s) => ({ catalogFilter: capability, filterFocus: s.filterFocus + 1 })),
    retry: check,
    saved: () => {
      const working = get().working
      if (!working?.edited) return
      const next = { ...working, edited: false }
      set({ working: next })
      // the copy in storage must hold a check result for exactly its graph
      if (get().checkState === 'current') persist(next)
    },
    setDragFrom: (dragFrom) => set({ dragFrom }),
    select: (selection) => set({ selection }),
    focusModule: (instance) => set({ selection: { kind: 'module', instance }, focusRequest: instance }),
    focused: () => set({ focusRequest: null }),
  }
})

function snapshot(working: WorkingGraph): Snapshot {
  return { graph: working.graph, positions: working.positions ?? {} }
}

function samePositions(a: Positions, b: Positions | undefined): boolean {
  if (!b) return false
  const names = Object.keys(a)
  return names.length === Object.keys(b).length && names.every((n) => b[n]?.x === a[n]?.x && b[n]?.y === a[n]?.y)
}

function toRendererError(error: unknown): RendererError {
  if (error instanceof RendererError) return error
  return new RendererError('The renderer could not be reached. Check your connection and retry.', 0)
}
