import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRouter, RouterProvider } from '@tanstack/react-router'
import '@xyflow/react/dist/style.css'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './app.css'
import { basePath } from './lib/config'
import { routeTree } from './routeTree.gen'

const router = createRouter({ routeTree, basepath: basePath || '/' })

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

// one call per open and per download keeps the editor far below the public rate limit
// managed mode: any call that finds the session gone sends the editor back to the signed out view
const queryClient: QueryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (error, query) => {
      const status = (error as { status?: number }).status
      if (status === 401 && query.queryKey[0] !== 'session') void queryClient.invalidateQueries({ queryKey: ['session'] })
    },
  }),
  defaultOptions: {
    queries: { retry: false, staleTime: Infinity, refetchOnWindowFocus: false, refetchOnReconnect: false },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
