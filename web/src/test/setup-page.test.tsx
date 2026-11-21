import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { SetupState } from '../api/platform'
import { SetupPage } from '../components/Setup'
import { fetchHandler } from './setup'

// The setup page (specs 0036 and 0037): each state of the page, in the order the spec fixes, and
// that no answer reaches storage (AC-31, AC-32, AC-39 of 0036; AC-3 of 0037).

type Call = { method: string; path: string; body?: unknown }

let state: SetupState
let calls: Call[]
let enrollAnswer: () => Response
const assign = vi.fn()

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  return render(
    <QueryClientProvider client={client}>
      <SetupPage />
    </QueryClientProvider>,
  )
}

const asks = [
  { name: 'admin-user', description: 'User name', secret: false, minLength: 3 },
  { name: 'admin-password', description: 'Password', secret: true, minLength: 8 },
]

beforeEach(() => {
  calls = []
  assign.mockClear()
  localStorage.clear()
  sessionStorage.clear()
  Object.defineProperty(window, 'location', { configurable: true, value: { ...window.location, assign, search: '' } })
  state = { state: 'pending', session: true, providerReady: true }
  enrollAnswer = () => new Response(null, { status: 204 })
  fetchHandler.current = async (req) => {
    const path = new URL(req.url).pathname
    const call: Call = { method: req.method, path }
    if (req.method === 'POST') call.body = await req.json().catch(() => undefined)
    calls.push(call)
    if (path === '/api/v1/setup' && req.method === 'GET') return Response.json(state)
    if (path === '/api/v1/setup/code') return new Response(null, { status: 204 })
    if (path === '/api/v1/setup/enroll') return enrollAnswer()
    if (path === '/api/v1/setup/identity-link')
      return Response.json({ url: 'https://auth.platform.test/api/v1/setup/identity-enter?t=abc' })
    return new Response('not found', { status: 404 })
  }
})

describe('SetupPage', () => {
  it('asks for the code with no session and clears it after the try', async () => {
    state = { state: 'pending', session: false }
    show()
    const field = await screen.findByLabelText('Setup code')
    fireEvent.change(field, { target: { value: 'the-code' } })
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await waitFor(() => expect(calls).toContainEqual({ method: 'POST', path: '/api/v1/setup/code', body: { code: 'the-code' } }))
    await waitFor(() => expect((screen.getByLabelText('Setup code') as HTMLInputElement).value).toBe(''))
  })

  it('says what to do when there is no code file', async () => {
    state = { state: 'pending', reason: 'no_code' }
    show()
    expect(await screen.findByText('There is no setup code')).toBeTruthy()
    expect(screen.queryByLabelText('Setup code')).toBeNull()
  })

  it('waits while the identity provider is not ready', async () => {
    state = { state: 'pending', session: true, providerReady: false }
    show()
    expect(await screen.findByText('Waiting for the identity provider')).toBeTruthy()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('shows the create account step and goes to the link the api gives (spec 0037 AC-3)', async () => {
    state = { state: 'pending', session: true, providerReady: true, firstAdmin: true }
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Create your admin account' }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://auth.platform.test/api/v1/setup/identity-enter?t=abc'))
    expect(calls).toContainEqual({ method: 'POST', path: '/api/v1/setup/identity-link' })
    // the sign in is on the same page, for the visit after the account exists
    expect(screen.getByRole('link', { name: 'Sign in to finish the setup' }).getAttribute('href')).toBe('/api/v1/auth/login?return_to=%2F')
  })

  it('shows only the sign in when the module has no enroll and no first admin page', async () => {
    show()
    expect(await screen.findByRole('heading', { name: 'Sign in to finish the setup' })).toBeTruthy()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('the enroll form wins when a module declares both blocks', async () => {
    state = { state: 'pending', session: true, providerReady: true, enroll: { asks }, firstAdmin: true }
    show()
    expect(await screen.findByLabelText('User name')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Create your admin account' })).toBeNull()
    expect((screen.getByLabelText('Password') as HTMLInputElement).type).toBe('password')
  })

  it('checks the rules before the call, then enrolls, clears the answers and shows the sign in (AC-32, AC-39)', async () => {
    state = { state: 'pending', session: true, providerReady: true, enroll: { asks } }
    show()
    fireEvent.change(await screen.findByLabelText('User name'), { target: { value: 'ab' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'long-enough-1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Register' }))
    expect(await screen.findByText('Use at least 3 characters.')).toBeTruthy()
    expect(calls.some((c) => c.path === '/api/v1/setup/enroll')).toBe(false)

    fireEvent.change(screen.getByLabelText('User name'), { target: { value: 'admin' } })
    fireEvent.click(screen.getByRole('button', { name: 'Register' }))
    expect(await screen.findByRole('heading', { name: 'Sign in to finish the setup' })).toBeTruthy()
    expect(calls).toContainEqual({
      method: 'POST',
      path: '/api/v1/setup/enroll',
      body: { answers: { 'admin-user': 'admin', 'admin-password': 'long-enough-1' } },
    })
    expect(localStorage.length + sessionStorage.length).toBe(0)
  })

  it('marks the field a refusal names and clears the answers (AC-32)', async () => {
    state = { state: 'pending', session: true, providerReady: true, enroll: { asks } }
    enrollAnswer = () =>
      Response.json(
        {
          problems: [
            { severity: 'error', code: 'invalid_answer', path: 'answers.admin-password', message: 'The answer breaks the rule: strength.' },
          ],
        },
        { status: 422 },
      )
    show()
    fireEvent.change(await screen.findByLabelText('User name'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'weak-password' } })
    fireEvent.click(screen.getByRole('button', { name: 'Register' }))
    expect(await screen.findByText('The answer breaks the rule: strength.')).toBeTruthy()
    expect(screen.getByLabelText('Password').getAttribute('aria-invalid')).toBe('true')
    expect((screen.getByLabelText('Password') as HTMLInputElement).value).toBe('')
    expect((screen.getByLabelText('User name') as HTMLInputElement).value).toBe('')
  })

  it('shows the failure message of a failed registration', async () => {
    state = { state: 'pending', session: true, providerReady: true, enroll: { asks } }
    enrollAnswer = () =>
      Response.json(
        { problems: [{ severity: 'error', code: 'enroll_failed', message: 'Registration failed; check your answers and try again.' }] },
        { status: 502 },
      )
    show()
    fireEvent.change(await screen.findByLabelText('User name'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'long-enough-1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Register' }))
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Registration failed; check your answers and try again.')
  })

  it('marks the code field when the code is refused and clears it (0036 AC-36)', async () => {
    state = { state: 'pending', session: false }
    const answer = fetchHandler.current
    fetchHandler.current = async (req) =>
      new URL(req.url).pathname === '/api/v1/setup/code'
        ? Response.json(
            { problems: [{ severity: 'error', code: 'code_refused', message: 'The setup code is not accepted.' }] },
            { status: 401 },
          )
        : answer(req)
    show()
    fireEvent.change(await screen.findByLabelText('Setup code'), { target: { value: 'a-wrong-code' } })
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByText('The setup code is not accepted.')).toBeTruthy()
    const field = screen.getByLabelText('Setup code') as HTMLInputElement
    expect(field.getAttribute('aria-invalid')).toBe('true')
    expect(field.value).toBe('')
    expect(localStorage.length + sessionStorage.length).toBe(0)
  })

  it('shows the failure with Retry when the setup state cannot be read, and never guesses the next step (AC-3)', async () => {
    let up = false
    const answer = fetchHandler.current
    fetchHandler.current = async (req) =>
      !up && new URL(req.url).pathname === '/api/v1/setup'
        ? Response.json({ problems: [{ severity: 'error', code: 'unavailable', message: 'The platform is not ready.' }] }, { status: 503 })
        : answer(req)
    state = { state: 'pending', session: true, providerReady: true, firstAdmin: true }
    show()
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'The platform is not ready.Retry')
    expect(screen.queryByRole('button', { name: 'Create your admin account' })).toBeNull()
    expect(screen.queryByLabelText('Setup code')).toBeNull()

    up = true
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByRole('button', { name: 'Create your admin account' })).toBeTruthy()
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('says why the identity link was refused and stays on the page (AC-3, AC-4)', async () => {
    state = { state: 'pending', session: true, providerReady: true, firstAdmin: true }
    const answer = fetchHandler.current
    fetchHandler.current = async (req) =>
      new URL(req.url).pathname === '/api/v1/setup/identity-link'
        ? Response.json({ problems: [{ severity: 'error', code: 'setup_done', message: 'The setup is done.' }] }, { status: 409 })
        : answer(req)
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Create your admin account' }))
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'The setup is done.')
    expect(assign).not.toHaveBeenCalled()
    // the button stays usable for another try, and the sign in is still there
    await waitFor(() => expect(screen.getByRole('button', { name: 'Create your admin account' }).hasAttribute('disabled')).toBe(false))
    expect(screen.getByRole('link', { name: 'Sign in to finish the setup' })).toBeTruthy()
  })

  it('treats a 404 from an older api as done and shows nothing', async () => {
    fetchHandler.current = async () => new Response('not found', { status: 404 })
    const { container } = show()
    await waitFor(() => expect(container.textContent).toBe(''))
  })
})
