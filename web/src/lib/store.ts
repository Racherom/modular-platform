import { create } from 'zustand'
import type { WorkingGraph } from './storage'

interface EditorState {
  /** undefined until IndexedDB answered; null when nothing was opened yet */
  working: WorkingGraph | null | undefined
  setWorking: (working: WorkingGraph | null) => void
}

export const useEditor = create<EditorState>()((set) => ({
  working: undefined,
  setWorking: (working) => set({ working }),
}))
