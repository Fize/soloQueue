import { memo, useMemo } from 'react'
import { splitChunks } from '@/lib/markdown/chunks'
import { cn } from '@/lib/utils'
import { MarkdownPreview } from './markdown-preview'

export interface LongMarkdownProps {
  content?: string
  className?: string
  /** Streams incomplete markdown for the growing tail only. */
  isAnimating?: boolean
  basePath?: string
}

/**
 * Markdown for output that grows token by token.
 *
 * The document is cut into immutable blocks at markdown-safe boundaries, so
 * every completed block is parsed exactly once and only the tail (a few KB) is
 * re-parsed per delta. Without this, each delta re-parses the whole answer:
 * at 100 KB that is tens of milliseconds per token and the page stops
 * responding.
 *
 * Short documents keep the plain renderer, so nothing changes for them.
 */
function LongMarkdownInner({ content = '', className, isAnimating = false, basePath }: LongMarkdownProps) {
  const { chunks, growing } = useMemo(() => splitChunks(content), [content])

  if (!content) return null
  if (chunks.length === 0) {
    return <MarkdownPreview content={content} isAnimating={isAnimating} className={className} basePath={basePath} />
  }

  return (
    <div className={cn('long-markdown', className)}>
      {chunks.map((chunk, index) => (
        // Block boundaries are append-only, so the index is a stable key and
        // every completed block keeps its identity across renders.
        <MarkdownPreview key={index} content={chunk} basePath={basePath} />
      ))}
      {growing ? <MarkdownPreview content={growing} isAnimating={isAnimating} basePath={basePath} /> : null}
    </div>
  )
}

export const LongMarkdown = memo(
  LongMarkdownInner,
  (prev, next) =>
    prev.content === next.content &&
    prev.className === next.className &&
    prev.isAnimating === next.isAnimating &&
    prev.basePath === next.basePath
)
