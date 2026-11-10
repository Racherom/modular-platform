import { defineConfig, devices } from '@playwright/test'

// Drives the running online editor (scripts/smoke.sh starts it). The smoke stack uses Caddy's
// internal certificate authority, so certificate errors are ignored, and only when SMOKE_TLS is
// internal (smoke.sh sets it); against any other origin certificates are checked.
export default defineConfig({
  testDir: '.',
  outputDir: 'results',
  reporter: 'list',
  retries: 0,
  use: {
    baseURL: process.env.SMOKE_ORIGIN ?? 'https://platform.localhost',
    ignoreHTTPSErrors: process.env.SMOKE_TLS === 'internal',
    acceptDownloads: true,
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
