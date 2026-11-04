import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

// AC-3: every focus indicator (the outline from base.css and the components, the node outline
// and the edge stroke in reactflow.css) uses --color-focus, and every focusable element sits on
// one of these backgrounds. Axe does not measure focus indicators, so this test checks the
// tokens: 3:1 against each background, in light and in dark.
const tokens = readFileSync(join(import.meta.dirname, '../styles/tokens.css'), 'utf8')
const BACKGROUNDS = [
  '--color-bg',
  '--color-surface',
  '--color-surface-raised',
  '--color-canvas',
  '--color-danger-surface',
  '--color-warning-surface',
  '--color-success-surface',
]

/** The hex color tokens declared inside the first block that follows `selector`. */
function block(selector: string): Record<string, string> {
  const start = tokens.indexOf(`${selector} {`)
  if (start < 0) throw new Error(`tokens.css has no block for ${selector}`)
  const body = tokens.slice(start, tokens.indexOf('}', start))
  return Object.fromEntries([...body.matchAll(/(--color-[\w-]+):\s*(#[0-9a-f]{6})\s*;/gi)].map((m) => [m[1], m[2]]))
}

/** Relative luminance of a #rrggbb color (WCAG 2.2). */
function luminance(hex: string): number {
  const [r = 0, g = 0, b = 0] = [1, 3, 5].map((i) => {
    const c = Number.parseInt(hex.slice(i, i + 2), 16) / 255
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

/** WCAG contrast ratio of two #rrggbb colors. */
function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number]
  return (hi + 0.05) / (lo + 0.05)
}

const light = block(':root')
const dark = block(':root[data-theme="dark"]')
const osDark = block(':root:not([data-theme="light"])')

describe('focus indicator contrast (AC-3)', () => {
  it('computes WCAG contrast', () => {
    expect(contrast('#000000', '#ffffff')).toBeCloseTo(21)
    expect(contrast('#777777', '#777777')).toBeCloseTo(1)
  })

  it('the OS dark block holds the same colors as the dark theme block', () => {
    expect(osDark).toEqual(dark)
  })

  for (const [theme, colors] of [
    ['light', light],
    ['dark', dark],
  ] as const) {
    it.each(BACKGROUNDS)(`${theme}: --color-focus has 3:1 against %s`, (bg) => {
      const focus = colors['--color-focus']
      const back = colors[bg]
      expect(focus).toBeDefined()
      expect(back).toBeDefined()
      expect(contrast(focus as string, back as string)).toBeGreaterThanOrEqual(3)
    })

    it(`${theme}: dimmed edges keep 3:1 against the canvas`, () => {
      expect(contrast(colors['--color-edge-dimmed'] as string, colors['--color-canvas'] as string)).toBeGreaterThanOrEqual(3)
    })
  }
})
