import { useState, useEffect, useCallback } from 'react'
import { getFileRoots, listFiles } from '@/lib/api'
import type { FileRoot, FileInfo } from '@/types'
import { FileContentView } from './FileContentView'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Button } from '@/components/ui/button'
import {
  Folder,
  FolderOpen,
  File,
  FileText,
  FileCode,
  FileImage,
  FileAudio,
  FileVideo,
  Loader2,
  ChevronRight,
  FolderTree,
  PanelRight,
  RefreshCw,
} from 'lucide-react'
import { cn } from '@/lib/utils'

const codeExts: Record<string, boolean> = {
  '.ts': true,
  '.tsx': true,
  '.js': true,
  '.jsx': true,
  '.go': true,
  '.py': true,
  '.rs': true,
  '.c': true,
  '.cpp': true,
  '.cc': true,
  '.h': true,
  '.hpp': true,
  '.java': true,
  '.kt': true,
  '.json': true,
  '.yaml': true,
  '.yml': true,
  '.toml': true,
  '.css': true,
  '.scss': true,
  '.html': true,
  '.xml': true,
  '.svg': true,
  '.sh': true,
  '.sql': true,
  '.proto': true,
  '.dockerfile': true,
}
const imgExts: Record<string, boolean> = {
  '.png': true,
  '.jpg': true,
  '.jpeg': true,
  '.gif': true,
  '.webp': true,
  '.bmp': true,
  '.ico': true,
  '.svg': true,
}
const audioExts: Record<string, boolean> = {
  '.mp3': true,
  '.wav': true,
  '.ogg': true,
  '.flac': true,
  '.aac': true,
}
const videoExts: Record<string, boolean> = {
  '.mp4': true,
  '.webm': true,
  '.mov': true,
  '.avi': true,
  '.mkv': true,
}

function entryIcon(info: { isDir: boolean; ext: string }, open: boolean) {
  if (info.isDir)
    return open ? (
      <FolderOpen className="h-4 w-4 text-sky-500 shrink-0" />
    ) : (
      <Folder className="h-4 w-4 text-sky-500 shrink-0" />
    )
  if (info.ext === '.md' || info.ext === '.markdown')
    return <FileText className="h-4 w-4 text-blue-500 shrink-0" />
  if (codeExts[info.ext]) return <FileCode className="h-4 w-4 text-emerald-500 shrink-0" />
  if (imgExts[info.ext]) return <FileImage className="h-4 w-4 text-purple-500 shrink-0" />
  if (audioExts[info.ext]) return <FileAudio className="h-4 w-4 text-orange-500 shrink-0" />
  if (videoExts[info.ext]) return <FileVideo className="h-4 w-4 text-red-500 shrink-0" />
  return <File className="h-4 w-4 text-muted-foreground shrink-0" />
}

function formatSize(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

function labelFromPath(p: string): string {
  const parts = p.replace(/\/$/, '').split('/')
  return parts[parts.length - 1] || p
}

function isDotSoloqueue(p: string): boolean {
  return p.endsWith('/.soloqueue') || p === '~/.soloqueue' || p.endsWith('/.soloqueue/')
}

interface TreeNode {
  path: string
  name: string
  isDir: boolean
  ext: string
  size: number
  children: TreeNode[] | null
  loading: boolean
}

export function FilesPage() {
  const [roots, setRoots] = useState<FileRoot[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedPath, setSelectedPath] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [children, setChildren] = useState<Record<string, TreeNode[]>>({})
  const [loadingNodes, setLoadingNodes] = useState<Record<string, boolean>>({})
  const [showTree, setShowTree] = useState(false)
  const [contentVersion, setContentVersion] = useState(0)

  const fetchRoots = useCallback(() => {
    setLoading(true)
    setError(null)
    return getFileRoots()
      .then(setRoots)
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    fetchRoots()
  }, [fetchRoots])

  const loadChildren = useCallback(async (path: string) => {
    setLoadingNodes((prev) => ({ ...prev, [path]: true }))
    try {
      const files = await listFiles(path)
      const nodes: TreeNode[] = files.map((f: FileInfo) => ({
        path: f.path,
        name: f.name,
        isDir: f.isDir,
        ext: f.ext,
        size: f.size,
        children: null,
        loading: false,
      }))
      setChildren((prev) => ({ ...prev, [path]: nodes }))
    } catch {
      /* ignore */
    } finally {
      setLoadingNodes((prev) => ({ ...prev, [path]: false }))
    }
  }, [])

  const handleRefresh = useCallback(() => {
    setContentVersion((v) => v + 1)
    fetchRoots()
    // preserve expanded state, re-fetch children for all open directories
    Object.entries(expanded).forEach(([key, isExpanded]) => {
      if (isExpanded && !key.startsWith(SECTION + ':')) {
        loadChildren(key)
      }
    })
  }, [fetchRoots, expanded, loadChildren])

  const toggleNode = useCallback(
    async (path: string, isDir: boolean) => {
      if (!isDir) {
        setSelectedPath(path)
        return
      }
      if (expanded[path]) {
        setExpanded((prev) => ({ ...prev, [path]: false }))
        return
      }
      if (!children[path]) {
        await loadChildren(path)
      }
      setExpanded((prev) => ({ ...prev, [path]: true }))
    },
    [expanded, children, loadChildren]
  )

  function renderFileNodes(nodes: TreeNode[], depth: number) {
    return nodes.map((node) => {
      const isOpen = expanded[node.path] ?? false
      const childNodes = children[node.path]
      const childLoading = loadingNodes[node.path] ?? false

      return (
        <div key={node.path}>
          <button
            type="button"
            onClick={() => toggleNode(node.path, node.isDir)}
            className={cn(
              'flex w-full items-center gap-1.5 rounded px-2 py-0.5 text-left text-sm transition-colors hover:bg-muted/50',
              selectedPath === node.path && 'bg-accent text-accent-foreground'
            )}
            style={{ paddingLeft: `${depth * 1 + 0.5}rem` }}
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
            {!node.isDir && (
              <span className="text-xs text-muted-foreground shrink-0 tabular-nums">
                {formatSize(node.size)}
              </span>
            )}
          </button>
          {node.isDir && isOpen && (
            <div>
              {childLoading && (
                <div
                  className="flex items-center gap-2 py-1"
                  style={{ paddingLeft: `${depth * 1 + 2}rem` }}
                >
                  <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
                  <span className="text-xs text-muted-foreground">Loading...</span>
                </div>
              )}
              {childNodes && renderFileNodes(childNodes, depth + 1)}
            </div>
          )}
        </div>
      )
    })
  }

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        {error}
      </div>
    )
  }

  // Group roots and filter .soloqueue
  const filtered = roots.filter((r) => !isDotSoloqueue(r.path))
  const globalRoots = filtered.filter((r) => !r.group)
  const groupMap: Record<string, FileRoot[]> = {}
  for (const r of filtered) {
    if (!r.group) continue
    if (!groupMap[r.group]) groupMap[r.group] = []
    groupMap[r.group].push(r)
  }
  const groupNames = Object.keys(groupMap).sort()

  function rootToNode(r: FileRoot): TreeNode {
    return {
      path: r.path,
      name: labelFromPath(r.path),
      isDir: true,
      ext: '',
      size: 0,
      children: null,
      loading: false,
    }
  }

  const SECTION = 'section' as const

  const treeContent = (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="border-b border-border px-3 py-2 flex items-center justify-between">
        <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          Files
        </span>
        <button
          type="button"
          onClick={handleRefresh}
          className="inline-flex items-center justify-center rounded h-5 w-5 text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors"
          title="Refresh file tree"
        >
          <RefreshCw className="h-3 w-3" />
        </button>
      </div>
      <ScrollArea className="flex-1 min-h-0">
        <div className="py-1">
          {/* Global */}
          {globalRoots.length > 0 && (
            <div>
              <div className="px-3 py-1.5 text-xs font-semibold text-muted-foreground">
                Plan (global)
              </div>
              {renderFileNodes(globalRoots.map(rootToNode), 0)}
            </div>
          )}

          {/* Groups */}
          {groupNames.map((name) => {
            const groupRoots = groupMap[name]
            const groupKey = `${SECTION}:${name}`
            const isOpen = expanded[groupKey] ?? false

            return (
              <div key={name}>
                <button
                  type="button"
                  onClick={() => setExpanded((prev) => ({ ...prev, [groupKey]: !isOpen }))}
                  className="flex w-full items-center gap-1 px-2 py-1.5 text-left text-xs font-semibold text-muted-foreground hover:text-foreground transition-colors"
                >
                  <ChevronRight
                    className={cn('h-3 w-3 shrink-0 transition-transform', isOpen && 'rotate-90')}
                  />
                  {name}
                </button>
                {isOpen && renderFileNodes(groupRoots.map(rootToNode), 0)}
              </div>
            )
          })}
        </div>
      </ScrollArea>
    </div>
  )

  return (
    <div className="flex h-full p-2 sm:p-3 gap-2 sm:gap-3">
      {/* Desktop tree — always visible */}
      <div className="hidden md:flex w-64 shrink-0 border-r border-border flex-col rounded-lg border overflow-hidden">
        {treeContent}
      </div>

      {/* Mobile tree overlay */}
      {showTree && (
        <div className="fixed inset-0 z-40 md:hidden">
          <div
            className="absolute inset-0 bg-black/30 animate-in fade-in-0"
            onClick={() => setShowTree(false)}
          />
          <div className="absolute inset-y-0 left-0 w-[280px] bg-card border-r border-border flex flex-col shadow-2xl animate-in slide-in-from-left overflow-hidden">
            {treeContent}
          </div>
        </div>
      )}

      <div className="relative flex-1 min-w-0 flex flex-col">
        {/* Mobile header with tree toggle */}
        <div className="flex items-center gap-2 md:hidden mb-2">
          <Button variant="ghost" size="icon-sm" onClick={() => setShowTree(true)}>
            <FolderTree className="h-4 w-4" />
          </Button>
          {selectedPath && (
            <Button variant="ghost" size="icon-sm" onClick={() => setSelectedPath(null)}>
              <PanelRight className="h-4 w-4" />
            </Button>
          )}
          <span className="text-xs text-muted-foreground truncate flex-1">
            {selectedPath ? selectedPath.split('/').pop() : 'Select a file'}
          </span>
        </div>
        <div className="flex-1 min-h-0">
          <FileContentView
            path={selectedPath}
            key={contentVersion}
            onError={() => setSelectedPath(null)}
          />
        </div>
      </div>
    </div>
  )
}
