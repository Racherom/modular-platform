import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import type { Module } from '../api/client'
import { CatalogPanel } from '../components/CatalogPanel'
import { filterCatalog } from '../graph/catalog'
import { useEditor } from '../lib/store'
import { testModule } from './fixtures'

const entry = (name: string, description: string, more: Parameters<typeof testModule>[1] = {}): Module =>
  testModule(name, { ...more, description })
const catalog: Module[] = [
  entry('postgres', 'PostgreSQL database server.', { provides: { postgresql: { port: 5432 } } }),
  entry('whoami', 'Tiny HTTP service.', { provides: { http: { port: 80 } }, uses: { postgresql: { optional: true } } }),
  entry('test-hostile', '<img src=x onerror=alert(1)> Test entry whose description holds HTML.'),
]

describe('filterCatalog (AC-19)', () => {
  it.each([
    ['empty keeps all', '', ['postgres', 'whoami', 'test-hostile']],
    ['blank keeps all', '   ', ['postgres', 'whoami', 'test-hostile']],
    ['name, ignoring case', 'WhoAmI', ['whoami']],
    ['description', 'database', ['postgres']],
    ['provides capability', 'http', ['whoami']],
    ['uses and provides capability', 'postgresql', ['postgres', 'whoami']],
    ['nothing matches', 'redis', []],
  ])('%s', (_, text, names) => {
    expect(filterCatalog(catalog, text).map((e) => e.name)).toEqual(names)
  })
})

describe('CatalogPanel', () => {
  beforeEach(() => useEditor.setState({ catalogFilter: '', filterFocus: 0 }))

  it('lists every entry with its capabilities, and renders descriptions as plain text (AC-19, AC-20)', () => {
    const { container } = render(<CatalogPanel entries={catalog} />)
    expect(screen.getAllByRole('listitem')).toHaveLength(3)
    expect(screen.getByText('3 modules')).toBeTruthy()
    expect(screen.getByText(/<img src=x onerror=alert\(1\)>/)).toBeTruthy()
    expect(container.querySelector('img')).toBeNull()
  })

  it('filters from the store and shows an empty state that clears the filter', () => {
    render(<CatalogPanel entries={catalog} />)
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filter the catalog' }), { target: { value: 'postgresql' } })
    expect(useEditor.getState().catalogFilter).toBe('postgresql')
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
    expect(screen.getByText('2 of 3 modules')).toBeTruthy()

    act(() => useEditor.getState().setCatalogFilter('redis'))
    expect(screen.getByText('No module matches')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Clear the filter' }))
    expect(useEditor.getState().catalogFilter).toBe('')
    expect(screen.getAllByRole('listitem')).toHaveLength(3)
  })

  it('Find providers moves focus to the filter (AC-19)', () => {
    render(<CatalogPanel entries={catalog} />)
    act(() => useEditor.getState().findProviders('http'))
    const field = screen.getByRole<HTMLInputElement>('searchbox', { name: 'Filter the catalog' })
    expect(document.activeElement).toBe(field)
    expect(field.value).toBe('http')
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
  })
})
