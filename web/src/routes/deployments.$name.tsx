import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useMemo } from 'react'
import { type Deployment, fetchDeployment, fetchOperations, type ModuleState } from '../api/platform'
import { Failure } from '../components/Failure'
import { useCatalog } from '../components/GraphScreen'
import { GraphView } from '../components/GraphView'
import { StateBadge } from '../components/StateBadge'
import { useEvents } from '../hooks/useEvents'
import { mode } from '../lib/config'
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
  useEvents(name)
  const states = useMemo(() => byInstance(deployment.data?.modules), [deployment.data?.modules])

  if (deployment.isError) return <Failure error={deployment.error} onRetry={() => void deployment.refetch()} />
  if (!deployment.data || !catalog.data) return <p className="screen">Loading {name}…</p>
  const d = deployment.data
  return (
    <div className="screen">
      <div className="toolbar">
        <h1>{d.name}</h1>
        <StateBadge deployment={d.state} />
      </div>
      <Progress deployment={d} />
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
              <li key={op.id}>
                {new Date(op.startedAt).toLocaleString()}: {op.action} by {op.startedBy}, <strong>{op.status}</strong>
                {op.step && ` at ${op.step}`}
                {op.error && <pre className="history__error">{op.error}</pre>}
              </li>
            ))}
          </ol>
        )}
      </section>
    </div>
  )
}

/** The running operation's step in a live region, or why the last one failed. */
function Progress({ deployment: d }: { deployment: Deployment }) {
  const op = d.latestOperation
  let text = ''
  if (op?.status === 'running') text = `${op.action === 'apply' ? 'Applying' : 'Removing'}: step ${op.step ?? 'prepare'}.`
  return (
    <>
      <p aria-live="polite" className="progress">
        {text}
      </p>
      {op && (d.state === 'failed' || d.state === 'unknown') && (
        <div role="alert" className="banner banner--error">
          <p>
            {d.state === 'failed'
              ? `The last ${op.action} failed at step ${op.step ?? 'prepare'}.`
              : `The outcome of the last ${op.action} is unknown: the platform lost track of it.`}
            {d.state === 'failed' && (op.step === 'prepare' || op.step === 'pull') && ' The previous version keeps running.'}
          </p>
          {op.error && <pre className="history__error">{op.error}</pre>}
        </div>
      )}
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
