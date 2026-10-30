import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
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
const queryClient = new QueryClient({
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
