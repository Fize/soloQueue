import { useState, useCallback } from 'react'
import { Search, X, Loader2, AlertCircle } from 'lucide-react'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { readFile } from '@/lib/api'

interface ExploreFileChipProps {
  /** Absolute path to the explore .md file, e.g. /Users/x/.soloqueue/explore/foo_agent.md */
  path: string
  /** Optional display label; falls back to the filename. */
  label?: string
}

/**
 * Renders an inline chip for explore artifact paths found in agent messages.
 * Clicking the chip opens a Modal with the file's Markdown content.
 */
export function ExploreFileChip({ path, label }: ExploreFileChipProps) {
  const [open, setOpen] = useState(false)
  const [content, setContent] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const filename = path.split('/').pop() ?? path
  const displayLabel = label && label !== filename ? label : filename.replace(/\.md$/, '')

  const loadContent = useCallback(async () => {
    if (content !== null) return // already loaded
    setLoading(true)
    setError(null)
    try {
      setContent(await readFile(path))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load file')
    } finally {
      setLoading(false)
    }
  }, [path, content])

  const handleOpen = () => {
    setOpen(true)
    loadContent()
  }

  return (
    <>
      <button
        type="button"
        onClick={handleOpen}
        title={path}
        className="
          inline-flex items-center gap-1 px-1.5 py-0.5 mx-0.5
          rounded-md text-[11px] font-mono font-medium
          bg-primary/8 text-primary/80 border border-primary/20
          hover:bg-primary/15 hover:text-primary hover:border-primary/35
          transition-colors cursor-pointer align-middle
        "
      >
        <Search className="h-3 w-3 shrink-0" />
        <span className="truncate max-w-[24ch]">{displayLabel}</span>
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent
          className="max-w-3xl h-[80vh] flex flex-col p-0 overflow-hidden rounded-2xl bg-background"
          showCloseButton={false}
        >
          {/* Header */}
          <div className="flex items-center justify-between px-4 py-3 border-b border-border/40 bg-card/40 backdrop-blur-md shrink-0">
            <div className="flex items-center gap-2 min-w-0">
              <Search className="h-4 w-4 text-primary shrink-0" />
              <DialogTitle className="text-sm font-semibold text-foreground/90 truncate">
                {displayLabel}
              </DialogTitle>
            </div>
            <button
              type="button"
              onClick={() => setOpen(false)}
              className="shrink-0 p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-foreground/5 transition-colors cursor-pointer"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          {/* Path bar */}
          <div className="px-4 py-1.5 border-b border-border/20 bg-muted/20 shrink-0">
            <span className="text-[10px] font-mono text-muted-foreground/60 break-all select-all">
              {path}
            </span>
          </div>

          {/* Body */}
          <div className="flex-1 min-h-0 overflow-y-auto p-4">
            {loading && (
              <div className="flex h-full items-center justify-center">
                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
              </div>
            )}
            {error && (
              <div className="flex h-full flex-col items-center justify-center gap-2 text-sm text-destructive">
                <AlertCircle className="h-5 w-5" />
                <span>{error}</span>
              </div>
            )}
            {!loading && !error && content !== null && (
              <div className="prose dark:prose-invert prose-sm max-w-none">
                <MarkdownPreview content={content} basePath={path} />
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

/**
 * Returns true if the href points to an explore artifact:
 *   - contains /.soloqueue/explore/ in the path
 *   - ends with .md
 */
export function isExplorePath(href: string): boolean {
  return href.includes('/.soloqueue/explore/') && href.endsWith('.md')
}

/**
 * Extract a plain-text label from React children (e.g. the link text).
 * Falls back to undefined so ExploreFileChip uses the filename instead.
 */
export function getExploreLabel(children: React.ReactNode): string | undefined {
  if (typeof children === 'string') return children || undefined
  if (Array.isArray(children)) {
    const flat = children
      .map((c) => (typeof c === 'string' ? c : ''))
      .join('')
    return flat || undefined
  }
  return undefined
}

/**
 * Pre-process markdown content before passing to Streamdown.
 *
 * Converts explore file paths written as plain text or inline code into
 * markdown links so the `a` component renderer can intercept them.
 *
 * Handles three forms:
 *   1. plain text:  /Users/x/.soloqueue/explore/foo.md
 *   2. inline code: `/Users/x/.soloqueue/explore/foo.md`
 *   3. already a markdown link: [text](path) — left untouched
 *
 * The regex matches an optional leading backtick, the absolute path
 * (must contain /.soloqueue/explore/ and end with .md), and an optional
 * matching trailing backtick.  A check on the two characters immediately
 * before the match prevents double-processing already-linked paths.
 */
const EXPLORE_PATH_RE = /(`?)([a-zA-Z0-9_.~\-/]*\/\.soloqueue\/explore\/[^\s`\[\]()"'<>\n]+\.md)\1/g

export function preprocessExplorePaths(content: string): string {
  return content.replace(EXPLORE_PATH_RE, (match, _backtick, path, offset) => {
    // Skip paths that are already inside a markdown link href: ...](path)
    if (offset >= 2 && content[offset - 1] === '(' && content[offset - 2] === ']') {
      return match
    }
    const filename = path.split('/').pop() ?? path
    return `[${filename}](${path})`
  })
}
