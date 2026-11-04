import { useMutation } from '@tanstack/react-query'
import { createRootRoute, Link, Outlet } from '@tanstack/react-router'
import { SearchX } from 'lucide-react'
import { logout } from '../api/platform'
import { AppShell } from '../components/AppShell'
import m from '../components/Managed.module.css'
import { SessionGate, useSession } from '../components/SessionGate'
import { Button } from '../components/ui/Button'
import { EmptyState } from '../components/ui/EmptyState'
import { mode } from '../lib/config'

export const Route = createRootRoute({
  component: Root,
  notFoundComponent: () => (
    <EmptyState icon={SearchX} level={1} title="Page not found">
      <p>
        There is nothing here. <Link to="/">Go to the editor</Link>.
      </p>
    </EmptyState>
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
    <div className={m.account}>
      <span>{session?.email}</span>
      <Button size="sm" variant="ghost" onPress={() => signOut.mutate()} isDisabled={signOut.isPending}>
        Sign out
      </Button>
    </div>
  )
}
