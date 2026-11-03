import type { DeploymentState, ModuleState } from '../api/platform'

/** The badge text of a module: its state, with the exit code once it exited. */
export function moduleLabel(m: ModuleState): string {
  return m.state === 'exited' && m.exitCode !== undefined ? `exited ${m.exitCode}` : m.state
}

/**
 * A state as text plus colour, never colour alone (AC-13). A module badge carries its image,
 * health and restart count in its title.
 */
export function StateBadge({ module, deployment }: { module?: ModuleState; deployment?: DeploymentState }) {
  if (module) {
    const details = [module.image, module.health && `health ${module.health}`, `restarts ${module.restartCount}`].filter(Boolean).join(', ')
    return (
      <span className={`badge badge--${module.state}`} title={details} data-testid={`badge-${module.instance}`}>
        {moduleLabel(module)}
        {module.restartCount > 0 && ` · ${module.restartCount} restarts`}
      </span>
    )
  }
  return <span className={`badge badge--${deployment}`}>{deployment}</span>
}
