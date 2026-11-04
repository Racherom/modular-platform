/** A failed call, with a retry button when retrying can help. */
export function Failure({ error, onRetry }: { error: Error; onRetry?: () => void }) {
  return (
    <div role="alert" className="failure">
      <p>{error.message}</p>
      {onRetry && (
        <button type="button" onClick={onRetry}>
          Retry
        </button>
      )}
    </div>
  )
}
