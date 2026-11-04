import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { SearchX } from 'lucide-react'
import { useMemo, useRef } from 'react'
import { planGraph } from '../api/client'
import { GraphScreen } from '../components/GraphScreen'
import type { GraphEdit } from '../components/GraphView'
import { useReplaceGuard } from '../components/ReplaceDialog'
import { EmptyState } from '../components/ui/EmptyState'
import { Failure } from '../components/ui/Failure'
import { layout } from '../graph/layout'
import { samples } from '../graph/samples'
import { parseGraph } from '../graph/yaml'
import { loadWorking } from '../lib/storage'
import { useEditor } from '../lib/store'

export const Route = createFileRoute('/samples/$name')({ component: Sample })

function Sample() {
  const { name } = Route.useParams()
  // own keys only, so a path like /samples/constructor is just an unknown sample
  const text = Object.hasOwn(samples, name) ? samples[name] : undefined
  if (text === undefined) {
    return (
      <EmptyState icon={SearchX} level={1} title={`No sample named ${name}`}>
        <p>
          <Link to="/">Go to the editor</Link>
        </p>
      </EmptyState>
    )
  }
  return <SampleGraph name={name} text={text} />
}

/**
 * The bundled sample in the same editor (AC-18). It never changes: the first edit is held, the
 * replace prompt asks when the working graph is edited, and then the editor makes a copy with
 * the edit applied and moves to the editor root. Browser storage is read only at that first edit.
 */
function SampleGraph({ name, text }: { name: string; text: string }) {
  const graph = useMemo(() => parseGraph(text), [text])
  const plan = useQuery({ queryKey: ['sample-plan', name], queryFn: () => planGraph(graph) })
  const planned = plan.data
  const positions = useMemo(() => (planned ? layout(planned.graph) : {}), [planned])
  const navigate = useNavigate()
  const replace = useReplaceGuard()
  // one held edit at a time; more edits while the prompt is open are dropped
  const holding = useRef(false)

  if (plan.isError) return <Failure error={plan.error} onRetry={() => void plan.refetch()} />
  if (!planned) return <p>Planning the sample…</p>

  const onEdit = (change: GraphEdit) => {
    if (holding.current || change(planned.graph) === planned.graph) return
    holding.current = true
    const copy = () => {
      holding.current = false
      const base = { fileName: `${planned.graph.name}.yaml`, graph: planned.graph, plan: planned, positions }
      if (useEditor.getState().copySample(base, change)) void navigate({ to: '/' })
    }
    const ask = () => replace.guard(copy, () => (holding.current = false))
    // the edited flag lives in the working copy, which this page has not read yet
    if (useEditor.getState().working !== undefined) ask()
    else {
      void loadWorking()
        .catch(() => undefined)
        .then((w) => {
          useEditor.getState().restore(w ?? null)
          ask()
        })
    }
  }

  return (
    <>
      {replace.dialog}
      <GraphScreen
        graph={planned.graph}
        plan={planned}
        fileName="graph.yaml"
        notice="This is a sample. Your first change makes a copy in the editor; the sample itself stays as it is."
        editing={{
          positions,
          edited: false,
          planState: 'current',
          planError: null,
          onRetry: () => undefined,
          onEdit,
          onSaved: () => undefined,
        }}
      />
    </>
  )
}
