import { SearchX } from 'lucide-react'
import { useEffect, useRef } from 'react'
import type { CatalogEntry } from '../api/client'
import { filterCatalog } from '../graph/catalog'
import { useEditor } from '../lib/store'
import styles from './CatalogPanel.module.css'
import { Button } from './ui/Button'
import { EmptyState } from './ui/EmptyState'
import { Panel } from './ui/Panel'
import { SearchField } from './ui/SearchField'

/**
 * The catalog panel (AC-19): every entry with its name, description, and the capabilities it
 * provides and uses, filtered by the text in the editor store. Find providers in the inspector
 * sets the filter and moves focus here. Every text from the catalog renders as plain text (AC-20).
 */
export function CatalogPanel({ entries }: { entries: CatalogEntry[] }) {
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
                  <h3 className={styles.name}>{e.name}</h3>
                  <p className={styles.description}>{e.description}</p>
                  <Capabilities label="Provides" names={Object.keys(e.provides ?? {})} />
                  <Capabilities label="Uses" names={Object.keys(e.uses ?? {})} />
                </li>
              ))}
            </ul>
          )}
        </div>
      </Panel>
    </div>
  )
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
