import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
import { useState, useEffect, memo } from 'react'
import { cn } from '@/lib/utils'
import { getFileUrl } from '@/lib/api'
import { PrismLight as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneLight, oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { useIsDarkMode } from '@/hooks/useIsDarkMode'
import { CODE_PREVIEW_CONFIG } from '@/lib/theme'
import { ensureLanguage, preloadCommonLanguages } from '@/lib/syntax-languages'

// Eagerly load the most common languages so the first code block renders instantly
preloadCommonLanguages()

interface MarkdownPreviewProps {
  content: string
  className?: string
  onToggleCheckbox?: (index: number) => void
  /** Absolute path to the .md file on disk, used to resolve relative image paths. */
  basePath?: string
}

function CodeBlock({
  language,
  value,
}: {
  language: string | null
  value: string
}) {
  const isDark = useIsDarkMode()
  const [, setLoaded] = useState(0)

  // Lazy-load the language grammar on first encounter
  useEffect(() => {
    if (!language) return
    ensureLanguage(language).then(() => {
      setLoaded((n) => n + 1) // force re-render after language loads
    })
  }, [language])

  return (
    <div className="my-3 rounded-lg border border-border/60 overflow-hidden">
      {language && (
        <div className="flex items-center justify-between px-3 py-1.5 bg-muted/50 border-b border-border/40">
          <span className="text-[10px] font-mono font-medium text-muted-foreground uppercase tracking-wider">
            {language}
          </span>
          <button
            onClick={() => navigator.clipboard.writeText(value)}
            className="text-[10px] text-muted-foreground/60 hover:text-foreground transition-colors cursor-pointer"
          >
            Copy
          </button>
        </div>
      )}
      <SyntaxHighlighter
        language={language || 'text'}
        style={isDark ? oneDark : oneLight}
        customStyle={{
          margin: 0,
          padding: CODE_PREVIEW_CONFIG.padding,
          fontSize: CODE_PREVIEW_CONFIG.fontSize,
          lineHeight: CODE_PREVIEW_CONFIG.lineHeight,
          background: 'transparent',
        }}
        codeTagProps={{
          style: {
            fontFamily: CODE_PREVIEW_CONFIG.fontFamily,
          },
        }}
      >
        {value}
      </SyntaxHighlighter>
    </div>
  )
}

function MarkdownPreviewInner({ content, className, onToggleCheckbox, basePath }: MarkdownPreviewProps) {
  if (!content) return null

  let checkboxIndex = 0
  return (
    <div className={cn('markdown-preview', className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeRaw]}
        components={{
          img({ src, alt }) {
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
          code({ node, className: codeClass, children, ...props }) {
            const match = /language-(\w+)/.exec(codeClass || '')
            const isInline = !match && !String(children).includes('\n')

            if (isInline) {
              return (
                <code
                  className="px-1.5 py-0.5 mx-[0.1em] rounded-md bg-muted text-[0.85em] font-mono text-foreground border border-border/40 shadow-sm"
                  {...props}
                >
                  {children}
                </code>
              )
            }

            // Fenced code block
            const language = match ? match[1] : null
            const value = String(children).replace(/\n$/, '')
            return <CodeBlock language={language} value={value} />
          },
          input({ node: _node, ...props }) {
            if (props.type === 'checkbox') {
              const currentIndex = checkboxIndex++
              return (
                <input
                  type="checkbox"
                  checked={props.checked}
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
            return <input {...props} />
          },
          a({ node: _node, href, children, ...props }) {
            return (
              <a href={href} {...props}>
                {children}
              </a>
            )
          },
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}

/**
 * MarkdownPreview is wrapped in React.memo. The `content` prop is the
 * streaming-accumulated text. As long as the upstream segment reference
 * (and therefore this content string) is unchanged, this component
 * skips re-rendering — which is the single biggest win for streaming
 * markdown because react-markdown has no internal memoization and would
 * otherwise reparse the entire accumulated content on every token.
 *
 * Note: this is a guard for the case where the parent re-renders without
 * content actually changing. It does not help when content IS changing
 * (e.g. the actively-streaming segment) — that is addressed by
 * streamdown-preview in Phase 4.
 */
export const MarkdownPreview = memo(
  MarkdownPreviewInner,
  (prev, next) =>
    prev.content === next.content &&
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
