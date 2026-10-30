import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMemo } from 'react'
import { planGraph } from '../api/client'
import { Failure } from '../components/Failure'
import { GraphScreen } from '../components/GraphScreen'
import { samples } from '../graph/samples'
import { parseGraph } from '../graph/yaml'

export const Route = createFileRoute('/samples/$name')({ component: Sample })

function Sample() {
  const { name } = Route.useParams()
  const text = samples[name]
  if (text === undefined) {
    return (
      <div className="screen empty">
        <h1>No sample named {name}</h1>
        <p>
          <Link to="/">Go to the editor</Link>
        </p>
      </div>
    )
  }
  return <SampleGraph name={name} text={text} />
}

/** The bundled sample, planned once and shown read only; it never touches browser storage. */
function SampleGraph({ name, text }: { name: string; text: string }) {
  const graph = useMemo(() => parseGraph(text), [text])
  const plan = useQuery({ queryKey: ['sample-plan', name], queryFn: () => planGraph(graph) })
  if (plan.isError) return <Failure error={plan.error} onRetry={() => void plan.refetch()} />
  if (!plan.data) return <p>Planning the sample…</p>
  return <GraphScreen graph={plan.data.graph} plan={plan.data} fileName="graph.yaml" />
}
