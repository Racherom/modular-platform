import { expect, test } from '@playwright/test'
import { drag, expectNoAxeViolations, handle, openGraph, UNBOUND, useTheme } from './helpers'
import { mockRenderer } from './mock-renderer'

const BOUND = UNBOUND.replace('postgresql: null', 'postgresql: postgres')
const PROBLEMS = `${UNBOUND}  app:
    module: test-app
    uses:
      postgresql: null
`

// AC-3: zero axe violations in light and in dark, at 1280×800, on the editor states and /design.
for (const theme of ['light', 'dark'] as const) {
  test.describe(`${theme} theme`, () => {
    test.beforeEach(async ({ page }) => {
      await useTheme(page, theme)
      await mockRenderer(page)
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

    test('the /design showcase, with every token and component (AC-5)', async ({ page }) => {
      await page.goto('/design')
      await expect(page.getByRole('heading', { name: 'Design system', level: 1 })).toBeVisible()
      // the marker the production bundle search looks for (preview/bundle.spec.ts) is really here
      await expect(page.locator('[data-showcase="mp-design-showcase"]')).toHaveCount(1)
      await expect(page.getByTestId('node-broken')).toBeVisible()
      await expectNoAxeViolations(page)
    })

    test('the /design showcase with its Dialog open', async ({ page }) => {
      await page.goto('/design')
      const opener = page.getByRole('button', { name: 'Open the dialog' })
      await opener.click()
      const dialog = page.getByRole('alertdialog', { name: 'Replace the edited graph?' })
      await expect(dialog).toBeVisible()
      await expectNoAxeViolations(page)
      await page.keyboard.press('Escape')
      await expect(dialog).toBeHidden()
      await expect(opener).toBeFocused()
    })

    test('the empty editor', async ({ page }) => {
      await page.goto('/')
      await expect(page.getByRole('heading', { name: 'No graph open' })).toBeVisible()
      await expectNoAxeViolations(page)
    })
  })
}
