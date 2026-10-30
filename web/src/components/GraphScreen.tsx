import { useMutation, useQuery } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { fetchCatalog, renderGraph, type Graph, type PlanResult } from '../api/client'
import { stringifyGraph } from '../graph/yaml'
import { download } from '../lib/download'
import { CatalogList } from './CatalogList'
import { Failure } from './Failure'
import { GraphView } from './GraphView'
import { ProblemList } from './ProblemList'

export function useCatalog() {
  return useQuery({ queryKey: ['catalog'], queryFn: fetchCatalog })
}

interface Props {
  graph: Graph
  plan: PlanResult
  fileName: string
  toolbar?: ReactNode
}

/** A planned graph on the canvas, its problems, and save and download. */
export function GraphScreen({ graph, plan, fileName, toolbar }: Props) {
  const catalog = useCatalog()
  // a render that finds an error returns a newer plan to show, but only for the plan it came from
  const [rendered, setRendered] = useState<{ from: PlanResult; plan: PlanResult }>()
  const shownPlan = rendered?.from === plan ? rendered.plan : plan
  const zip = useMutation({
    mutationFn: () => renderGraph(graph),
    onSuccess: (result) => {
      if ('zip' in result) download(result.zip, `${graph.name}.zip`)
      else setRendered({ from: plan, plan: result.plan })
    },
  })

  if (catalog.isError) return <Failure error={catalog.error} onRetry={() => void catalog.refetch()} />
  if (!catalog.data) return <p>Loading the catalog…</p>

  const save = () => download(new Blob([stringifyGraph(graph)], { type: 'application/yaml' }), fileName)
  return (
    <div className="screen">
      <div className="toolbar">
        <h1>{graph.name}</h1>
        {toolbar}
        <button type="button" onClick={save}>
          Save as file
        </button>
        <button type="button" onClick={() => zip.mutate()} disabled={zip.isPending}>
          {zip.isPending ? 'Rendering…' : 'Download zip'}
        </button>
      </div>
      {zip.isError && <Failure error={zip.error} onRetry={() => zip.mutate()} />}
      <div className="workspace">
        <GraphView graph={graph} catalog={catalog.data} problems={shownPlan.problems} />
        <CatalogList entries={catalog.data} />
      </div>
      <ProblemList problems={shownPlan.problems} />
    </div>
  )
}
