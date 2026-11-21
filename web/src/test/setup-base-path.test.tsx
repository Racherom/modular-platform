import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import type { ComponentType } from 'react'
import { beforeAll, describe, expect, it } from 'vitest'
import { fetchHandler } from './setup'

// The setup page under the base path `/platform` (spec 0037 AC-3, verify row "the sign in link
// carries the base path"). `config.ts` reads the base path from a meta tag when it first loads, so
// this file sets the tag before it imports the page; the other setup tests run at the site root.

let SetupPage: ComponentType
const paths: string[] = []

beforeAll(async () => {
  const tag = document.createElement('meta')
  tag.name = 'mp-base-path'
  tag.content = '/platform'
  document.head.append(tag)
  ;({ SetupPage } = await import('../components/Setup'))
})

describe('SetupPage under /platform', () => {
  it('asks the api under the base path and the sign in link returns to it (AC-3)', async () => {
    fetchHandler.current = async (req) => {
      const path = new URL(req.url).pathname
      paths.push(path)
      if (path === '/platform/api/v1/setup') return Response.json({ state: 'pending', session: true, providerReady: true })
      return new Response('not found', { status: 404 })
    }
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
    render(
      <QueryClientProvider client={client}>
        <SetupPage />
      </QueryClientProvider>,
    )
    const link = await screen.findByRole('link', { name: 'Sign in to finish the setup' })
    expect(link.getAttribute('href')).toBe('/platform/api/v1/auth/login?return_to=%2Fplatform%2F')
    expect(paths).toEqual(['/platform/api/v1/setup'])
  })
})
