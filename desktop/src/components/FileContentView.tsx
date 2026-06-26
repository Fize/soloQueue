import { useState, useEffect, useRef } from 'react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { getFileUrl, toggleFileCheckbox } from '@/lib/api'
import { Loader2, FileIcon, Copy, Check } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { PrismLight as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism'

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

const extToLanguage: Record<string, string> = {
  '.ts': 'typescript',
  '.tsx': 'tsx',
  '.js': 'javascript',
  '.jsx': 'jsx',
  '.mjs': 'javascript',
  '.cjs': 'javascript',
  '.py': 'python',
  '.pyi': 'python',
  '.rs': 'rust',
  '.go': 'go',
  '.c': 'c',
  '.cpp': 'cpp',
  '.cc': 'cpp',
  '.cxx': 'cpp',
  '.h': 'c',
  '.hpp': 'cpp',
  '.hxx': 'cpp',
  '.java': 'java',
  '.kt': 'java',
  '.json': 'json',
  '.yaml': 'yaml',
  '.yml': 'yaml',
  '.css': 'css',
  '.scss': 'css',
  '.html': 'markup',
  '.htm': 'markup',
  '.xml': 'markup',
  '.svg': 'markup',
  '.sh': 'bash',
  '.bash': 'bash',
  '.zsh': 'bash',
  '.sql': 'sql',
  '.toml': 'toml',
  '.dockerfile': 'docker',
  '.graphql': 'graphql',
  '.gql': 'graphql',
  '.proto': 'protobuf',
}
const imageExtensions = ['.png', '.jpg', '.jpeg', '.gif', '.svg', '.webp', '.bmp', '.ico']
const audioExtensions = ['.mp3', '.wav', '.ogg', '.flac', '.aac', '.m4a', '.opus']
const videoExtensions = ['.mp4', '.webm', '.mov', '.avi', '.mkv']

// Known plain text extensions (mirrors backend textExtensions) — anything NOT in
// extToLanguage + image/audio/video + plainText + markdown is treated as binary.
const plainTextExtensions = new Set([
  '.markdown', '.txt', '.log', '.mod', '.sum', '.pyx',
  '.kts', '.scala', '.ini', '.cfg', '.less', '.fish', '.psql',
  '.Makefile', '.dockerignore', '.gitignore', '.env', '.envrc',
  '.lua', '.rb', '.php', '.swift', '.r', '.dart',
  '.tf', '.hcl', '.vue', '.svelte',
])

// Well-known text filenames that don't have extensions.
const knownTextFilenames = new Set([
  'Dockerfile', 'Makefile', 'README', 'LICENSE', 'CHANGELOG',
  'Procfile', 'Jenkinsfile', 'Vagrantfile',
])

function isBinaryFile(path: string): boolean {
  const ext = getExt(path)
  if (!ext) {
    // No extension — only treat as text if it's a well-known text filename.
    const name = path.split('/').pop() ?? path
    return !knownTextFilenames.has(name)
  }
  if (extToLanguage[ext]) return false
  if (ext === '.md' || ext === '.markdown') return false
  if (imageExtensions.includes(ext)) return false
  if (audioExtensions.includes(ext)) return false
  if (videoExtensions.includes(ext)) return false
  if (plainTextExtensions.has(ext)) return false
  return true
}

// looksBinary checks whether the first N bytes contain a NUL byte (0x00),
// matching the backend's binary detection heuristic.
function looksBinary(buf: ArrayBuffer): boolean {
  const n = Math.min(buf.byteLength, 512)
  const view = new Uint8Array(buf, 0, n)
  for (let i = 0; i < n; i++) {
    if (view[i] === 0) return true
  }
  return false
}

function getExt(path: string): string {
  const name = path.split('/').pop() ?? path
  const dot = name.lastIndexOf('.')
  if (dot === -1) return ''
  return name.slice(dot).toLowerCase()
}

interface FileContentViewProps {
  path: string | null
  onError?: (path: string) => void
}

export function FileContentView({ path, onError }: FileContentViewProps) {
  const [content, setContent] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [viewMode, setViewMode] = useState<'preview' | 'raw'>('preview')

  // Use a ref so we can call the latest onError without adding it to the
  // effect dependency array (avoiding re-fetch when the parent re-renders).
  const onErrorRef = useRef(onError)
  onErrorRef.current = onError

  useEffect(() => {
    if (!path) {
      setContent(null)
      setError(null)
      return
    }

    const ext = getExt(path)
    // Reset view mode when switching files
    setViewMode('preview')

    if (
      imageExtensions.includes(ext) ||
      audioExtensions.includes(ext) ||
      videoExtensions.includes(ext) ||
      isBinaryFile(path)
    ) {
      setContent(null)
      setError(null)
      setLoading(false)
      return
    }

    setLoading(true)
    setError(null)
    fetch(getFileUrl(path))
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.arrayBuffer()
      })
      .then((buf) => {
        // Content-based binary detection: if the file contains NUL bytes it
        // is binary regardless of extension. Prevents freezing the renderer.
        if (looksBinary(buf)) {
          setContent(null)
          setError(null)
          setLoading(false)
          return
        }
        const decoder = new TextDecoder()
        setContent(decoder.decode(buf))
        setLoading(false)
      })
      .catch((err) => {
        setError(err.message)
        setLoading(false)
        onErrorRef.current?.(path)
      })
  }, [path, refreshKey])

  if (!path) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-sm text-muted-foreground">Select a file to preview</p>
      </div>
    )
  }

  const ext = getExt(path)
  const isImage = imageExtensions.includes(ext)
  const isAudio = audioExtensions.includes(ext)
  const isVideo = videoExtensions.includes(ext)
  const isBinary = isBinaryFile(path)
  const isTextFile = !isImage && !isAudio && !isVideo && !isBinary
  const isMarkdown = ext === '.md' || ext === '.markdown'
  const language = extToLanguage[ext]
  const fileName = path.split('/').pop() ?? path

  function handleCopy() {
    if (!content) return
    navigator.clipboard
      .writeText(content)
      .then(() => {
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
      })
      .catch(() => {})
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
      <div className="flex h-full flex-col items-center justify-center gap-2 text-muted-foreground">
        <FileIcon className="h-8 w-8" />
        <p className="text-sm">Unable to preview</p>
        <p className="text-xs opacity-60">{error}</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full">
      <div className="px-4 py-2 border-b flex items-center justify-between shrink-0 gap-2">
        <p className="text-xs font-mono text-muted-foreground truncate flex-1 min-w-0">{fileName}</p>
        {isTextFile && content !== null && (
          <div className="flex items-center rounded-md border border-border/60 overflow-hidden">
            <button
              type="button"
              onClick={() => setViewMode('preview')}
              className={`px-2.5 py-1 text-[11px] font-medium transition-colors cursor-pointer ${
                viewMode === 'preview'
                  ? 'bg-accent text-accent-foreground'
                  : 'text-muted-foreground hover:text-foreground hover:bg-muted/30'
              }`}
            >
              Preview
            </button>
            <button
              type="button"
              onClick={() => setViewMode('raw')}
              className={`px-2.5 py-1 text-[11px] font-medium transition-colors cursor-pointer border-l border-border/60 ${
                viewMode === 'raw'
                  ? 'bg-accent text-accent-foreground'
                  : 'text-muted-foreground hover:text-foreground hover:bg-muted/30'
              }`}
            >
              Raw
            </button>
          </div>
        )}
        {isTextFile && content !== null && (
          <Button
            variant="ghost"
            size="icon"
            onClick={handleCopy}
            title={copied ? 'Copied!' : 'Copy content'}
          >
            {copied ? (
              <Check className="h-3.5 w-3.5 text-[var(--success)]" />
            ) : (
              <Copy className="h-3.5 w-3.5" />
            )}
          </Button>
        )}
      </div>

      <ScrollArea className="flex-1 min-h-0">
        <div className="p-4">
          {isBinary && (
            <div className="flex flex-col items-center justify-center py-16 gap-3 text-muted-foreground">
              <FileIcon className="h-10 w-10 opacity-30" />
              <p className="text-sm font-medium">Cannot preview binary file</p>
              <p className="text-xs opacity-50">This file format is not supported for preview.</p>
            </div>
          )}

          {isImage && (
            <div className="flex items-center justify-center">
              <img
                src={getFileUrl(path)}
                alt={fileName}
                className="max-h-[65vh] max-w-full rounded object-contain"
              />
            </div>
          )}

          {isAudio && (
            <div className="flex flex-col items-center gap-3 py-8">
              <audio controls src={getFileUrl(path)} className="w-full max-w-md" />
            </div>
          )}

          {isVideo && (
            <div className="flex flex-col items-center gap-3 py-4">
              <video controls src={getFileUrl(path)} className="max-h-[55vh] max-w-full rounded" />
            </div>
          )}

          {isTextFile && content !== null && viewMode === 'raw' && (
            <pre className="whitespace-pre-wrap break-words rounded-lg bg-muted p-4 text-xs font-mono leading-relaxed">
              {content}
            </pre>
          )}

          {isTextFile && content !== null && viewMode === 'preview' && (
            <>
              {isMarkdown && (
                <MarkdownPreview
                  content={content}
                  onToggleCheckbox={async (index) => {
                    if (!path) return
                    try {
                      await toggleFileCheckbox(path, index)
                      setRefreshKey((k) => k + 1)
                    } catch (err) {
                      console.error('Failed to toggle checkbox:', err)
                    }
                  }}
                />
              )}
              {language && (
                <SyntaxHighlighter
                  language={language}
                  style={oneLight}
                  customStyle={{
                    margin: 0,
                    borderRadius: '0.5rem',
                    fontSize: '0.8125rem',
                    lineHeight: '1.6',
                  }}
                  showLineNumbers
                >
                  {content}
                </SyntaxHighlighter>
              )}
              {!isMarkdown && !language && (
                <pre className="whitespace-pre-wrap break-words rounded-lg bg-muted p-4 text-xs font-mono leading-relaxed">
                  {content}
                </pre>
              )}
            </>
          )}
        </div>
      </ScrollArea>
    </div>
  )
}
