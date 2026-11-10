import { expect, test } from '@playwright/test'
import { drag, handle, UNBOUND } from '../helpers'
import { graphFile, mockRenderer } from '../mock-renderer'

// The preview server sends web/Caddyfile's content policy (style-src 'self', script-src 'self').
// Nothing the editor does may need an inline style or script: React Aria's pressable <style>
// is kept out by the marker in index.html, and its rule lives in base.css.
test('the editor runs under the content policy, with no blocked style or script (spec 0001, spec 0010)', async ({ page }) => {
  const violations: string[] = []
  await page.exposeFunction('reportViolation', (v: string) => violations.push(v))
  await page.addInitScript(() => {
    document.addEventListener('securitypolicyviolation', (e) => {
      const w = window as unknown as { reportViolation: (v: string) => void }
      w.reportViolation(`${e.violatedDirective} ${e.blockedURI} ${e.sample}`)
    })
  })
  await mockRenderer(page)

  const reply = await page.goto('/samples/whoami-postgres')
  expect(reply?.headers()['content-security-policy']).toContain("style-src 'self'")
  await expect(page.getByTestId('node-whoami')).toBeVisible()
  await expect(page.locator('#react-aria-pressable-style')).toHaveCount(1) // the marker, not an injected <style>
  await expect(page.locator('style#react-aria-pressable-style')).toHaveCount(0)

  await page.goto('/')
  await page.getByRole('button', { name: 'Open file' }).waitFor()
  await page.locator('input[type=file]').setInputFiles(graphFile('graph.yaml', UNBOUND))
  await page.locator('.react-flow__node[data-id="whoami"]').click()
  await page
    .getByRole('region', { name: 'Inspector' })
    .getByRole('button', { name: /Bound to/ })
    .click()
  await expect(page.getByRole('listbox')).toBeVisible()
  await page.keyboard.press('Escape')
  await drag(page, handle(page, 'whoami', 'uses:postgresql'), handle(page, 'postgres', 'provides:postgresql'))
  await page.getByRole('radio', { name: 'Dark theme' }).click()

  expect(violations).toEqual([])
})
