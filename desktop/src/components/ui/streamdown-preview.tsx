import { Streamdown } from 'streamdown'
import { useRef, memo } from 'react'
import { cn } from '@/lib/utils'
import { getFileUrl } from '@/lib/api'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'

interface StreamdownPreviewProps {
  content: string
  isAnimating?: boolean
  className?: string
  onToggleCheckbox?: (index: number) => void
  basePath?: string
}

/**
 * StreamdownPreview is a streaming-friendly drop-in for the static
 * MarkdownPreview. Unlike react-markdown (which has no memoization and
 * reparses the entire accumulated text on every chat_chunk), Streamdown
 * is designed for LLM streaming:
 *   - It parses content in blocks; when a new chunk arrives, only the
 *     affected block is re-parsed.
 *   - Incomplete code fences are rendered as plain text until they close,
 *     avoiding the per-token full re-tokenize that react-syntax-highlighter
 *     would otherwise do.
 *   - It memoizes the parsed blocks internally.
 *
 * We further wrap the whole thing in React.memo so that when the parent
 * (SegmentView) re-renders for unrelated reasons, the heavy parse work
 * is skipped as long as `content` and `isAnimating` are unchanged.
 */
function StreamdownPreviewInner({
  content,
  isAnimating = false,
  className,
  onToggleCheckbox,
  basePath,
}: StreamdownPreviewProps) {
  if (!content) return null

  // Use a ref so checkboxIndex resets each render — Streamdown calls
  // the input renderer once per checkbox in source order on each parse.
  const checkboxIndexRef = useRef(0)
  checkboxIndexRef.current = 0

  return (
    <div className={cn('markdown-preview', className)}>
      <Streamdown
        parseIncompleteMarkdown={isAnimating}
        isAnimating={isAnimating}
        shikiTheme={['github-light', 'github-dark']}
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeRaw]}
        components={{
          img({ src, alt }: any) {
            if (!src) return null
            let resolvedSrc = src
            if (basePath && !/^(https?:\/\/|data:|blob:|#|\/\/)/.test(src)) {
              const resolved = resolveRelativePath(basePath, src)
              resolvedSrc = getFileUrl(resolved)
            }
            return (
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={resolvedSrc}
                alt={alt || ''}
                className="max-w-full h-auto rounded-lg my-2"
                loading="lazy"
              />
            )
          },
          a({ href, children, ...props }: any) {
            return (
              <a href={href} {...props}>
                {children}
              </a>
            )
          },
          input({ type, checked, ...props }: any) {
            if (type === 'checkbox') {
              const currentIndex = checkboxIndexRef.current++
              return (
                <input
                  type="checkbox"
                  checked={checked}
                  disabled={!onToggleCheckbox}
                  onChange={() => {
                    if (onToggleCheckbox) {
                      onToggleCheckbox(currentIndex)
                    }
                  }}
                  className="mr-2 cursor-pointer h-4 w-4 rounded border-border text-primary focus:ring-primary"
                />
              )
            }
            return <input type={type} checked={checked} {...props} />
          },
        }}
      >
        {content}
      </Streamdown>
    </div>
  )
}

/**
 * React.memo wrapper. The streaming content is a string so referential
 * equality short-circuits identical-content re-renders (e.g. a parent
 * re-render that didn't change the content).
 */
export const StreamdownPreview = memo(
  StreamdownPreviewInner,
  (prev, next) =>
    prev.content === next.content &&
    prev.isAnimating === next.isAnimating &&
    prev.className === next.className &&
    prev.basePath === next.basePath &&
    prev.onToggleCheckbox === next.onToggleCheckbox,
)

function resolveRelativePath(basePath: string, relativePath: string): string {
  const baseDir = basePath.endsWith('/')
    ? basePath
    : basePath.includes('/')
      ? basePath.substring(0, basePath.lastIndexOf('/') + 1)
      : '/'
  const stack = baseDir.split('/').filter(Boolean)
  const parts = relativePath.split('/')
  for (const part of parts) {
    if (part === '..') {
      if (stack.length > 0) stack.pop()
    } else if (part !== '.' && part !== '') {
      stack.push(part)
    }
  }
  const result = stack.join('/')
  return result.startsWith('/') ? result : '/' + result
}
