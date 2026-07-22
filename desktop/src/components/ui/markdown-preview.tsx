import { Streamdown } from 'streamdown'
import { code } from '@streamdown/code'
import { useRef, memo } from 'react'
import { cn } from '@/lib/utils'
import { getFileUrl } from '@/lib/api'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import { ExploreFileChip, isExplorePath, getExploreLabel, preprocessExplorePaths } from '@/components/chat/ExploreFileChip'

interface MarkdownPreviewProps {
  content?: string
  children?: string
  className?: string
  onToggleCheckbox?: (index: number) => void
  /** Absolute path to the .md file on disk, used to resolve relative image paths. */
  basePath?: string
  // Ignore these props to act as a drop-in replacement for ReactMarkdown
  remarkPlugins?: any[]
  rehypePlugins?: any[]
}

function MarkdownPreviewInner({
  content,
  children,
  className,
  onToggleCheckbox,
  basePath,
}: MarkdownPreviewProps) {
  const contentVal = content ?? children ?? ''
  if (!contentVal) return null

  // Use a ref so checkboxIndex resets each render — Streamdown calls
  // the input renderer once per checkbox in source order on each parse.
  const checkboxIndexRef = useRef(0)
  checkboxIndexRef.current = 0

  return (
    <div className={cn('markdown-preview', className)}>
      <Streamdown
        parseIncompleteMarkdown={false}
        isAnimating={false}
        shikiTheme={['github-light', 'github-dark']}
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeRaw]}
        plugins={{ code }}
        controls={{ table: false, code: { copy: true, download: false } }}
        translations={{
          copyCode: '复制代码',
          copied: '已复制',
          copyTable: '复制表格',
          copyTableAsMarkdown: '复制为 Markdown',
          copyTableAsCsv: '复制为 CSV',
          copyTableAsTsv: '复制为 TSV',
          tableFormatMarkdown: 'Markdown',
          tableFormatCsv: 'CSV',
          tableFormatTsv: 'TSV',
        }}
        components={{
          img({ src, alt }: any) {
            if (!src) return null
            let resolvedSrc = src
            if (basePath && !/^(https?:\/\/|data:|blob:|#|\/\/)/.test(src)) {
              const resolved = resolveRelativePath(basePath, src)
              resolvedSrc = getFileUrl(resolved)
            }
            return (
              <img
                src={resolvedSrc}
                alt={alt || ''}
                className="max-w-full h-auto rounded-lg my-2"
                loading="lazy"
              />
            )
          },
          a({ href, children, ...props }: any) {
            if (href && isExplorePath(href)) {
              return <ExploreFileChip path={href} label={getExploreLabel(children)} />
            }
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
        {preprocessExplorePaths(contentVal)}
      </Streamdown>
    </div>
  )
}

/**
 * MarkdownPreview is wrapped in React.memo. The `content` prop is the
 * text to render. As long as the content string is unchanged, this component
 * skips re-rendering.
 */
export const MarkdownPreview = memo(
  MarkdownPreviewInner,
  (prev, next) =>
    (prev.content ?? prev.children) === (next.content ?? next.children) &&
    prev.className === next.className &&
    prev.basePath === next.basePath &&
    prev.onToggleCheckbox === next.onToggleCheckbox,
)

/**
 * Resolve a relative path (e.g. `./images/foo.png` or `../other.md`) against an
 * absolute base path (the .md file on disk). Returns a normalized absolute path
 * suitable for passing to getFileUrl().
 */
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
  // Ensure it starts with /
  return result.startsWith('/') ? result : '/' + result
}
