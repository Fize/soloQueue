import { beforeEach, describe, expect, it } from 'vitest'
import { filesCacheKey, useSessionFilesStore } from './sessionFilesStore'

describe('session files memory cache', () => {
  beforeEach(() => {
    useSessionFilesStore.setState({ sessions: {} })
  })

  it('preserves file state in memory and scopes it by backend, session, and project', () => {
    const localKey = filesCacheKey('', 'l2:files', '/project')
    const remoteKey = filesCacheKey('https://remote.example', 'l2:files', '/project')

    useSessionFilesStore.getState().updateSession(localKey, {
      expanded: { '/project': true, '/project/src': true },
      selectedPath: '/project/src/main.ts',
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
              modTime: '2026-10-15T12:00:00.123456789Z',
            },
          ],
        },
      },
      preview: {
        path: '/project/src/main.ts',
        content: 'cached',
        etag: '"file-v1"',
        viewMode: 'raw',
        scrollTop: 42,
      },
    })

    expect(useSessionFilesStore.getState().sessions[localKey]?.preview?.scrollTop).toBe(42)
    expect(useSessionFilesStore.getState().sessions[remoteKey]).toBeUndefined()
    expect(localStorage.getItem('soloqueue_session_files')).toBeNull()
  })

  it('atomically clears a terminal preview and removes a lost directory subtree', () => {
    const key = filesCacheKey('', 'l2:files', '/project')
    useSessionFilesStore.getState().updateSession(key, {
      directories: {
        '/project': {
          etag: 'root',
          files: [
            { name: 'src', path: '/project/src', size: 0, isDir: true, ext: '', modTime: '' },
          ],
        },
        '/project/src': {
          etag: 'src',
          files: [
            {
              name: 'main.ts',
              path: '/project/src/main.ts',
              size: 1,
              isDir: false,
              ext: '.ts',
              modTime: '',
            },
          ],
        },
      },
      selectedPath: '/project/src/main.ts',
      preview: {
        path: '/project/src/main.ts',
        content: 'stale',
        etag: 'old',
        viewMode: 'raw',
        scrollTop: 4,
      },
    })

    useSessionFilesStore.getState().clearSelectedFile(key, '/project/src/main.ts')
    expect(useSessionFilesStore.getState().sessions[key]).toMatchObject({
      selectedPath: null,
      preview: null,
    })

    useSessionFilesStore.getState().removeDirectoryTree(key, '/project/src')
    const state = useSessionFilesStore.getState().sessions[key]
    expect(state.directories['/project/src']).toBeUndefined()
    expect(state.directories['/project'].files).toEqual([])
  })
})
