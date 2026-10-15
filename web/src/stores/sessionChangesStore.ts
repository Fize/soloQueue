import { create } from 'zustand'
import type { ChangesResponse, FileChange } from '@/types'

export interface SessionChangesEntry {
  summary: ChangesResponse | null
  loading: boolean
  error: string
  expandedFile: string | null
  diffs: Record<string, FileChange>
  diffLoading: Record<string, boolean>
  diffErrors: Record<string, string>
  scrollTop: number
  summaryRequestGeneration: number
}

interface SessionChangesStore {
  sessions: Record<string, SessionChangesEntry>
  beginSummaryRequest: (sessionId: string) => number
  setSummary: (sessionId: string, summary: ChangesResponse, generation?: number) => void
  setLoading: (sessionId: string, loading: boolean, generation?: number) => void
  setError: (sessionId: string, error: string, generation?: number) => void
  setExpandedFile: (sessionId: string, path: string | null) => void
  setDiff: (sessionId: string, diff: FileChange) => void
  setDiffLoading: (sessionId: string, path: string, loading: boolean) => void
  setDiffError: (sessionId: string, path: string, error: string) => void
  setScrollTop: (sessionId: string, scrollTop: number) => void
}

export function emptySessionChangesEntry(): SessionChangesEntry {
  return {
    summary: null,
    loading: false,
    error: '',
    expandedFile: null,
    diffs: {},
    diffLoading: {},
    diffErrors: {},
    scrollTop: 0,
    summaryRequestGeneration: 0,
  }
}

export const useSessionChangesStore = create<SessionChangesStore>((set) => {
  const update = (
    sessionId: string,
    updater: (entry: SessionChangesEntry) => SessionChangesEntry
  ) =>
    set((state) => ({
      sessions: {
        ...state.sessions,
        [sessionId]: updater(state.sessions[sessionId] ?? emptySessionChangesEntry()),
      },
    }))

  return {
    sessions: {},
    beginSummaryRequest: (sessionId) => {
      let generation = 0
      update(sessionId, (entry) => {
        generation = entry.summaryRequestGeneration + 1
        return { ...entry, summaryRequestGeneration: generation }
      })
      return generation
    },
    setSummary: (sessionId, summary, generation) =>
      update(sessionId, (entry) => {
        if (generation !== undefined && entry.summaryRequestGeneration !== generation) return entry
        const revisions = new Map(summary.changes.map((change) => [change.path, change.revision]))
        const previousRevisions = new Map(
          entry.summary?.changes.map((change) => [change.path, change.revision]) ?? []
        )
        const diffs = Object.fromEntries(
          Object.entries(entry.diffs).filter(
            ([path, diff]) => revisions.get(path) === diff.revision
          )
        )
        const expandedFile =
          entry.expandedFile && revisions.has(entry.expandedFile) ? entry.expandedFile : null
        const diffLoading = Object.fromEntries(
          Object.entries(entry.diffLoading).filter(([path]) => revisions.has(path))
        )
        const diffErrors = Object.fromEntries(
          Object.entries(entry.diffErrors).filter(
            ([path]) => revisions.get(path) === previousRevisions.get(path)
          )
        )
        return { ...entry, summary, diffs, diffLoading, diffErrors, expandedFile }
      }),
    setLoading: (sessionId, loading, generation) =>
      update(sessionId, (entry) =>
        generation === undefined || entry.summaryRequestGeneration === generation
          ? { ...entry, loading }
          : entry
      ),
    setError: (sessionId, error, generation) =>
      update(sessionId, (entry) =>
        generation === undefined || entry.summaryRequestGeneration === generation
          ? { ...entry, error }
          : entry
      ),
    setExpandedFile: (sessionId, expandedFile) =>
      update(sessionId, (entry) => ({ ...entry, expandedFile })),
    setDiff: (sessionId, diff) =>
      update(sessionId, (entry) => ({
        ...entry,
        diffs: { ...entry.diffs, [diff.path]: diff },
        diffErrors: { ...entry.diffErrors, [diff.path]: '' },
      })),
    setDiffLoading: (sessionId, path, loading) =>
      update(sessionId, (entry) => ({
        ...entry,
        diffLoading: { ...entry.diffLoading, [path]: loading },
      })),
    setDiffError: (sessionId, path, error) =>
      update(sessionId, (entry) => ({
        ...entry,
        diffErrors: { ...entry.diffErrors, [path]: error },
      })),
    setScrollTop: (sessionId, scrollTop) => update(sessionId, (entry) => ({ ...entry, scrollTop })),
  }
})
