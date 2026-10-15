import { create } from 'zustand'
import type { FileInfo } from '@/types'

export type FilePreviewMode = 'preview' | 'raw' | 'design'

export interface CachedDirectory {
  files: FileInfo[]
  etag: string | null
}

export interface CachedFilePreview {
  path: string
  content: string | null
  etag: string | null
  viewMode: FilePreviewMode
  scrollTop: number
}

export interface SessionFilesState {
  directories: Record<string, CachedDirectory>
  expanded: Record<string, boolean>
  selectedPath: string | null
  preview: CachedFilePreview | null
}

interface SessionFilesStore {
  sessions: Record<string, SessionFilesState>
  updateSession: (key: string, patch: Partial<SessionFilesState>) => void
  clearSelectedFile: (key: string, path: string) => void
  removeDirectoryTree: (key: string, path: string) => void
}

function isAtOrBelow(path: string, root: string): boolean {
  const normalizedPath = path.replace(/\\/g, '/').replace(/\/+$/, '')
  const normalizedRoot = root.replace(/\\/g, '/').replace(/\/+$/, '')
  return normalizedPath === normalizedRoot || normalizedPath.startsWith(`${normalizedRoot}/`)
}

export function filesCacheKey(backend: string, sessionId: string, projectPath: string): string {
  return `${backend || 'same-origin'}\u0000${sessionId}\u0000${projectPath}`
}

export const useSessionFilesStore = create<SessionFilesStore>((set) => ({
  sessions: {},
  updateSession: (key, patch) =>
    set((state) => {
      const current =
        state.sessions[key] ??
        ({
          directories: {},
          expanded: {},
          selectedPath: null,
          preview: null,
        } satisfies SessionFilesState)
      return {
        sessions: {
          ...state.sessions,
          [key]: { ...current, ...patch },
        },
      }
    }),
  clearSelectedFile: (key, path) =>
    set((state) => {
      const current = state.sessions[key]
      if (!current) return state
      return {
        sessions: {
          ...state.sessions,
          [key]: {
            ...current,
            selectedPath: current.selectedPath === path ? null : current.selectedPath,
            preview: current.preview?.path === path ? null : current.preview,
          },
        },
      }
    }),
  removeDirectoryTree: (key, path) =>
    set((state) => {
      const current = state.sessions[key]
      if (!current) return state
      const directories = Object.fromEntries(
        Object.entries(current.directories)
          .filter(([directoryPath]) => !isAtOrBelow(directoryPath, path))
          .map(([directoryPath, directory]) => [
            directoryPath,
            {
              ...directory,
              files: directory.files.filter((file) => !isAtOrBelow(file.path, path)),
            },
          ])
      )
      const selectedRemoved = current.selectedPath ? isAtOrBelow(current.selectedPath, path) : false
      const previewRemoved = current.preview ? isAtOrBelow(current.preview.path, path) : false
      return {
        sessions: {
          ...state.sessions,
          [key]: {
            ...current,
            directories,
            selectedPath: selectedRemoved ? null : current.selectedPath,
            preview: previewRemoved ? null : current.preview,
          },
        },
      }
    }),
}))
