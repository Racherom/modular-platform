import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Graph } from '../api/client'
import type { Review } from '../api/platform'
import { ReviewDialog } from '../components/ReviewDialog'
import { fetchHandler } from './setup'

// The review dialog's image thread (spec 0031): apply sends back the review's digest, which covers
// the frozen lock, and the same update list the review used (AC-2, AC-6, AC-7).

const navigate = vi.fn()
vi.mock('@tanstack/react-router', async (actual) => ({
  ...(await actual<typeof import('@tanstack/react-router')>()),
  useNavigate: () => navigate,
}))

// jsdom may lack the modal dialog API; the dialog only needs to open
HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
  this.open = true
}
HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
  this.open = false
}

const d = (c: string) => `sha256:${c.repeat(64)}`
const whoami = 'traefik/whoami:v1.12.0'
const graph: Graph = { apiVersion: 'modular-platform/v1', kind: 'Graph', name: 'demo', modules: { web: { module: 'whoami' } } }

type ReviewBody = { checkNewer: boolean; update: string[] }
type ApplyBody = { digest: string; update: string[] }

/** The review the fake api answers: frozen at b, b newer with checkNewer, taken with update. */
function reviewFor(body: ReviewBody): Review {
  const taken = body.update.includes(whoami)
  return {
    check: { ok: true, graph, bindings: [], problems: [], digest: 'sha256:check' },
    digest: taken ? 'review-updated' : body.checkNewer ? 'review-checked' : 'review-frozen',
    modules: [{ instance: 'web', module: 'whoami', image: `${whoami}@${taken ? d('c') : d('b')}` }],
    images: [
      {
        reference: whoami,
        usedBy: [{ kind: 'module', instance: 'web' }],
        digest: taken ? d('c') : d('b'),
        change: taken ? 'updated' : 'frozen',
        ...(taken ? { previousDigest: d('b') } : {}),
        ...(body.checkNewer && !taken ? { available: d('c') } : {}),
        present: true,
      },
    ],
    bindings: [],
    publishedPorts: [],
    placed: [],
  }
}

let reviews: ReviewBody[]
let applies: ApplyBody[]
let applyAnswer: () => Response

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <ReviewDialog graph={graph} onClose={() => undefined} />
    </QueryClientProvider>,
  )
}

describe('ReviewDialog images (spec 0031)', () => {
  beforeEach(() => {
    reviews = []
    applies = []
    navigate.mockClear()
    applyAnswer = () =>
      Response.json({ id: 'op-1', action: 'apply', status: 'running', startedAt: '2026-11-15T00:00:00Z' }, { status: 202 })
    fetchHandler.current = async (req) => {
      const url = new URL(req.url)
      if (req.method === 'POST' && url.pathname === '/api/v1/stacks/demo/review') {
        const body = (await req.json()) as ReviewBody
        reviews.push(body)
        return Response.json(reviewFor(body))
      }
      if (req.method === 'POST' && url.pathname === '/api/v1/stacks/demo/apply') {
        applies.push((await req.json()) as ApplyBody)
        return applyAnswer()
      }
      return new Response('not found', { status: 404 })
    }
  })

  it('reviews with no checkNewer and no update, and Deploy sends the review digest, not the check digest (AC-2, AC-7)', async () => {
    show()
    await screen.findByText('frozen')
    expect(reviews).toEqual([{ graph, checkNewer: false, update: [] }])
    fireEvent.click(screen.getByRole('button', { name: 'Deploy' }))
    await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: '/stacks/$name', params: { name: 'demo' } }))
    expect(applies).toHaveLength(1)
    expect(applies[0]).toMatchObject({ digest: 'review-frozen', update: [] })
  })

  it('Check for newer images reviews again with checkNewer and offers the newer version (AC-6)', async () => {
    show()
    await screen.findByText('frozen')
    fireEvent.click(screen.getByRole('button', { name: 'Check for newer images' }))
    expect(await screen.findByText('newer available')).toBeTruthy()
    expect(reviews.at(-1)).toMatchObject({ checkNewer: true, update: [] })
    expect(screen.getByRole('checkbox', { name: `Take the newer version of ${whoami}` })).toBeTruthy()
  })

  it('a ticked newer version reviews with update, and Deploy sends that update and its digest (AC-6, AC-7)', async () => {
    show()
    await screen.findByText('frozen')
    fireEvent.click(screen.getByRole('button', { name: 'Check for newer images' }))
    fireEvent.click(await screen.findByRole('checkbox', { name: `Take the newer version of ${whoami}` }))
    expect(await screen.findByText(/updating bbbbbbbbbbbb → cccccccccccc/)).toBeTruthy()
    expect(reviews.at(-1)).toMatchObject({ update: [whoami] })
    fireEvent.click(screen.getByRole('button', { name: 'Deploy' }))
    await waitFor(() => expect(applies).toHaveLength(1))
    expect(applies[0]).toMatchObject({ digest: 'review-updated', update: [whoami] })
  })

  it('graph_changed on Deploy says an image version may have moved and reviews again (AC-7)', async () => {
    applyAnswer = () =>
      Response.json({ problems: [{ severity: 'error', code: 'graph_changed', path: 'digest', message: 'Review again.' }] }, { status: 409 })
    show()
    await screen.findByText('frozen')
    fireEvent.click(screen.getByRole('button', { name: 'Deploy' }))
    expect(await screen.findByRole('status')).toHaveProperty(
      'textContent',
      'The graph or an image version changed since your review. Review it again below.',
    )
    await waitFor(() => expect(reviews).toHaveLength(2))
    expect(navigate).not.toHaveBeenCalled()
  })
})
