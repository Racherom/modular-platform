import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { type DeploymentSummary, fetchDeployments, type Page } from '../api/platform'
import { Failure } from '../components/Failure'
import { StateBadge } from '../components/StateBadge'
import { useEvents } from '../hooks/useEvents'
import { mode } from '../lib/config'

export const Route = createFileRoute('/deployments/')({ component: Deployments })

function Deployments() {
  if (mode !== 'managed') return <OnlyManaged />
  return <DeploymentList />
}

export function OnlyManaged() {
  return (
    <div className="screen empty">
      <h1>Deployments live in the managed platform</h1>
      <p>
        The online tool stores nothing. <Link to="/">Go to the editor</Link> and download a zip instead.
      </p>
    </div>
  )
}

function DeploymentList() {
  const queryClient = useQueryClient()
  const list = useQuery({ queryKey: ['deployments'], queryFn: () => fetchDeployments() })
  useEvents()
  // the next page joins the loaded ones in the cache, which the event stream keeps current
  const more = useMutation({
    mutationFn: (cursor: string) => fetchDeployments(cursor),
    onSuccess: (page) =>
      queryClient.setQueryData<Page<DeploymentSummary>>(['deployments'], (list) => ({
        items: [...(list?.items ?? []), ...page.items.filter((d) => !list?.items.some((l) => l.name === d.name))],
        ...(page.next ? { next: page.next } : {}),
      })),
  })
  if (list.isError) return <Failure error={list.error} onRetry={() => void list.refetch()} />
  if (!list.data) return <p className="screen">Loading the deployments…</p>
  return (
    <div className="screen">
      <div className="toolbar">
        <h1>Deployments</h1>
      </div>
      {list.data.items.length === 0 ? (
        <p>
          Nothing is deployed yet. Open a graph in the <Link to="/">editor</Link> and choose Deploy.
        </p>
      ) : (
        <table className="deployments">
          <thead>
            <tr>
              <th scope="col">Name</th>
              <th scope="col">State</th>
              <th scope="col">Latest operation</th>
              <th scope="col">Started</th>
            </tr>
          </thead>
          <tbody>
            {list.data.items.map((d) => (
              <tr key={d.name}>
                <td>
                  <Link to="/deployments/$name" params={{ name: d.name }}>
                    {d.name}
                  </Link>
                </td>
                <td>
                  <StateBadge deployment={d.state} />
                </td>
                <td>{d.latestOperation ? `${d.latestOperation.action} ${d.latestOperation.status}` : 'none'}</td>
                <td>{d.latestOperation ? new Date(d.latestOperation.startedAt).toLocaleString() : ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {list.data.next && (
        <p>
          <button type="button" disabled={more.isPending} onClick={() => list.data.next && more.mutate(list.data.next)}>
            {more.isPending ? 'Loading…' : 'Show more deployments'}
          </button>
        </p>
      )}
      {more.isError && <Failure error={more.error} onRetry={() => list.data.next && more.mutate(list.data.next)} />}
    </div>
  )
}
