import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { SessionFilePanel } from './SessionFilePanel'
import {
  INSPECTOR_EXPANDED_DIRECTORIES_MAX,
  useInspectorStateStore,
} from '@/stores/inspectorStateStore'
import { filesCacheKey, useSessionFilesStore } from '@/stores/sessionFilesStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { APIError } from '@/transport/http-client'

const mocks = vi.hoisted(() => ({
  listFilesConditional: vi.fn(),
  previewProps: {
    current: null as null | {
      path: string | null
      refreshRequest: { sequence: number; force: boolean }
      onError?: (path: string, error: unknown) => void
    },
  },
}))

vi.mock('@/lib/api', () => ({
  listFilesConditional: mocks.listFilesConditional,
}))

vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('./FileContentView', () => ({
  FileContentView: (props: {
    path: string | null
    refreshRequest: { sequence: number; force: boolean }
    onError?: (path: string, error: unknown) => void
  }) => {
    mocks.previewProps.current = props
    return <div>{props.path}</div>
  },
}))

vi.mock('@/components/ui/scroll-area', () => ({
  ScrollArea: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}))

beforeEach(() => {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
  localStorage.clear()
  useInspectorStateStore.setState({ sessions: {} })
  useSessionFilesStore.setState({ sessions: {} })
  useConnectionStore.setState({ mode: 'local', remoteUrl: '' })
  vi.clearAllMocks()
  mocks.previewProps.current = null
  mocks.listFilesConditional.mockResolvedValue({
    status: 'modified',
    etag: '"root-v1"',
    data: [
      {
        name: 'package.json',
        path: '/project/package.json',
        size: 10,
        isDir: false,
        ext: '.json',
        modTime: '2026-10-15T12:00:00Z',
      },
    ],
  })
})

afterEach(() => {
  vi.useRealTimers()
})

describe('SessionFilePanel', () => {
  it('does not render a manual refresh button or empty toolbar row', async () => {
    const { container } = render(
      <SessionFilePanel sessionId="l2:files" projectPath="/project" />
    )
    await waitFor(() => expect(mocks.listFilesConditional).toHaveBeenCalledTimes(1))
    expect(screen.queryByRole('button', { name: 'common.refresh' })).not.toBeInTheDocument()
    expect(container.querySelector('.h-8')).toBeNull()
  })
  it('renders the in-memory tree immediately and conditionally validates it', async () => {
    const cacheKey = filesCacheKey('', 'l2:files', '/project')
    useSessionFilesStore.getState().updateSession(cacheKey, {
      directories: {
        '/project': {
          etag: '"root-v1"',
          files: [
            {
              name: 'cached.ts',
              path: '/project/cached.ts',
              size: 10,
              isDir: false,
              ext: '.ts',
              modTime: '2026-10-15T12:00:00.123456789Z',
            },
          ],
        },
      },
      expanded: { '/project': true },
    })

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    expect(screen.getByRole('button', { name: 'cached.ts' })).toBeInTheDocument()
    await waitFor(() =>
      expect(mocks.listFilesConditional).toHaveBeenCalledWith('/project', '"root-v1"')
    )
  })

  it('polls only visible expanded directories and resumes immediately after browser visibility returns', async () => {
    vi.useFakeTimers()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    const cacheKey = filesCacheKey('', 'l2:files', '/project')
    useSessionFilesStore.getState().updateSession(cacheKey, {
      directories: {
        '/project': {
          files: [
            { name: 'src', path: '/project/src', size: 0, isDir: true, ext: '', modTime: '' },
            { name: 'dist', path: '/project/dist', size: 0, isDir: true, ext: '', modTime: '' },
          ],
          etag: '"root-v1"',
        },
        '/project/src': { files: [], etag: '"src-v1"' },
        '/project/dist': { files: [], etag: '"dist-v1"' },
      },
      expanded: {
        '/project': true,
        '/project/src': true,
        '/project/dist': false,
      },
    })
    mocks.listFilesConditional.mockResolvedValue({ status: 'not-modified', etag: null })

    const { unmount } = render(
      <SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />
    )
    await act(async () => {})
    expect(mocks.listFilesConditional).toHaveBeenCalledWith('/project', '"root-v1"')
    expect(mocks.listFilesConditional).toHaveBeenCalledWith('/project/src', '"src-v1"')
    expect(mocks.listFilesConditional).not.toHaveBeenCalledWith('/project/dist', '"dist-v1"')

    mocks.listFilesConditional.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(mocks.listFilesConditional).toHaveBeenCalledTimes(2)

    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    mocks.listFilesConditional.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000)
    })
    expect(mocks.listFilesConditional).not.toHaveBeenCalled()

    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(mocks.listFilesConditional).toHaveBeenCalledTimes(2)

    unmount()
    vi.useRealTimers()
  })

  it('does not poll an expanded descendant beneath a collapsed ancestor', async () => {
    const cacheKey = filesCacheKey('', 'l2:files', '/project')
    useSessionFilesStore.getState().updateSession(cacheKey, {
      directories: {
        '/project': {
          etag: '"root-v1"',
          files: [
            {
              name: 'src',
              path: '/project/src',
              size: 0,
              isDir: true,
              ext: '',
              modTime: '',
            },
          ],
        },
        '/project/src': {
          etag: '"src-v1"',
          files: [
            {
              name: 'nested',
              path: '/project/src/nested',
              size: 0,
              isDir: true,
              ext: '',
              modTime: '',
            },
          ],
        },
        '/project/src/nested': { files: [], etag: '"nested-v1"' },
      },
      expanded: {
        '/project': true,
        '/project/src': false,
        '/project/src/nested': true,
      },
    })
    mocks.listFilesConditional.mockResolvedValue({ status: 'not-modified', etag: null })

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    await waitFor(() =>
      expect(mocks.listFilesConditional).toHaveBeenCalledWith('/project', '"root-v1"')
    )
    expect(mocks.listFilesConditional).not.toHaveBeenCalledWith('/project/src', '"src-v1"')
    expect(mocks.listFilesConditional).not.toHaveBeenCalledWith(
      '/project/src/nested',
      '"nested-v1"'
    )
  })

  it('retains cached tree content and reports a mixed directory refresh failure', async () => {
    const cacheKey = filesCacheKey('', 'l2:files', '/project')
    useSessionFilesStore.getState().updateSession(cacheKey, {
      directories: {
        '/project': {
          etag: '"root-v1"',
          files: [
            { name: 'src', path: '/project/src', size: 0, isDir: true, ext: '', modTime: '' },
            { name: 'docs', path: '/project/docs', size: 0, isDir: true, ext: '', modTime: '' },
          ],
        },
        '/project/src': {
          etag: '"src-v1"',
          files: [
            {
              name: 'keep.ts',
              path: '/project/src/keep.ts',
              size: 10,
              isDir: false,
              ext: '.ts',
              modTime: '',
            },
          ],
        },
        '/project/docs': { files: [], etag: '"docs-v1"' },
      },
      expanded: {
        '/project': true,
        '/project/src': true,
        '/project/docs': true,
      },
    })
    mocks.listFilesConditional.mockImplementation(async (path: string) => {
      if (path === '/project/src') throw new Error('forbidden')
      return { status: 'not-modified', etag: null }
    })

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    expect(await screen.findByRole('button', { name: 'keep.ts' })).toBeInTheDocument()
    expect(await screen.findByText('Unable to refresh files')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'keep.ts' })).toBeInTheDocument()
    expect(mocks.listFilesConditional).toHaveBeenCalledWith('/project', '"root-v1"')
    expect(mocks.listFilesConditional).toHaveBeenCalledWith('/project/src', '"src-v1"')
    expect(mocks.listFilesConditional).toHaveBeenCalledWith('/project/docs', '"docs-v1"')
  })

  it('clears cached tree and selection after a terminal directory response', async () => {
    const cacheKey = filesCacheKey('', 'l2:files', '/project')
    useSessionFilesStore.getState().updateSession(cacheKey, {
      directories: {
        '/project': {
          etag: '"root-v1"',
          files: [
            {
              name: 'secret.md',
              path: '/project/secret.md',
              size: 10,
              isDir: false,
              ext: '.md',
              modTime: '',
            },
          ],
        },
      },
      expanded: { '/project': true },
      selectedPath: '/project/secret.md',
      preview: {
        path: '/project/secret.md',
        content: 'cached secret',
        etag: 'W/"file-v1"',
        viewMode: 'raw',
        scrollTop: 0,
      },
    })
    mocks.listFilesConditional.mockRejectedValue(new APIError('forbidden', 403))

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    expect(await screen.findByText('Files unavailable')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'secret.md' })).not.toBeInTheDocument()
    expect(screen.queryByText('/project/secret.md')).not.toBeInTheDocument()
    expect(useSessionFilesStore.getState().sessions[cacheKey]).toMatchObject({
      directories: {},
      selectedPath: null,
      preview: null,
    })
  })

  it('does not overlap slow polling cycles or emit duplicate preview validations', async () => {
    vi.useFakeTimers()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    const cacheKey = filesCacheKey('', 'l2:files', '/project')
    useSessionFilesStore.getState().updateSession(cacheKey, {
      directories: {
        '/project': {
          files: [
            {
              name: 'readme.md',
              path: '/project/readme.md',
              size: 1,
              isDir: false,
              ext: '.md',
              modTime: '',
            },
          ],
          etag: '"root-v1"',
        },
      },
      expanded: { '/project': true },
      selectedPath: '/project/readme.md',
    })
    let resolveFirst!: (value: { status: 'not-modified'; etag: string }) => void
    let resolveSecond!: (value: { status: 'not-modified'; etag: string }) => void
    mocks.listFilesConditional
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveFirst = resolve
          })
      )
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveSecond = resolve
          })
      )

    const { unmount } = render(
      <SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />
    )
    await act(async () => {})
    expect(mocks.listFilesConditional).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(15_000)
    })
    expect(mocks.listFilesConditional).toHaveBeenCalledTimes(1)
    expect(mocks.previewProps.current?.refreshRequest.sequence).toBe(0)

    await act(async () => {
      resolveFirst({ status: 'not-modified', etag: '"root-v1"' })
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(mocks.previewProps.current?.refreshRequest.sequence).toBe(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(mocks.listFilesConditional).toHaveBeenCalledTimes(2)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(15_000)
    })
    expect(mocks.listFilesConditional).toHaveBeenCalledTimes(2)

    await act(async () => {
      resolveSecond({ status: 'not-modified', etag: '"root-v1"' })
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(mocks.previewProps.current?.refreshRequest.sequence).toBe(2)
    unmount()
    vi.useRealTimers()
  })

  it('clears a selected file when its refreshed parent proves it was deleted', async () => {
    const cacheKey = filesCacheKey('', 'l2:files', '/project')
    useSessionFilesStore.getState().updateSession(cacheKey, {
      directories: {
        '/project': {
          etag: '"root-v1"',
          files: [
            {
              name: 'gone.ts',
              path: '/project/gone.ts',
              size: 1,
              isDir: false,
              ext: '.ts',
              modTime: '',
            },
          ],
        },
      },
      expanded: { '/project': true },
      selectedPath: '/project/gone.ts',
      preview: {
        path: '/project/gone.ts',
        content: 'stale',
        etag: 'old',
        viewMode: 'raw',
        scrollTop: 0,
      },
    })
    mocks.listFilesConditional.mockResolvedValue({
      status: 'modified',
      etag: '"root-v2"',
      data: [],
    })

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    expect(await screen.findByText('Selected file was deleted or moved')).toBeInTheDocument()
    expect(useSessionFilesStore.getState().sessions[cacheKey]).toMatchObject({
      selectedPath: null,
      preview: null,
    })
    expect(screen.queryByText('/project/gone.ts')).not.toBeInTheDocument()
  })

  it.each([
    { status: 401, apiMessage: 'unauthorized', uiMessage: 'File unavailable' },
    { status: 404, apiMessage: 'not found', uiMessage: 'File was deleted or moved' },
  ])(
    'clears terminal preview cache and closes a narrow preview after HTTP $status',
    async ({ status, apiMessage, uiMessage }) => {
      const cacheKey = filesCacheKey('', 'l2:files', '/project')
      useSessionFilesStore.getState().updateSession(cacheKey, {
        directories: {
          '/project': {
            etag: '"root-v1"',
            files: [
              {
                name: 'readme.md',
                path: '/project/readme.md',
                size: 1,
                isDir: false,
                ext: '.md',
                modTime: '',
              },
            ],
          },
        },
        expanded: { '/project': true },
      })
      mocks.listFilesConditional.mockResolvedValue({ status: 'not-modified', etag: '"root-v1"' })
      render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={300} />)
      fireEvent.click(await screen.findByRole('button', { name: 'readme.md' }))
      expect(await screen.findByText('/project/readme.md')).toBeInTheDocument()

      act(() => {
        mocks.previewProps.current?.onError?.(
          '/project/readme.md',
          new APIError(apiMessage, status)
        )
      })

      expect(await screen.findByText(uiMessage)).toBeInTheDocument()
      expect(screen.queryByText('/project/readme.md')).not.toBeInTheDocument()
      expect(useSessionFilesStore.getState().sessions[cacheKey]).toMatchObject({
        selectedPath: null,
        preview: null,
      })
    }
  )

  it('applies hover styling only while a file row is inactive', async () => {
    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    const fileRow = await screen.findByRole('button', { name: 'package.json' })
    expect(fileRow).toHaveClass('hover:bg-muted/50')
    expect(fileRow).not.toHaveClass('bg-accent', 'text-accent-foreground')

    fireEvent.click(fileRow)

    const activeFileRow = screen.getByRole('button', { name: 'package.json' })
    expect(activeFileRow).toHaveClass('bg-accent', 'text-accent-foreground')
    expect(activeFileRow).not.toHaveClass('hover:bg-muted/50')
  })

  it('restores a project-relative selected file without persisting its contents', async () => {
    useInspectorStateStore.getState().updateSession('l2:files', {
      projectPath: '/project',
      selectedFile: 'package.json',
    })

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    const fileRow = await screen.findByRole('button', { name: 'package.json' })
    expect(fileRow).toHaveClass('bg-accent', 'text-accent-foreground')
    expect(screen.getByText('/project/package.json')).toBeInTheDocument()
    expect(
      JSON.parse(localStorage.getItem('soloqueue_inspector_state') || '{}')
    ).not.toHaveProperty('sessions.l2:files.children')
  })

  it('reloads persisted expanded directories without storing fetched children', async () => {
    mocks.listFilesConditional.mockImplementation(async (path: string) => {
      if (path === '/project') {
        return {
          status: 'modified',
          etag: '"root-v1"',
          data: [{ name: 'src', path: '/project/src', size: 0, isDir: true, ext: '', modTime: '' }],
        }
      }
      return {
        status: 'modified',
        etag: '"src-v1"',
        data: [
          {
            name: 'main.ts',
            path: '/project/src/main.ts',
            size: 10,
            isDir: false,
            ext: '.ts',
            modTime: '',
          },
        ],
      }
    })
    useInspectorStateStore.getState().updateSession('l2:files', {
      projectPath: '/project',
      expandedDirectories: ['src'],
    })

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    expect(await screen.findByRole('button', { name: 'main.ts' })).toBeInTheDocument()
    expect(mocks.listFilesConditional).toHaveBeenCalledWith('/project/src', undefined)
    expect(
      JSON.parse(localStorage.getItem('soloqueue_inspector_state') || '{}')
    ).not.toHaveProperty('sessions.l2:files.children')
  })

  it('discards file API nodes that resolve outside the project root', async () => {
    mocks.listFilesConditional.mockResolvedValue({
      status: 'modified',
      etag: '"root-v1"',
      data: [
        {
          name: 'safe.ts',
          path: '/project/safe.ts',
          size: 10,
          isDir: false,
          ext: '.ts',
          modTime: '',
        },
        {
          name: 'outside.ts',
          path: '/outside.ts',
          size: 10,
          isDir: false,
          ext: '.ts',
          modTime: '',
        },
        {
          name: 'traversal',
          path: '/project/../outside',
          size: 0,
          isDir: true,
          ext: '',
          modTime: '',
        },
        {
          name: 'drive.ts',
          path: 'C:\\outside.ts',
          size: 10,
          isDir: false,
          ext: '.ts',
          modTime: '',
        },
      ],
    })

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    expect(await screen.findByRole('button', { name: 'safe.ts' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'outside.ts' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'traversal' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'drive.ts' })).not.toBeInTheDocument()
  })

  it('defensively bounds restored directory request fan-out', async () => {
    const expandedDirectories = Array.from(
      { length: INSPECTOR_EXPANDED_DIRECTORIES_MAX + 5 },
      (_, index) => `dir-${index}`
    )
    mocks.listFilesConditional.mockImplementation(async (path: string) => {
      if (path === '/project') {
        return {
          status: 'modified',
          etag: '"root-v1"',
          data: expandedDirectories.map((relativePath) => ({
            name: relativePath,
            path: `/project/${relativePath}`,
            size: 0,
            isDir: true,
            ext: '',
            modTime: '',
          })),
        }
      }
      return { status: 'not-modified', etag: null }
    })
    useInspectorStateStore.setState({
      sessions: {
        'l2:files': {
          open: true,
          tab: 'files',
          selectedPlan: '',
          panelWidth: 500,
          projectPath: '/project',
          expandedDirectories,
          selectedFile: null,
          lastActivity: Date.now(),
        },
      },
    })

    render(<SessionFilePanel sessionId="l2:files" projectPath="/project" panelWidth={500} />)

    await screen.findByRole('button', { name: 'dir-0' })
    expect(mocks.listFilesConditional).toHaveBeenCalledTimes(INSPECTOR_EXPANDED_DIRECTORIES_MAX + 1)
    expect(mocks.listFilesConditional).not.toHaveBeenCalledWith(
      `/project/dir-${INSPECTOR_EXPANDED_DIRECTORIES_MAX}`,
      undefined
    )
  })
})
