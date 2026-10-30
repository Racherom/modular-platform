import { createRootRoute, Link, Outlet } from '@tanstack/react-router'

export const Route = createRootRoute({
  component: Root,
  notFoundComponent: () => (
    <p>
      There is nothing here. <Link to="/">Go to the editor</Link>.
    </p>
  ),
})

function Root() {
  return (
    <div className="app">
      <header className="app__header">
        <span className="app__name">Modular Platform</span>
        <nav aria-label="Main">
          <Link to="/">Editor</Link>
          <Link to="/samples/$name" params={{ name: 'whoami-postgres' }}>
            Sample
          </Link>
        </nav>
      </header>
      <main>
        <Outlet />
      </main>
    </div>
  )
}
