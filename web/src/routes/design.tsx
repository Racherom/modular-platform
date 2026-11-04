import { createFileRoute, notFound } from '@tanstack/react-router'
import { lazy, Suspense } from 'react'

// The showcase exists in dev builds only (spec 0010, AC-5). In a production build DEV is the
// constant false, so this branch and its dynamic import are dropped and the chunk is never built.
const Showcase = import.meta.env.DEV ? lazy(() => import('../design/Showcase').then((m) => ({ default: m.Showcase }))) : null

export const Route = createFileRoute('/design')({
  beforeLoad: () => {
    if (!import.meta.env.DEV) throw notFound()
  },
  component: DesignRoute,
})

function DesignRoute() {
  if (!Showcase) return null
  return (
    <Suspense fallback={<p>Loading the showcase…</p>}>
      <Showcase />
    </Suspense>
  )
}
