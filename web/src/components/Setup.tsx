import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Outlet, useLocation, useNavigate } from '@tanstack/react-router'
import { type FormEvent, type ReactNode, useEffect, useRef, useState } from 'react'
import {
  ApiError,
  createIdentityLink,
  enrollSetup,
  fetchSetup,
  loginURL,
  redeemSetupCode,
  type SetupAsk,
  type SetupState,
} from '../api/platform'
import { basePath } from '../lib/config'
import { AppShell } from './AppShell'
import m from './Managed.module.css'
import { reasons } from './SessionGate'
import s from './Setup.module.css'
import { Button } from './ui/Button'
import { Failure } from './ui/Failure'
import { TextField } from './ui/TextField'

const setupKey = ['setup']

/** The setup state; while the identity provider is not ready it asks again every 2 seconds. */
export function useSetup() {
  return useQuery({
    queryKey: setupKey,
    queryFn: fetchSetup,
    refetchInterval: (query) => {
      const d = query.state.data
      return d?.state === 'pending' && d.session && !d.providerReady ? 2000 : false
    },
  })
}

/**
 * Managed mode: while no admin is bound every page leads to the setup page, and once the setup is
 * done the setup page leads home (spec 0036).
 */
export function SetupGate({ children }: { children: ReactNode }) {
  const setup = useSetup()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const onSetup = pathname === '/setup' || pathname === '/setup/'
  const pending = setup.data?.state === 'pending'
  useEffect(() => {
    if (!setup.data) return
    if (pending && !onSetup) void navigate({ to: '/setup', replace: true })
    if (!pending && onSetup) void navigate({ to: '/', replace: true })
  }, [setup.data, pending, onSetup, navigate])
  if (setup.isError) return <Failure error={setup.error} onRetry={() => void setup.refetch()} />
  if (!setup.data || pending !== onSetup) return <p className={m.loading}>Checking the setup…</p>
  if (pending) {
    return (
      <AppShell nav={null}>
        <Outlet />
      </AppShell>
    )
  }
  return children
}

/** The page at `<base>/setup`. */
export function SetupPage() {
  const setup = useSetup()
  if (setup.isError) return <Failure error={setup.error} onRetry={() => void setup.refetch()} />
  if (!setup.data) return <p className={m.loading}>Checking the setup…</p>
  if (setup.data.state === 'done') return null
  return <Pending state={setup.data} />
}

function Pending({ state }: { state: SetupState }) {
  const reason = new URLSearchParams(window.location.search).get('signed_out') ?? ''
  return (
    <div className={`${m.screen} ${m.narrow}`}>
      <h1>Set up your platform</h1>
      {reasons[reason] && (
        <p role="alert" className={m.banner} data-tone="danger">
          {reasons[reason]}
        </p>
      )}
      <Step state={state} />
    </div>
  )
}

/** One state of the page, picked in the order of spec 0037. */
function Step({ state }: { state: SetupState }) {
  if (state.reason === 'no_code') {
    return (
      <section aria-labelledby="no-code" className={s.form}>
        <h2 id="no-code">There is no setup code</h2>
        <p>
          In the install folder, delete the files in <code>secrets/api/setup/</code> (the files, never the folder), then run{' '}
          <code>sh setup.sh</code>. It makes a new code and prints it.
        </p>
      </section>
    )
  }
  if (!state.session) return <CodeForm />
  if (!state.providerReady) {
    return (
      <section aria-labelledby="waiting" className={s.form}>
        <h2 id="waiting">Waiting for the identity provider</h2>
        <p role="status" className={m.loading}>
          This page checks again every 2 seconds.
        </p>
      </section>
    )
  }
  if (state.enroll) return <EnrollForm asks={state.enroll.asks} />
  if (state.firstAdmin) return <CreateAdmin />
  return <SignIn />
}

/** Recovery: no valid session, so the code is asked. The code is cleared after each try. */
function CodeForm() {
  const queryClient = useQueryClient()
  const [code, setCode] = useState('')
  const redeem = useMutation({
    mutationFn: (value: string) => redeemSetupCode(value),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: setupKey }),
    onSettled: () => setCode(''),
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (code !== '') redeem.mutate(code)
  }
  return (
    <form onSubmit={submit} className={s.form} aria-labelledby="code-heading">
      <h2 id="code-heading">Enter the setup code</h2>
      <TextField
        label="Setup code"
        type="password"
        autoComplete="off"
        value={code}
        onChange={setCode}
        description="The path of the code file is in the install README."
        isInvalid={redeem.isError}
        errorMessage={redeem.error?.message}
      />
      <div className={m.actions}>
        <Button type="submit" variant="primary" isDisabled={redeem.isPending || code === ''}>
          Continue
        </Button>
      </div>
    </form>
  )
}

/** Why an answer breaks the ask's own rules, checked before the call; the server still decides. */
function localError(ask: SetupAsk, value: string): string | undefined {
  if (ask.minLength !== undefined && value.length < ask.minLength) return `Use at least ${ask.minLength} characters.`
  if (ask.pattern) {
    try {
      if (!new RegExp(ask.pattern).test(value)) return 'This answer is not in the form the identity module accepts.'
    } catch {
      // a rule the browser cannot read is left to the server
    }
  }
  return undefined
}

/** The identity module's enroll questions. Answers live only here and are cleared after each request. */
function EnrollForm({ asks }: { asks: SetupAsk[] }) {
  const formRef = useRef<HTMLFormElement>(null)
  const [values, setValues] = useState<Record<string, string>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [enrolled, setEnrolled] = useState(false)
  const enroll = useMutation({
    mutationFn: (answers: Record<string, string>) => enrollSetup(answers),
    onSuccess: () => {
      setErrors({})
      setEnrolled(true)
    },
    onError: (error) => {
      if (!(error instanceof ApiError)) return
      const next: Record<string, string> = {}
      for (const p of error.problems) {
        const name = p.path?.startsWith('answers.') ? p.path.slice('answers.'.length) : undefined
        if (name) next[name] = p.message
      }
      setErrors(next)
    },
    onSettled: () => setValues({}),
  })
  // focus moves to the first field with a problem
  useEffect(() => {
    if (Object.keys(errors).length > 0) formRef.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
  }, [errors])
  if (enrolled) return <SignIn />
  const submit = (e: FormEvent) => {
    e.preventDefault()
    const found: Record<string, string> = {}
    for (const ask of asks) {
      const problem = localError(ask, values[ask.name] ?? '')
      if (problem) found[ask.name] = problem
    }
    setErrors(found)
    if (Object.keys(found).length === 0) enroll.mutate({ ...values })
  }
  return (
    <form ref={formRef} onSubmit={submit} className={s.form} aria-labelledby="enroll-heading" noValidate>
      <h2 id="enroll-heading">Create your admin account</h2>
      {asks.map((ask) => (
        <TextField
          key={ask.name}
          label={ask.description}
          type={ask.secret ? 'password' : 'text'}
          autoComplete={ask.secret ? 'new-password' : 'off'}
          value={values[ask.name] ?? ''}
          onChange={(v) => setValues((prev) => ({ ...prev, [ask.name]: v }))}
          description={ask.minLength === undefined ? undefined : `At least ${ask.minLength} characters.`}
          isInvalid={errors[ask.name] !== undefined}
          errorMessage={errors[ask.name]}
        />
      ))}
      {enroll.isError && Object.keys(errors).length === 0 && (
        <p role="alert" className={m.banner} data-tone="danger">
          {enroll.error.message}
        </p>
      )}
      <div className={m.actions}>
        <Button type="submit" variant="primary" isDisabled={enroll.isPending}>
          Register
        </Button>
      </div>
    </form>
  )
}

/**
 * The identity module has its own first admin page (spec 0037). The button asks `api` for a link
 * with a one use token and goes there; when you return, the page is fresh, so both steps stay on it.
 */
function CreateAdmin() {
  const link = useMutation({
    mutationFn: createIdentityLink,
    onSuccess: (url) => window.location.assign(url),
  })
  return (
    <ol className={s.steps}>
      <li className={s.step}>
        <h2>1. Create your admin account</h2>
        <p>The identity provider asks you to register a passkey. The platform never sees it.</p>
        <Button variant="primary" onPress={() => link.mutate()} isDisabled={link.isPending}>
          Create your admin account
        </Button>
        {link.isError && <Failure error={link.error} />}
      </li>
      <li className={s.step}>
        <h2>2. Come back and sign in</h2>
        <p>When your account exists, sign in once to finish the setup.</p>
        <SignInLink />
      </li>
    </ol>
  )
}

/** The last step: the first sign in with a valid setup session binds you as the admin. */
function SignIn() {
  return (
    <section aria-labelledby="sign-in" className={s.form}>
      <h2 id="sign-in">Sign in to finish the setup</h2>
      <p>Your first sign in makes you the admin of this platform.</p>
      <SignInLink />
    </section>
  )
}

function SignInLink() {
  return (
    <a className={m.linkButton} href={loginURL(`${basePath}/`)}>
      Sign in to finish the setup
    </a>
  )
}
