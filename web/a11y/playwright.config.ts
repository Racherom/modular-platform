import { fileURLToPath } from 'node:url'
import { defineConfig, devices } from '@playwright/test'

// The browser check (spec 0010): Playwright with the renderer mocked in each test
// (mock-renderer.ts), so it needs no Docker. Two servers, at a 1280×800 viewport:
// - dev (vite): axe in both themes on the editor states and /design, and the canvas flows
// - preview (vite build, then vite preview): the 50 module scale check and the search of dist/
//   for the showcase, since both are about the production build
const devPort = Number(process.env.A11Y_PORT ?? 5199)
const previewPort = Number(process.env.A11Y_PREVIEW_PORT ?? 5198)
const web = fileURLToPath(new URL('..', import.meta.url))
const env = { MP_BASE_PATH: '', MP_API_ROOT: '/renderer', MP_MODE: 'online' }
const desktop = { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } }

export default defineConfig({
  testDir: '.',
  outputDir: 'results',
  reporter: process.env.CI ? 'github' : 'list',
  forbidOnly: !!process.env.CI,
  retries: 0,
  fullyParallel: true,
  use: { trace: 'retain-on-failure' },
  projects: [
    { name: 'dev', testIgnore: 'preview/**', use: { ...desktop, baseURL: `http://127.0.0.1:${devPort}` } },
    { name: 'preview', testDir: 'preview', use: { ...desktop, baseURL: `http://127.0.0.1:${previewPort}` } },
  ],
  webServer: [
    {
      command: `pnpm exec vite --host 127.0.0.1 --port ${devPort} --strictPort`,
      url: `http://127.0.0.1:${devPort}/`,
      // Playwright starts the servers in the config's folder; Vite must run in web/
      cwd: web,
      reuseExistingServer: !process.env.CI,
      env,
    },
    {
      command: `pnpm exec vite build && pnpm exec vite preview --host 127.0.0.1 --port ${previewPort} --strictPort`,
      url: `http://127.0.0.1:${previewPort}/`,
      cwd: web,
      reuseExistingServer: !process.env.CI,
      timeout: 180_000,
      env,
    },
  ],
})
