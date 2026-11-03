import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { fetchDeployments } from '../api/platform'
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
  const list = useQuery({ queryKey: ['deployments'], queryFn: fetchDeployments })
  useEvents()
  if (list.isError) return <Failure error={list.error} onRetry={() => void list.refetch()} />
  if (!list.data) return <p className="screen">Loading the deployments…</p>
  return (
    <div className="screen">
      <div className="toolbar">
        <h1>Deployments</h1>
      </div>
      {list.data.length === 0 ? (
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
            {list.data.map((d) => (
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
    </div>
  )
}
