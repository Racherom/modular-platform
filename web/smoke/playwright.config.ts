import { defineConfig, devices } from '@playwright/test'

// Drives the running online tool (scripts/smoke.sh starts it). CI only: the local stack uses
// Caddy's internal certificate authority, so certificate errors are ignored here.
export default defineConfig({
  testDir: '.',
  outputDir: 'results',
  reporter: 'list',
  retries: 0,
  use: {
    baseURL: process.env.SMOKE_ORIGIN ?? 'https://platform.localhost',
    ignoreHTTPSErrors: true,
    acceptDownloads: true,
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
