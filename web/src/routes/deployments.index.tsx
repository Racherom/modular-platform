import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { Server } from 'lucide-react'
import { type DeploymentSummary, fetchDeployments, type Page } from '../api/platform'
import m from '../components/Managed.module.css'
import { StateBadge } from '../components/StateBadge'
import { Button } from '../components/ui/Button'
import { EmptyState } from '../components/ui/EmptyState'
import { Failure } from '../components/ui/Failure'
import { useEvents } from '../hooks/useEvents'
import { mode } from '../lib/config'

export const Route = createFileRoute('/deployments/')({ component: Deployments })

function Deployments() {
  if (mode !== 'managed') return <OnlyManaged />
  return <DeploymentList />
}

export function OnlyManaged() {
  return (
    <EmptyState icon={Server} level={1} title="Deployments live in the managed platform">
      <p>
        The online editor stores nothing. <Link to="/">Go to the editor</Link> and download a zip instead.
      </p>
    </EmptyState>
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
  if (!list.data) return <p className={m.loading}>Loading the deployments…</p>
  return (
    <div className={m.screen}>
      <div className={m.heading}>
        <h1>Deployments</h1>
      </div>
      {list.data.items.length === 0 ? (
        <p>
          Nothing is deployed yet. Open a graph in the <Link to="/">editor</Link> and choose Deploy.
        </p>
      ) : (
        <div className={m.tableWrap}>
          <table className={m.table}>
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
        </div>
      )}
      {list.data.next && (
        <p>
          <Button size="sm" isDisabled={more.isPending} onPress={() => list.data.next && more.mutate(list.data.next)}>
            {more.isPending ? 'Loading…' : 'Show more deployments'}
          </Button>
        </p>
      )}
      {more.isError && <Failure error={more.error} onRetry={() => list.data.next && more.mutate(list.data.next)} />}
    </div>
  )
}
