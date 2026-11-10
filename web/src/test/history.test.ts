import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CheckResult, Graph } from '../api/client'
import { bind, letPlannerChoose, unbind } from '../graph/edit'
import { layout } from '../graph/layout'
import { useEditor } from '../lib/store'
import { fetchHandler } from './setup'

const saveWorking = vi.hoisted(() => vi.fn((_: unknown) => Promise.resolve()))
vi.mock('../lib/storage', async (actual) => ({ ...(await actual<typeof import('../lib/storage')>()), saveWorking }))

const graph: Graph = {
  apiVersion: 'modular-platform/v1',
  kind: 'Graph',
  name: 'g',
  modules: { db: { module: 'postgres' }, db2: { module: 'postgres' }, web: { module: 'whoami', uses: { postgresql: null } } },
}
const resultFor = (g: Graph): CheckResult => ({ ok: true, graph: g, bindings: [], problems: [] })

/** Replies to check calls by hand, in any order. */
function heldRenderer() {
  const pending: { graph: Graph; reply: (r: Response) => void }[] = []
  fetchHandler.current = async (req) => {
    const g = (await req.json()) as Graph
    return new Promise<Response>((resolve) => pending.push({ graph: g, reply: resolve }))
  }
  return pending
}

/** Answers every check call at once with the posted graph. */
function echoRenderer() {
  const calls: Graph[] = []
  fetchHandler.current = async (req) => {
    const g = (await req.json()) as Graph
    calls.push(g)
    return Response.json(resultFor(g))
  }
  return calls
}

const settle = () => new Promise((r) => setTimeout(r, 0))
const uses = () => useEditor.getState().working?.graph.modules.web?.uses
const s = () => useEditor.getState()

describe('undo and redo (AC-15)', () => {
  beforeEach(() => {
    saveWorking.mockClear()
    useEditor.setState({ working: undefined, checkState: 'current', checkSeq: 0, checkError: null, selection: null, handoff: null })
    s().open({ fileName: 'g.yaml', graph, check: resultFor(graph) })
    saveWorking.mockClear()
  })

  it('steps back and forth through edits, one step each, checking after each', async () => {
    const calls = echoRenderer()
    s().edit((g) => bind(g, 'web', 'postgresql', 'db'))
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    s().edit((g) => bind(g, 'web', 'postgresql', 'db2'))
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    s().edit((g) => letPlannerChoose(g, 'web', 'postgresql'))
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    expect(s().history.past).toHaveLength(3)

    s().undo()
    expect(uses()).toEqual({ postgresql: 'db2' })
    expect(s().checkState).toBe('checking')
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    s().undo()
    expect(uses()).toEqual({ postgresql: 'db' })
    s().undo()
    expect(uses()).toEqual({ postgresql: null })
    expect(s().history.past).toHaveLength(0)
    s().undo() // nothing left: no change, no call
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    expect(calls).toHaveLength(6)

    s().redo()
    expect(uses()).toEqual({ postgresql: 'db' })
    s().redo()
    s().redo()
    expect(uses()).toEqual({})
    expect(s().history.future).toHaveLength(0)
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    expect(calls).toHaveLength(9)
  })

  it('a check reply never adds a step; the edited flag stays set after undo (AC-17)', async () => {
    echoRenderer()
    s().edit((g) => bind(g, 'web', 'postgresql', 'db'))
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    expect(s().history.past).toHaveLength(1)
    s().undo()
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    expect(s().history).toMatchObject({ past: [], future: [expect.anything()] })
    expect(s().working?.edited).toBe(true)
  })

  it('a new edit clears the redo steps', async () => {
    echoRenderer()
    s().edit((g) => bind(g, 'web', 'postgresql', 'db'))
    s().undo()
    expect(s().history.future).toHaveLength(1)
    s().edit((g) => bind(g, 'web', 'postgresql', 'db2'))
    expect(s().history.future).toHaveLength(0)
    expect(s().history.past).toHaveLength(1)
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
  })

  it('undo while a check is running drops the late reply (AC-12)', async () => {
    const pending = heldRenderer()
    s().edit((g) => bind(g, 'web', 'postgresql', 'db'))
    await vi.waitFor(() => expect(pending).toHaveLength(1))
    s().undo()
    await vi.waitFor(() => expect(pending).toHaveLength(2))
    // the bind's reply arrives after the undo and is dropped
    pending[0]!.reply(Response.json(resultFor(pending[0]!.graph)))
    await settle()
    expect(uses()).toEqual({ postgresql: null })
    pending[1]!.reply(Response.json(resultFor(pending[1]!.graph)))
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    expect(uses()).toEqual({ postgresql: null })
    expect(saveWorking).toHaveBeenCalledTimes(1)
  })

  it('opening a graph clears the history', () => {
    echoRenderer()
    s().edit((g) => bind(g, 'web', 'postgresql', 'db'))
    s().undo()
    s().open({ fileName: 'g.yaml', graph, check: resultFor(graph) })
    expect(s().history).toEqual({ past: [], future: [] })
    expect(s().working?.edited).toBe(false)
  })

  it('undo restores the positions stored with the step', async () => {
    echoRenderer()
    const before = s().working?.positions
    s().tidy() // already tidy: nothing happens
    expect(s().history.past).toHaveLength(0)
    useEditor.setState({ working: { ...s().working!, positions: { db: { x: 1, y: 1 }, db2: { x: 2, y: 2 }, web: { x: 3, y: 3 } } } })
    s().tidy()
    expect(s().working?.positions).toEqual(before)
    s().undo()
    expect(s().working?.positions).toEqual({ db: { x: 1, y: 1 }, db2: { x: 2, y: 2 }, web: { x: 3, y: 3 } })
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
  })
})

describe('Tidy layout (AC-16)', () => {
  beforeEach(() => {
    saveWorking.mockClear()
    useEditor.setState({ working: undefined, checkState: 'current', checkSeq: 0, checkError: null })
    s().open({ fileName: 'g.yaml', graph, check: resultFor(graph) })
    useEditor.setState({ working: { ...s().working!, positions: { db: { x: 9, y: 9 }, db2: { x: 8, y: 8 }, web: { x: 7, y: 7 } } } })
    saveWorking.mockClear()
  })

  it('lays out again, adds one step, sets the edited flag, checks nothing and saves', async () => {
    const calls = echoRenderer()
    const seq = s().checkSeq
    s().tidy()
    expect(s().working?.positions).toEqual(layout(graph))
    expect(s().working?.edited).toBe(true)
    expect(s().history.past).toHaveLength(1)
    expect(s().checkSeq).toBe(seq)
    expect(s().checkState).toBe('current')
    await settle()
    expect(calls).toHaveLength(0)
    expect(saveWorking).toHaveBeenCalledTimes(1)
  })

  it('saves nothing while the check failed', async () => {
    const pending = heldRenderer()
    s().edit((g) => bind(g, 'web', 'postgresql', 'db'))
    await vi.waitFor(() => expect(pending).toHaveLength(1))
    pending[0]!.reply(new Response(null, { status: 503 }))
    await vi.waitFor(() => expect(s().checkState).toBe('failed'))
    s().tidy()
    await settle()
    expect(saveWorking).not.toHaveBeenCalled()
  })
})

describe('sample copy (AC-18)', () => {
  const sample: Graph = { ...graph, name: 'sample', modules: { ...graph.modules, web: { module: 'whoami', uses: { postgresql: 'db' } } } }
  const positions = layout(sample)

  beforeEach(() => {
    useEditor.setState({ working: null, history: { past: [], future: [] }, selection: null, handoff: null, checkSeq: 0 })
  })

  it('copies with the edit applied, history holding the unedited copy, and checks it', async () => {
    const calls = echoRenderer()
    useEditor.setState({ selection: { kind: 'binding', consumer: 'web', slot: 'postgresql' } })
    const made = s().copySample({ fileName: 'sample.yaml', graph: sample, check: resultFor(sample), positions }, (g) =>
      unbind(g, 'web', 'postgresql'),
    )
    expect(made).toBe(true)
    const w = s().working!
    expect(w.fileName).toBe('sample.yaml')
    expect(w.edited).toBe(true)
    expect(w.positions).toEqual(positions)
    expect(w.graph.modules.web?.uses).toEqual({ postgresql: null })
    expect(s().history.past).toEqual([{ graph: sample, positions }])
    expect(s().checkState).toBe('checking')
    // the binding is gone, so its consumer is carried over as the selection
    expect(s().takeHandoff()).toEqual({ kind: 'module', instance: 'web' })
    expect(s().takeHandoff()).toBeNull()
    await vi.waitFor(() => expect(calls).toHaveLength(1))

    s().undo()
    expect(s().working?.graph).toBe(sample)
    // the sample itself never changes
    expect(sample.modules.web?.uses).toEqual({ postgresql: 'db' })
  })

  it('makes no copy for an edit that changes nothing', () => {
    const made = s().copySample({ fileName: 'sample.yaml', graph: sample, check: resultFor(sample), positions }, (g) => g)
    expect(made).toBe(false)
    expect(s().working).toBeNull()
  })
})

describe('catalog filter (AC-19)', () => {
  it('Find providers sets the filter and asks for focus each time', () => {
    useEditor.setState({ catalogFilter: '', filterFocus: 0 })
    s().findProviders('postgresql')
    expect(s().catalogFilter).toBe('postgresql')
    s().findProviders('postgresql')
    expect(s().filterFocus).toBe(2)
    s().setCatalogFilter('who')
    expect(s().catalogFilter).toBe('who')
  })
})
