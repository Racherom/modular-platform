import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import type { Graph } from '../api/client'
import { type Deployment, fetchDeployment, fetchOperations, type ModuleState, type Operation } from '../api/platform'
import { Failure } from '../components/Failure'
import { useCatalog } from '../components/GraphScreen'
import { GraphView } from '../components/GraphView'
import { RemoveDialog } from '../components/RemoveDialog'
import { ReviewDialog } from '../components/ReviewDialog'
import { StateBadge } from '../components/StateBadge'
import { useEvents } from '../hooks/useEvents'
import { mode } from '../lib/config'
import { deploymentNotice } from '../lib/notice'
import { OnlyManaged } from './deployments.index'

export const Route = createFileRoute('/deployments/$name')({ component: DeploymentRoute })

function DeploymentRoute() {
  const { name } = Route.useParams()
  if (mode !== 'managed') return <OnlyManaged />
  return <DeploymentScreen name={name} />
}

/** One deployment: its graph with a live badge per module, the running step, and the history. */
function DeploymentScreen({ name }: { name: string }) {
  const deployment = useQuery({ queryKey: ['deployment', name], queryFn: () => fetchDeployment(name) })
  const history = useQuery({ queryKey: ['operations', name], queryFn: () => fetchOperations(name) })
  const catalog = useCatalog()
  const [removing, setRemoving] = useState<{ deleteData: boolean }>()
  useEvents(name)
  const states = useMemo(() => byInstance(deployment.data?.modules), [deployment.data?.modules])

  if (deployment.isError) return <Failure error={deployment.error} onRetry={() => void deployment.refetch()} />
  if (!deployment.data || !catalog.data) return <p className="screen">Loading {name}…</p>
  const d = deployment.data
  const running = d.latestOperation?.status === 'running'
  return (
    <div className="screen">
      <div className="toolbar">
        <h1>{d.name}</h1>
        <StateBadge deployment={d.state} />
        <button type="button" disabled={running} onClick={() => setRemoving({ deleteData: false })}>
          Remove
        </button>
      </div>
      <Progress deployment={d} onRemoveAgain={(deleteData) => setRemoving({ deleteData })} />
      {d.stateError && (
        <p role="alert" className="banner banner--error">
          The live state could not be read: {d.stateError}
        </p>
      )}
      {d.graph ? (
        <div className="workspace">
          <GraphView graph={d.graph} catalog={catalog.data} problems={[]} states={states} />
          <ModuleList modules={d.modules ?? []} />
        </div>
      ) : (
        <p>No version of this stack has run yet.</p>
      )}
      <section aria-labelledby="history-title">
        <h2 id="history-title">History</h2>
        {history.data && (
          <ol className="history">
            {history.data.map((op) => (
              <li key={op.id} id={`operation-${op.id}`}>
                {new Date(op.startedAt).toLocaleString()}: {operationLabel(op)} by {op.startedBy}, <strong>{op.status}</strong>
                {op.step && ` at ${op.step}`}
                {op.error && <pre className="history__error">{op.error}</pre>}
              </li>
            ))}
          </ol>
        )}
      </section>
      {removing && <RemoveDialog name={d.name} deleteData={removing.deleteData} onClose={() => setRemoving(undefined)} />}
    </div>
  )
}

function operationLabel(op: Operation): string {
  if (op.action === 'remove') return op.deleteData ? 'remove with its data' : 'remove'
  return op.rollbackOf ? 'rollback' : 'apply'
}

/**
 * The running operation's step in a live region, and the banner about the latest operation:
 * why it failed or was rolled back, with *Apply again* and *Remove again* where they help.
 */
function Progress({ deployment: d, onRemoveAgain }: { deployment: Deployment; onRemoveAgain: (deleteData: boolean) => void }) {
  const queryClient = useQueryClient()
  const [reviewing, setReviewing] = useState<Graph>()
  // Apply again reviews the newest apply's graph as it is now, never a copy another tab outdated
  const applyAgain = useMutation({
    mutationFn: () => fetchDeployment(d.name),
    onSuccess: (fresh) => {
      queryClient.setQueryData(['deployment', d.name], fresh)
      if (fresh.latestApplyGraph) setReviewing(fresh.latestApplyGraph)
    },
  })
  const op = d.latestOperation
  const notice = deploymentNotice(d)
  let text = ''
  if (op?.status === 'running') {
    const doing = op.action === 'remove' ? 'Removing' : op.rollbackOf ? 'Returning to the last version that worked' : 'Applying'
    text = `${doing}: step ${op.step ?? 'prepare'}.`
  }
  return (
    <>
      <p aria-live="polite" className="progress">
        {text}
      </p>
      {notice && (
        <div role={notice.tone === 'error' ? 'alert' : 'status'} className={notice.tone === 'error' ? 'banner banner--error' : 'banner'}>
          <p>
            {notice.text}
            {d.rolledBackFrom && (
              <>
                {' '}
                <a href={`#operation-${d.rolledBackFrom.id}`}>See what failed.</a>
              </>
            )}
          </p>
          {(d.rolledBackFrom?.error ?? op?.error) && <pre className="history__error">{d.rolledBackFrom?.error ?? op?.error}</pre>}
          {(notice.applyAgain || notice.removeAgain) && (
            <div className="review__actions">
              {notice.removeAgain && (
                <button type="button" onClick={() => onRemoveAgain(op?.deleteData ?? false)}>
                  Remove again
                </button>
              )}
              {notice.applyAgain && (
                <button type="button" disabled={applyAgain.isPending} onClick={() => applyAgain.mutate()}>
                  Apply again
                </button>
              )}
            </div>
          )}
          {applyAgain.isError && <Failure error={applyAgain.error} onRetry={() => applyAgain.mutate()} />}
        </div>
      )}
      {reviewing && <ReviewDialog graph={reviewing} onClose={() => setReviewing(undefined)} />}
    </>
  )
}

function ModuleList({ modules }: { modules: ModuleState[] }) {
  return (
    <aside className="catalog" aria-label="Modules">
      <h2>Modules</h2>
      <ul>
        {modules.map((m) => (
          <li key={m.instance}>
            <strong>{m.instance}</strong> <StateBadge module={m} />
            {m.image && (
              <p>
                <code>{m.image}</code>
              </p>
            )}
          </li>
        ))}
      </ul>
    </aside>
  )
}

function byInstance(modules?: ModuleState[]): Record<string, ModuleState> | undefined {
  if (!modules) return undefined
  return Object.fromEntries(modules.map((m) => [m.instance, m]))
}
