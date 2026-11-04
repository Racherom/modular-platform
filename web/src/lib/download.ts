import type { Graph } from '../api/client'
import { stringifyGraph } from '../graph/yaml'

/** Save as file: exactly `stringifyGraph(graph)`, never a position or editor state (AC-16). */
export function saveGraphFile(graph: Graph, fileName: string): void {
  download(new Blob([stringifyGraph(graph)], { type: 'application/yaml' }), fileName)
}

/** Hands the browser a file to save. */
export function download(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = fileName
  document.body.append(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
