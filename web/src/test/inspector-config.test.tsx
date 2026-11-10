import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import type { CheckResult, Graph, Module, Problem } from '../api/client'
import type { GraphEdit } from '../components/GraphView'
import { Inspector } from '../components/Inspector'
import { settableConfig } from '../graph/model'
import { useEditor } from '../lib/store'
import { testCatalog, testModule } from './fixtures'

// A module with a value of each kind: settable text, int, enum and bool values, and a secret, a
// {uid} default and a host_mounts source, which the graph may not set (spec 0025, AC-27, AC-29).
const tuned: Module = {
  ...testModule('tuned'),
  config: {
    label: { env: 'TUNED_LABEL', type: 'string', optional: true, description: 'A label.' },
    level: { env: 'TUNED_LEVEL', type: 'int', default: '5', description: 'A level.' },
    mode: { env: 'TUNED_MODE', type: 'enum', values: ['fast', 'slow'], default: 'fast', description: 'How fast.' },
    verbose: { env: 'TUNED_VERBOSE', type: 'bool', description: 'Logs more.' },
    token: { env: 'TUNED_TOKEN', type: 'secret', description: 'A secret.' },
    uid: { env: 'TUNED_UID', type: 'int', default: '{uid}', description: 'The user.' },
    root: { env: 'TUNED_ROOT', type: 'string', default: '/srv', description: 'The host folder.' },
  } as unknown as Module['config'],
  host_mounts: [{ host: { config: 'root' }, path: '/data', read_only: true, create: false }],
}
const catalog = testCatalog([tuned, testModule('postgres', { provides: { postgresql: { port: 5432 } } })])

const graphWith = (config?: Record<string, string>): Graph => ({
  apiVersion: 'modular-platform/v1',
  kind: 'Graph',
  name: 'g',
  modules: {
    t: config ? { module: 'tuned', config } : { module: 'tuned' },
    ext: { external: { provides: { db: { capability: 'postgresql', version: '16', fields: {} } } } },
  },
})

const resultOf = (graph: Graph, problems: Problem[] = []): CheckResult => ({ ok: true, graph, bindings: [], problems })

/** Renders the inspector on the graph; edits apply to `state.graph` and render again. */
function show(start: Graph, problems: Problem[] = []) {
  const state = { graph: start }
  const onEdit = (edit: GraphEdit) => {
    state.graph = edit(state.graph)
    view.rerender(<Inspector graph={state.graph} catalog={catalog} check={resultOf(state.graph, problems)} checkCurrent onEdit={onEdit} />)
  }
  const view = render(<Inspector graph={start} catalog={catalog} check={resultOf(start, problems)} checkCurrent onEdit={onEdit} />)
  return state
}

describe('settableConfig (AC-27)', () => {
  it('leaves out secrets, install time defaults and host_mounts sources', () => {
    expect(settableConfig(tuned).map((f) => f.name)).toEqual(['label', 'level', 'mode', 'verbose'])
    expect(settableConfig(undefined)).toEqual([])
  })
})

describe('the inspector Config section (spec 0025, AC-29)', () => {
  beforeEach(() => act(() => useEditor.getState().select({ kind: 'module', instance: 't' })))

  it('lists each settable value with its description, default and type', () => {
    show(graphWith())
    const section = screen.getByRole('region', { name: 'Config' })
    expect(within(section).getByRole('textbox', { name: 'label' })).toBeTruthy()
    expect(within(section).getByRole('textbox', { name: 'level' })).toBeTruthy()
    expect(within(section).queryByRole('textbox', { name: 'token' })).toBeNull()
    expect(within(section).queryByRole('textbox', { name: 'root' })).toBeNull()
    expect(within(section).getByText('A level. Default 5.')).toBeTruthy()
    expect(within(section).getByText('A label. No default, optional.')).toBeTruthy()
    expect(within(section).getByText('int')).toBeTruthy()
    expect(within(section).getByRole('button', { name: /mode/ })).toBeTruthy()
    expect(within(section).getByRole('button', { name: /verbose/ })).toBeTruthy()
  })

  it('writes a typed value on Enter and removes the key when cleared', () => {
    const state = show(graphWith())
    const level = screen.getByRole('textbox', { name: 'level' })
    fireEvent.change(level, { target: { value: '7' } })
    expect(state.graph.modules.t?.config).toBeUndefined() // nothing is written while typing
    fireEvent.keyDown(level, { key: 'Enter' })
    expect(state.graph.modules.t?.config).toEqual({ level: '7' })

    const again = screen.getByRole('textbox', { name: 'level' })
    fireEvent.change(again, { target: { value: '' } })
    fireEvent.blur(again)
    expect(state.graph.modules.t).toEqual({ module: 'tuned' })
  })

  it('shows a check problem under its field', () => {
    const problem: Problem = {
      severity: 'error',
      code: 'invalid_config',
      path: 'modules.t.config.level',
      message: 'Config value level of instance t: 12 is above the maximum 9.',
    }
    show(graphWith({ level: '12' }), [problem])
    const field = screen.getByRole('textbox', { name: 'level' })
    expect((field as HTMLInputElement).value).toBe('12')
    const row = field.closest('li')
    expect(row && within(row).getByText(problem.message)).toBeTruthy()
    expect(screen.getAllByText(problem.message)).toHaveLength(1) // not repeated at the top
  })

  it('lists a key the entry does not offer as an unknown value with a remove button', () => {
    const problem: Problem = {
      severity: 'error',
      code: 'unknown_config',
      path: 'modules.t.config.ghost',
      message: 'Instance t sets config value ghost, but module tuned declares no such value.',
    }
    const state = show(graphWith({ ghost: 'x', level: '3' }), [problem])
    const list = screen.getByRole('list', { name: 'Unknown values' })
    expect(within(list).getAllByText('ghost').length).toBeGreaterThan(0)
    expect(within(list).getByText(problem.message)).toBeTruthy()
    fireEvent.click(within(list).getByRole('button', { name: 'Remove ghost' }))
    expect(state.graph.modules.t?.config).toEqual({ level: '3' })
    expect(screen.queryByRole('list', { name: 'Unknown values' })).toBeNull()
  })

  it('shows no config section for an external instance', () => {
    show(graphWith())
    act(() => useEditor.getState().select({ kind: 'module', instance: 'ext' }))
    expect(screen.queryByRole('region', { name: 'Config' })).toBeNull()
    expect(screen.getByText('external service, outside the stack')).toBeTruthy()
  })
})
