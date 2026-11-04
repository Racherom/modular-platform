import { fileURLToPath } from 'node:url'
import { defineConfig, devices } from '@playwright/test'

// The browser check (spec 0010): Playwright against the Vite dev server, with the planner mocked
// in each test (mock-planner.ts), so it needs no Docker. A preview server joins for the scale
// check and the bundle search later in the build plan.
const port = Number(process.env.A11Y_PORT ?? 5199)

export default defineConfig({
  testDir: '.',
  outputDir: 'results',
  reporter: process.env.CI ? 'github' : 'list',
  forbidOnly: !!process.env.CI,
  retries: 0,
  fullyParallel: true,
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    viewport: { width: 1280, height: 800 },
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } } }],
  webServer: {
    command: `pnpm exec vite --host 127.0.0.1 --port ${port} --strictPort`,
    url: `http://127.0.0.1:${port}/`,
    // Playwright starts the server in the config's folder; Vite must run in web/
    cwd: fileURLToPath(new URL('..', import.meta.url)),
    reuseExistingServer: !process.env.CI,
    env: { MP_BASE_PATH: '', MP_API_ROOT: '/planner', MP_MODE: 'online' },
  },
})
