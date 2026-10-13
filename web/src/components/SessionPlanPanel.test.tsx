import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SessionPlanPanel } from './SessionPlanPanel'

const mocks = vi.hoisted(() => ({
  listFiles: vi.fn(),
  readFile: vi.fn(),
  toggleFileCheckbox: vi.fn(),
  t: (key: string) => key,
}))

vi.mock('@/lib/api', () => ({
  listFiles: mocks.listFiles,
  readFile: mocks.readFile,
  toggleFileCheckbox: mocks.toggleFileCheckbox,
}))

vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({ t: mocks.t }),
}))

vi.mock('@/components/ui/markdown-preview', () => ({
  MarkdownPreview: ({ content }: { content: string }) => <div>{content}</div>,
}))

function file(path: string, modTime: string) {
  return {
    name: path.split('/').at(-1)!,
    path,
    size: 10,
    isDir: false,
    ext: '.md',
    modTime,
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.toggleFileCheckbox.mockResolvedValue({ status: 'ok' })
})

describe('SessionPlanPanel', () => {
  it('orders plans by modification time and initially opens the newest plan', async () => {
    mocks.listFiles.mockResolvedValue([
      file('/plans/older.md', '2026-10-12T08:00:00Z'),
      file('/plans/newest.md', '2026-10-13T08:00:00Z'),
    ])
    mocks.readFile.mockImplementation(async (path: string) => `content:${path}`)

    render(<SessionPlanPanel plans={['/plans/older.md', '/plans/newest.md']} />)

    expect(await screen.findByText('content:/plans/newest.md')).toBeInTheDocument()
    const tabs = screen.getAllByRole('button').filter((button) => button.title.startsWith('/plans/'))
    expect(tabs.map((button) => button.title)).toEqual(['/plans/newest.md', '/plans/older.md'])
  })

  it('keeps old content visible during same-plan refresh and ignores stale responses', async () => {
    const stale = deferred<string>()
    const latest = deferred<string>()
    mocks.listFiles.mockResolvedValue([file('/plans/current.md', '2026-10-13T08:00:00Z')])
    mocks.readFile
      .mockResolvedValueOnce('old content')
      .mockReturnValueOnce(stale.promise)
      .mockReturnValueOnce(latest.promise)

    const { rerender } = render(<SessionPlanPanel plans={['/plans/current.md']} />)
    expect(await screen.findByText('old content')).toBeInTheDocument()

    rerender(<SessionPlanPanel plans={['/plans/current.md']} />)
    await waitFor(() => expect(mocks.readFile).toHaveBeenCalledTimes(2))
    expect(screen.getByText('old content')).toBeInTheDocument()

    rerender(<SessionPlanPanel plans={['/plans/current.md']} />)
    await waitFor(() => expect(mocks.readFile).toHaveBeenCalledTimes(3))
    expect(screen.getByText('old content')).toBeInTheDocument()

    latest.resolve('latest content')
    expect(await screen.findByText('latest content')).toBeInTheDocument()
    stale.resolve('stale content')
    await Promise.resolve()
    expect(screen.queryByText('stale content')).not.toBeInTheDocument()
    expect(screen.getByText('latest content')).toBeInTheDocument()
  })

  it('does not supersede an in-flight selected-plan load when metadata refresh resolves', async () => {
    const refreshedMetadata = deferred<ReturnType<typeof file>[]>()
    const selectedContent = deferred<string>()
    mocks.listFiles
      .mockResolvedValueOnce([
        file('/plans/a.md', '2026-10-12T08:00:00Z'),
        file('/plans/b.md', '2026-10-13T08:00:00Z'),
      ])
      .mockReturnValueOnce(refreshedMetadata.promise)
    mocks.readFile
      .mockResolvedValueOnce('content:/plans/b.md')
      .mockReturnValueOnce(selectedContent.promise)

    const { rerender } = render(<SessionPlanPanel plans={['/plans/a.md', '/plans/b.md']} />)
    expect(await screen.findByText('content:/plans/b.md')).toBeInTheDocument()

    fireEvent.click(screen.getByTitle('/plans/a.md'))
    await waitFor(() => expect(mocks.readFile).toHaveBeenCalledWith('/plans/a.md'))

    rerender(<SessionPlanPanel plans={['/plans/a.md', '/plans/b.md']} />)
    refreshedMetadata.resolve([
      file('/plans/a.md', '2026-10-14T08:00:00Z'),
      file('/plans/b.md', '2026-10-13T08:00:00Z'),
    ])
    await waitFor(() => expect(mocks.listFiles).toHaveBeenCalledTimes(2))
    expect(mocks.readFile).toHaveBeenCalledTimes(2)

    selectedContent.resolve('content:/plans/a.md')
    expect(await screen.findByText('content:/plans/a.md')).toBeInTheDocument()
  })

  it('ignores metadata that resolves after unmount', async () => {
    const staleMetadata = deferred<ReturnType<typeof file>[]>()
    mocks.listFiles
      .mockReturnValueOnce(staleMetadata.promise)
      .mockResolvedValueOnce([
        file('/plans/a.md', '2026-10-12T08:00:00Z'),
        file('/plans/b.md', '2026-10-13T08:00:00Z'),
      ])
    mocks.readFile.mockResolvedValue('content:/plans/a.md')
    const onSelectedPlanChange = vi.fn()

    const first = render(
      <SessionPlanPanel
        plans={['/plans/a.md', '/plans/b.md']}
        onSelectedPlanChange={onSelectedPlanChange}
      />,
    )
    first.unmount()

    render(
      <SessionPlanPanel
        plans={['/plans/a.md', '/plans/b.md']}
        selectedPlan="/plans/a.md"
        onSelectedPlanChange={onSelectedPlanChange}
      />,
    )
    expect(await screen.findByText('content:/plans/a.md')).toBeInTheDocument()
    onSelectedPlanChange.mockClear()

    staleMetadata.resolve([
      file('/plans/a.md', '2026-10-12T08:00:00Z'),
      file('/plans/b.md', '2026-10-13T08:00:00Z'),
    ])
    await Promise.resolve()
    await Promise.resolve()
    expect(onSelectedPlanChange).not.toHaveBeenCalled()
  })

  it('preserves a manual selection across reordering and falls back when it disappears', async () => {
    mocks.listFiles
      .mockResolvedValueOnce([
        file('/plans/a.md', '2026-10-12T08:00:00Z'),
        file('/plans/b.md', '2026-10-13T08:00:00Z'),
      ])
      .mockResolvedValueOnce([
        file('/plans/a.md', '2026-10-14T08:00:00Z'),
        file('/plans/b.md', '2026-10-13T08:00:00Z'),
      ])
      .mockResolvedValueOnce([file('/plans/b.md', '2026-10-13T08:00:00Z')])
    mocks.readFile.mockImplementation(async (path: string) => `content:${path}`)
    const onSelectedPlanChange = vi.fn()

    const { rerender } = render(
      <SessionPlanPanel
        plans={['/plans/a.md', '/plans/b.md']}
        onSelectedPlanChange={onSelectedPlanChange}
      />,
    )
    expect(await screen.findByText('content:/plans/b.md')).toBeInTheDocument()

    fireEvent.click(screen.getByTitle('/plans/a.md'))
    expect(await screen.findByText('content:/plans/a.md')).toBeInTheDocument()

    rerender(
      <SessionPlanPanel
        plans={['/plans/a.md', '/plans/b.md']}
        onSelectedPlanChange={onSelectedPlanChange}
      />,
    )
    await waitFor(() => expect(mocks.listFiles).toHaveBeenCalledTimes(2))
    expect(screen.getByText('content:/plans/a.md')).toBeInTheDocument()

    rerender(
      <SessionPlanPanel plans={['/plans/b.md']} onSelectedPlanChange={onSelectedPlanChange} />,
    )
    expect(await screen.findByText('content:/plans/b.md')).toBeInTheDocument()
    expect(onSelectedPlanChange).toHaveBeenLastCalledWith('/plans/b.md')
  })

  it('keeps rendered content when metadata refresh fails and handles an empty plan list', async () => {
    mocks.listFiles
      .mockResolvedValueOnce([file('/plans/current.md', '2026-10-13T08:00:00Z')])
      .mockRejectedValueOnce(new Error('metadata unavailable'))
    mocks.readFile.mockResolvedValue('current content')

    const { rerender } = render(<SessionPlanPanel plans={['/plans/current.md']} />)
    expect(await screen.findByText('current content')).toBeInTheDocument()

    rerender(<SessionPlanPanel plans={['/plans/current.md']} />)
    await waitFor(() => expect(mocks.listFiles).toHaveBeenCalledTimes(2))
    expect(screen.getByText('current content')).toBeInTheDocument()

    rerender(<SessionPlanPanel plans={[]} />)
    expect(await screen.findByText('chat.noPlans')).toBeInTheDocument()
  })

  it('falls back to the remaining plan when metadata refresh fails after deletion', async () => {
    mocks.listFiles
      .mockResolvedValueOnce([
        file('/plans/a.md', '2026-10-13T08:00:00Z'),
        file('/plans/b.md', '2026-10-12T08:00:00Z'),
      ])
      .mockRejectedValueOnce(new Error('metadata unavailable'))
    mocks.readFile.mockImplementation(async (path: string) => `content:${path}`)
    const onSelectedPlanChange = vi.fn()

    const { rerender } = render(
      <SessionPlanPanel
        plans={['/plans/a.md', '/plans/b.md']}
        onSelectedPlanChange={onSelectedPlanChange}
      />,
    )
    expect(await screen.findByText('content:/plans/a.md')).toBeInTheDocument()

    rerender(
      <SessionPlanPanel plans={['/plans/b.md']} onSelectedPlanChange={onSelectedPlanChange} />,
    )

    expect(await screen.findByText('content:/plans/b.md')).toBeInTheDocument()
    expect(screen.queryByTitle('/plans/a.md')).not.toBeInTheDocument()
    expect(onSelectedPlanChange).toHaveBeenLastCalledWith('/plans/b.md')
  })

  it('silently refreshes the selected plan when metadata refresh fails', async () => {
    const refreshedContent = deferred<string>()
    mocks.listFiles
      .mockResolvedValueOnce([file('/plans/current.md', '2026-10-13T08:00:00Z')])
      .mockRejectedValueOnce(new Error('metadata unavailable'))
    mocks.readFile
      .mockResolvedValueOnce('old content')
      .mockReturnValueOnce(refreshedContent.promise)

    const { rerender } = render(<SessionPlanPanel plans={['/plans/current.md']} />)
    expect(await screen.findByText('old content')).toBeInTheDocument()

    rerender(<SessionPlanPanel plans={['/plans/current.md']} />)
    await waitFor(() => expect(mocks.readFile).toHaveBeenCalledTimes(2))
    expect(screen.getByText('old content')).toBeInTheDocument()

    refreshedContent.resolve('new content')
    expect(await screen.findByText('new content')).toBeInTheDocument()
  })
})
