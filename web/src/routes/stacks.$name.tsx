import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import type { Graph } from '../api/client'
import { fetchOperations, fetchStack, type ModuleState, type Operation, type Stack } from '../api/platform'
import { useCatalog } from '../components/GraphScreen'
import { GraphView } from '../components/GraphView'
import m from '../components/Managed.module.css'
import { RemoveDialog } from '../components/RemoveDialog'
import { ReviewDialog } from '../components/ReviewDialog'
import { StateBadge } from '../components/StateBadge'
import { Button } from '../components/ui/Button'
import { Failure } from '../components/ui/Failure'
import { useEvents } from '../hooks/useEvents'
import { mode } from '../lib/config'
import { stackNotice } from '../lib/notice'
import { OnlyManaged } from './stacks.index'

export const Route = createFileRoute('/stacks/$name')({ component: StackRoute })

function StackRoute() {
  const { name } = Route.useParams()
  if (mode !== 'managed') return <OnlyManaged />
  return <StackScreen name={name} />
}

/** One stack: its graph with a live badge per module, the running step, and the history. */
function StackScreen({ name }: { name: string }) {
  const stack = useQuery({ queryKey: ['stack', name], queryFn: () => fetchStack(name) })
  const history = useInfiniteQuery({
    queryKey: ['operations', name],
    queryFn: ({ pageParam }) => fetchOperations(name, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next,
  })
  const catalog = useCatalog()
  const [removing, setRemoving] = useState<{ deleteData: boolean }>()
  useEvents(name)
  const states = useMemo(() => byInstance(stack.data?.modules), [stack.data?.modules])

  if (stack.isError) return <Failure error={stack.error} onRetry={() => void stack.refetch()} />
  if (!stack.data || !catalog.data) return <p className={m.loading}>Loading {name}…</p>
  const d = stack.data
  const running = d.latestOperation?.status === 'running'
  return (
    <div className={m.screen}>
      <div className={m.heading}>
        <h1>{d.name}</h1>
        <StateBadge stack={d.state} />
        <Button size="sm" variant="danger" isDisabled={running} onPress={() => setRemoving({ deleteData: false })}>
          Remove
        </Button>
      </div>
      <Progress stack={d} onRemoveAgain={(deleteData) => setRemoving({ deleteData })} />
      {d.stateError && (
        <p role="alert" className={m.banner} data-tone="danger">
          The live state could not be read: {d.stateError}
        </p>
      )}
      {d.graph ? (
        <div className={m.workspace}>
          <div className={m.graph}>
            <GraphView graph={d.graph} catalog={catalog.data} problems={[]} states={states} />
          </div>
          <ModuleList modules={d.modules ?? []} />
        </div>
      ) : (
        <p>No version of this stack has run yet.</p>
      )}
      <section aria-labelledby="history-title">
        <h2 id="history-title">History</h2>
        {history.data && (
          <ol className={m.history}>
            {history.data.pages
              .flatMap((p) => p.items)
              .map((op) => (
                <li key={op.id} id={`operation-${op.id}`}>
                  {new Date(op.startedAt).toLocaleString()}: {operationLabel(op)} by {op.startedBy}, <strong>{op.status}</strong>
                  {op.step && ` at ${op.step}`}
                  {op.error && <pre className={m.error}>{op.error}</pre>}
                </li>
              ))}
          </ol>
        )}
        {history.hasNextPage && (
          <p>
            <Button size="sm" isDisabled={history.isFetchingNextPage} onPress={() => void history.fetchNextPage()}>
              {history.isFetchingNextPage ? 'Loading…' : 'Show older operations'}
            </Button>
          </p>
        )}
        {history.isError && <Failure error={history.error} onRetry={() => void history.refetch()} />}
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
function Progress({ stack: d, onRemoveAgain }: { stack: Stack; onRemoveAgain: (deleteData: boolean) => void }) {
  const queryClient = useQueryClient()
  const [reviewing, setReviewing] = useState<Graph>()
  // Apply again reviews the newest apply's graph as it is now, never a copy another tab outdated
  const applyAgain = useMutation({
    mutationFn: () => fetchStack(d.name),
    onSuccess: (fresh) => {
      queryClient.setQueryData(['stack', d.name], fresh)
      if (fresh.latestApplyGraph) setReviewing(fresh.latestApplyGraph)
    },
  })
  const op = d.latestOperation
  const notice = stackNotice(d)
  let text = ''
  if (op?.status === 'running') {
    const doing = op.action === 'remove' ? 'Removing' : op.rollbackOf ? 'Returning to the last version that worked' : 'Applying'
    text = `${doing}: step ${op.step ?? 'prepare'}.`
  }
  return (
    <>
      <p aria-live="polite" className={m.progress}>
        {text}
      </p>
      {notice && (
        <div
          role={notice.tone === 'error' ? 'alert' : 'status'}
          className={m.banner}
          data-tone={notice.tone === 'error' ? 'danger' : undefined}
        >
          <p>
            {notice.text}
            {d.rolledBackFrom && (
              <>
                {' '}
                <a href={`#operation-${d.rolledBackFrom.id}`}>See what failed.</a>
              </>
            )}
          </p>
          {(d.rolledBackFrom?.error ?? op?.error) && <pre className={m.error}>{d.rolledBackFrom?.error ?? op?.error}</pre>}
          {(notice.applyAgain || notice.removeAgain) && (
            <div className={m.actions}>
              {notice.removeAgain && (
                <Button size="sm" variant="danger" onPress={() => onRemoveAgain(op?.deleteData ?? false)}>
                  Remove again
                </Button>
              )}
              {notice.applyAgain && (
                <Button size="sm" variant="primary" isDisabled={applyAgain.isPending} onPress={() => applyAgain.mutate()}>
                  Apply again
                </Button>
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
    <aside className={m.modules} aria-label="Modules">
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
