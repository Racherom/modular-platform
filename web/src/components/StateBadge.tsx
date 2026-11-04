import type { DeploymentState, ModuleState } from '../api/platform'
import { StatusBadge, type StatusTone } from './ui/StatusBadge'

const tones: Record<ModuleState['state'] | DeploymentState, StatusTone> = {
  running: 'success',
  pending: 'info',
  starting: 'info',
  applying: 'info',
  'rolling-back': 'info',
  removing: 'info',
  'rolled-back': 'warning',
  paused: 'neutral',
  removed: 'neutral',
  unhealthy: 'danger',
  restarting: 'danger',
  exited: 'danger',
  missing: 'danger',
  failed: 'danger',
  unknown: 'danger',
}

/** The badge text of a module: its state, with the exit code once it exited. */
export function moduleLabel(m: ModuleState): string {
  return m.state === 'exited' && m.exitCode !== undefined ? `exited ${m.exitCode}` : m.state
}

/**
 * The restart line beside a module's state: how often Docker restarted it, and the code of its
 * most recent exit (any cause). Empty when it never restarted and no exit is known. An exited
 * module's last exit is its exit code, which the badge shows already.
 */
export function restartLine(m: ModuleState): string {
  const parts: string[] = []
  if (m.restartCount > 0) parts.push(m.restartCount === 1 ? '1 restart' : `${m.restartCount} restarts`)
  if (m.lastExitCode !== undefined && m.state !== 'exited') parts.push(`last exit ${m.lastExitCode}`)
  return parts.join(', ')
}

/**
 * A state as text plus a colored dot on StatusBadge, never color alone (AC-13). A module badge adds its restart count
 * and last exit code, and carries its image and health in its title.
 */
export function StateBadge({ module, deployment }: { module?: ModuleState; deployment?: DeploymentState }) {
  if (module) {
    const restarts = restartLine(module)
    const details = [module.image, module.health && `health ${module.health}`, `restarts ${module.restartCount}`].filter(Boolean).join(', ')
    return (
      <StatusBadge tone={tones[module.state]} title={details} data-testid={`badge-${module.instance}`}>
        {moduleLabel(module)}
        {restarts && ` · ${restarts}`}
      </StatusBadge>
    )
  }
  if (!deployment) return null
  return (
    <StatusBadge tone={tones[deployment]} data-testid="deployment-state">
      {deployment}
    </StatusBadge>
  )
}
