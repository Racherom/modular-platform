import { readdirSync, readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'

// AC-1: tokens.css is the only file with color literals. Every other CSS file under src/, and
// every inline style object in a .tsx file under src/ (tests excluded), uses tokens only.
const src = join(import.meta.dirname, '..')
const TOKENS = 'styles/tokens.css'
const LITERAL = /#[0-9a-f]{3,8}\b|\b(?:rgba?|hsla?)\(/i

function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const path = join(dir, e.name)
    if (e.isDirectory()) return e.name === 'test' ? [] : files(path)
    return [path]
  })
}

/** The color literals in a CSS text, without its comments. */
function cssLiterals(css: string): string[] {
  return css
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .split('\n')
    .filter((line) => LITERAL.test(line))
    .map((line) => line.trim())
}

/** The color literals inside the inline style objects (`style={{ ... }}`) of a TSX text. */
function styleLiterals(tsx: string): string[] {
  return [...tsx.matchAll(/style=\{\{([\s\S]*?)\}\}/g)].map((m) => m[1] ?? '').filter((body) => LITERAL.test(body))
}

describe('color literals (AC-1)', () => {
  it('finds hex, rgb, rgba, hsl and hsla in CSS, and ignores comments and tokens', () => {
    expect(cssLiterals('a { color: #fff; }')).toHaveLength(1)
    expect(cssLiterals('a { color: #1b1f24aa; }')).toHaveLength(1)
    expect(cssLiterals('a { color: rgb(0 0 0); }\nb { color: RGBA(0,0,0,1) }')).toHaveLength(2)
    expect(cssLiterals('a { color: hsl(0 0% 0%); }\nb { color: hsla(0, 0%, 0%, 1) }')).toHaveLength(2)
    expect(cssLiterals('/* #fff */ a { color: var(--color-text); }')).toEqual([])
  })

  it('finds color literals in inline style objects only', () => {
    expect(styleLiterals("<div style={{ background: '#fff' }} />")).toHaveLength(1)
    expect(styleLiterals("<div style={{ color: 'rgb(1 2 3)' }} />")).toHaveLength(1)
    expect(styleLiterals('<div style={{ background: "var(--color-bg)" }} />')).toEqual([])
    expect(styleLiterals('<a href="#top">top</a>')).toEqual([])
  })

  it('no CSS file but tokens.css holds a color literal', () => {
    const css = files(src).filter((f) => f.endsWith('.css') && relative(src, f) !== TOKENS)
    expect(css.length).toBeGreaterThan(5)
    const found = css.flatMap((f) => cssLiterals(readFileSync(f, 'utf8')).map((l) => `${relative(src, f)}: ${l}`))
    expect(found).toEqual([])
  })

  it('no style prop in a .tsx file holds a color literal', () => {
    const tsx = files(src).filter((f) => f.endsWith('.tsx') && !f.endsWith('.test.tsx'))
    const found = tsx.flatMap((f) => styleLiterals(readFileSync(f, 'utf8')).map((l) => `${relative(src, f)}: ${l.trim()}`))
    expect(found).toEqual([])
  })
})
