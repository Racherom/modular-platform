import { expect, test } from '@playwright/test'
import { drag, expectNoAxeViolations, handle, openGraph, UNBOUND, useTheme } from './helpers'
import { mockPlanner } from './mock-planner'

const BOUND = UNBOUND.replace('postgresql: null', 'postgresql: postgres')
const PROBLEMS = `${UNBOUND}  app:
    module: test-app
    uses:
      postgresql: null
`

// AC-3: zero axe violations in light and in dark, at 1280×800. /design joins with build plan task 9.
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

    test('the editor with a module selected', async ({ page }) => {
      await openGraph(page, BOUND)
      await page.locator('.react-flow__node[data-id="whoami"]').click()
      await expect(page.getByRole('region', { name: 'Inspector' }).getByRole('heading', { name: 'whoami' })).toBeVisible()
      await expect(page.getByTestId('node-postgres')).not.toHaveAttribute('data-dimmed')
      await expectNoAxeViolations(page)
    })

    test('the editor with a binding selected and the select open', async ({ page }) => {
      await openGraph(page, BOUND)
      await page.locator('.react-flow__edge[data-id="whoami.postgresql"]').click()
      await page
        .getByRole('region', { name: 'Inspector' })
        .getByRole('button', { name: /Bound to/ })
        .click()
      await expect(page.getByRole('listbox')).toBeVisible()
      await expectNoAxeViolations(page)
    })

    test('the editor with problems shown', async ({ page }) => {
      await openGraph(page, PROBLEMS)
      await expect(page.getByTestId('node-app').getByRole('img', { name: /app needs postgresql/ })).toBeVisible()
      await expect(page.getByText('Fix 1 error to download')).toBeVisible()
      await page.locator('.react-flow__node[data-id="postgres"]').click() // dims the unrelated nodes too
      await expectNoAxeViolations(page)
    })

    test('the empty editor', async ({ page }) => {
      await page.goto('/')
      await expect(page.getByRole('heading', { name: 'No graph open' })).toBeVisible()
      await expectNoAxeViolations(page)
    })
  })
}
