import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { PrismLight as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism'

// Register common languages for chat code blocks
import tsx from 'react-syntax-highlighter/dist/esm/languages/prism/tsx'
import typescript from 'react-syntax-highlighter/dist/esm/languages/prism/typescript'
import javascript from 'react-syntax-highlighter/dist/esm/languages/prism/javascript'
import jsx from 'react-syntax-highlighter/dist/esm/languages/prism/jsx'
import python from 'react-syntax-highlighter/dist/esm/languages/prism/python'
import rust from 'react-syntax-highlighter/dist/esm/languages/prism/rust'
import go from 'react-syntax-highlighter/dist/esm/languages/prism/go'
import c from 'react-syntax-highlighter/dist/esm/languages/prism/c'
import cpp from 'react-syntax-highlighter/dist/esm/languages/prism/cpp'
import java from 'react-syntax-highlighter/dist/esm/languages/prism/java'
import json from 'react-syntax-highlighter/dist/esm/languages/prism/json'
import yaml from 'react-syntax-highlighter/dist/esm/languages/prism/yaml'
import css from 'react-syntax-highlighter/dist/esm/languages/prism/css'
import markup from 'react-syntax-highlighter/dist/esm/languages/prism/markup'
import bash from 'react-syntax-highlighter/dist/esm/languages/prism/bash'
import sql from 'react-syntax-highlighter/dist/esm/languages/prism/sql'
import toml from 'react-syntax-highlighter/dist/esm/languages/prism/toml'
import docker from 'react-syntax-highlighter/dist/esm/languages/prism/docker'
import graphql from 'react-syntax-highlighter/dist/esm/languages/prism/graphql'
import protobuf from 'react-syntax-highlighter/dist/esm/languages/prism/protobuf'

SyntaxHighlighter.registerLanguage('tsx', tsx)
SyntaxHighlighter.registerLanguage('typescript', typescript)
SyntaxHighlighter.registerLanguage('javascript', javascript)
SyntaxHighlighter.registerLanguage('jsx', jsx)
SyntaxHighlighter.registerLanguage('python', python)
SyntaxHighlighter.registerLanguage('rust', rust)
SyntaxHighlighter.registerLanguage('go', go)
SyntaxHighlighter.registerLanguage('c', c)
SyntaxHighlighter.registerLanguage('cpp', cpp)
SyntaxHighlighter.registerLanguage('java', java)
SyntaxHighlighter.registerLanguage('json', json)
SyntaxHighlighter.registerLanguage('yaml', yaml)
SyntaxHighlighter.registerLanguage('css', css)
SyntaxHighlighter.registerLanguage('markup', markup)
SyntaxHighlighter.registerLanguage('bash', bash)
SyntaxHighlighter.registerLanguage('sql', sql)
SyntaxHighlighter.registerLanguage('toml', toml)
SyntaxHighlighter.registerLanguage('docker', docker)
SyntaxHighlighter.registerLanguage('graphql', graphql)
SyntaxHighlighter.registerLanguage('protobuf', protobuf)

interface MarkdownPreviewProps {
  content: string
  className?: string
  onToggleCheckbox?: (index: number) => void
}

function CodeBlock({
  language,
  value,
}: {
  language: string | null
  value: string
}) {
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
        style={oneLight}
        customStyle={{
          margin: 0,
          padding: language ? '0.75rem 1rem' : '0.75rem 1rem',
          fontSize: '0.8125rem',
          lineHeight: '1.6',
          background: 'transparent',
        }}
        codeTagProps={{
          style: {
            fontFamily:
              'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
          },
        }}
      >
        {value}
      </SyntaxHighlighter>
    </div>
  )
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
          code({ node, className: codeClass, children, ...props }) {
            const match = /language-(\w+)/.exec(codeClass || '')
            const isInline = !match && !String(children).includes('\n')

            if (isInline) {
              return (
                <code
                  className="px-1.5 py-0.5 rounded-md bg-muted/60 text-[0.85em] font-mono text-amber-600 dark:text-amber-400"
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
