import { useState, useEffect } from 'react'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { getFileUrl } from '@/lib/api'
import { Loader2, FileIcon } from 'lucide-react'
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

function getExt(path: string): string {
  const name = path.split('/').pop() ?? path
  const dot = name.lastIndexOf('.')
  if (dot === -1) return ''
  return name.slice(dot).toLowerCase()
}

interface FilePreviewProps {
  path: string
  open: boolean
  onClose: () => void
}

export function FilePreview({ path, open, onClose }: FilePreviewProps) {
  const [content, setContent] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const ext = getExt(path)
  const fileName = path.split('/').pop() ?? path
  const isImage = imageExtensions.includes(ext)
  const isAudio = audioExtensions.includes(ext)
  const isVideo = videoExtensions.includes(ext)
  const isMarkdown = ext === '.md' || ext === '.markdown'
  const language = extToLanguage[ext]

  useEffect(() => {
    if (!open) {
      setContent(null)
      setError(null)
      return
    }

    if (isImage || isAudio || isVideo) {
      setLoading(false)
      return
    }

    setLoading(true)
    setError(null)
    fetch(getFileUrl(path))
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.text()
      })
      .then((text) => {
        setContent(text)
        setLoading(false)
      })
      .catch((err) => {
        setError(err.message)
        setLoading(false)
      })
  }, [open, path, isImage, isAudio, isVideo])

  function renderContent() {
    if (loading) {
      return (
        <div className="flex items-center justify-center py-20">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
      )
    }

    if (error) {
      return (
        <div className="flex flex-col items-center justify-center gap-2 py-16 text-muted-foreground">
          <FileIcon className="h-8 w-8" />
          <p className="text-sm">Unable to preview this file</p>
          <p className="text-xs opacity-60">{error}</p>
        </div>
      )
    }

    if (isImage) {
      return (
        <div className="flex items-center justify-center p-2">
          <img
            src={getFileUrl(path)}
            alt={fileName}
            className="max-h-[70vh] max-w-full rounded object-contain"
          />
        </div>
      )
    }

    if (isAudio) {
      return (
        <div className="flex flex-col items-center gap-3 py-8">
          <span className="text-sm text-muted-foreground">{fileName}</span>
          <audio controls src={getFileUrl(path)} className="w-full max-w-md" />
        </div>
      )
    }

    if (isVideo) {
      return (
        <div className="flex flex-col items-center gap-3 py-4">
          <video controls src={getFileUrl(path)} className="max-h-[65vh] max-w-full rounded" />
        </div>
      )
    }

    if (content === null) {
      return null
    }

    if (isMarkdown) {
      return <MarkdownPreview content={content} />
    }

    if (language) {
      return (
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
      )
    }

    return (
      <pre className="whitespace-pre-wrap break-words rounded-lg bg-muted p-4 text-xs font-mono leading-relaxed">
        {content}
      </pre>
    )
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-2xl max-h-[85vh] flex flex-col p-0 overflow-hidden">
        <DialogHeader className="px-6 pt-6 pb-0">
          <DialogTitle className="text-sm font-mono truncate pr-4">{fileName}</DialogTitle>
          <p className="text-xs text-muted-foreground truncate">{path}</p>
        </DialogHeader>

        <ScrollArea className="flex-1 max-h-[calc(85vh-7rem)] px-6 py-4">
          {renderContent()}
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
