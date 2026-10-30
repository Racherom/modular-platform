import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { parseGraph, stringifyGraph } from '../graph/yaml'

const root = join(__dirname, '../../..')
const fixtureDir = join(root, 'contracts/fixtures/graphs')
const files = [
  ...readdirSync(fixtureDir)
    .filter((f) => f.endsWith('.yaml'))
    .map((f) => join(fixtureDir, f)),
  join(root, 'samples/whoami-postgres/graph.yaml'),
]

// The Go test reads the same files; both writers must give back each fixture byte for byte (AC-9).
describe('canonical graph text', () => {
  it.each(files)('round trips %s', (file) => {
    const text = readFileSync(file, 'utf8')
    expect(stringifyGraph(parseGraph(text))).toBe(text)
  })

  it('keeps a missing key, null and an empty map apart', () => {
    const graph = parseGraph(readFileSync(join(fixtureDir, 'sample-null.yaml'), 'utf8'))
    expect(graph.modules.whoami?.uses).toEqual({ postgresql: null })
    expect(parseGraph(readFileSync(join(fixtureDir, 'empty-uses.yaml'), 'utf8')).modules.db?.uses).toEqual({})
    expect(graph.modules.postgres?.uses).toBeUndefined()
  })

  it('refuses another version, another kind and duplicate keys', () => {
    const sample = readFileSync(files.at(-1)!, 'utf8')
    expect(() => parseGraph(sample.replace('v0alpha1', 'v1'))).toThrow(/apiVersion/)
    expect(() => parseGraph(sample.replace('kind: Graph', 'kind: Module'))).toThrow(/kind/)
    expect(() => parseGraph(`${sample}name: again\n`)).toThrow()
    expect(() => parseGraph('x'.repeat((1 << 20) + 1))).toThrow(/1 MB/)
  })
})
