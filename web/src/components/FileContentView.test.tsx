import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { FileContentView } from './FileContentView'
import { useSessionFilesStore } from '@/stores/sessionFilesStore'
import { APIError } from '@/transport/http-client'

const mocks = vi.hoisted(() => ({
  readFileBytesConditional: vi.fn(),
}))

vi.mock('@/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api')>()),
  readFileBytesConditional: mocks.readFileBytesConditional,
}))

vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

describe('FileContentView refresh cache', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useSessionFilesStore.setState({ sessions: {} })
  })

  it('renders cached text immediately and validates it with its ETag', async () => {
    useSessionFilesStore.getState().updateSession('files-key', {
      preview: {
        path: '/project/readme.md',
        content: 'cached preview',
        etag: '"file-v1"',
        viewMode: 'raw',
        scrollTop: 24,
      },
    })
    mocks.readFileBytesConditional.mockResolvedValue({
      status: 'not-modified',
      etag: '"file-v1"',
    })

    const { container } = render(
      <FileContentView
        path="/project/readme.md"
        cacheKey="files-key"
        refreshRequest={{ sequence: 0, force: false }}
      />
    )

    expect(screen.getByText('cached preview')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Raw' })).toHaveClass('bg-accent')
    await waitFor(() =>
      expect(mocks.readFileBytesConditional).toHaveBeenCalledWith('/project/readme.md', '"file-v1"')
    )
    const viewport = container.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]')
    expect(viewport?.scrollTop).toBe(24)
    if (!viewport) throw new Error('missing scroll viewport')
    viewport.scrollTop = 80
    fireEvent.scroll(viewport)
    expect(useSessionFilesStore.getState().sessions['files-key']?.preview?.scrollTop).toBe(80)
  })

  it('updates changed text without resetting the cached view mode and retains it on refresh failure', async () => {
    useSessionFilesStore.getState().updateSession('files-key', {
      preview: {
        path: '/project/readme.md',
        content: 'cached preview',
        etag: '"file-v1"',
        viewMode: 'raw',
        scrollTop: 24,
      },
    })
    mocks.readFileBytesConditional
      .mockResolvedValueOnce({ status: 'not-modified', etag: '"file-v1"' })
      .mockResolvedValueOnce({
        status: 'modified',
        etag: '"file-v2"',
        data: new TextEncoder().encode('updated preview').buffer,
      })
      .mockRejectedValueOnce(new Error('offline'))
      .mockRejectedValueOnce(new APIError('server error', 500))

    const { rerender } = render(
      <FileContentView
        path="/project/readme.md"
        cacheKey="files-key"
        refreshRequest={{ sequence: 0, force: false }}
      />
    )
    await waitFor(() => expect(mocks.readFileBytesConditional).toHaveBeenCalledTimes(1))

    rerender(
      <FileContentView
        path="/project/readme.md"
        cacheKey="files-key"
        refreshRequest={{ sequence: 1, force: false }}
      />
    )
    expect(await screen.findByText('updated preview')).toBeInTheDocument()
    expect(useSessionFilesStore.getState().sessions['files-key']?.preview).toMatchObject({
      content: 'updated preview',
      etag: '"file-v2"',
      viewMode: 'raw',
      scrollTop: 24,
    })

    rerender(
      <FileContentView
        path="/project/readme.md"
        cacheKey="files-key"
        refreshRequest={{ sequence: 2, force: false }}
      />
    )
    expect(await screen.findByText('Refresh failed')).toBeInTheDocument()
    expect(screen.getByText('updated preview')).toBeInTheDocument()

    rerender(
      <FileContentView
        path="/project/readme.md"
        cacheKey="files-key"
        refreshRequest={{ sequence: 3, force: false }}
      />
    )
    await waitFor(() => expect(mocks.readFileBytesConditional).toHaveBeenCalledTimes(4))
    expect(screen.getByText('Refresh failed')).toBeInTheDocument()
    expect(screen.getByText('updated preview')).toBeInTheDocument()
  })

  it.each([401, 403, 404])(
    'clears stale preview content after terminal HTTP %s',
    async (status) => {
      const onError = vi.fn()
      useSessionFilesStore.getState().updateSession('files-key', {
        selectedPath: '/project/readme.md',
        preview: {
          path: '/project/readme.md',
          content: 'stale secret body',
          etag: 'W/"file-v1"',
          viewMode: 'raw',
          scrollTop: 24,
        },
      })
      mocks.readFileBytesConditional.mockRejectedValue(
        new APIError(status === 404 ? 'not found' : 'forbidden', status)
      )

      render(
        <FileContentView
          path="/project/readme.md"
          cacheKey="files-key"
          refreshRequest={{ sequence: 0, force: false }}
          onError={onError}
        />
      )

      expect(
        await screen.findByText(
          status === 404 ? 'File was deleted or moved' : 'File is no longer available'
        )
      ).toBeInTheDocument()
      expect(screen.queryByText('stale secret body')).not.toBeInTheDocument()
      expect(useSessionFilesStore.getState().sessions['files-key']).toMatchObject({
        selectedPath: null,
        preview: null,
      })
      expect(onError).toHaveBeenCalledWith(
        '/project/readme.md',
        expect.objectContaining({ status })
      )
    }
  )
})
