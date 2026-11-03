import type { Deployment } from '../api/platform'

/** What the deployment screen says about the latest operation, and what it offers to do next. */
export interface Notice {
  tone: 'error' | 'info'
  text: string
  /** Offer *Apply again*: review the newest apply's graph again. */
  applyAgain: boolean
  /** Offer *Remove again*, with *Delete data too* preset to the failed remove's choice. */
  removeAgain: boolean
}

/**
 * The banner for a deployment, derived from its state and latest operation (spec 0006, "State
 * transitions"), or undefined when there is nothing to say. Running, applying and removing show
 * none: the progress line covers them.
 */
export function deploymentNotice(d: Deployment): Notice | undefined {
  const op = d.latestOperation
  if (!op) return undefined
  const step = op.step ?? 'prepare'
  const applyAgain = d.latestApplyGraph !== undefined
  const remove = op.action === 'remove'
  switch (d.state) {
    case 'rolled-back': {
      const from = d.rolledBackFrom
      const failedAt = from ? ` at step ${from.step ?? 'prepare'}` : ''
      return {
        tone: 'info',
        text: `A change failed${failedAt}, so the platform returned to the last version that worked.`,
        applyAgain: false,
        removeAgain: false,
      }
    }
    case 'failed': {
      let text: string
      if (remove) text = `The last remove failed at step ${step} and did not finish.`
      else if (op.rollbackOf) text = `Returning to the last version that worked failed at step ${step}.`
      else if (step === 'prepare' || step === 'pull') text = `The last apply failed at step ${step}. The previous version keeps running.`
      else text = `The last apply failed at step ${step}. No earlier version exists to return to.`
      return { tone: 'error', text, applyAgain, removeAgain: remove }
    }
    case 'unknown':
      return {
        tone: 'error',
        text: remove
          ? 'The platform lost track of the last remove, so it may not have finished.'
          : 'The platform lost track of the last apply, so it did not return to an earlier version.',
        applyAgain,
        removeAgain: remove,
      }
    default:
      return undefined
  }
}
