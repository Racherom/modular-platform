import { useQuery } from '@tanstack/react-query'
import { createContext, type ReactNode, useContext, useEffect } from 'react'
import { fetchSession, isSignedOut, loginURL, type Session } from '../api/platform'
import { basePath, signedOutView } from '../lib/config'
import { Failure } from './Failure'

const SessionContext = createContext<Session | undefined>(undefined)

/** The signed in admin's session; only inside a SessionGate. */
export function useSession(): Session | undefined {
  return useContext(SessionContext)
}

/** Managed mode: shows children to the signed in admin, the signed out view to everyone else. */
export function SessionGate({ children }: { children: ReactNode }) {
  const session = useQuery({ queryKey: ['session'], queryFn: fetchSession })
  if (session.isError && isSignedOut(session.error)) return <SignedOut />
  if (session.isError) return <Failure error={session.error} onRetry={() => void session.refetch()} />
  if (!session.data) return <p className="screen">Checking your session…</p>
  return <SessionContext.Provider value={session.data}>{children}</SessionContext.Provider>
}

const reasons: Record<string, string> = {
  not_admin: 'This account is not the platform admin. Sign in with the admin account.',
  email_unverified: 'The identity provider has not verified the admin email of this account yet.',
  state_mismatch: 'The sign in took too long or came from another tab. Try again.',
  provider_error: 'The identity provider could not finish the sign in. Try again.',
}

/** What a signed out visitor sees: the index with Sign in, or straight to the provider. */
export function SignedOut() {
  const reason = new URLSearchParams(window.location.search).get('signed_out') ?? ''
  const here = window.location.pathname.startsWith(`${basePath}/`) ? window.location.pathname : `${basePath}/`
  const straight = signedOutView === 'sign-in' && reason === ''
  useEffect(() => {
    if (straight) window.location.assign(loginURL(here))
  }, [straight, here])
  if (straight) return <p className="screen">Taking you to sign in…</p>
  return (
    <div className="screen signed-out">
      <h1>Modular Platform</h1>
      <p>Plan your stack and deploy it to this host. Sign in as the admin to continue.</p>
      {reasons[reason] && (
        <p role="alert" className="banner banner--error">
          {reasons[reason]}
        </p>
      )}
      <p>
        <a className="button" href={loginURL(here)}>
          Sign in
        </a>
      </p>
    </div>
  )
}
