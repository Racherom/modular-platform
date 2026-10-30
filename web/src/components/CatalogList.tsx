import type { CatalogEntry } from '../api/client'

export function CatalogList({ entries }: { entries: CatalogEntry[] }) {
  return (
    <section className="catalog" aria-labelledby="catalog-title">
      <h2 id="catalog-title">Catalog</h2>
      <ul>
        {entries.map((e) => (
          <li key={e.name}>
            <strong>{e.name}</strong>
            <code>{e.image}</code>
            <p>{e.description}</p>
          </li>
        ))}
      </ul>
    </section>
  )
}
