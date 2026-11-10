import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { ApiError, removeStack } from '../api/platform'
import m from './Managed.module.css'
import { Button } from './ui/Button'
import { Failure } from './ui/Failure'

/**
 * Remove a stack (spec 0006, AC-15): stops its containers and keeps volumes, secrets and the
 * history, so a later apply brings it back with its data. *Delete data too* deletes them for good
 * and asks you to type the name first.
 */
export function RemoveDialog({ name, deleteData: preset = false, onClose }: { name: string; deleteData?: boolean; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const queryClient = useQueryClient()
  const [deleteData, setDeleteData] = useState(preset)
  const [confirmName, setConfirmName] = useState('')
  const remove = useMutation({
    mutationFn: () => removeStack(name, deleteData, confirmName),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['stack', name] })
      void queryClient.invalidateQueries({ queryKey: ['operations', name] })
      dialog.current?.close()
    },
  })

  useEffect(() => {
    dialog.current?.showModal()
  }, [])

  const confirmed = !deleteData || confirmName === name
  const alreadyRemoved = remove.error instanceof ApiError && remove.error.code === 'already_removed'

  return (
    <dialog ref={dialog} className={m.dialog} aria-labelledby="remove-title" onClose={onClose}>
      <h2 id="remove-title">Remove {name}</h2>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          remove.mutate()
        }}
      >
        <p>
          This stops and removes the stack's containers. Its volumes, secrets and history stay, so deploying it again brings it back with
          its data.
        </p>
        <label className={m.check}>
          <input type="checkbox" checked={deleteData} onChange={(e) => setDeleteData(e.target.checked)} /> Delete data too
        </label>
        {deleteData && (
          <div className={m.field}>
            <p className={m.banner} data-tone="danger">
              The volumes and secrets are deleted for good. Placed credentials must be entered again on the next deploy.
            </p>
            <label htmlFor="remove-confirm">
              Type <strong>{name}</strong> to confirm
            </label>
            <input id="remove-confirm" autoComplete="off" required value={confirmName} onChange={(e) => setConfirmName(e.target.value)} />
          </div>
        )}
        {alreadyRemoved && (
          <p role="alert" className={m.banner}>
            The stack is removed already. Choose <em>Delete data too</em> to delete its data.
          </p>
        )}
        {remove.isError && !alreadyRemoved && <Failure error={remove.error} onRetry={() => remove.mutate()} />}
        <div className={m.actions}>
          <Button onPress={() => dialog.current?.close()}>Cancel</Button>
          <Button type="submit" variant="danger" isDisabled={!confirmed || remove.isPending}>
            {remove.isPending ? 'Removing…' : deleteData ? 'Remove and delete data' : 'Remove'}
          </Button>
        </div>
      </form>
    </dialog>
  )
}
