import { Plus, SearchX } from 'lucide-react'
import { useEffect, useRef } from 'react'
import type { Module } from '../api/client'
import { filterCatalog } from '../graph/catalog'
import { useEditor } from '../lib/store'
import styles from './CatalogPanel.module.css'
import { Button } from './ui/Button'
import { EmptyState } from './ui/EmptyState'
import { Panel } from './ui/Panel'
import { SearchField } from './ui/SearchField'

/**
 * The catalog panel (AC-19): every module with its name, description, what it provides (each
 * provide's capability and version, spec 0011 AC-19) and what it uses (each slot's capability and
 * range), filtered by the text in the editor store. Find providers in the inspector
 * sets the filter and moves focus here. Every text from the catalog renders as plain text (AC-20).
 * With `onAdd`, each entry has an Add button (spec 0029, AC-1); the managed stack view passes none.
 */
export function CatalogPanel({ entries, onAdd }: { entries: Module[]; onAdd?: (module: string) => void }) {
  const filter = useEditor((s) => s.catalogFilter)
  const filterFocus = useEditor((s) => s.filterFocus)
  const { setCatalogFilter } = useEditor.getState()
  const root = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const shown = filterCatalog(entries, filter)

  // Find providers: bring the panel into view (also when stacked below 1024px), then focus the filter
  useEffect(() => {
    if (filterFocus === 0) return
    root.current?.scrollIntoView({ block: 'nearest' })
    input.current?.focus({ preventScroll: true })
  }, [filterFocus])

  return (
    <div ref={root} className={styles.root}>
      <Panel title="Catalog">
        <div className={styles.content}>
          <SearchField
            label="Filter the catalog"
            hideLabel
            placeholder="Filter by name or capability"
            value={filter}
            onChange={setCatalogFilter}
            inputRef={input}
          />
          <p role="status" className={styles.count}>
            {filter.trim() === '' ? `${entries.length} modules` : `${shown.length} of ${entries.length} modules`}
          </p>
          {shown.length === 0 ? (
            <EmptyState
              icon={SearchX}
              title="No module matches"
              actions={
                <Button size="sm" onPress={() => setCatalogFilter('')}>
                  Clear the filter
                </Button>
              }
            >
              Nothing in the catalog has <q className={styles.query}>{filter.trim()}</q> in its name, description or capabilities.
            </EmptyState>
          ) : (
            <ul className={styles.list}>
              {shown.map((e) => (
                <li key={e.name} className={styles.entry}>
                  <div className={styles.head}>
                    <h3 className={styles.name}>{e.name}</h3>
                    {onAdd && (
                      <Button size="sm" aria-label={`Add ${e.name}`} onPress={() => onAdd(e.name)}>
                        <Plus aria-hidden="true" />
                        Add
                      </Button>
                    )}
                  </div>
                  <p className={styles.description}>{e.description}</p>
                  <Capabilities
                    label="Provides"
                    names={Object.entries(e.provides ?? {}).map(([name, p]) => portLabel(name, p.capability, p.version))}
                  />
                  <Capabilities
                    label="Uses"
                    names={Object.entries(e.uses ?? {}).map(([name, u]) => portLabel(name, u.capability, u.versions ?? ''))}
                  />
                </li>
              ))}
            </ul>
          )}
        </div>
      </Panel>
    </div>
  )
}

/** A provide or slot as the panel shows it: the capability (after the name when they differ) and a version or range. */
function portLabel(name: string, capability: string, version: string): string {
  return [name === capability ? capability : `${name}: ${capability}`, version].filter((t) => t !== '').join(' ')
}

function Capabilities({ label, names }: { label: string; names: string[] }) {
  if (names.length === 0) return null
  return (
    <p className={styles.capabilities}>
      <span className={styles.capabilitiesLabel}>{label}</span>
      {names.sort().map((n) => (
        <code key={n} className={styles.capability}>
          {n}
        </code>
      ))}
    </p>
  )
}
