import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useEffect, useRef, useState } from 'react'
import type { Graph } from '../api/client'
import { ApiError, applyStack, isRefusal, type PlacedInput, reviewStack } from '../api/platform'
import m from './Managed.module.css'
import { ProblemList } from './ProblemList'
import { ReviewImages } from './ReviewImages'
import { Button } from './ui/Button'
import { Failure } from './ui/Failure'

/**
 * The review (spec 0006): what an apply of graph would run, the modules with their images, the
 * image versions frozen for the apply (spec 0031), the bindings, the published ports and a field
 * for each placed credential. Deploy applies exactly the reviewed check result and image
 * versions; when either changed since, the dialog reviews again.
 */
export function ReviewDialog({ graph, onClose }: { graph: Graph; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const navigate = useNavigate()
  const [values, setValues] = useState<Record<string, string>>({})
  const [replacing, setReplacing] = useState<Record<string, boolean>>({})
  const [notice, setNotice] = useState('')
  const [checkNewer, setCheckNewer] = useState(false)
  const [update, setUpdate] = useState<string[]>([])
  const review = useQuery({
    queryKey: ['review', graph, checkNewer, update],
    queryFn: () => reviewStack(graph, { checkNewer, update }),
    gcTime: 0,
    placeholderData: (previous) => previous,
  })
  const apply = useMutation({
    mutationFn: () => {
      const placed: PlacedInput[] = (review.data?.placed ?? [])
        .filter((p) => !p.exists || replacing[p.path])
        .map((p) => ({ path: p.path, value: values[p.path] ?? '', replace: p.exists }))
      return applyStack(graph, review.data?.digest ?? '', placed, update)
    },
    onSuccess: () => void navigate({ to: '/stacks/$name', params: { name: graph.name } }),
    onError: (error) => {
      if (error instanceof ApiError && error.code === 'graph_changed') {
        setNotice('The graph or an image version changed since your review. Review it again below.')
        void review.refetch()
      }
    },
  })

  useEffect(() => {
    dialog.current?.showModal()
  }, [])

  const needed = (review.data?.placed ?? []).filter((p) => !p.exists || replacing[p.path])
  const complete = needed.every((p) => (values[p.path] ?? '') !== '')
  const graphChanged = apply.error instanceof ApiError && apply.error.code === 'graph_changed'

  return (
    <dialog ref={dialog} className={m.dialog} aria-labelledby="review-title" onClose={onClose}>
      <div className={m.dialogBody}>
        <h2 id="review-title">Deploy {graph.name}</h2>
        {notice && (
          <p role="status" className={m.banner}>
            {notice}
          </p>
        )}
        {review.isError &&
          (isRefusal(review.error) ? (
            <ProblemList problems={review.error.problems} />
          ) : (
            <Failure error={review.error} onRetry={() => void review.refetch()} />
          ))}
        {review.isPending && <p>Reviewing the graph…</p>}
        {review.data && !review.data.check.ok && <ProblemList problems={review.data.check.problems} />}
        {review.data?.check.ok && (
          <form
            onSubmit={(e) => {
              e.preventDefault()
              apply.mutate()
            }}
          >
            <h3>Modules</h3>
            <ul className={m.list}>
              {review.data.modules.map((m) => (
                <li key={m.instance}>
                  <strong>{m.instance}</strong> runs <code>{m.image}</code>
                </li>
              ))}
            </ul>
            <ReviewImages
              images={review.data.images}
              update={update}
              checking={review.isFetching}
              onCheckNewer={() => setCheckNewer(true)}
              onToggle={(reference, take) =>
                setUpdate((u) => (take ? [...u.filter((r) => r !== reference), reference].sort() : u.filter((r) => r !== reference)))
              }
            />
            <h3>Bindings</h3>
            {review.data.bindings.length === 0 ? (
              <p>None.</p>
            ) : (
              <ul className={m.list}>
                {review.data.bindings.map((b) => (
                  <li key={`${b.consumer}.${b.capability}`}>
                    {b.consumer} uses {b.capability} from {b.provider}
                  </li>
                ))}
              </ul>
            )}
            <h3>Published ports</h3>
            <p>{review.data.publishedPorts.length === 0 ? 'None: nothing of this stack is reachable from outside the host.' : ''}</p>
            {review.data.placed.length > 0 && <h3>Credentials you place</h3>}
            {review.data.placed.map((p) => (
              <div key={p.path} className={m.field}>
                <label htmlFor={`placed-${p.path}`}>
                  {p.instance}: {p.field === 'password' ? 'password' : 'secret key'} for {p.capability}
                  {p.exists && !replacing[p.path] && ' (already set)'}
                </label>
                {p.exists && (
                  <label className={m.check}>
                    <input
                      type="checkbox"
                      checked={replacing[p.path] ?? false}
                      onChange={(e) => setReplacing({ ...replacing, [p.path]: e.target.checked })}
                    />{' '}
                    Replace
                  </label>
                )}
                {(!p.exists || replacing[p.path]) && (
                  <input
                    id={`placed-${p.path}`}
                    type="password"
                    autoComplete="new-password"
                    maxLength={4096}
                    required
                    value={values[p.path] ?? ''}
                    onChange={(e) => setValues({ ...values, [p.path]: e.target.value })}
                  />
                )}
              </div>
            ))}
            {apply.isError &&
              !graphChanged &&
              (isRefusal(apply.error) ? (
                <ProblemList problems={apply.error.problems} />
              ) : (
                <Failure error={apply.error} onRetry={() => apply.mutate()} />
              ))}
            <div className={m.actions}>
              <Button onPress={() => dialog.current?.close()}>Cancel</Button>
              <Button type="submit" variant="primary" isDisabled={!complete || apply.isPending || review.isFetching}>
                {apply.isPending ? 'Deploying…' : 'Deploy'}
              </Button>
            </div>
          </form>
        )}
        {(review.isError || (review.data && !review.data.check.ok)) && (
          <div className={m.actions}>
            <Button onPress={() => dialog.current?.close()}>Close</Button>
          </div>
        )}
      </div>
    </dialog>
  )
}
