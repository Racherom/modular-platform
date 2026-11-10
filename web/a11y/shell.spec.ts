import { expect, test } from '@playwright/test'
import { openGraph } from './helpers'
import { mockRenderer } from './mock-renderer'

test.beforeEach(async ({ page }) => {
  await mockRenderer(page)
})

test.describe('theme (AC-2)', () => {
  test('a stored Dark is applied before the app runs', async ({ page }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem('mp-theme', 'dark')
      document.addEventListener('DOMContentLoaded', () => {
        ;(window as unknown as { themeAtLoad: string | null }).themeAtLoad = document.documentElement.getAttribute('data-theme')
      })
    })
    await page.goto('/samples/whoami-postgres')
    expect(await page.evaluate(() => (window as unknown as { themeAtLoad: string | null }).themeAtLoad)).toBe('dark')
    await expect(page.locator('.react-flow')).toHaveClass(/\bdark\b/)
  })

  test('System follows the OS, and an unknown stored value means System', async ({ page }) => {
    await page.addInitScript(() => window.localStorage.setItem('mp-theme', 'purple'))
    await page.emulateMedia({ colorScheme: 'dark' })
    await page.goto('/samples/whoami-postgres')
    await expect(page.locator('html')).not.toHaveAttribute('data-theme', /.*/)
    await expect(page.getByRole('radio', { name: 'System theme' })).toBeChecked()
    await expect(page.locator('.react-flow')).toHaveClass(/\bdark\b/)
    await page.emulateMedia({ colorScheme: 'light' })
    await expect(page.locator('.react-flow')).toHaveClass(/\blight\b/)
  })

  test('the toggle stores the choice and React Flow follows', async ({ page }) => {
    await page.emulateMedia({ colorScheme: 'light' })
    await page.goto('/samples/whoami-postgres')
    await page.getByRole('radiogroup', { name: 'Theme' }).getByRole('radio', { name: 'Dark theme' }).click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    await expect(page.locator('.react-flow')).toHaveClass(/\bdark\b/)
    expect(await page.evaluate(() => window.localStorage.getItem('mp-theme'))).toBe('dark')
    await page.reload()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  })

  test('the toggle still works when storage throws', async ({ page }) => {
    await page.addInitScript(() => {
      const blocked = () => {
        throw new DOMException('blocked', 'SecurityError')
      }
      Object.defineProperty(window, 'localStorage', { get: blocked, configurable: true })
    })
    await page.goto('/samples/whoami-postgres')
    await page.getByRole('radiogroup', { name: 'Theme' }).getByRole('radio', { name: 'Dark theme' }).click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    await page.getByRole('radiogroup', { name: 'Theme' }).getByRole('radio', { name: 'Light theme' }).click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  })
})

test.describe('layout (AC-6)', () => {
  test('from 1024px the four regions sit side by side, and problems collapse', async ({ page }) => {
    await page.setViewportSize({ width: 1024, height: 768 })
    await openGraph(page)
    const box = async (name: string) => (await page.getByRole('region', { name, exact: true }).boundingBox())!
    const catalog = await box('Catalog')
    const canvas = await box('Graph whoami-postgres')
    const inspector = await box('Inspector')
    const problems = await box('Problems')
    expect(catalog.x + catalog.width).toBeLessThanOrEqual(canvas.x + 1)
    expect(canvas.x + canvas.width).toBeLessThanOrEqual(inspector.x + 1)
    expect(problems.y).toBeGreaterThanOrEqual(canvas.y + canvas.height - 1)
    expect(problems.x).toBeCloseTo(canvas.x, 0)

    const toggle = page.getByRole('region', { name: 'Problems' }).getByRole('button', { name: 'Hide' })
    await expect(toggle).toHaveAttribute('aria-expanded', 'true')
    await toggle.click()
    await expect(page.getByText('No problems.')).toBeHidden()
    await page.getByRole('button', { name: 'Show' }).click()
    await expect(page.getByText('No problems.')).toBeVisible()
  })

  for (const width of [1000, 768, 360]) {
    test(`at ${width}px the panels stack under the canvas, with no horizontal scroll`, async ({ page }) => {
      await page.setViewportSize({ width, height: 800 })
      await openGraph(page)
      const canvas = (await page.getByRole('region', { name: 'Graph whoami-postgres' }).boundingBox())!
      const catalog = (await page.getByRole('region', { name: 'Catalog', exact: true }).boundingBox())!
      expect(catalog.y).toBeGreaterThan(canvas.y + canvas.height - 1)
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
      await expect(page.getByTestId('node-postgres')).toBeVisible()
    })
  }
})
