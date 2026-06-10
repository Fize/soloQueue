import type { ChatMessage } from '@/types'
import {
  User, Sparkles, ChevronDown, ChevronRight, Loader2,
  AlertCircle, Copy, Check, Bot,
} from 'lucide-react'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { useState, useRef, useEffect } from 'react'

export interface ChatMessageProps {
  message: ChatMessage
  agentName?: string
}

export function ChatMessageView({ message, agentName = 'Assistant' }: ChatMessageProps) {
  const isUser = message.role === 'user'
  const isEmpty = message.segments.length === 0

  return (
    <div className={`group/message px-4 py-3 ${isUser ? 'flex justify-end' : ''}`}>
      <div className={`flex gap-3 ${isUser ? 'max-w-[80%] sm:max-w-[70%] lg:max-w-[60%] flex-row-reverse' : 'max-w-[90%] sm:max-w-[80%] lg:max-w-[70%]'}`}>
        {/* Avatar */}
        <div className="shrink-0 self-start">
          {isUser ? (
            <div className="h-7 w-7 rounded-full bg-primary/15 flex items-center justify-center">
              <User className="h-3.5 w-3.5 text-primary/70" />
            </div>
          ) : (
            <div className="h-7 w-7 rounded-full bg-gradient-to-br from-violet-500/20 to-purple-500/20 flex items-center justify-center">
              <Sparkles className="h-3.5 w-3.5 text-violet-500" />
            </div>
          )}
        </div>

        {/* Bubble */}
        <div className="min-w-0 w-fit max-w-full">
          {/* Role label */}
          <div className={`flex items-center gap-2 mb-1 ${isUser ? 'justify-end' : ''}`}>
            <span className={`text-[11px] font-medium ${isUser ? 'text-primary/60' : 'text-violet-500/60'}`}>
              {isUser ? 'You' : agentName}
            </span>
          </div>

          {/* Bubble content */}
          <div className={`rounded-2xl px-4 py-2.5 ${
            isUser
              ? 'bg-primary text-primary-foreground rounded-br-md'
              : 'bg-muted/60 text-foreground rounded-bl-md'
          }`}>
            {/* Message body */}
            {isEmpty ? (
              <LoadingIndicator isUser={isUser} />
            ) : (
              <div className="space-y-2">
                {message.segments.map((seg, i) => (
                  <SegmentView key={i} segment={seg} isUser={isUser} />
                ))}
              </div>
            )}
          </div>

          {/* Actions bar */}
          {!isEmpty && !isUser && (
            <div className="flex items-center gap-1 mt-1.5 opacity-0 group-hover/message:opacity-100 transition-opacity">
              <CopyButton text={extractFullContent(message)} />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function LoadingIndicator({ isUser }: { isUser?: boolean }) {
  return (
    <div className="flex items-center gap-2 py-1">
      <span className="inline-flex gap-0.5">
        <span className={`h-1.5 w-1.5 rounded-full animate-bounce [animation-delay:0ms] ${isUser ? 'bg-primary-foreground/60' : 'bg-violet-400'}`} />
        <span className={`h-1.5 w-1.5 rounded-full animate-bounce [animation-delay:150ms] ${isUser ? 'bg-primary-foreground/60' : 'bg-violet-400'}`} />
        <span className={`h-1.5 w-1.5 rounded-full animate-bounce [animation-delay:300ms] ${isUser ? 'bg-primary-foreground/60' : 'bg-violet-400'}`} />
      </span>
      <span className={`text-sm ${isUser ? 'text-primary-foreground/60' : 'text-muted-foreground/60'}`}>Thinking...</span>
    </div>
  )
}

function SegmentView({ segment, isUser }: { segment: ChatMessage['segments'][number]; isUser?: boolean }) {
  switch (segment.type) {
    case 'content':
      return (
        <MarkdownPreview
          content={segment.text}
          className={isUser ? 'text-primary-foreground [&_a]:text-primary-foreground/80 [&_code]:bg-primary-foreground/20 [&_pre]:bg-primary-foreground/10' : ''}
        />
      )
    case 'thinking':
      return <ThinkingSegment segment={segment} isUser={isUser} />
    case 'tool_call':
      return <ToolCallSegment segment={segment} isUser={isUser} />
    case 'delegation':
      return <SubagentCard segment={segment} isUser={isUser} />
    case 'error':
      return (
        <div className={`flex items-start gap-2 p-3 rounded-lg text-sm ${isUser ? 'bg-destructive/20 text-destructive-foreground' : 'bg-destructive/5 border border-destructive/20 text-destructive/90'}`}>
          <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
          <span className="whitespace-pre-wrap">{segment.text}</span>
        </div>
      )
    default:
      return null
  }
}

function SubagentCard({
  segment,
  isUser,
}: {
  segment: Extract<ChatMessage['segments'][number], { type: 'delegation' }>
  isUser?: boolean
}) {
  const [modalOpen, setModalOpen] = useState(false)
  const running = segment.status === 'running'
  const failed = segment.status === 'failed'
  const hasDetail = !!segment.resultContent

  return (
    <>
      <button
        onClick={() => { if (!running && hasDetail) setModalOpen(true) }}
        className={`w-full text-left text-xs border rounded-xl overflow-hidden transition-colors ${
          !running && hasDetail
            ? 'cursor-pointer hover:ring-1 hover:ring-violet-500/30'
            : 'cursor-default'
        } ${isUser ? 'border-primary-foreground/15 bg-primary-foreground/5' : 'border-violet-500/20 bg-violet-500/5'}`}
      >
        <div className={`flex items-center gap-2 w-full px-3 py-2 ${isUser ? 'text-primary-foreground/70' : 'text-muted-foreground'}`}>
          {running ? (
            <Loader2 className={`h-3.5 w-3.5 animate-spin ${isUser ? 'text-primary-foreground' : 'text-violet-500'}`} />
          ) : failed ? (
            <AlertCircle className="h-3.5 w-3.5 text-destructive" />
          ) : (
            <div className="h-3.5 w-3.5 rounded-full bg-emerald-500/20 flex items-center justify-center">
              <div className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
            </div>
          )}
          <Bot className={`h-3 w-3 ${isUser ? 'text-primary-foreground/50' : 'text-violet-500/60'}`} />
          <span className="font-medium">{segment.agentName}</span>
          {segment.durationMs != null && (
            <span className={`tabular-nums ${isUser ? 'text-primary-foreground/50' : 'text-muted-foreground/50'}`}>
              {(segment.durationMs / 1000).toFixed(1)}s
            </span>
          )}
          <span className="flex-1" />
          <span className={`text-[10px] uppercase tracking-wider ${isUser ? 'text-primary-foreground/40' : 'text-muted-foreground/40'}`}>
            {running ? 'Running...' : failed ? 'Failed' : 'Completed'}
          </span>
          {!running && hasDetail && (
            <ChevronRight className="h-3 w-3 text-muted-foreground/40" />
          )}
        </div>
      </button>

      {/* Modal for subagent detail */}
      {modalOpen && hasDetail && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm" onClick={() => setModalOpen(false)}>
          <div className="bg-background border border-border/60 rounded-2xl shadow-2xl max-w-2xl w-[90vw] max-h-[80vh] overflow-hidden" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between px-5 py-3 border-b border-border/40">
              <div className="flex items-center gap-2">
                <Bot className="h-4 w-4 text-violet-500" />
                <span className="text-sm font-semibold text-foreground">{segment.agentName}</span>
              </div>
              <button onClick={() => setModalOpen(false)} className="text-muted-foreground/50 hover:text-muted-foreground transition-colors p-1">
                <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M18 6L6 18M6 6l12 12"/></svg>
              </button>
            </div>
            <div className="p-5 overflow-y-auto max-h-[calc(80vh-60px)]">
              <pre className="whitespace-pre-wrap text-xs leading-relaxed text-foreground/80 font-mono">{segment.resultContent}</pre>
            </div>
          </div>
        </div>
      )}
    </>
  )
}

function ThinkingSegment({
  segment,
  isUser,
}: {
  segment: Extract<ChatMessage['segments'][number], { type: 'thinking' }>
  isUser?: boolean
}) {
  // Determine if this is still being streamed — no trailing newline means in-flight.
  const isStreaming = !segment.text.endsWith('\n')

  return (
    <details className="group/thinking" open={isStreaming}>
      <summary className={`flex items-center gap-1.5 text-xs cursor-pointer transition-colors py-1 ${isUser ? 'text-primary-foreground/60 hover:text-primary-foreground/80' : 'text-muted-foreground hover:text-foreground/70'}`}>
        {isStreaming ? (
          <span className="relative flex h-2 w-2 shrink-0">
            <span className={`absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping ${isUser ? 'bg-primary-foreground/40' : 'bg-violet-400'}`} />
            <span className={`relative inline-flex h-2 w-2 rounded-full ${isUser ? 'bg-primary-foreground/60' : 'bg-violet-500'}`} />
          </span>
        ) : (
          <div className="h-2 w-2 rounded-full bg-emerald-500/30 shrink-0 flex items-center justify-center">
            <div className="h-1 w-1 rounded-full bg-emerald-500" />
          </div>
        )}
        <span className="font-medium">thinking</span>
        <ChevronRight className="h-3 w-3 ml-auto group-open/thinking:hidden" />
        <ChevronDown className="h-3 w-3 ml-auto hidden group-open/thinking:block" />
      </summary>
      <div className={`mt-1 ml-5 pl-3 border-l-2 text-xs whitespace-pre-wrap leading-relaxed ${isUser ? 'border-primary-foreground/15 text-primary-foreground/65' : 'border-muted-foreground/20 text-muted-foreground/75'}`}>
        {segment.text}
      </div>
    </details>
  )
}

function ToolCallSegment({
  segment,
  isUser,
}: {
  segment: Extract<ChatMessage['segments'][number], { type: 'tool_call' }>
  isUser?: boolean
}) {
  const [expanded, setExpanded] = useState(false)
  const running = !segment.done

  return (
    <div className={`text-xs border rounded-xl overflow-hidden ${isUser ? 'border-primary-foreground/15 bg-primary-foreground/5' : 'border-border/60 bg-muted/20'}`}>
      <button
        onClick={() => setExpanded(!expanded)}
        className={`flex items-center gap-2 w-full px-3 py-2 transition-colors ${isUser ? 'text-primary-foreground/70 hover:text-primary-foreground' : 'text-muted-foreground hover:text-foreground'}`}
      >
        {running ? (
          <Loader2 className={`h-3.5 w-3.5 animate-spin ${isUser ? 'text-primary-foreground' : 'text-violet-500'}`} />
        ) : segment.error ? (
          <AlertCircle className="h-3.5 w-3.5 text-destructive" />
        ) : (
          <div className="h-3.5 w-3.5 rounded-full bg-emerald-500/20 flex items-center justify-center">
            <div className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
          </div>
        )}
        <span className="font-medium">{segment.name}</span>
        {segment.durationMs != null && (
          <span className={`tabular-nums ${isUser ? 'text-primary-foreground/50' : 'text-muted-foreground/50'}`}>{(segment.durationMs / 1000).toFixed(1)}s</span>
        )}
        <span className="flex-1" />
        <span className={`text-[10px] uppercase tracking-wider ${isUser ? 'text-primary-foreground/40' : 'text-muted-foreground/40'}`}>
          {running ? 'Running' : segment.error ? 'Failed' : 'Done'}
        </span>
        {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
      </button>
      {expanded && (
        <div className={`px-3 pb-2 space-y-2 border-t pt-2 ${isUser ? 'border-primary-foreground/10' : 'border-border/30'}`}>
          {segment.args && (
            <div>
              <div className={`text-[10px] font-semibold uppercase tracking-wider mb-1 ${isUser ? 'text-primary-foreground/40' : 'text-muted-foreground/50'}`}>
                Arguments
              </div>
              <pre className={`text-[11px] leading-relaxed whitespace-pre-wrap overflow-x-auto rounded-lg p-2 max-h-[150px] overflow-y-auto font-mono ${isUser ? 'bg-primary-foreground/10' : 'bg-muted/40'}`}>
                {tryPrettify(segment.args)}
              </pre>
            </div>
          )}
          {(segment.result || segment.error) && (
            <div>
              <div className={`text-[10px] font-semibold uppercase tracking-wider mb-1 ${isUser ? 'text-primary-foreground/40' : 'text-muted-foreground/50'}`}>
                {segment.error ? 'Error' : 'Result'}
              </div>
              <pre
                className={`text-[11px] leading-relaxed whitespace-pre-wrap overflow-x-auto rounded-lg p-2 max-h-[250px] overflow-y-auto font-mono ${
                  segment.error
                    ? 'bg-destructive/5 text-destructive/90'
                    : isUser ? 'bg-primary-foreground/10' : 'bg-muted/40'
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
