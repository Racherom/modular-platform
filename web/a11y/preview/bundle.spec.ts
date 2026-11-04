import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test } from '@playwright/test'
import { mockPlanner } from '../mock-planner'

// AC-5: the showcase exists in dev builds only. The marker is the data-showcase value on the
// showcase's root (src/design/Showcase.tsx); the dev check in axe.spec.ts proves it is there.
const MARKER = 'mp-design-showcase'
const dist = fileURLToPath(new URL('../../dist', import.meta.url))

function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? files(join(dir, e.name)) : [join(dir, e.name)]))
}

test('the production bundle holds none of the showcase (AC-5)', () => {
  const all = files(dist)
  expect(all.some((f) => f.endsWith('.js'))).toBe(true)
  expect(all.filter((f) => readFileSync(f, 'utf8').includes(MARKER))).toEqual([])
})

test('/design shows the not found screen in a production build (AC-5)', async ({ page }) => {
  await mockPlanner(page)
  await page.goto('/design')
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible()
  await expect(page.locator(`[data-showcase="${MARKER}"]`)).toHaveCount(0)
})
