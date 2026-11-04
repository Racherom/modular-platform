import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Graph, PlanResult } from '../api/client'
import { bind } from '../graph/edit'
import { useEditor } from '../lib/store'
import { fetchHandler } from './setup'

const saveWorking = vi.hoisted(() => vi.fn((_: unknown) => Promise.resolve()))
vi.mock('../lib/storage', async (actual) => ({ ...(await actual<typeof import('../lib/storage')>()), saveWorking }))

const graph: Graph = {
  apiVersion: 'modular-platform/v0alpha1',
  kind: 'Graph',
  name: 'g',
  modules: { db: { module: 'postgres' }, db2: { module: 'postgres' }, web: { module: 'whoami', uses: { postgresql: null } } },
}
const planFor = (g: Graph): PlanResult => ({ ok: true, graph: g, bindings: [], problems: [] })

/** Replies to plan calls by hand, in any order. */
function heldPlanner() {
  const pending: { graph: Graph; reply: (r: Response) => void }[] = []
  fetchHandler.current = async (req) => {
    const g = (await req.json()) as Graph
    return new Promise<Response>((resolve) => pending.push({ graph: g, reply: resolve }))
  }
  return pending
}

const settle = () => new Promise((r) => setTimeout(r, 0))

describe('editor store', () => {
  beforeEach(() => {
    saveWorking.mockClear()
    useEditor.setState({ working: undefined, planState: 'current', planSeq: 0, planError: null, dragFrom: null, storageFailed: false })
    useEditor.getState().open({ fileName: 'g.yaml', graph, plan: planFor(graph) })
    saveWorking.mockClear()
  })

  it('lays out a newly opened graph and keeps it', () => {
    const w = useEditor.getState().working
    expect(Object.keys(w?.positions ?? {}).sort()).toEqual(['db', 'db2', 'web'])
    expect(w?.edited).toBe(false)
  })

  it('shows an edit at once, plans it, then adopts the reply and saves (AC-12)', async () => {
    const pending = heldPlanner()
    const before = useEditor.getState().working?.positions
    useEditor.getState().edit((g) => bind(g, 'web', 'postgresql', 'db'))

    let s = useEditor.getState()
    expect(s.working?.graph.modules.web?.uses).toEqual({ postgresql: 'db' })
    expect(s.working?.edited).toBe(true)
    expect(s.planState).toBe('checking')
    expect(saveWorking).not.toHaveBeenCalled()

    await vi.waitFor(() => expect(pending).toHaveLength(1))
    const planned: Graph = { ...pending[0]!.graph, name: 'from-planner' }
    pending[0]!.reply(Response.json(planFor(planned)))
    await vi.waitFor(() => expect(useEditor.getState().planState).toBe('current'))

    s = useEditor.getState()
    expect(s.working?.graph.name).toBe('from-planner')
    expect(s.working?.plan.graph).toBe(s.working?.graph)
    expect(s.working?.positions).toEqual(before)
    expect(saveWorking).toHaveBeenCalledTimes(1)
    expect(saveWorking.mock.calls[0]?.[0]).toBe(s.working)
  })

  it('uses only the reply for the latest edit (AC-12)', async () => {
    const pending = heldPlanner()
    useEditor.getState().edit((g) => bind(g, 'web', 'postgresql', 'db'))
    await vi.waitFor(() => expect(pending).toHaveLength(1))
    useEditor.getState().edit((g) => bind(g, 'web', 'postgresql', 'db2'))
    await vi.waitFor(() => expect(pending).toHaveLength(2))

    pending[1]!.reply(Response.json(planFor(pending[1]!.graph)))
    await vi.waitFor(() => expect(useEditor.getState().planState).toBe('current'))
    // the first reply arrives late and is dropped
    pending[0]!.reply(Response.json(planFor(pending[0]!.graph)))
    await settle()

    expect(useEditor.getState().working?.graph.modules.web?.uses).toEqual({ postgresql: 'db2' })
    expect(saveWorking).toHaveBeenCalledTimes(1)
  })

  it('keeps the edit and saves nothing when the plan call fails', async () => {
    const pending = heldPlanner()
    useEditor.getState().edit((g) => bind(g, 'web', 'postgresql', 'db'))
    await vi.waitFor(() => expect(pending).toHaveLength(1))
    pending[0]!.reply(new Response(null, { status: 503 }))
    await vi.waitFor(() => expect(useEditor.getState().planState).toBe('failed'))

    const s = useEditor.getState()
    expect(s.working?.graph.modules.web?.uses).toEqual({ postgresql: 'db' })
    expect(s.planError?.status).toBe(503)
    expect(saveWorking).not.toHaveBeenCalled()

    s.retry()
    await vi.waitFor(() => expect(pending).toHaveLength(2))
    pending[1]!.reply(Response.json(planFor(pending[1]!.graph)))
    await vi.waitFor(() => expect(useEditor.getState().planState).toBe('current'))
    expect(saveWorking).toHaveBeenCalledTimes(1)
  })

  it('says 429 means the planner is busy', async () => {
    const pending = heldPlanner()
    useEditor.getState().edit((g) => bind(g, 'web', 'postgresql', 'db'))
    await vi.waitFor(() => expect(pending).toHaveLength(1))
    pending[0]!.reply(new Response(null, { status: 429 }))
    await vi.waitFor(() => expect(useEditor.getState().planState).toBe('failed'))
    expect(useEditor.getState().planError?.message).toBe('The planner is busy, try again in a minute.')
  })

  it('notes when the working copy cannot be kept', async () => {
    saveWorking.mockImplementationOnce(() => Promise.reject(new Error('quota')))
    useEditor.getState().open({ fileName: 'g.yaml', graph, plan: planFor(graph) })
    await vi.waitFor(() => expect(useEditor.getState().storageFailed).toBe(true))
  })

  it('an edit that changes nothing plans nothing', () => {
    const pending = heldPlanner()
    useEditor.getState().edit((g) => g)
    expect(useEditor.getState().planState).toBe('current')
    expect(pending).toHaveLength(0)
  })
})
