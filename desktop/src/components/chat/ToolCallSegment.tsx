import { useState, useMemo } from 'react'
import { Loader2, AlertCircle, ChevronRight, ChevronDown } from 'lucide-react'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { getFileUrl } from '@/lib/api'
import { formatToolCallHeader } from '@/lib/utils'
import { useTranslation } from '@/lib/i18n'
import type { ChatMessage } from '@/types'

export function ToolCallSegment({
  segment,
  isUser,
  onUserInteraction,
}: {
  segment: Extract<ChatMessage['segments'][number], { type: 'tool_call' }>
  isUser?: boolean
  onUserInteraction?: () => void
}) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const isDesignMode = useRuntimeStore((s) => s.isDesignMode)
  const compact = isDesignMode
  const running = !segment.done

  return (
    <div
      className={`text-xs border rounded-xl overflow-hidden w-full border-border/60 bg-muted/20`}
    >
      <button
        onClick={() => {
          setExpanded(!expanded)
          onUserInteraction?.()
        }}
        className={`flex items-center gap-2 w-full ${compact ? 'px-2 py-1.5' : 'px-3 py-2'} transition-colors text-muted-foreground hover:text-foreground`}
      >
        {running ? (
          <Loader2
            className={`h-3.5 w-3.5 animate-spin ${isUser ? 'text-primary-foreground' : 'text-primary'}`}
          />
        ) : segment.error ? (
          <AlertCircle className="h-3.5 w-3.5 text-destructive" />
        ) : (
          <div className="h-3.5 w-3.5 rounded-full bg-success/20 flex items-center justify-center">
            <div className="h-1.5 w-1.5 rounded-full bg-success" />
          </div>
        )}
        <span className="font-mono text-[11px] text-left truncate flex-1 min-w-0 whitespace-nowrap">
          {formatToolCallHeader(segment.name, segment.args).replace(/\r?\n/g, ' ')}
        </span>
        <div className="flex items-center gap-2 text-xs shrink-0 select-none">
          {segment.durationMs != null && (
            <span
              className={`tabular-nums text-[10px] text-muted-foreground/50`}
            >
              {(segment.durationMs / 1000).toFixed(1)}s
            </span>
          )}
          <span
            className={`text-[10px] uppercase tracking-wider text-muted-foreground/40`}
          >
            {running ? t('common.running') : segment.error ? t('common.failed') : t('common.done')}
          </span>
        </div>
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground/60" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground/60" />
        )}
      </button>
      {expanded && (
        <div
          className={`${compact ? 'px-2 pb-1.5 pt-1.5' : 'px-3 pb-2 pt-2'} space-y-2 border-t border-border/30`}
        >
          {segment.args && (
            <div>
              <div
                className={`text-[10px] font-semibold uppercase tracking-wider mb-1 text-muted-foreground/50`}
              >
                {t('common.arguments')}
              </div>
              <pre
                className={`text-[11px] leading-relaxed whitespace-pre-wrap overflow-x-auto rounded-lg p-2 max-h-[150px] overflow-y-auto font-mono bg-muted/40`}
              >
                {tryPrettify(segment.args)}
              </pre>
            </div>
          )}
          {(segment.result || segment.error) && (
            <div>
              <div
                className={`text-[10px] font-semibold uppercase tracking-wider mb-1 text-muted-foreground/50`}
              >
                {segment.error ? t('common.error') : t('common.result')}
              </div>
              <pre
                className={`text-[11px] leading-relaxed whitespace-pre-wrap overflow-x-auto rounded-lg p-2 max-h-[250px] overflow-y-auto font-mono ${
                  segment.error
                    ? 'bg-destructive/5 text-destructive/90'
                    : 'bg-muted/40'
                }`}
              >
                {segment.error || segment.result}
              </pre>
            </div>
          )}
          {!segment.error &&
            segment.result &&
            (segment.name === 'ImageGenerate' ||
              segment.name === 'ImageEdit' ||
              segment.name === 'SendFile') && (
              <ImageResultPreviews
                result={segment.result}
                toolName={segment.name}
                isUser={isUser}
              />
            )}
        </div>
      )}
    </div>
  )
}

export function tryPrettify(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

export function extractImagePaths(result: string, toolName: string): string[] {
  try {
    const parsed = JSON.parse(result)
    if (toolName === 'SendFile') {
      if (parsed.status === 'success' && parsed.file_type === 'image' && parsed.path) {
        return [parsed.path]
      }
    } else {
      if (
        parsed.status === 'completed' &&
        Array.isArray(parsed.local_paths) &&
        parsed.local_paths.length > 0
      ) {
        return parsed.local_paths
      }
    }
  } catch {
    // Ignore JSON parsing errors
  }
  return []
}

export function MessageImageGallery({
  segments,
  isUser,
}: {
  segments: ChatMessage['segments']
  isUser?: boolean
}) {
  const { t } = useTranslation()
  const isDesignMode = useRuntimeStore((s) => s.isDesignMode)
  const compact = isDesignMode
  const imagePaths = useMemo(() => {
    const paths: string[] = []
    for (const seg of segments) {
      if (seg.type === 'tool_call' && seg.done && !seg.error && seg.result) {
        paths.push(...extractImagePaths(seg.result, seg.name))
      }
    }
    return paths
  }, [segments])

  if (imagePaths.length === 0) return null

  return (
    <div className="mt-3 pt-3 border-t border-border/10">
      <div
        className={`text-[10px] font-semibold uppercase tracking-wider mb-2 ${
          isUser ? 'text-primary-foreground/40' : 'text-muted-foreground/50'
        }`}
      >
        {t('common.images', { count: imagePaths.length })}
      </div>
      <div className={`grid ${compact ? 'grid-cols-1' : 'grid-cols-2 sm:grid-cols-3'} gap-2`}>
        {imagePaths.map((path, i) => {
          const url = getFileUrl(path)
          return (
            <a
              key={i}
              href={url}
              target="_blank"
              rel="noopener noreferrer"
              className="block rounded-lg overflow-hidden border border-border/40 hover:border-primary/40 transition-colors"
            >
              <img
                src={url}
                alt={`Image ${i + 1}`}
                className="w-full h-32 object-cover bg-black/5"
                loading="lazy"
              />
            </a>
          )
        })}
      </div>
    </div>
  )
}

export function ImageResultPreviews({
  result,
  toolName,
  isUser,
}: {
  result: string
  toolName: string
  isUser?: boolean
}) {
  const { t } = useTranslation()
  const paths = extractImagePaths(result, toolName)
  if (paths.length === 0) return null
  return (
    <div>
      <div
        className={`text-[10px] font-semibold uppercase tracking-wider mb-2 ${isUser ? 'text-primary-foreground/40' : 'text-muted-foreground/50'}`}
      >
        {t('common.generatedImages')}
      </div>
      <div className="grid grid-cols-1 gap-2">
        {paths.map((path, i) => {
          const url = getFileUrl(path)
          return (
            <a key={i} href={url} target="_blank" rel="noopener noreferrer" className="block">
              <img
                src={url}
                alt={`Generated image ${i + 1}`}
                className="rounded-lg border border-border/50 max-h-64 object-contain bg-black/5"
                loading="lazy"
              />
            </a>
          )
        })}
      </div>
    </div>
  )
}
