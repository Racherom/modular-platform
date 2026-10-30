// Samples bundled into the editor at build; the sample route never reads browser storage.
import whoamiPostgres from '../../../samples/whoami-postgres/graph.yaml?raw'

export const samples: Record<string, string> = {
  'whoami-postgres': whoamiPostgres,
}
