import { expect, test } from '@playwright/test'
import { drag, expectNoAxeViolations, handle, openGraph, useTheme } from './helpers'
import { mockPlanner } from './mock-planner'

// AC-3: zero axe violations in light and in dark, at 1280×800. More editor states join as the
// build plan adds them (selection, problems, /design).
for (const theme of ['light', 'dark'] as const) {
  test.describe(`${theme} theme`, () => {
    test.beforeEach(async ({ page }) => {
      await useTheme(page, theme)
      await mockPlanner(page)
    })

    test('the sample graph', async ({ page }) => {
      await page.goto('/samples/whoami-postgres')
      await expect(page.getByTestId('node-whoami')).toBeVisible()
      await expect(page.locator('.react-flow__edge')).toHaveCount(1)
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
      await expectNoAxeViolations(page)
    })

    test('the editor with a graph open', async ({ page }) => {
      await openGraph(page)
      await expectNoAxeViolations(page)
    })

    test('the editor during a connect drag', async ({ page }) => {
      await openGraph(page)
      await drag(page, handle(page, 'whoami', 'uses:postgresql'), handle(page, 'postgres', 'provides:postgresql'), async () => {
        await expect(handle(page, 'postgres', 'provides:postgresql')).toHaveAttribute('data-drop', 'valid')
        await expectNoAxeViolations(page)
      })
    })

    test('the empty editor', async ({ page }) => {
      await page.goto('/')
      await expect(page.getByRole('heading', { name: 'No graph open' })).toBeVisible()
      await expectNoAxeViolations(page)
    })
  })
}
