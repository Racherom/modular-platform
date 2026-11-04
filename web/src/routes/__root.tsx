import { useMutation } from '@tanstack/react-query'
import { createRootRoute, Link, Outlet } from '@tanstack/react-router'
import { logout } from '../api/platform'
import { AppShell } from '../components/AppShell'
import { SessionGate, useSession } from '../components/SessionGate'
import { mode } from '../lib/config'

export const Route = createRootRoute({
  component: Root,
  notFoundComponent: () => (
    <p>
      There is nothing here. <Link to="/">Go to the editor</Link>.
    </p>
  ),
})

function Root() {
  if (mode === 'managed') {
    return (
      <SessionGate>
        <Shell />
      </SessionGate>
    )
  }
  return <Shell />
}

function Shell() {
  return (
    <AppShell
      nav={
        <nav aria-label="Main">
          <Link to="/">Editor</Link>
          <Link to="/samples/$name" params={{ name: 'whoami-postgres' }}>
            Sample
          </Link>
          {mode === 'managed' && <Link to="/deployments">Deployments</Link>}
        </nav>
      }
      account={mode === 'managed' ? <Account /> : undefined}
    >
      <Outlet />
    </AppShell>
  )
}

/** The signed in admin and Sign out. */
function Account() {
  const session = useSession()
  const signOut = useMutation({ mutationFn: logout, onSuccess: (next) => window.location.assign(next) })
  return (
    <div className="app__account">
      <span>{session?.email}</span>
      <button type="button" onClick={() => signOut.mutate()} disabled={signOut.isPending}>
        Sign out
      </button>
    </div>
  )
}
