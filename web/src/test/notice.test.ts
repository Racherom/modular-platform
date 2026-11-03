import { describe, expect, it } from 'vitest'
import type { Deployment, Operation } from '../api/platform'
import { deploymentNotice } from '../lib/notice'

const graph = { apiVersion: 'modular-platform/v0alpha1', kind: 'Graph', name: 'sample', modules: {} } as Deployment['graph']

function op(more: Partial<Operation>): Operation {
  return {
    id: 'op-2',
    action: 'apply',
    status: 'failed',
    startedAt: '2026-11-03T10:00:00Z',
    startedBy: 'admin@example.com',
    deleteData: false,
    placedSupplied: [],
    ...more,
  }
}

function deployment(state: Deployment['state'], latest: Partial<Operation>, more: Partial<Deployment> = {}): Deployment {
  return {
    name: 'sample',
    project: 'mp-sample',
    createdAt: '2026-11-03T09:00:00Z',
    state,
    latestOperation: op(latest),
    latestApplyGraph: graph,
    ...more,
  }
}

describe('deploymentNotice', () => {
  it('says nothing while running or applying', () => {
    expect(deploymentNotice(deployment('running', { status: 'succeeded' }))).toBeUndefined()
    expect(deploymentNotice(deployment('applying', { status: 'running' }))).toBeUndefined()
  })

  it('keeps the previous version running after a failure before up', () => {
    const n = deploymentNotice(deployment('failed', { step: 'pull' }))
    expect(n?.text).toContain('previous version keeps running')
    expect(n).toMatchObject({ tone: 'error', applyAgain: true, removeAgain: false })
  })

  it('says no earlier version exists after a failure at up without a rollback', () => {
    expect(deploymentNotice(deployment('failed', { step: 'up' }))?.text).toContain('No earlier version exists')
  })

  it('names a failed rollback', () => {
    expect(deploymentNotice(deployment('failed', { step: 'up', rollbackOf: 'op-1' }))?.text).toContain('Returning to the last version')
  })

  it('offers Remove again beside Apply again after a failed or unknown remove', () => {
    for (const state of ['failed', 'unknown'] as const) {
      expect(deploymentNotice(deployment(state, { action: 'remove', status: state, step: 'down' }))).toMatchObject({
        applyAgain: true,
        removeAgain: true,
      })
    }
  })

  it('hides Apply again without a graph to review', () => {
    expect(deploymentNotice(deployment('unknown', { status: 'unknown' }, { latestApplyGraph: undefined }))?.applyAgain).toBe(false)
  })

  it('tells what failed when rolled back', () => {
    const n = deploymentNotice(
      deployment('rolled-back', { status: 'succeeded', rollbackOf: 'op-1' }, { rolledBackFrom: op({ id: 'op-1', step: 'up' }) }),
    )
    expect(n).toMatchObject({ tone: 'info', applyAgain: false })
    expect(n?.text).toContain('failed at step up')
  })
})
