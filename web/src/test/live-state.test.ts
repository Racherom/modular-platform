import { describe, expect, it } from 'vitest'
import type { DeploymentSummary, ModuleState, Page } from '../api/platform'
import { moduleLabel, restartLine } from '../components/StateBadge'
import { upsert } from '../hooks/useEvents'

const module = (m: Partial<ModuleState>): ModuleState => ({ instance: 'web', module: 'whoami', state: 'running', restartCount: 0, ...m })

describe('module badge (AC-13)', () => {
  it('shows a crash Docker restarted as a restart count with the last exit code', () => {
    const m = module({ restartCount: 1, lastExitCode: 137 })
    expect(moduleLabel(m)).toBe('running')
    expect(restartLine(m)).toBe('1 restart, last exit 137')
  })

  it('counts restarts in the plural', () => {
    expect(restartLine(module({ restartCount: 3 }))).toBe('3 restarts')
  })

  it('shows no last exit code when none is known, never 0', () => {
    expect(restartLine(module({ restartCount: 2 }))).toBe('2 restarts')
    expect(restartLine(module({}))).toBe('')
  })

  it('shows a known clean exit as 0', () => {
    expect(restartLine(module({ lastExitCode: 0 }))).toBe('last exit 0')
  })

  it('does not repeat the exit code of an exited module', () => {
    const m = module({ state: 'exited', exitCode: 137, lastExitCode: 137 })
    expect(moduleLabel(m)).toBe('exited 137')
    expect(restartLine(m)).toBe('')
  })

  it('keeps the code a restarting module exited with', () => {
    expect(restartLine(module({ state: 'restarting', restartCount: 4, lastExitCode: 1 }))).toBe('4 restarts, last exit 1')
  })
})

const summary = (name: string): DeploymentSummary => ({ name, state: 'running' }) as DeploymentSummary

describe('deployment list updates', () => {
  it('inserts in name order and replaces a known deployment', () => {
    let list: Page<DeploymentSummary> = { items: [summary('a'), summary('c')] }
    list = upsert(list, summary('b'))
    list = upsert(list, { ...summary('a'), state: 'removed' } as DeploymentSummary)
    expect(list.items.map((d) => d.name)).toEqual(['a', 'b', 'c'])
    expect(list.items[0]?.state).toBe('removed')
  })

  it('leaves a new deployment past the loaded pages to the next page', () => {
    const list: Page<DeploymentSummary> = { items: [summary('a'), summary('b')], next: 'b' }
    expect(upsert(list, summary('z'))).toBe(list)
    expect(upsert(list, summary('aa')).items.map((d) => d.name)).toEqual(['a', 'aa', 'b'])
  })

  it('keeps loaded pages when a reconnect sends the first page again', () => {
    const loaded: Page<DeploymentSummary> = { items: [summary('a'), summary('m'), summary('z')] }
    const after = [summary('a'), summary('b')].reduce(upsert, loaded)
    expect(after.items.map((d) => d.name)).toEqual(['a', 'b', 'm', 'z'])
  })
})
