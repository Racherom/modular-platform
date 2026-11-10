import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CheckResult, Graph } from '../api/client'
import { EMPTY_GRAPH, useEditor } from '../lib/store'
import { testCatalog, testModule } from './fixtures'
import { fetchHandler } from './setup'

const saveWorking = vi.hoisted(() => vi.fn((_: unknown) => Promise.resolve()))
vi.mock('../lib/storage', async (actual) => ({ ...(await actual<typeof import('../lib/storage')>()), saveWorking }))

const catalog = testCatalog([
  testModule('postgres', { provides: { postgresql: {} } }),
  testModule('whoami', { provides: { http: {} }, uses: { postgresql: {} } }),
])
const resultFor = (g: Graph): CheckResult => ({ ok: true, graph: g, bindings: [], problems: [] })
const s = () => useEditor.getState()
const names = () => Object.keys(s().working?.graph.modules ?? {})

function echoRenderer() {
  const calls: Graph[] = []
  fetchHandler.current = async (req) => {
    const g = (await req.json()) as Graph
    calls.push(g)
    return Response.json(resultFor(g))
  }
  return calls
}

describe('adding a module (spec 0029)', () => {
  beforeEach(() => {
    saveWorking.mockClear()
    useEditor.setState({ working: null, checkState: 'current', checkSeq: 0, checkError: null, selection: null, handoff: null })
    s().open({ fileName: 'g.yaml', graph: EMPTY_GRAPH, check: resultFor(EMPTY_GRAPH) })
    useEditor.setState({ announcement: null, centerRequest: null, focusRequest: null })
    saveWorking.mockClear()
  })

  it('adds with a position, selects, announces and asks to center, with one check call (AC-4, AC-6, AC-8)', async () => {
    const calls = echoRenderer()
    expect(s().addModule('postgres', catalog)).toBe('postgres')
    expect(s().working?.graph.modules).toEqual({ postgres: { module: 'postgres' } })
    expect(s().working?.positions).toEqual({ postgres: { x: 0, y: 0 } })
    expect(s().working?.edited).toBe(true)
    expect(s().selection).toEqual({ kind: 'module', instance: 'postgres' })
    expect(s().focusRequest).toBe('postgres')
    expect(s().centerRequest).toBe('postgres')
    expect(s().announcement).toBe('Added postgres')
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    expect(calls).toHaveLength(1)
    // the reply keeps the position: no full layout ran
    expect(s().working?.positions).toEqual({ postgres: { x: 0, y: 0 } })
  })

  it('places a second node below the first and moves nothing (AC-4)', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    const first = s().working?.positions?.postgres
    s().addModule('whoami', catalog)
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    expect(s().working?.positions?.postgres).toEqual(first)
    expect(s().working?.positions?.whoami?.x).toBe(0)
    expect(s().working?.positions?.whoami?.y).toBeGreaterThan(0)
  })

  it('writes no binding (AC-7)', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().addModule('whoami', catalog)
    expect(s().working?.graph.modules.whoami).toEqual({ module: 'whoami' })
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
  })

  it('undo removes the instance and position, redo brings both back selected (AC-5)', async () => {
    const calls = echoRenderer()
    s().addModule('postgres', catalog)
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    const at = s().working?.positions?.postgres
    s().undo()
    expect(names()).toEqual([])
    expect(s().working?.positions).toEqual({})
    expect(s().selection).toBeNull()
    expect(s().announcement).toBeNull()
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    s().redo()
    expect(names()).toEqual(['postgres'])
    expect(s().working?.positions?.postgres).toEqual(at)
    expect(s().selection).toEqual({ kind: 'module', instance: 'postgres' })
    expect(s().focusRequest).toBe('postgres')
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
    expect(calls).toHaveLength(3)
  })

  it('an add after an undo clears redo and is announced again (AC-5, AC-8)', async () => {
    echoRenderer()
    s().addModule('postgres', catalog)
    s().undo()
    expect(s().history.future).toHaveLength(1)
    s().addModule('postgres', catalog)
    expect(s().history.future).toHaveLength(0)
    expect(s().announcement).toBe('Added postgres')
    s().tidy()
    s().edit((g) => ({ ...g }))
    expect(s().announcement).toBeNull()
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
  })

  it('only the latest reply is used and a failed check keeps the add (AC-6)', async () => {
    const pending: { graph: Graph; reply: (r: Response) => void }[] = []
    fetchHandler.current = async (req) => {
      const g = (await req.json()) as Graph
      return new Promise<Response>((resolve) => pending.push({ graph: g, reply: resolve }))
    }
    s().addModule('postgres', catalog)
    await vi.waitFor(() => expect(pending).toHaveLength(1))
    s().addModule('whoami', catalog) // while the first check runs
    await vi.waitFor(() => expect(pending).toHaveLength(2))
    pending[0]?.reply(Response.json(resultFor(pending[0].graph)))
    await new Promise((r) => setTimeout(r, 0))
    expect(s().checkState).toBe('checking')
    pending[1]?.reply(new Response('down', { status: 503 }))
    await vi.waitFor(() => expect(s().checkState).toBe('failed'))
    expect(names().sort()).toEqual(['postgres', 'whoami'])
    expect(saveWorking).not.toHaveBeenCalled()
  })

  it('with nothing open, makes the empty copy first; one undo returns to the empty graph (AC-9)', async () => {
    useEditor.setState({ working: null })
    echoRenderer()
    s().addModule('postgres', catalog)
    expect(s().working?.fileName).toBe('untitled.yaml')
    expect(s().history.past).toHaveLength(1)
    expect(saveWorking).not.toHaveBeenCalled()
    await vi.waitFor(() => expect(saveWorking).toHaveBeenCalledTimes(1))
    s().undo()
    expect(s().working?.graph.modules).toEqual({})
    expect(s().working).not.toBeNull()
    await vi.waitFor(() => expect(s().checkState).toBe('current'))
  })

  it('a prototype name is just a name', () => {
    echoRenderer()
    expect(s().addModule('constructor', testCatalog([testModule('constructor')]))).toBe('constructor')
  })
})
