import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useEffect, useRef, useState } from 'react'
import type { Graph } from '../api/client'
import { ApiError, applyDeployment, type PlacedInput, reviewDeployment } from '../api/platform'
import { Failure } from './Failure'
import { ProblemList } from './ProblemList'

/**
 * Plan review (spec 0006): what an apply of graph would run, the modules with their images, the
 * bindings, the published ports and a field for each placed credential. Deploy applies exactly
 * the reviewed plan; when the plan changed since, the dialog reviews again.
 */
export function ReviewDialog({ graph, onClose }: { graph: Graph; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const navigate = useNavigate()
  const [values, setValues] = useState<Record<string, string>>({})
  const [replacing, setReplacing] = useState<Record<string, boolean>>({})
  const [notice, setNotice] = useState('')
  const review = useQuery({ queryKey: ['review', graph], queryFn: () => reviewDeployment(graph), gcTime: 0 })
  const apply = useMutation({
    mutationFn: () => {
      const placed: PlacedInput[] = (review.data?.placed ?? [])
        .filter((p) => !p.exists || replacing[p.path])
        .map((p) => ({ path: p.path, value: values[p.path] ?? '', replace: p.exists }))
      return applyDeployment(graph, review.data?.plan.digest ?? '', placed)
    },
    onSuccess: () => void navigate({ to: '/deployments/$name', params: { name: graph.name } }),
    onError: (error) => {
      if (error instanceof ApiError && error.code === 'plan_changed') {
        setNotice('The plan changed since you reviewed it, for example after a platform upgrade. Review it again below.')
        void review.refetch()
      }
    },
  })

  useEffect(() => {
    dialog.current?.showModal()
  }, [])

  const needed = (review.data?.placed ?? []).filter((p) => !p.exists || replacing[p.path])
  const complete = needed.every((p) => (values[p.path] ?? '') !== '')
  const planChanged = apply.error instanceof ApiError && apply.error.code === 'plan_changed'

  return (
    <dialog ref={dialog} className="review" aria-labelledby="review-title" onClose={onClose}>
      <h2 id="review-title">Deploy {graph.name}</h2>
      {notice && (
        <p role="status" className="banner">
          {notice}
        </p>
      )}
      {review.isError && <Failure error={review.error} onRetry={() => void review.refetch()} />}
      {review.isPending && <p>Reviewing the plan…</p>}
      {review.data && !review.data.plan.ok && <ProblemList problems={review.data.plan.problems} />}
      {review.data?.plan.ok && (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            apply.mutate()
          }}
        >
          <h3>Modules</h3>
          <ul className="review__list">
            {review.data.modules.map((m) => (
              <li key={m.instance}>
                <strong>{m.instance}</strong> runs <code>{m.image}</code>
              </li>
            ))}
          </ul>
          <h3>Bindings</h3>
          {review.data.bindings.length === 0 ? (
            <p>None.</p>
          ) : (
            <ul className="review__list">
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
            <div key={p.path} className="review__field">
              <label htmlFor={`placed-${p.path}`}>
                {p.instance}: {p.field === 'password' ? 'password' : 'secret key'} for {p.capability}
                {p.exists && !replacing[p.path] && ' (already set)'}
              </label>
              {p.exists && (
                <label>
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
          {apply.isError && !planChanged && <Failure error={apply.error} onRetry={() => apply.mutate()} />}
          <div className="review__actions">
            <button type="button" onClick={() => dialog.current?.close()}>
              Cancel
            </button>
            <button type="submit" disabled={!complete || apply.isPending || review.isFetching}>
              {apply.isPending ? 'Deploying…' : 'Deploy'}
            </button>
          </div>
        </form>
      )}
      {(review.isError || (review.data && !review.data.plan.ok)) && (
        <div className="review__actions">
          <button type="button" onClick={() => dialog.current?.close()}>
            Close
          </button>
        </div>
      )}
    </dialog>
  )
}
