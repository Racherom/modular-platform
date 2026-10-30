/** A failed call, with a retry button. */
export function Failure({ error, onRetry }: { error: Error; onRetry: () => void }) {
  return (
    <div role="alert" className="failure">
      <p>{error.message}</p>
      <button type="button" onClick={onRetry}>
        Retry
      </button>
    </div>
  )
}
