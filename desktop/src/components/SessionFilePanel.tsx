import { useState, useEffect, useCallback, memo } from 'react'
import { listFiles } from '@/lib/api'
import type { FileInfo } from '@/types'
import { FileContentView } from './FileContentView'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from '@/components/ui/dialog'
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

interface TreeNode {
  path: string
  name: string
  isDir: boolean
  ext: string
  size: number
}

// ── Binary detection ──────────────────────────────────────────────────────

// Extensions that are definitely previewable (text, code, media, markdown).
// Mirrors FileContentView's detection — anything NOT in these sets is binary.
const codeExts = new Set([
  '.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs', '.py', '.pyi', '.pyx',
  '.rs', '.go', '.c', '.cpp', '.cc', '.cxx', '.h', '.hpp', '.hxx',
  '.java', '.kt', '.kts', '.scala', '.json', '.yaml', '.yml', '.toml',
  '.css', '.scss', '.less', '.html', '.htm', '.xml', '.svg',
  '.sh', '.bash', '.zsh', '.fish', '.sql', '.psql',
  '.dockerfile', '.proto', '.graphql', '.gql',
  '.lua', '.rb', '.php', '.swift', '.r', '.dart',
  '.tf', '.hcl', '.vue', '.svelte', '.ini', '.cfg',
])
const markdownExts = new Set(['.md', '.markdown'])
const mediaExts = new Set([
  // images
  '.png', '.jpg', '.jpeg', '.gif', '.webp', '.bmp', '.ico', '.svg',
  // audio
  '.mp3', '.wav', '.ogg', '.flac', '.aac', '.m4a', '.opus',
  // video
  '.mp4', '.webm', '.mov', '.avi', '.mkv',
])
const plainExts = new Set([
  '.txt', '.log', '.mod', '.sum', '.Makefile',
  '.dockerignore', '.gitignore', '.env', '.envrc',
])

const allPreviewableExts = new Set([
  ...codeExts,
  ...markdownExts,
  ...mediaExts,
  ...plainExts,
])

// Well-known text filenames that don't have extensions.
const knownTextFilenames = new Set([
  'Dockerfile', 'Makefile', 'README', 'LICENSE', 'CHANGELOG',
  'Procfile', 'Jenkinsfile', 'Vagrantfile',
])

function isPreviewableExt(ext: string, name?: string): boolean {
  if (!ext) {
    // No extension — only treat as text if it's a well-known text filename.
    if (name && knownTextFilenames.has(name)) return true
    return false
  }
  return allPreviewableExts.has(ext.toLowerCase())
}

function entryIcon(node: TreeNode, isOpen: boolean) {
  if (node.isDir) {
    return isOpen ? (
      <FolderOpen className="h-3.5 w-3.5 text-primary shrink-0" />
    ) : (
      <Folder className="h-3.5 w-3.5 text-primary shrink-0" />
    )
  }
  const ext = node.ext.toLowerCase()
  if (markdownExts.has(ext))
    return <FileText className="h-3.5 w-3.5 text-primary/70 shrink-0" />
  if (codeExts.has(ext))
    return <FileCode className="h-3.5 w-3.5 text-emerald-600 shrink-0" />
  if (['.png', '.jpg', '.jpeg', '.gif', '.svg', '.webp', '.bmp', '.ico'].includes(ext))
    return <FileImage className="h-3.5 w-3.5 text-sky-600 shrink-0" />
  return <File className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
}

// ── Component ─────────────────────────────────────────────────────────────

const INLINE_THRESHOLD = 450 // min panel width to render preview inline

interface SessionFilePanelProps {
  projectPath: string
  panelWidth?: number
}

function SessionFilePanelInner({ projectPath, panelWidth = 0 }: SessionFilePanelProps) {
  const [children, setChildren] = useState<Record<string, TreeNode[]>>({})
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [loadingNodes, setLoadingNodes] = useState<Record<string, boolean>>({})
  const [modalOpen, setModalOpen] = useState(false)
  const [selectedPath, setSelectedPath] = useState<string | null>(null)

  const isInline = panelWidth >= INLINE_THRESHOLD

  const handleError = useCallback(() => {
    setModalOpen(false)
  }, [])

  const deselectFile = useCallback(() => {
    setSelectedPath(null)
  }, [])

  // Load root on mount / projectPath change
  useEffect(() => {
    if (!projectPath) return
    deselectFile()
    setExpanded({ [projectPath]: true })
    loadNode(projectPath)
    setChildren({})
  }, [projectPath])

  const loadNode = useCallback(async (path: string) => {
    setLoadingNodes((prev) => ({ ...prev, [path]: true }))
    try {
      const files = await listFiles(path)
      const nodes: TreeNode[] = files.map((f: FileInfo) => ({
        path: f.path,
        name: f.name,
        isDir: f.isDir,
        ext: f.ext,
        size: f.size,
      }))
      setChildren((prev) => ({ ...prev, [path]: nodes }))
    } catch {
      /* ignore */
    } finally {
      setLoadingNodes((prev) => ({ ...prev, [path]: false }))
    }
  }, [])

  const toggleNode = useCallback(
    async (node: TreeNode) => {
      if (!node.isDir) {
        // Binary files: prevent preview entirely
        if (!isPreviewableExt(node.ext, node.name)) return

        setSelectedPath(node.path)
        if (!isInline) {
          setModalOpen(true)
        }
        return
      }
      // Directory: expand / collapse
      if (expanded[node.path]) {
        setExpanded((prev) => ({ ...prev, [node.path]: false }))
        return
      }
      if (!children[node.path]) {
        await loadNode(node.path)
      }
      setExpanded((prev) => ({ ...prev, [node.path]: true }))
    },
    [expanded, children, loadNode, isInline]
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
              'flex w-full items-center gap-1.5 rounded px-2 py-0.5 text-left text-xs transition-colors hover:bg-muted/50',
              isActive && 'bg-accent text-accent-foreground'
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
                  <span className="text-[10px] text-muted-foreground">
                    Loading...
                  </span>
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
            <span className="text-[10px] text-muted-foreground">
              Loading...
            </span>
          </div>
        )}
        {rootNodes && rootNodes.length === 0 && (
          <div className="flex items-center justify-center py-4">
            <span className="text-[10px] text-muted-foreground">
              Empty directory
            </span>
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
            path={selectedPath}
            onError={deselectFile}
          />
        </div>
      </div>
    ) : (
      treeContent
    )

  return (
    <div className="flex flex-col h-full">
      {body}

      {/* Modal preview — only used when panel is too narrow for inline */}
      {!isInline && (
        <Dialog open={modalOpen} onOpenChange={setModalOpen}>
          <DialogContent className="max-w-4xl h-[85vh] flex flex-col p-0 overflow-hidden rounded-2xl">
            <DialogTitle className="sr-only">File Preview</DialogTitle>
            <div className="flex-1 min-h-0 overflow-hidden">
              <FileContentView
                path={selectedPath}
                onError={handleError}
              />
            </div>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}

export const SessionFilePanel = memo(SessionFilePanelInner)