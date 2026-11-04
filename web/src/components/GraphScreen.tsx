import { useMutation, useQuery } from '@tanstack/react-query'
import { ReactFlowProvider } from '@xyflow/react'
import { Download, Rocket, Save } from 'lucide-react'
import { type ReactNode, useId, useState } from 'react'
import { fetchCatalog, type Graph, type PlannerError, type PlanResult, renderGraph } from '../api/client'
import type { Positions } from '../graph/flow'
import { stringifyGraph } from '../graph/yaml'
import { mode } from '../lib/config'
import { download } from '../lib/download'
import type { PlanState } from '../lib/store'
import { HeaderContent, Workspace } from './AppShell'
import { CatalogList } from './CatalogList'
import { Failure } from './Failure'
import styles from './GraphScreen.module.css'
import { type GraphEdit, GraphView } from './GraphView'
import { Inspector } from './Inspector'
import { ProblemsPanel } from './ProblemsPanel'
import { ReviewDialog } from './ReviewDialog'
import { Button } from './ui/Button'

/** The catalog, fetched once per page. */
export function useCatalog() {
  return useQuery({ queryKey: ['catalog'], queryFn: fetchCatalog })
}

/** What an editable screen adds: the working copy's state and its edit actions. */
export interface Editing {
  positions: Positions
  edited: boolean
  planState: PlanState
  planError: PlannerError | null
  onRetry: () => void
  /** Applies a user edit to the working graph. */
  onEdit: (edit: GraphEdit) => void
  /** Called after Save as file. */
  onSaved: () => void
}

interface Props {
  graph: Graph
  plan: PlanResult
  fileName: string
  toolbar?: ReactNode
  /** Makes the canvas editable; without it the graph is only shown. */
  editing?: Editing
}

/**
 * Why Download zip (and Deploy) is disabled, or null when it is allowed (AC-14): only a current
 * plan without errors allows it; warnings never block.
 */
export function downloadBlocked(planState: PlanState, plan: PlanResult): string | null {
  if (planState === 'checking') return 'Checking the graph'
  if (planState === 'failed') return 'Not checked, retry first'
  if (plan.ok) return null
  const errors = plan.problems.filter((p) => p.severity === 'error').length
  return errors > 0 ? `Fix ${errors} ${errors === 1 ? 'error' : 'errors'} to download` : 'Fix the errors to download'
}

/** A planned graph in the editor: header toolbar, catalog, canvas, inspector and problems. */
export function GraphScreen({ graph, plan, fileName, toolbar, editing }: Props) {
  const catalog = useCatalog()
  // a render that finds an error returns a newer plan to show, but only for the plan it came from
  const [rendered, setRendered] = useState<{ from: PlanResult; plan: PlanResult }>()
  const shownPlan = rendered?.from === plan ? rendered.plan : plan
  const [reviewing, setReviewing] = useState(false)
  const reasonId = useId()
  const zip = useMutation({
    mutationFn: () => renderGraph(graph),
    onSuccess: (result) => {
      if ('zip' in result) download(result.zip, `${graph.name}.zip`)
      else setRendered({ from: plan, plan: result.plan })
    },
  })

  if (catalog.isError) return <Failure error={catalog.error} onRetry={() => void catalog.refetch()} />
  if (!catalog.data) return <p>Loading the catalog…</p>

  const planState = editing?.planState ?? 'current'
  const blocked = downloadBlocked(planState, shownPlan)
  const save = () => {
    download(new Blob([stringifyGraph(graph)], { type: 'application/yaml' }), fileName)
    editing?.onSaved()
  }
  return (
    <>
      <HeaderContent>
        <div className={styles.file}>
          <h1 className={styles.fileName}>{fileName}</h1>
          {editing?.edited && <span className={styles.edited}>Edited</span>}
        </div>
        <div className={styles.toolbar}>
          {toolbar}
          <Button size="sm" onPress={save}>
            <Save aria-hidden="true" />
            Save as file
          </Button>
          <Button
            size="sm"
            onPress={() => zip.mutate()}
            isDisabled={zip.isPending || blocked !== null}
            aria-describedby={blocked ? reasonId : undefined}
          >
            <Download aria-hidden="true" />
            {zip.isPending ? 'Rendering…' : 'Download zip'}
          </Button>
          {mode === 'managed' && (
            <Button
              size="sm"
              variant="primary"
              onPress={() => setReviewing(true)}
              isDisabled={blocked !== null}
              aria-describedby={blocked ? reasonId : undefined}
            >
              <Rocket aria-hidden="true" />
              Deploy
            </Button>
          )}
          {blocked && (
            <span id={reasonId} className={styles.reason}>
              {blocked}
            </span>
          )}
        </div>
      </HeaderContent>
      {reviewing && <ReviewDialog graph={graph} onClose={() => setReviewing(false)} />}
      <ReactFlowProvider>
        <Workspace
          catalog={<CatalogList entries={catalog.data} />}
          canvas={
            <div className={styles.canvas}>
              {zip.isError && <Failure error={zip.error} onRetry={() => zip.mutate()} />}
              <GraphView
                graph={graph}
                catalog={catalog.data}
                problems={shownPlan.problems}
                positions={editing?.positions}
                onEdit={editing?.onEdit}
              />
            </div>
          }
          inspector={
            <Inspector
              graph={graph}
              catalog={catalog.data}
              plan={shownPlan}
              planCurrent={planState === 'current'}
              onEdit={editing?.onEdit}
            />
          }
          problemsStatus={
            <span role="status" className={styles.status}>
              {planState === 'checking' ? 'Checking the graph…' : planState === 'failed' ? 'Not checked' : ''}
            </span>
          }
          problems={
            planState === 'failed' && editing?.planError ? (
              // a 400 means the planner refused this graph: retrying cannot help, undo is the way back
              <Failure error={editing.planError} onRetry={editing.planError.status === 400 ? undefined : editing.onRetry} />
            ) : (
              <ProblemsPanel problems={shownPlan.problems} graph={graph} />
            )
          }
        />
      </ReactFlowProvider>
    </>
  )
}
