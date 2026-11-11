import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CheckResult, Graph } from '../api/client'
import { bind } from '../graph/edit'
import { nodeHeight } from '../graph/flow'
import { ROW_GAP } from '../graph/layout'
import { EMPTY_GRAPH, useEditor } from '../lib/store'
import { testCatalog, testModule } from './fixtures'
import { fetchHandler } from './setup'

const saveWorking = vi.hoisted(() => vi.fn((_: unknown) => Promise.resolve()))
vi.mock('../lib/storage', async (actual) => ({ ...(await actual<typeof import('../lib/storage')>()), saveWorking }))

const catalog = testCatalog([
  testModule('postgres', { provides: { postgresql: {} } }),
  testModule('whoami', { provides: { http: {} }, uses: { postgresql: {} } }),
  testModule('tall', { provides: { a: {} }, uses: { b: {}, c: {}, d: {}, e: {} } }),
])
const resultFor = (g: Graph): CheckResult => ({ ok: true, graph: g, bindings: [], problems: [] })
const s = () => useEditor.getState()
const names = () => Object.keys(s().working?.graph.modules ?? {})
const pos = () => s().working?.positions ?? {}
const settled = () => vi.waitFor(() => expect(s().checkState).toBe('current'))

function echoRenderer() {
  const calls: Graph[] = []
  fetchHandler.current = async (req) => {
    const g = (await req.json()) as Graph
    calls.push(g)
    return Response.json(resultFor(g))
  }
  return calls
}

function start(graph: Graph = EMPTY_GRAPH, positions?: Record<string, { x: number; y: number }>) {
  useEditor.setState({ working: null, checkState: 'current', checkSeq: 0, checkError: null, selection: null, handoff: null })
  s().open({ fileName: 'g.yaml', graph, check: resultFor(graph) })
  if (positions) useEditor.setState({ working: { ...s().working!, positions } })
  useEditor.setState({ announcement: null, centerRequest: null, focusRequest: null })
  saveWorking.mockClear()
}

describe('adding the same module again and again in the store (AC-3)', () => {
  beforeEach(() => start())

  it('adds as often as asked: unique names, one step and one check call each, rows that do not overlap', async () => {
    const calls = echoRenderer()
    const added: string[] = []
    for (let i = 0; i < 6; i++) {
      added.push(s().addModule('postgres', catalog))
      await settled()
    }
    expect(added).toEqual(['postgres', 'postgres-2', 'postgres-3', 'postgres-4', 'postgres-5', 'postgres-6'])
    expect(names()).toEqual(added)
    expect(s().history.past).toHaveLength(6)
    expect(calls).toHaveLength(6)
    expect(Object.values(s().working?.graph.modules ?? {}).every((m) => m.module === 'postgres' && Object.keys(m).length === 1)).toBe(true)
    const ys = added.map((n) => pos()[n]?.y ?? Number.NaN)
    for (let i = 1; i < ys.length; i++) expect(ys[i]).toBe((ys[i - 1] ?? 0) + nodeHeight(1, 0) + ROW_GAP)
  })

  it('adds quickly one after another without losing one', async () => {
    echoRenderer()
    for (let i = 0; i < 5; i++) s().addModule('postgres', catalog)
    await settled()
    expect(names()).toHaveLength(5)
    expect(new Set(names()).size).toBe(5)
    expect(Object.keys(pos()).sort()).toEqual([...names()].sort())
  })
})

describe('placing nodes in the store (AC-4)', () => {
  it('moves no existing position and keeps them after the check reply, even when they differ from the auto layout', async () => {
    const g: Graph = {
      ...EMPTY_GRAPH,
      modules: { db: { module: 'postgres' }, web: { module: 'whoami' }, far: { module: 'postgres' } },
    }
    const placed = { db: { x: 0, y: 700 }, web: { x: 640, y: 33 }, far: { x: -40, y: 5 } }
    start(g, placed)
    echoRenderer()
    s().addModule('whoami', catalog)
    expect(pos()).toMatchObject(placed)
    expect(pos().whoami).toEqual({ x: 0, y: 700 + nodeHeight(1, 0) + ROW_GAP })
    await settled()
    expect(pos()).toMatchObject(placed)
    expect(pos().whoami?.x).toBe(0)
  })

  it('puts the node below a tall one at x 0 so the two do not overlap', async () => {
    start()
    echoRenderer()
    s().addModule('tall', catalog)
    s().addModule('postgres', catalog)
    await settled()
    const tall = pos().tall
    const next = pos().postgres
    expect(tall).toEqual({ x: 0, y: 0 })
    expect(next?.x).toBe(0)
    expect(next?.y).toBe(nodeHeight(1, 4) + ROW_GAP)
    expect(next?.y ?? 0).toBeGreaterThanOrEqual((tall?.y ?? 0) + nodeHeight(1, 4))
  })

  it('on a graph with a node that has no position, the add leaves that one for the layout and moves nothing else', async () => {
    const g: Graph = { ...EMPTY_GRAPH, modules: { db: { module: 'postgres' } } }
    start(g, { db: { x: 0, y: 50 } })
    echoRenderer()
    s().addModule('postgres', catalog)
    await settled()
    expect(pos().db).toEqual({ x: 0, y: 50 })
  })
})

describe('undo and redo of an add (AC-5)', () => {
  beforeEach(() => start())

  it('each step is one check call, and the edited flag stays set', async () => {
    const calls = echoRenderer()
    s().addModule('postgres', catalog)
    await settled()
    expect(s().working?.edited).toBe(true)
    expect(calls).toHaveLength(1)
    s().undo()
    await settled()
    expect(calls).toHaveLength(2)
    expect(calls[1]?.modules).toEqual({})
    expect(s().working?.edited).toBe(true)
    s().redo()
    await settled()
    expect(calls).toHaveLength(3)
    expect(Object.keys(calls[2]?.modules ?? {})).toEqual(['postgres'])
    expect(s().working?.edited).toBe(true)
  })

  it('moves the step between past and future', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    await settled()
    expect(s().history).toMatchObject({ past: [{ graph: { modules: {} }, positions: {} }], future: [] })
    s().undo()
    expect([s().history.past.length, s().history.future.length]).toEqual([0, 1])
    s().redo()
    expect([s().history.past.length, s().history.future.length]).toEqual([1, 0])
    await settled()
  })

  it('redo brings back the same name and position, also for a numbered one', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().addModule('postgres', catalog)
    await settled()
    const at = pos()['postgres-2']
    s().undo()
    expect(names()).toEqual(['postgres'])
    expect(pos()['postgres-2']).toBeUndefined()
    s().redo()
    expect(names()).toEqual(['postgres', 'postgres-2'])
    expect(pos()['postgres-2']).toEqual(at)
    await settled()
    expect(pos()['postgres-2']).toEqual(at)
  })

  it('undo twice and redo twice restore both adds in order', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().addModule('whoami', catalog)
    const both = structuredClone(pos())
    s().undo()
    s().undo()
    expect(names()).toEqual([])
    s().redo()
    expect(names()).toEqual(['postgres'])
    s().redo()
    expect(names()).toEqual(['postgres', 'whoami'])
    expect(pos()).toEqual(both)
    await settled()
  })

  it('an add after an undo drops redo and cannot be redone away', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().addModule('whoami', catalog)
    s().undo()
    s().addModule('tall', catalog)
    expect(s().history.future).toEqual([])
    s().redo() // nothing to redo
    expect(names()).toEqual(['postgres', 'tall'])
    await settled()
  })

  it('undo clears the selection of the added node; redo selects it again and asks for focus', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().addModule('whoami', catalog)
    await settled()
    expect(s().selection).toEqual({ kind: 'module', instance: 'whoami' })
    s().undo()
    expect(s().selection).toBeNull()
    s().focused()
    s().redo()
    expect(s().selection).toEqual({ kind: 'module', instance: 'whoami' })
    expect(s().focusRequest).toBe('whoami')
    await settled()
  })

  it('redo of another kind of edit asks for no focus', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().addModule('whoami', catalog)
    s().edit((g) => bind(g, 'whoami', 'postgresql', 'postgres'))
    s().undo()
    s().focused()
    useEditor.setState({ selection: null })
    s().redo()
    expect(s().focusRequest).toBeNull()
    expect(s().selection).toBeNull()
    await settled()
  })

  it('undo of an add does not ask the canvas to center', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().centered()
    s().undo()
    expect(s().centerRequest).toBeNull()
    s().redo()
    expect(s().centerRequest).toBeNull()
    await settled()
  })

  it('a late reply to the add is dropped by the undo', async () => {
    const pending: ((r: Response) => void)[] = []
    const posted: Graph[] = []
    fetchHandler.current = async (req) => {
      posted.push((await req.json()) as Graph)
      return new Promise<Response>((resolve) => pending.push(resolve))
    }
    s().addModule('postgres', catalog)
    await vi.waitFor(() => expect(pending).toHaveLength(1))
    s().undo()
    await vi.waitFor(() => expect(pending).toHaveLength(2))
    pending[0]?.(Response.json(resultFor(posted[0] as Graph)))
    await new Promise((r) => setTimeout(r, 0))
    expect(names()).toEqual([])
    pending[1]?.(Response.json(resultFor(posted[1] as Graph)))
    await settled()
    expect(names()).toEqual([])
  })
})

describe('focus, centering and the announcement (AC-8)', () => {
  beforeEach(() => start())

  it('sets selection, focus and centering for the new node and takes the old selection away', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().focused()
    s().centered()
    s().select({ kind: 'module', instance: 'postgres' })
    s().addModule('whoami', catalog)
    expect(s().selection).toEqual({ kind: 'module', instance: 'whoami' })
    expect(s().focusRequest).toBe('whoami')
    expect(s().centerRequest).toBe('whoami')
    await settled()
  })

  it('asks again for the same name after the canvas answered', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().focused()
    s().centered()
    expect([s().focusRequest, s().centerRequest]).toEqual([null, null])
    s().undo()
    s().addModule('postgres', catalog)
    expect([s().focusRequest, s().centerRequest]).toEqual(['postgres', 'postgres'])
    await settled()
  })

  /** Every value the announcement took, in order. */
  function listen() {
    const heard: (string | null)[] = []
    const stop = useEditor.subscribe((state, prev) => {
      if (state.announcement !== prev.announcement) heard.push(state.announcement)
    })
    return { heard, stop }
  }

  it('an add after an undo is announced again: the line is cleared in between', async () => {
    echoRenderer()
    const { heard, stop } = listen()
    s().addModule('postgres', catalog)
    s().undo()
    s().addModule('postgres', catalog)
    stop()
    expect(heard).toEqual(['Added postgres', null, 'Added postgres'])
    await settled()
  })

  it('the next add replaces the line with its own name', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().addModule('postgres', catalog)
    expect(s().announcement).toBe('Added postgres-2')
    await settled()
  })

  it.each([
    ['undo', () => s().undo()],
    ['Tidy', () => s().tidy()],
    ['another edit', () => s().edit((g) => bind(g, 'whoami', 'postgresql', 'postgres'))],
    ['open', () => s().open({ fileName: 'x.yaml', graph: EMPTY_GRAPH, check: resultFor(EMPTY_GRAPH) })],
  ])('%s clears it', async (_, act) => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().addModule('whoami', catalog)
    expect(s().announcement).toBe('Added whoami')
    act()
    expect(s().announcement).toBeNull()
    await settled()
  })

  it('redo clears it', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().undo()
    useEditor.setState({ announcement: 'Added postgres' })
    s().redo()
    expect(s().announcement).toBeNull()
    await settled()
  })

  it('a failed check keeps the line', async () => {
    fetchHandler.current = async () => new Response('down', { status: 503 })
    s().addModule('postgres', catalog)
    await vi.waitFor(() => expect(s().checkState).toBe('failed'))
    expect(s().announcement).toBe('Added postgres')
  })
})
