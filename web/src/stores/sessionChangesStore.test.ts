import { beforeEach, describe, expect, it } from 'vitest'
import type { ChangesResponse, FileChange } from '@/types'
import { useSessionChangesStore } from './sessionChangesStore'

function change(path: string, revision: string): FileChange {
  return {
    path,
    status: 'modified',
    additions: 1,
    deletions: 1,
    binary: false,
    revision,
  }
}

function summary(changes: FileChange[]): ChangesResponse {
  return {
    changes,
    total_additions: changes.length,
    total_deletions: changes.length,
    is_git_repo: true,
    base_ref: 'HEAD',
  }
}

beforeEach(() => {
  useSessionChangesStore.setState({ sessions: {} })
})

describe('sessionChangesStore', () => {
  it('preserves per-session UI state and invalidates only stale diff revisions', () => {
    const store = useSessionChangesStore.getState()
    store.setSummary('one', summary([change('a.ts', 'r1'), change('b.ts', 'r1')]))
    store.setExpandedFile('one', 'a.ts')
    store.setScrollTop('one', 128)
    store.setDiff('one', change('a.ts', 'r1'))
    store.setDiff('one', change('b.ts', 'r1'))
    store.setDiffError('one', 'a.ts', 'old revision failed')
    store.setSummary('two', summary([change('other.ts', 'x1')]))

    store.setSummary('one', summary([change('a.ts', 'r2'), change('b.ts', 'r1')]))

    const state = useSessionChangesStore.getState().sessions
    expect(state.one.expandedFile).toBe('a.ts')
    expect(state.one.scrollTop).toBe(128)
    expect(state.one.diffs['a.ts']).toBeUndefined()
    expect(state.one.diffErrors['a.ts']).toBeUndefined()
    expect(state.one.diffs['b.ts']?.revision).toBe('r1')
    expect(state.two.summary?.changes[0].path).toBe('other.ts')
  })

  it('accepts summary request state only from the latest session-scoped generation', () => {
    const store = useSessionChangesStore.getState()
    const first = store.beginSummaryRequest('one')
    store.setLoading('one', true, first)
    const second = store.beginSummaryRequest('one')

    store.setSummary('one', summary([change('stale.ts', 'r1')]), first)
    store.setError('one', 'stale error', first)
    store.setLoading('one', false, first)
    store.setSummary('one', summary([change('fresh.ts', 'r2')]), second)
    store.setLoading('one', false, second)

    const entry = useSessionChangesStore.getState().sessions.one
    expect(entry.summary?.changes[0].path).toBe('fresh.ts')
    expect(entry.error).toBe('')
    expect(entry.loading).toBe(false)
  })
})
