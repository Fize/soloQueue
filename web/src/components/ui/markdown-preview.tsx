import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'

interface MarkdownPreviewProps {
  content: string
  className?: string
  onToggleCheckbox?: (index: number) => void
}

export function MarkdownPreview({ content, className, onToggleCheckbox }: MarkdownPreviewProps) {
  const navigate = useNavigate()
  if (!content) return null

  let checkboxIndex = 0

  return (
    <div className={cn('markdown-preview', className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
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
                  className="mr-2 cursor-pointer h-4 w-4 rounded border-gray-300 text-primary focus:ring-primary"
                />
              )
            }
            return <input {...props} />
          },
          a({ node: _node, href, children, ...props }) {
            if (
              href &&
              (href.startsWith('file://') ||
                href.startsWith('/') ||
                href.startsWith('\\') ||
                href.includes(':\\') ||
                href.includes(':/'))
            ) {
              let cleanPath = href
              if (href.startsWith('file://')) {
                cleanPath = href.replace(/^file:\/\//, '')
                // On Windows, file:///C:/path starts with /C:/, strip leading /
                if (cleanPath.match(/^\/[a-zA-Z]:/)) {
                  cleanPath = cleanPath.substring(1)
                }
              }

              return (
                <a
                  href="#"
                  onClick={(e) => {
                    e.preventDefault()
                    navigate(`/files?path=${encodeURIComponent(cleanPath)}`)
                  }}
                  className="text-primary hover:underline cursor-pointer"
                  {...props}
                >
                  {children}
                </a>
              )
            }
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
