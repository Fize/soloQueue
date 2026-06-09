import type { ChatMessage } from '@/types'
import {
  User, Sparkles, ChevronDown, ChevronRight, Loader2,
  AlertCircle, Copy, Check,
} from 'lucide-react'
import { useState, useRef, useEffect } from 'react'

export interface ChatMessageProps {
  message: ChatMessage
}

export function ChatMessageView({ message }: ChatMessageProps) {
  const isUser = message.role === 'user'
  const hasError = message.segments.some((s) => s.type === 'error')
  const isEmpty = message.segments.length === 0

  return (
    <div className={`group/message ${isUser ? '' : 'bg-card/30'}`}>
      <div className={`mx-auto max-w-3xl px-4 py-4 ${isUser ? '' : ''}`}>
        <div className="flex gap-4">
          {/* Avatar */}
          <div className="shrink-0">
            {isUser ? (
              <div className="h-8 w-8 rounded-full bg-primary/10 flex items-center justify-center ring-1 ring-primary/20">
                <User className="h-4 w-4 text-primary" />
              </div>
            ) : (
              <div className="h-8 w-8 rounded-full bg-gradient-to-br from-violet-500/20 to-purple-500/20 flex items-center justify-center ring-1 ring-violet-500/30">
                <Sparkles className="h-4 w-4 text-violet-500" />
              </div>
            )}
          </div>

          {/* Content */}
          <div className="flex-1 min-w-0">
            {/* Role label */}
            <div className="flex items-center gap-2 mb-1">
              <span className={`text-xs font-semibold ${isUser ? 'text-primary/70' : 'text-violet-500/70'}`}>
                {isUser ? 'You' : 'SoloQueue'}
              </span>
            </div>

            {/* Message body */}
            {isEmpty ? (
              <LoadingIndicator />
            ) : (
              <div className="space-y-2">
                {message.segments.map((seg, i) => (
                  <SegmentView key={i} segment={seg} />
                ))}
              </div>
            )}

            {/* Actions bar */}
            {!isEmpty && !isUser && (
              <div className="flex items-center gap-1 mt-3 opacity-0 group-hover/message:opacity-100 transition-opacity">
                <CopyButton text={extractFullContent(message)} />
              </div>
            )}
          </div>

          {/* Error indicator */}
          {hasError && (
            <div className="shrink-0">
              <AlertCircle className="h-5 w-5 text-destructive" />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function LoadingIndicator() {
  return (
    <div className="flex items-center gap-2 py-1">
      <span className="inline-flex gap-0.5">
        <span className="h-1.5 w-1.5 rounded-full bg-violet-400 animate-bounce [animation-delay:0ms]" />
        <span className="h-1.5 w-1.5 rounded-full bg-violet-400 animate-bounce [animation-delay:150ms]" />
        <span className="h-1.5 w-1.5 rounded-full bg-violet-400 animate-bounce [animation-delay:300ms]" />
      </span>
      <span className="text-sm text-muted-foreground/60">Thinking...</span>
    </div>
  )
}

function SegmentView({ segment }: { segment: ChatMessage['segments'][number] }) {
  switch (segment.type) {
    case 'content':
      return (
        <div className="prose prose-sm dark:prose-invert max-w-none text-foreground/90 leading-relaxed whitespace-pre-wrap">
          {segment.text}
        </div>
      )
    case 'thinking':
      return (
        <details className="group/thinking">
          <summary className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer hover:text-foreground/70 transition-colors py-1">
            <ChevronRight className="h-3 w-3 group-open/thinking:hidden" />
            <ChevronDown className="h-3 w-3 hidden group-open/thinking:block" />
            <span className="font-medium">Reasoning</span>
          </summary>
          <div className="mt-1 ml-5 pl-3 border-l-2 border-muted-foreground/20 text-xs text-muted-foreground/75 whitespace-pre-wrap leading-relaxed">
            {segment.text}
          </div>
        </details>
      )
    case 'tool_call':
      return <ToolCallSegment segment={segment} />
    case 'error':
      return (
        <div className="flex items-start gap-2 p-3 rounded-lg bg-destructive/5 border border-destructive/20 text-sm text-destructive/90">
          <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
          <span className="whitespace-pre-wrap">{segment.text}</span>
        </div>
      )
    default:
      return null
  }
}

function ToolCallSegment({
  segment,
}: {
  segment: Extract<ChatMessage['segments'][number], { type: 'tool_call' }>
}) {
  const [expanded, setExpanded] = useState(false)
  const running = !segment.done

  return (
    <div className="text-xs border border-border/60 rounded-xl overflow-hidden bg-muted/20">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 w-full px-3 py-2 text-muted-foreground hover:text-foreground transition-colors"
      >
        {running ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin text-violet-500" />
        ) : segment.error ? (
          <AlertCircle className="h-3.5 w-3.5 text-destructive" />
        ) : (
          <div className="h-3.5 w-3.5 rounded-full bg-emerald-500/20 flex items-center justify-center">
            <div className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
          </div>
        )}
        <span className="font-medium">{segment.name}</span>
        {segment.durationMs != null && (
          <span className="text-muted-foreground/50 tabular-nums">{(segment.durationMs / 1000).toFixed(1)}s</span>
        )}
        <span className="flex-1" />
        <span className="text-[10px] uppercase tracking-wider text-muted-foreground/40">
          {running ? 'Running' : segment.error ? 'Failed' : 'Done'}
        </span>
        {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
      </button>
      {expanded && (
        <div className="px-3 pb-2 space-y-2 border-t border-border/30 pt-2">
          {segment.args && (
            <div>
              <div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/50 mb-1">
                Arguments
              </div>
              <pre className="text-[11px] leading-relaxed whitespace-pre-wrap overflow-x-auto bg-muted/40 rounded-lg p-2 max-h-[150px] overflow-y-auto font-mono">
                {tryPrettify(segment.args)}
              </pre>
            </div>
          )}
          {(segment.result || segment.error) && (
            <div>
              <div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/50 mb-1">
                {segment.error ? 'Error' : 'Result'}
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
        </div>
      )}
    </div>
  )
}

function tryPrettify(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    clearTimeout(timer.current)
    timer.current = setTimeout(() => setCopied(false), 2000)
  }

  useEffect(() => () => clearTimeout(timer.current), [])

  return (
    <button
      onClick={handleCopy}
      className="inline-flex items-center gap-1 px-2 py-1 rounded-md text-xs text-muted-foreground/50 hover:text-muted-foreground hover:bg-muted/50 transition-colors"
      title="Copy response"
    >
      {copied ? <Check className="h-3 w-3 text-emerald-500" /> : <Copy className="h-3 w-3" />}
      {copied ? 'Copied' : 'Copy'}
    </button>
  )
}

function extractFullContent(msg: ChatMessage): string {
  return msg.segments
    .filter((s) => s.type === 'content')
    .map((s: any) => s.text)
    .join('')
}
