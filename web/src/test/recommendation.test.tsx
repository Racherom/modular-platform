import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import type { CheckResult, Graph, Problem } from '../api/client'
import type { GraphEdit } from '../components/GraphView'
import { Inspector } from '../components/Inspector'
import { useEditor } from '../lib/store'
import { testCatalog, testModule } from './fixtures'

// Recommended bindings in the inspector (spec 0028): the checks recommend, you accept.
const catalog = testCatalog([
  testModule('postgres', { provides: { postgresql: { port: 5432 } } }),
  testModule('app', { uses: { postgresql: { optional: false }, cache: { capability: 'postgresql', optional: true } } }),
])

const graph: Graph = {
  apiVersion: 'modular-platform/v1',
  kind: 'Graph',
  name: 'g',
  modules: { app: { module: 'app', uses: { cache: null } }, db: { module: 'postgres' } },
}

const recommended: Problem = {
  severity: 'error',
  code: 'binding_recommended',
  path: 'modules.app.uses.postgresql',
  message: 'Instance app needs postgresql for postgresql; db provides it. Accept the recommendation or pick another provider.',
  candidates: ['db'],
}

const resultOf = (g: Graph, problems: Problem[]): CheckResult => ({ ok: false, graph: g, bindings: [], problems })

/** Renders the inspector; edits apply to `state.graph` and render again with no problems left. */
function show(start: Graph, problems: Problem[], current = true) {
  const state = { graph: start }
  const onEdit = (edit: GraphEdit) => {
    state.graph = edit(state.graph)
    view.rerender(<Inspector graph={state.graph} catalog={catalog} check={resultOf(state.graph, [])} checkCurrent onEdit={onEdit} />)
  }
  const view = render(
    <Inspector graph={start} catalog={catalog} check={resultOf(start, problems)} checkCurrent={current} onEdit={onEdit} />,
  )
  return state
}

describe('a recommended binding in the inspector (spec 0028, AC-9)', () => {
  beforeEach(() => act(() => useEditor.getState().select({ kind: 'module', instance: 'app' })))

  it('shows the recommendation, and Accept writes the binding and moves focus to the binding field', () => {
    const state = show(graph, [recommended])
    expect(screen.getByText('Recommended: db')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Accept db for postgresql' }))
    expect(state.graph.modules.app?.uses).toEqual({ cache: null, postgresql: 'db' })
    expect(screen.queryByRole('button', { name: /^Accept/ })).toBeNull()
    expect(document.activeElement?.textContent).toContain('db')
  })

  it('offers no Accept on a stale check result', () => {
    show(graph, [recommended], false)
    expect(screen.queryByRole('button', { name: /^Accept/ })).toBeNull()
  })

  it('offers Accept for an optional use with exactly one candidate (AC-10)', () => {
    const g: Graph = { ...graph, modules: { ...graph.modules, app: { module: 'app', uses: { postgresql: 'db' } } } }
    show(g, [{ ...recommended, severity: 'warning', code: 'optional_unbound', path: 'modules.app.uses.cache' }])
    expect(screen.getByRole('button', { name: 'Accept db for cache' })).toBeTruthy()
  })
})
