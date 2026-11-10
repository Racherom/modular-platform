import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { eventsURL, type ModuleState, type Operation, type Page, type Stack, type StackSummary } from '../api/platform'

/**
 * Follows the platform's event stream while a screen is open and writes each update into the
 * query cache: the list of stacks, or one stack with its modules. A reconnect gets a
 * fresh snapshot, and signed-out sends the editor back to the signed out view.
 */
export function useEvents(stack?: string) {
  const queryClient = useQueryClient()
  useEffect(() => {
    const source = new EventSource(eventsURL(stack), { withCredentials: true })
    const on = <T>(name: string, apply: (data: T) => void) =>
      source.addEventListener(name, (e) => apply(JSON.parse((e as MessageEvent<string>).data) as T))

    on<{ stacks: StackSummary[] } | Stack>('snapshot', (data) => {
      if ('stacks' in data) {
        queryClient.setQueryData<Page<StackSummary>>(['stacks'], (list) => data.stacks.reduce(upsert, list ?? { items: [] }))
      } else queryClient.setQueryData(['stack', data.name], data)
    })
    on<StackSummary>('stack', (summary) => {
      queryClient.setQueryData<Page<StackSummary>>(['stacks'], (list) => (list ? upsert(list, summary) : list))
      queryClient.setQueryData<Stack>(['stack', summary.name], (d) =>
        d ? { ...d, state: summary.state, latestOperation: summary.latestOperation } : d,
      )
    })
    on<{ stack: string; operation: Operation }>('operation', ({ stack: name, operation }) => {
      void queryClient.invalidateQueries({ queryKey: ['operations', name] })
      if (operation.status !== 'running') void queryClient.invalidateQueries({ queryKey: ['stack', name] })
    })
    on<{ stack: string; modules: ModuleState[] }>('modules', ({ stack: name, modules }) => {
      queryClient.setQueryData<Stack>(['stack', name], (d) => (d ? { ...d, modules, stateError: undefined } : d))
    })
    on('signed-out', () => {
      source.close()
      void queryClient.invalidateQueries({ queryKey: ['session'] })
    })
    return () => source.close()
  }, [stack, queryClient])
}

/**
 * Puts a stack into the loaded pages of the list, sorted by name. One that sorts after the
 * last loaded page is left for that page to bring, so a reconnect never drops pages already shown.
 */
export function upsert(list: Page<StackSummary>, item: StackSummary): Page<StackSummary> {
  const last = list.items.at(-1)
  const known = list.items.some((d) => d.name === item.name)
  if (!known && list.next !== undefined && last && item.name > last.name) return list
  const items = [...list.items.filter((d) => d.name !== item.name), item].sort((a, b) => (a.name < b.name ? -1 : 1))
  return { ...list, items }
}
