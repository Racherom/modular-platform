import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { type Deployment, type DeploymentSummary, eventsURL, type ModuleState, type Operation, type Page } from '../api/platform'

/**
 * Follows the platform's event stream while a screen is open and writes each update into the
 * query cache: the list of deployments, or one deployment with its modules. A reconnect gets a
 * fresh snapshot, and signed-out sends the editor back to the signed out view.
 */
export function useEvents(deployment?: string) {
  const queryClient = useQueryClient()
  useEffect(() => {
    const source = new EventSource(eventsURL(deployment), { withCredentials: true })
    const on = <T>(name: string, apply: (data: T) => void) =>
      source.addEventListener(name, (e) => apply(JSON.parse((e as MessageEvent<string>).data) as T))

    on<{ deployments: DeploymentSummary[] } | Deployment>('snapshot', (data) => {
      if ('deployments' in data) {
        queryClient.setQueryData<Page<DeploymentSummary>>(['deployments'], (list) => data.deployments.reduce(upsert, list ?? { items: [] }))
      } else queryClient.setQueryData(['deployment', data.name], data)
    })
    on<DeploymentSummary>('deployment', (summary) => {
      queryClient.setQueryData<Page<DeploymentSummary>>(['deployments'], (list) => (list ? upsert(list, summary) : list))
      queryClient.setQueryData<Deployment>(['deployment', summary.name], (d) =>
        d ? { ...d, state: summary.state, latestOperation: summary.latestOperation } : d,
      )
    })
    on<{ deployment: string; operation: Operation }>('operation', ({ deployment: name, operation }) => {
      void queryClient.invalidateQueries({ queryKey: ['operations', name] })
      if (operation.status !== 'running') void queryClient.invalidateQueries({ queryKey: ['deployment', name] })
    })
    on<{ deployment: string; modules: ModuleState[] }>('modules', ({ deployment: name, modules }) => {
      queryClient.setQueryData<Deployment>(['deployment', name], (d) => (d ? { ...d, modules, stateError: undefined } : d))
    })
    on('signed-out', () => {
      source.close()
      void queryClient.invalidateQueries({ queryKey: ['session'] })
    })
    return () => source.close()
  }, [deployment, queryClient])
}

/**
 * Puts a deployment into the loaded pages of the list, sorted by name. One that sorts after the
 * last loaded page is left for that page to bring, so a reconnect never drops pages already shown.
 */
export function upsert(list: Page<DeploymentSummary>, item: DeploymentSummary): Page<DeploymentSummary> {
  const last = list.items.at(-1)
  const known = list.items.some((d) => d.name === item.name)
  if (!known && list.next !== undefined && last && item.name > last.name) return list
  const items = [...list.items.filter((d) => d.name !== item.name), item].sort((a, b) => (a.name < b.name ? -1 : 1))
  return { ...list, items }
}
