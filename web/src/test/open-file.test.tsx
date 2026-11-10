import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import type { CheckResult, Graph } from '../api/client'
import { routeTree } from '../routeTree.gen'
import { testCatalog, testModule } from './fixtures'
import { fetchHandler } from './setup'

const catalog = testCatalog([testModule('whoami', { description: 'test', image: 'traefik/whoami:v1.12.0' })])

// two versions of one file: same file name, same graph name, only the module differs
const file = (module: string) =>
  new File([`apiVersion: modular-platform/v1\nkind: Graph\nname: g\nmodules:\n  web:\n    module: ${module}\n`], 'graph.yaml', {
    type: 'application/yaml',
  })

function renderEditor() {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ['/'] }) })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

describe('open file', () => {
  beforeEach(() => {
    fetchHandler.current = async (req) => {
      const url = new URL(req.url)
      if (url.pathname === '/renderer/v1/catalog') return Response.json(catalog)
      if (url.pathname === '/renderer/v1/check') {
        const graph = (await req.json()) as Graph
        const known = graph.modules.web?.module === 'whoami'
        const check: CheckResult = {
          ok: known,
          graph,
          bindings: [],
          problems: known
            ? []
            : [{ severity: 'error', code: 'unknown_module', path: 'modules.web.module', message: 'No catalog entry is named nope.' }],
        }
        return Response.json(check)
      }
      return new Response('not found', { status: 404 })
    }
  })

  it('shows the new check when a fixed file with the same names is opened again', async () => {
    const { container } = renderEditor()
    await screen.findByRole('button', { name: 'Open file' })
    const input = () => container.querySelector<HTMLInputElement>('input[type=file]')!

    fireEvent.change(input(), { target: { files: [file('nope')] } })
    expect(await screen.findByText('No catalog entry is named nope.')).toBeTruthy()

    fireEvent.change(input(), { target: { files: [file('whoami')] } })
    expect(await screen.findByText('No problems.')).toBeTruthy()
    expect(screen.queryByText('No catalog entry is named nope.')).toBeNull()
  })
})
