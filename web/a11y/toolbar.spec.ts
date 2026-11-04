import { expect, test } from '@playwright/test'

// AC-4: arrow keys move focus between the controls of a Toolbar; the toolbar is one Tab stop.
test('arrow keys move between the controls of a toolbar, and Tab leaves it (AC-4)', async ({ page }) => {
  await page.goto('/design')
  const toolbar = page.getByRole('toolbar', { name: 'History, example' })
  const undo = toolbar.getByRole('button', { name: 'Undo, example' })
  const redo = toolbar.getByRole('button', { name: 'Redo, example' })
  const tidy = toolbar.getByRole('button', { name: 'Tidy layout' })

  await undo.focus()
  await page.keyboard.press('ArrowRight')
  await expect(redo).toBeFocused()
  await page.keyboard.press('ArrowRight')
  await expect(tidy).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(redo).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(undo).toBeFocused()

  await page.keyboard.press('Tab')
  expect(await toolbar.evaluate((el) => el.contains(document.activeElement))).toBe(false)
})
