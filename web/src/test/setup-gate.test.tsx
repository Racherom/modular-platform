import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { SetupState } from '../api/platform'
import { SetupGate } from '../components/Setup'
import { fetchHandler } from './setup'

// While the setup is pending every page leads to /setup, and once it is done /setup leads home
// (spec 0036, editor).

function open(path: string, state: SetupState | 404) {
  fetchHandler.current = async () => (state === 404 ? new Response('not found', { status: 404 }) : Response.json(state))
  const root = createRootRoute({
    component: () => (
      <SetupGate>
        <p>the editor</p>
      </SetupGate>
    ),
  })
  const home = createRoute({ getParentRoute: () => root, path: '/', component: () => null })
  const setup = createRoute({ getParentRoute: () => root, path: '/setup', component: () => <p>the setup page</p> })
  const router = createRouter({ routeTree: root.addChildren([home, setup]), history: createMemoryHistory({ initialEntries: [path] }) })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return router
}

describe('SetupGate', () => {
  it('sends every page to /setup while the setup is pending', async () => {
    const router = open('/', { state: 'pending', session: true, providerReady: true })
    await waitFor(() => expect(router.state.location.pathname).toBe('/setup'))
  })

  it('sends /setup home once the setup is done', async () => {
    const router = open('/setup', { state: 'done' })
    await waitFor(() => expect(router.state.location.pathname).toBe('/'))
    expect(await screen.findByText('the editor')).toBeTruthy()
  })

  it('treats a 404 as done', async () => {
    open('/', 404)
    expect(await screen.findByText('the editor')).toBeTruthy()
  })
})
