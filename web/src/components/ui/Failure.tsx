import { CircleAlert, RotateCw } from 'lucide-react'
import { Button } from './Button'
import styles from './Failure.module.css'

/** Props of {@link Failure}. */
export interface FailureProps {
  /** What failed; its message is shown as it is. */
  error: Error
  /** Shows a Retry button; leave it out when retrying cannot help. */
  onRetry?: () => void
}

/** A failed call, announced at once, with a Retry button when retrying can help. */
export function Failure({ error, onRetry }: FailureProps) {
  return (
    <div role="alert" className={styles.failure}>
      <CircleAlert aria-hidden="true" className={styles.icon} />
      <p className={styles.message}>{error.message}</p>
      {onRetry && (
        <Button size="sm" onPress={onRetry}>
          <RotateCw aria-hidden="true" />
          Retry
        </Button>
      )}
    </div>
  )
}
