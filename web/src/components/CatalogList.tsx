import type { CatalogEntry } from '../api/client'
import styles from './CatalogList.module.css'
import { Panel } from './ui/Panel'

/** The catalog panel: every entry with its description and image. */
export function CatalogList({ entries }: { entries: CatalogEntry[] }) {
  return (
    <Panel title="Catalog">
      <ul className={styles.list}>
        {entries.map((e) => (
          <li key={e.name} className={styles.entry}>
            <strong className={styles.name}>{e.name}</strong>
            <code className={styles.image}>{e.image}</code>
            <p className={styles.description}>{e.description}</p>
          </li>
        ))}
      </ul>
    </Panel>
  )
}
