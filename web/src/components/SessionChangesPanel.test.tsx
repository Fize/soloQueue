import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ChangesResponse, FileChange } from '@/types'
import { useSessionChangesStore } from '@/stores/sessionChangesStore'
import { SessionChangesPanel } from './SessionChangesPanel'

const mocks = vi.hoisted(() => ({
  getSessionChanges: vi.fn(),
  getSessionChangeDiff: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  getSessionChanges: mocks.getSessionChanges,
  getSessionChangeDiff: mocks.getSessionChangeDiff,
}))

vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) =>
      values?.ref ? `${key}:${values.ref}` : key,
  }),
}))

const file: FileChange = {
  path: 'src/a.ts',
  status: 'modified',
  additions: 1,
  deletions: 0,
  binary: false,
  revision: 'r1',
}

const summary: ChangesResponse = {
  changes: [file],
  total_additions: 1,
  total_deletions: 0,
  is_git_repo: true,
  base_ref: 'HEAD',
}

beforeEach(() => {
  vi.clearAllMocks()
  useSessionChangesStore.setState({ sessions: {} })
  useSessionChangesStore.getState().setSummary('session-1', summary)
  mocks.getSessionChanges.mockResolvedValue(summary)
  mocks.getSessionChangeDiff.mockResolvedValue({
    ...file,
    hunks: [
      {
        old_start: 0,
        old_lines: 0,
        new_start: 1,
        new_lines: 1,
        lines: [{ type: 'add', content: 'lazy line', new_num: 1 }],
      },
    ],
  })
})

describe('SessionChangesPanel', () => {
  it('renders cached summary immediately and lazily reuses a file diff after remount', async () => {
    const first = render(<SessionChangesPanel sessionId="session-1" />)

    expect(screen.getByText('src/a.ts')).toBeInTheDocument()
    expect(mocks.getSessionChangeDiff).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: /src\/a\.ts/ }))
    expect(await screen.findByText('lazy line')).toBeInTheDocument()
    expect(mocks.getSessionChangeDiff).toHaveBeenCalledTimes(1)
    expect(mocks.getSessionChangeDiff).toHaveBeenCalledWith('session-1', 'src/a.ts', 'r1')

    const firstScroller = screen.getByTestId('changes-scroll')
    firstScroller.scrollTop = 96
    fireEvent.scroll(firstScroller)

    first.unmount()
    render(<SessionChangesPanel sessionId="session-1" />)

    expect(screen.getByText('lazy line')).toBeInTheDocument()
    expect(screen.getByTestId('changes-scroll').scrollTop).toBe(96)
    await waitFor(() => expect(mocks.getSessionChanges).toHaveBeenCalled())
    expect(mocks.getSessionChangeDiff).toHaveBeenCalledTimes(1)
  })

  it('refetches an expanded diff when silent summary revalidation changes its revision', async () => {
    const staleDiff: FileChange = {
      ...file,
      hunks: [
        {
          old_start: 1,
          old_lines: 1,
          new_start: 1,
          new_lines: 1,
          lines: [{ type: 'ctx', content: 'old cached line', old_num: 1, new_num: 1 }],
        },
      ],
    }
    useSessionChangesStore.getState().setExpandedFile('session-1', file.path)
    useSessionChangesStore.getState().setDiff('session-1', staleDiff)

    const changedFile = { ...file, revision: 'r2' }
    mocks.getSessionChanges.mockResolvedValue({ ...summary, changes: [changedFile] })
    mocks.getSessionChangeDiff.mockResolvedValue({
      ...changedFile,
      hunks: [
        {
          old_start: 1,
          old_lines: 1,
          new_start: 1,
          new_lines: 1,
          lines: [{ type: 'add', content: 'fresh line', new_num: 1 }],
        },
      ],
    })

    render(<SessionChangesPanel sessionId="session-1" />)

    expect(screen.getByText('old cached line')).toBeInTheDocument()
    expect(await screen.findByText('fresh line')).toBeInTheDocument()
    expect(mocks.getSessionChangeDiff).toHaveBeenCalledTimes(1)
  })

  it('does not automatically retry a failed expanded diff and retries only on click', async () => {
    mocks.getSessionChangeDiff.mockRejectedValueOnce(new Error('diff failed')).mockResolvedValueOnce({
      ...file,
      hunks: [],
    })

    render(<SessionChangesPanel sessionId="session-1" />)
    fireEvent.click(screen.getByRole('button', { name: /src\/a\.ts/ }))

    expect(await screen.findByText('diff failed')).toBeInTheDocument()
    await waitFor(() => expect(mocks.getSessionChangeDiff).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole('button', { name: 'chat.retry' }))
    await waitFor(() => expect(mocks.getSessionChangeDiff).toHaveBeenCalledTimes(2))
  })

  it('ignores an older summary response after a remount starts a newer request', async () => {
    let resolveOld: ((value: ChangesResponse) => void) | undefined
    const oldRequest = new Promise<ChangesResponse>((resolve) => {
      resolveOld = resolve
    })
    const newerFile = { ...file, path: 'src/newer.ts', revision: 'r2' }
    mocks.getSessionChanges
      .mockReturnValueOnce(oldRequest)
      .mockResolvedValueOnce({ ...summary, changes: [newerFile] })

    const first = render(<SessionChangesPanel sessionId="session-1" />)
    first.unmount()
    render(<SessionChangesPanel sessionId="session-1" />)

    expect(await screen.findByText('src/newer.ts')).toBeInTheDocument()
    resolveOld?.({ ...summary, changes: [{ ...file, path: 'src/stale.ts' }] })
    await Promise.resolve()

    expect(screen.queryByText('src/stale.ts')).not.toBeInTheDocument()
    expect(screen.getByText('src/newer.ts')).toBeInTheDocument()
  })
})
