import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { SearchX } from 'lucide-react'
import { useMemo, useRef } from 'react'
import { type Catalog, checkGraph, type Graph } from '../api/client'
import { GraphScreen } from '../components/GraphScreen'
import type { GraphEdit } from '../components/GraphView'
import { useReplaceGuard } from '../components/ReplaceDialog'
import { EmptyState } from '../components/ui/EmptyState'
import { Failure } from '../components/ui/Failure'
import { addModule } from '../graph/edit'
import { layout, placeNew } from '../graph/layout'
import { samples } from '../graph/samples'
import { parseGraph } from '../graph/yaml'
import { loadWorking } from '../lib/storage'
import { type EditResult, useEditor } from '../lib/store'

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
  const check = useQuery({ queryKey: ['sample-check', name], queryFn: () => checkGraph(graph) })
  const checked = check.data
  const positions = useMemo(() => (checked ? layout(checked.graph) : {}), [checked])
  const navigate = useNavigate()
  const replace = useReplaceGuard()
  // one held edit at a time; more edits while the prompt is open are dropped
  const holding = useRef(false)

  if (check.isError) return <Failure error={check.error} onRetry={() => void check.refetch()} />
  if (!checked) return <p>Checking the sample…</p>

  // an edit holds until the replace prompt is answered, then the copy is made with it applied
  const hold = (change: (graph: Graph) => EditResult, added?: () => string | undefined) => {
    if (holding.current) return
    holding.current = true
    const copy = () => {
      holding.current = false
      const base = { fileName: `${checked.graph.name}.yaml`, graph: checked.graph, check: checked, positions }
      if (useEditor.getState().copySample(base, change, added?.())) void navigate({ to: '/' })
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
  const onEdit = (change: GraphEdit) => {
    if (change(checked.graph) !== checked.graph) hold(change)
  }
  // Add on the sample (spec 0029, AC-11): the copy gets the module at the place the sample's layout leaves free
  const onAdd = (module: string, loaded: Catalog) => {
    if (holding.current) return
    const { graph: next, instance } = addModule(checked.graph, module)
    hold(
      () => ({ graph: next, positions: { ...positions, [instance]: placeNew(checked.graph, loaded, positions) } }),
      () => instance,
    )
  }

  return (
    <>
      {replace.dialog}
      <GraphScreen
        graph={checked.graph}
        check={checked}
        fileName="graph.yaml"
        notice="This is a sample. Your first change makes a copy in the editor; the sample itself stays as it is."
        editing={{
          positions,
          edited: false,
          checkState: 'current',
          checkError: null,
          onRetry: () => undefined,
          onEdit,
          onAdd,
          onSaved: () => undefined,
        }}
      />
    </>
  )
}
