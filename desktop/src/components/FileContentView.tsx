import { useTranslation } from '@/lib/i18n'
import { useState, useEffect, useRef } from 'react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { getFileUrl, toggleFileCheckbox } from '@/lib/api'
import { Loader2, FileIcon, Copy, Check, MousePointer2, Edit3, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { CODE_PREVIEW_CONFIG } from '@/lib/theme'
import { DesignPreview } from './DesignPreview'
import { useRuntimeStore } from '@/stores/runtimeStore'
import type { PreviewCommentSnapshot } from '@/types/annotation'

function wrapInCodeBlock(content: string, language: string): string {
  const matches = content.match(/`+/g)
  const maxBackticks = matches ? Math.max(...matches.map((m) => m.length)) : 0
  const fenceSize = Math.max(3, maxBackticks + 1)
  const fence = '`'.repeat(fenceSize)
  return `${fence}${language}\n${content}\n${fence}`
}

import {
  extToLanguage,
  imageExtensions,
  audioExtensions,
  videoExtensions,
  isBinaryFile,
  looksBinary,
  getExt,
} from '@/lib/file'

interface FileContentViewProps {
  path: string | null
  onError?: (path: string) => void
  onClose?: () => void
}

export function FileContentView({ path, onError, onClose }: FileContentViewProps) {
  const { t } = useTranslation()
  const [content, setContent] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [viewMode, setViewMode] = useState<'preview' | 'raw' | 'design'>('preview')
  const setSidebarCollapsed = useRuntimeStore((s) => s.setSidebarCollapsed)
  const [designMode, setDesignMode] = useState<'click' | 'draw' | 'interact'>('interact')
  const [selectedTarget, setSelectedTarget] = useState<PreviewCommentSnapshot | null>(null)

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
    setDesignMode('interact')
    setSelectedTarget(null)

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
    fetch(getFileUrl(path), { cache: 'no-store' })
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
            {(ext === '.html' || ext === '.htm') && (
              <button
                type="button"
                onClick={() => {
                  setViewMode('design')
                  setSidebarCollapsed(true)
                }}
                className={`px-2.5 py-1 text-[11px] font-medium transition-colors cursor-pointer border-l border-border/60 ${
                  viewMode === 'design'
                    ? 'bg-accent text-accent-foreground'
                    : 'text-muted-foreground hover:text-foreground hover:bg-muted/30'
                }`}
              >
                Design Mode
              </button>
            )}
          </div>
        )}
        <div className="flex items-center gap-0.5">
          {isTextFile && content !== null && (
            <Button
              variant="ghost"
              size="icon"
              onClick={handleCopy}
              title={copied ? t('common.copied') : t('common.copyRawContent')}
            >
              {copied ? (
                <Check className="h-3.5 w-3.5 text-[var(--success)]" />
              ) : (
                <Copy className="h-3.5 w-3.5" />
              )}
            </Button>
          )}
          {onClose && (
            <Button
              variant="ghost"
              size="icon"
              onClick={onClose}
              title={t('common.closeDialog')}
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
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
            <pre
              className="whitespace-pre-wrap break-words"
              style={{
                fontSize: CODE_PREVIEW_CONFIG.fontSize,
                lineHeight: CODE_PREVIEW_CONFIG.lineHeight,
                fontFamily: CODE_PREVIEW_CONFIG.fontFamily,
              }}
            >
              {content}
            </pre>
          )}

          {isTextFile && content !== null && viewMode === 'design' && (
            <div className="flex w-full h-[65vh] md:h-[75vh] border rounded-lg overflow-hidden border-border/60 shadow-sm">
              {/* Annotation Dialog (Left Side) */}
              <div className="w-[300px] border-r border-border/40 bg-card/50 flex flex-col shrink-0 overflow-hidden relative z-10">
                 <div className="p-3 border-b border-border/40 font-semibold text-xs flex items-center justify-between bg-card text-foreground/90 uppercase tracking-wider">
                   <span>Design Annotations</span>
                 </div>
                 <div className="p-3 flex gap-2 border-b border-border/20 bg-muted/20">
                   <Button 
                      variant={designMode === 'click' ? 'default' : 'outline'} 
                      size="sm"
                      onClick={() => setDesignMode(designMode === 'click' ? 'interact' : 'click')}
                   >
                     <MousePointer2 className="w-3.5 h-3.5 mr-1.5" />
                     Pick Element
                   </Button>
                   <Button 
                      variant={designMode === 'draw' ? 'default' : 'outline'} 
                      size="sm"
                      onClick={() => setDesignMode(designMode === 'draw' ? 'interact' : 'draw')}
                   >
                     <Edit3 className="w-3.5 h-3.5 mr-1.5" />
                     {t('common.drawRegion')}
                   </Button>
                 </div>
                 <div className="flex-1 overflow-y-auto p-4 space-y-4">
                   {selectedTarget ? (
                     <div className="text-xs bg-muted/30 border border-border/50 p-3 rounded-lg shadow-sm">
                       <div className="font-semibold mb-1 text-primary">Target Selected</div>
                       <div className="text-muted-foreground mb-3 break-all font-mono text-[10px]">{selectedTarget.label}</div>
                       <textarea 
                         placeholder={t('common.designPlaceholder')}
                         className="w-full h-24 bg-background border border-border/40 rounded p-2 text-xs resize-none focus:outline-none focus:ring-1 focus:ring-primary placeholder:text-muted-foreground/50"
                       />
                       <Button size="sm" className="w-full mt-3 h-8 text-xs font-medium">{t('common.sendAnnotation')}</Button>
                     </div>
                   ) : (
                     <div className="flex flex-col items-center justify-center text-center h-full text-muted-foreground opacity-70">
                       <MousePointer2 className="h-8 w-8 mb-3 opacity-20" />
                       <span className="text-[11px] max-w-[200px] leading-relaxed">
                         Select an element or draw a region on the right to add an annotation.
                       </span>
                     </div>
                   )}
                 </div>
              </div>

              {/* Design Preview (Right Side) */}
              <div className="flex-1 bg-white overflow-hidden relative">
                <DesignPreview
                  htmlContent={content}
                  mode={designMode}
                  onSelectTarget={(t) => {
                     setSelectedTarget(t)
                     if (t) setDesignMode('interact') // Auto-switch out of mode after picking
                  }}
                />
              </div>
            </div>
          )}

          {isTextFile && content !== null && viewMode === 'preview' && (
            <>
              {isMarkdown && (
                <MarkdownPreview
                  content={content}
                  basePath={path}
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
                <MarkdownPreview content={wrapInCodeBlock(content, language)} />
              )}
              {!isMarkdown && !language && (
                <pre
                  className="whitespace-pre-wrap break-words"
                  style={{
                    fontSize: CODE_PREVIEW_CONFIG.fontSize,
                    lineHeight: CODE_PREVIEW_CONFIG.lineHeight,
                    fontFamily: CODE_PREVIEW_CONFIG.fontFamily,
                  }}
                >
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
