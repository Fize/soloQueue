import { useState, useEffect, useCallback, memo, useMemo, useRef } from 'react'
import { listFilesConditional } from '@/lib/api'
import type { FileInfo } from '@/types'
import { FileContentView } from './FileContentView'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import {
  Folder,
  FolderOpen,
  File,
  FileText,
  FileCode,
  FileImage,
  ChevronRight,
  Loader2,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTranslation } from '@/lib/i18n'
import {
  INSPECTOR_EXPANDED_DIRECTORIES_MAX,
  normalizeInspectorRelativePath,
  useInspectorStateStore,
} from '@/stores/inspectorStateStore'
import { useConnectionStore } from '@/stores/connectionStore'
import {
  type CachedDirectory,
  filesCacheKey,
  useSessionFilesStore,
} from '@/stores/sessionFilesStore'
import { APIError } from '@/transport/http-client'

interface TreeNode {
  path: string
  name: string
  isDir: boolean
  ext: string
  size: number
  modTime: string
}

interface DirectoryLoad {
  promise: Promise<boolean>
  token: symbol
}

function isTerminalFileError(error: unknown): error is APIError {
  return error instanceof APIError && [401, 403, 404].includes(error.status)
}

function isAtOrBelow(path: string, root: string): boolean {
  const normalizedPath = path.replace(/\\/g, '/').replace(/\/+$/, '')
  const normalizedRoot = root.replace(/\\/g, '/').replace(/\/+$/, '')
  return normalizedPath === normalizedRoot || normalizedPath.startsWith(`${normalizedRoot}/`)
}

function selectedFileDisappeared(
  selectedPath: string,
  projectPath: string,
  visited: Set<string>,
  directories: Record<string, CachedDirectory>
): boolean {
  const selected = resolveProjectPath(projectPath, selectedPath)
  if (!selected) return true
  const parts = selected.relative.split('/')
  let parent = projectPath.replace(/\\/g, '/').replace(/\/+$/, '') || '/'
  for (const directoryName of parts.slice(0, -1)) {
    if (!visited.has(parent)) return false
    const next = parent === '/' ? `/${directoryName}` : `${parent}/${directoryName}`
    const directoryExists = directories[parent]?.files.some((file) => {
      const resolved = resolveProjectPath(projectPath, file.path)
      return resolved?.absolute === next && file.isDir
    })
    if (!directoryExists) return true
    parent = next
  }
  if (!visited.has(parent)) return false
  return !directories[parent]?.files.some(
    (file) =>
      resolveProjectPath(projectPath, file.path)?.absolute === selected.absolute && !file.isDir
  )
}

import { codeExts, markdownExts, imageExtensions, isPreviewableExt } from '@/lib/file'

function entryIcon(node: TreeNode, isOpen: boolean) {
  if (node.isDir) {
    return isOpen ? (
      <FolderOpen className="h-3.5 w-3.5 text-primary shrink-0" />
    ) : (
      <Folder className="h-3.5 w-3.5 text-primary shrink-0" />
    )
  }
  const ext = node.ext.toLowerCase()
  if (markdownExts.has(ext)) return <FileText className="h-3.5 w-3.5 text-primary/70 shrink-0" />
  if (codeExts.has(ext)) return <FileCode className="h-3.5 w-3.5 text-success shrink-0" />
  if (imageExtensions.includes(ext))
    return <FileImage className="h-3.5 w-3.5 text-sky-600 shrink-0" />
  return <File className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
}

// ── Component ─────────────────────────────────────────────────────────────

const INLINE_THRESHOLD = 450 // min panel width to render preview inline

interface SessionFilePanelProps {
  sessionId: string
  projectPath: string
  panelWidth?: number
}

function resolveProjectPath(
  projectPath: string,
  path: string
): { absolute: string; relative: string } | null {
  const normalizedRoot = projectPath.replace(/\\/g, '/').replace(/\/+$/, '') || '/'
  const slashPath = path.replace(/\\/g, '/')
  let relativePath = slashPath

  if (slashPath.startsWith('/')) {
    const rootPrefix = normalizedRoot === '/' ? '/' : `${normalizedRoot}/`
    if (!slashPath.startsWith(rootPrefix)) return null
    relativePath = slashPath.slice(rootPrefix.length)
  }

  const relative = normalizeInspectorRelativePath(relativePath)
  if (!relative) return null
  return {
    absolute: normalizedRoot === '/' ? `/${relative}` : `${normalizedRoot}/${relative}`,
    relative,
  }
}

function fromProjectRelativePath(projectPath: string, path: string | null): string | null {
  return path ? (resolveProjectPath(projectPath, path)?.absolute ?? null) : null
}

function SessionFilePanelInner({
  sessionId,
  projectPath,
  panelWidth = 0,
}: SessionFilePanelProps) {
  const backend = useConnectionStore((state) => state.getEffectiveBaseUrl())
  const cacheKey = filesCacheKey(backend, sessionId, projectPath)
  const cachedSession = useSessionFilesStore((state) => state.sessions[cacheKey])
  const updateFilesSession = useSessionFilesStore((state) => state.updateSession)
  const clearSelectedFile = useSessionFilesStore((state) => state.clearSelectedFile)
  const removeDirectoryTree = useSessionFilesStore((state) => state.removeDirectoryTree)
  const expanded =
    cachedSession?.expanded ??
    (() => {
      const filesSnapshot = useSessionFilesStore.getState().sessions[cacheKey]
      if (filesSnapshot) return filesSnapshot.expanded
      const snapshot = useInspectorStateStore.getState().sessions[sessionId]
      const restoredDirectories =
        snapshot?.projectPath === projectPath
          ? snapshot.expandedDirectories
              .slice(0, INSPECTOR_EXPANDED_DIRECTORIES_MAX)
              .map((path) => fromProjectRelativePath(projectPath, path))
              .filter((path): path is string => path !== null)
          : []
      return Object.fromEntries([projectPath, ...restoredDirectories].map((path) => [path, true]))
    })()
  const [loadingNodes, setLoadingNodes] = useState<Record<string, boolean>>({})
  const [modalOpen, setModalOpen] = useState(
    () => Boolean(cachedSession?.selectedPath) && panelWidth < INLINE_THRESHOLD
  )
  const [refreshError, setRefreshError] = useState(false)
  const [terminalMessage, setTerminalMessage] = useState<string | null>(null)
  const [previewRefresh, setPreviewRefresh] = useState({ sequence: 0, force: false })
  const mountedRef = useRef(true)
  const generationRef = useRef(0)
  const inFlightRef = useRef(new Map<string, DirectoryLoad>())
  const refreshCycleRef = useRef<{
    running: boolean
    promise: Promise<void> | null
  }>({ running: false, promise: null })
  const inspectorSnapshot = useInspectorStateStore((state) => state.sessions[sessionId])
  const updateInspectorSession = useInspectorStateStore((state) => state.updateSession)
  const selectedPath =
    cachedSession?.selectedPath ??
    (inspectorSnapshot?.projectPath === projectPath
      ? fromProjectRelativePath(projectPath, inspectorSnapshot.selectedFile)
      : null)
  const { t } = useTranslation()

  const isInline = panelWidth >= INLINE_THRESHOLD

  const children = useMemo<Record<string, TreeNode[]>>(() => {
    const directories = cachedSession?.directories ?? {}
    return Object.fromEntries(
      Object.entries(directories).map(([path, directory]) => [
        path,
        directory.files.flatMap((file) => {
          const resolved = resolveProjectPath(projectPath, file.path)
          return resolved
            ? [
                {
                  path: resolved.absolute,
                  name: file.name,
                  isDir: file.isDir,
                  ext: file.ext,
                  size: file.size,
                  modTime: file.modTime,
                },
              ]
            : []
        }),
      ])
    )
  }, [cachedSession?.directories, projectPath])

  const handleError = useCallback(
    (path: string, error: unknown) => {
      setRefreshError(true)
      if (!isTerminalFileError(error)) return
      clearSelectedFile(cacheKey, path)
      updateInspectorSession(sessionId, { selectedFile: null })
      setTerminalMessage(error.status === 404 ? 'File was deleted or moved' : 'File unavailable')
      setModalOpen(false)
    },
    [cacheKey, clearSelectedFile, sessionId, updateInspectorSession]
  )

  const startNodeLoad = useCallback(
    (path: string): Promise<boolean> => {
      const generation = generationRef.current
      const token = Symbol(path)
      const request = (async () => {
        setLoadingNodes((prev) => ({ ...prev, [path]: true }))
        try {
          const current = useSessionFilesStore.getState().sessions[cacheKey]
          const response = await listFilesConditional(
            path,
            current?.directories[path]?.etag ?? undefined
          )
          if (!mountedRef.current || generationRef.current !== generation) return false
          if (response.status === 'modified') {
            const files = response.data.filter((file: FileInfo) =>
              resolveProjectPath(projectPath, file.path)
            )
            const latest = useSessionFilesStore.getState().sessions[cacheKey]
            updateFilesSession(cacheKey, {
              directories: {
                ...latest?.directories,
                [path]: { files, etag: response.etag },
              },
            })
          }
          return true
        } catch (error) {
          if (!mountedRef.current || generationRef.current !== generation) return false
          if (isTerminalFileError(error)) {
            const selected = useSessionFilesStore.getState().sessions[cacheKey]?.selectedPath
            removeDirectoryTree(cacheKey, path)
            if (selected && isAtOrBelow(selected, path)) {
              updateInspectorSession(sessionId, { selectedFile: null })
              setTerminalMessage(
                error.status === 404 ? 'Selected file was deleted or moved' : 'Files unavailable'
              )
              setModalOpen(false)
            }
          }
          return false
        } finally {
          if (mountedRef.current && generationRef.current === generation) {
            setLoadingNodes((prev) => ({ ...prev, [path]: false }))
          }
          if (inFlightRef.current.get(path)?.token === token) inFlightRef.current.delete(path)
        }
      })()
      inFlightRef.current.set(path, { promise: request, token })
      return request
    },
    [
      cacheKey,
      projectPath,
      removeDirectoryTree,
      sessionId,
      updateFilesSession,
      updateInspectorSession,
    ]
  )

  const loadNode = useCallback(
    (path: string): Promise<boolean> => {
      const existing = inFlightRef.current.get(path)
      return existing?.promise ?? startNodeLoad(path)
    },
    [startNodeLoad]
  )

  useEffect(() => {
    mountedRef.current = true
    generationRef.current += 1
    const inFlight = inFlightRef.current
    return () => {
      mountedRef.current = false
      inFlight.clear()
    }
  }, [cacheKey])

  useEffect(() => {
    if (!projectPath) return
    updateInspectorSession(sessionId, { projectPath })
    const existing = useSessionFilesStore.getState().sessions[cacheKey]
    if (!existing) {
      const snapshot = useInspectorStateStore.getState().sessions[sessionId]
      const restoredDirectories =
        snapshot?.projectPath === projectPath
          ? snapshot.expandedDirectories
              .slice(0, INSPECTOR_EXPANDED_DIRECTORIES_MAX)
              .map((path) => fromProjectRelativePath(projectPath, path))
              .filter((path): path is string => path !== null)
          : []
      const restoredExpanded = Object.fromEntries(
        [projectPath, ...restoredDirectories].map((path) => [path, true])
      )
      const restoredSelected =
        snapshot?.projectPath === projectPath
          ? fromProjectRelativePath(projectPath, snapshot.selectedFile)
          : null
      updateFilesSession(cacheKey, {
        directories: {},
        expanded: restoredExpanded,
        selectedPath: restoredSelected,
        preview: null,
      })
    }
  }, [cacheKey, projectPath, sessionId, updateFilesSession, updateInspectorSession])

  const executeRefreshCycle = useCallback(
    async () => {
      if (!projectPath || document.visibilityState === 'hidden') return
      const generation = generationRef.current
      const outcomes: boolean[] = []
      const visited = new Set([projectPath])
      const refreshReachableDirectory = async (path: string): Promise<void> => {
        outcomes.push(await loadNode(path))
        const directories = useSessionFilesStore.getState().sessions[cacheKey]?.directories ?? {}
        const childPaths: string[] = []
        for (const file of directories[path]?.files ?? []) {
          if (!file.isDir || visited.size > INSPECTOR_EXPANDED_DIRECTORIES_MAX) continue
          const resolved = resolveProjectPath(projectPath, file.path)
          if (!resolved || visited.has(resolved.absolute) || !expanded[resolved.absolute]) continue
          visited.add(resolved.absolute)
          childPaths.push(resolved.absolute)
        }
        await Promise.all(childPaths.map((childPath) => refreshReachableDirectory(childPath)))
      }
      await refreshReachableDirectory(projectPath)
      if (mountedRef.current && generationRef.current === generation) {
        setRefreshError(outcomes.some((succeeded) => !succeeded))
        if (outcomes.every(Boolean)) {
          const state = useSessionFilesStore.getState().sessions[cacheKey]
          const selected = state?.selectedPath
          if (
            selected &&
            selectedFileDisappeared(selected, projectPath, visited, state.directories)
          ) {
            clearSelectedFile(cacheKey, selected)
            updateInspectorSession(sessionId, { selectedFile: null })
            setTerminalMessage('Selected file was deleted or moved')
            setModalOpen(false)
          }
        }
        setPreviewRefresh((current) => ({ sequence: current.sequence + 1, force: false }))
      }
    },
    [
      cacheKey,
      clearSelectedFile,
      expanded,
      loadNode,
      projectPath,
      sessionId,
      updateInspectorSession,
    ]
  )

  const refreshVisible = useCallback(
    (): Promise<void> => {
      const cycle = refreshCycleRef.current
      if (cycle.running && cycle.promise) {
        return cycle.promise
      }

      let resolveCycle!: () => void
      const promise = new Promise<void>((resolve) => {
        resolveCycle = resolve
      })
      cycle.running = true
      cycle.promise = promise

      void (async () => {
        try {
          await executeRefreshCycle()
        } finally {
          cycle.running = false
          cycle.promise = null
          resolveCycle()
        }
      })()
      return promise
    },
    [executeRefreshCycle]
  )

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    let cancelled = false
    const stop = () => {
      if (timer) clearTimeout(timer)
      timer = undefined
    }
    const schedule = () => {
      if (cancelled || timer || document.visibilityState === 'hidden') return
      timer = setTimeout(() => {
        timer = undefined
        void refreshVisible().finally(schedule)
      }, 5000)
    }
    const start = () => {
      if (document.visibilityState === 'hidden') return
      stop()
      void refreshVisible().finally(schedule)
    }
    const onVisibilityChange = () => {
      if (document.visibilityState === 'hidden') stop()
      else start()
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
    start()
    return () => {
      cancelled = true
      stop()
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  }, [refreshVisible])

  const toggleNode = useCallback(
    async (node: TreeNode) => {
      if (!node.isDir) {
        // Binary files: prevent preview entirely
        if (!isPreviewableExt(node.ext, node.name)) return

        const relativePath = resolveProjectPath(projectPath, node.path)?.relative
        if (!relativePath) return
        updateInspectorSession(sessionId, { selectedFile: relativePath })
        updateFilesSession(cacheKey, { selectedPath: node.path })
        setTerminalMessage(null)
        if (!isInline) {
          setModalOpen(true)
        }
        return
      }
      // Directory: expand / collapse
      if (expanded[node.path]) {
        const nextExpanded = { ...expanded, [node.path]: false }
        updateFilesSession(cacheKey, { expanded: nextExpanded })
        const relativePath = resolveProjectPath(projectPath, node.path)?.relative
        if (!relativePath) return
        updateInspectorSession(sessionId, {
          expandedDirectories:
            useInspectorStateStore
              .getState()
              .sessions[sessionId]?.expandedDirectories.filter((path) => path !== relativePath) ??
            [],
        })
        return
      }
      if (!children[node.path]) {
        const succeeded = await loadNode(node.path)
        if (!succeeded) setRefreshError(true)
      }
      const nextExpanded = { ...expanded, [node.path]: true }
      updateFilesSession(cacheKey, { expanded: nextExpanded })
      const relativePath = resolveProjectPath(projectPath, node.path)?.relative
      if (!relativePath) return
      const persisted =
        useInspectorStateStore.getState().sessions[sessionId]?.expandedDirectories ?? []
      updateInspectorSession(sessionId, {
        expandedDirectories: persisted.includes(relativePath)
          ? persisted
          : [...persisted, relativePath],
      })
    },
    [
      cacheKey,
      expanded,
      children,
      loadNode,
      isInline,
      projectPath,
      sessionId,
      updateFilesSession,
      updateInspectorSession,
    ]
  )

  function renderTree(nodes: TreeNode[], depth: number) {
    return nodes.map((node) => {
      const isOpen = expanded[node.path] ?? false
      const childNodes = children[node.path]
      const isLoading = loadingNodes[node.path] ?? false
      const isActive = selectedPath === node.path && !node.isDir

      return (
        <div key={node.path}>
          <button
            type="button"
            onClick={() => toggleNode(node)}
            className={cn(
              'flex w-full items-center gap-1.5 rounded px-2 py-0.5 text-left text-xs transition-colors',
              isActive ? 'bg-accent text-accent-foreground' : 'hover:bg-muted/50'
            )}
            style={{ paddingLeft: `${depth * 0.75 + 0.5}rem` }}
          >
            {node.isDir && (
              <ChevronRight
                className={cn(
                  'h-3 w-3 text-muted-foreground shrink-0 transition-transform',
                  isOpen && 'rotate-90'
                )}
              />
            )}
            {entryIcon(node, isOpen)}
            <span className="truncate flex-1">{node.name}</span>
          </button>
          {node.isDir && isOpen && (
            <div>
              {isLoading && (
                <div
                  className="flex items-center gap-2 py-1"
                  style={{ paddingLeft: `${depth * 0.75 + 2}rem` }}
                >
                  <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
                  <span className="text-[10px] text-muted-foreground">{t('common.loading')}</span>
                </div>
              )}
              {childNodes && renderTree(childNodes, depth + 1)}
            </div>
          )}
        </div>
      )
    })
  }

  const rootNodes = children[projectPath]

  const treeContent = (
    <ScrollArea className="flex-1 min-h-0 min-w-0">
      <div className="py-1 px-1">
        {!rootNodes && (loadingNodes[projectPath] ?? false) && (
          <div className="flex items-center justify-center py-4 gap-2">
            <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
            <span className="text-[10px] text-muted-foreground">{t('common.loading')}</span>
          </div>
        )}
        {rootNodes && rootNodes.length === 0 && (
          <div className="flex items-center justify-center py-4">
            <span className="text-[10px] text-muted-foreground">{t('chat.emptyDir')}</span>
          </div>
        )}
        {rootNodes && renderTree(rootNodes, 0)}
      </div>
    </ScrollArea>
  )

  // When inline and a file is selected: left-right split (tree | preview)
  // Otherwise: tree fills the panel, modal handles narrow previews
  const body =
    isInline && selectedPath ? (
      <div className="flex-1 min-h-0 flex flex-row overflow-hidden">
        <div className="w-[35%] min-w-[140px] max-w-[220px] border-r border-border/30 flex flex-col overflow-hidden">
          {treeContent}
        </div>
        <div className="flex-1 min-w-0 overflow-hidden">
          <FileContentView
            key={selectedPath}
            path={selectedPath}
            cacheKey={cacheKey}
            refreshRequest={previewRefresh}
            onError={handleError}
          />
        </div>
      </div>
    ) : (
      treeContent
    )

  return (
    <div className="flex flex-col h-full">
      {(refreshError || terminalMessage) && (
        <div
          role="status"
          className="flex shrink-0 flex-wrap gap-x-2 px-2 py-1 text-[10px] text-destructive"
        >
          {refreshError && <span>Unable to refresh files</span>}
          {terminalMessage && <span>{terminalMessage}</span>}
        </div>
      )}
      {body}

      {/* Modal preview — only used when panel is too narrow for inline */}
      {!isInline && (
        <Dialog open={modalOpen} onOpenChange={setModalOpen}>
          <DialogContent
            className="max-w-4xl h-[85vh] flex flex-col p-0 overflow-hidden rounded-2xl bg-background"
            showCloseButton={false}
          >
            <DialogTitle className="sr-only">{t('common.preview')}</DialogTitle>
            <div className="flex-1 min-h-0 overflow-hidden">
              <FileContentView
                key={selectedPath}
                path={selectedPath}
                cacheKey={cacheKey}
                refreshRequest={previewRefresh}
                onError={handleError}
                onClose={() => setModalOpen(false)}
              />
            </div>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}

export const SessionFilePanel = memo(SessionFilePanelInner)
