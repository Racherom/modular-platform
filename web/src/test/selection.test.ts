import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Graph, PlanResult } from '../api/client'
import { bind, unbind } from '../graph/edit'
import { followSelection, useEditor } from '../lib/store'
import { fetchHandler } from './setup'

vi.mock('../lib/storage', async (actual) => ({
  ...(await actual<typeof import('../lib/storage')>()),
  saveWorking: () => Promise.resolve(),
}))

const graph: Graph = {
  apiVersion: 'modular-platform/v0alpha1',
  kind: 'Graph',
  name: 'g',
  modules: { db: { module: 'postgres' }, db2: { module: 'postgres' }, web: { module: 'whoami', uses: { postgresql: 'db' } } },
}
const planFor = (g: Graph): PlanResult => ({ ok: true, graph: g, bindings: [], problems: [] })

describe('followSelection (AC-8)', () => {
  it.each([
    ['keeps a binding that still exists', { kind: 'binding', consumer: 'web', capability: 'postgresql' } as const, graph, false],
    ['keeps a module that still exists', { kind: 'module', instance: 'db' } as const, graph, false],
    ['keeps nothing selected', null, graph, false],
  ])('%s', (_, selection, g, moved) => {
    expect(followSelection(g, selection)).toEqual({ selection, moved })
  })

  it('hands a binding that stopped existing to its consumer module', () => {
    expect(followSelection(unbind(graph, 'web', 'postgresql'), { kind: 'binding', consumer: 'web', capability: 'postgresql' })).toEqual({
      selection: { kind: 'module', instance: 'web' },
      moved: true,
    })
  })

  it('keeps a rebound binding selected: it is the same need', () => {
    const g = bind(graph, 'web', 'postgresql', 'db2')
    expect(followSelection(g, { kind: 'binding', consumer: 'web', capability: 'postgresql' }).moved).toBe(false)
  })

  it('clears a selection whose module is gone, even one named like a prototype key', () => {
    expect(followSelection(graph, { kind: 'module', instance: 'gone' })).toEqual({ selection: null, moved: true })
    expect(followSelection(graph, { kind: 'binding', consumer: 'constructor', capability: 'x' })).toEqual({ selection: null, moved: true })
  })
})

describe('the store keeps the selection in step with the graph', () => {
  beforeEach(() => {
    fetchHandler.current = async (req) => Response.json(planFor((await req.json()) as Graph))
    useEditor.setState({ working: undefined, selection: null, focusRequest: null, planSeq: 0 })
    useEditor.getState().open({ fileName: 'g.yaml', graph, plan: planFor(graph) })
  })

  it('unbinding the selected binding selects the consumer and asks for focus', () => {
    useEditor.getState().select({ kind: 'binding', consumer: 'web', capability: 'postgresql' })
    useEditor.getState().edit((g) => unbind(g, 'web', 'postgresql'))
    const s = useEditor.getState()
    expect(s.selection).toEqual({ kind: 'module', instance: 'web' })
    expect(s.focusRequest).toBe('web')
    s.focused()
    expect(useEditor.getState().focusRequest).toBeNull()
  })

  it('a plan reply that drops the binding moves the selection too', async () => {
    fetchHandler.current = async (req) => {
      const g = (await req.json()) as Graph
      return Response.json(planFor(unbind(g, 'web', 'postgresql')))
    }
    useEditor.getState().select({ kind: 'binding', consumer: 'web', capability: 'postgresql' })
    useEditor.getState().edit((g) => bind(g, 'web', 'postgresql', 'db2'))
    expect(useEditor.getState().selection?.kind).toBe('binding')
    await vi.waitFor(() => expect(useEditor.getState().planState).toBe('current'))
    expect(useEditor.getState().selection).toEqual({ kind: 'module', instance: 'web' })
  })

  it('opening a graph clears the selection', () => {
    useEditor.getState().select({ kind: 'module', instance: 'db' })
    useEditor.getState().open({ fileName: 'g.yaml', graph, plan: planFor(graph) })
    expect(useEditor.getState().selection).toBeNull()
  })

  it('focusModule selects a module and asks for focus', () => {
    useEditor.getState().focusModule('db2')
    expect(useEditor.getState()).toMatchObject({ selection: { kind: 'module', instance: 'db2' }, focusRequest: 'db2' })
  })
})
