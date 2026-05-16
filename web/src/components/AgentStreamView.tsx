import { useState, useEffect, useRef } from 'react'
import type { AgentStreamState, Segment } from '@/types'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { Badge } from '@/components/ui/badge'
import { ChevronDown, ChevronRight, Loader2, CheckCircle2, XCircle, Clock } from 'lucide-react'

function ToolCallCard({ seg }: { seg: Segment & { type: 'tool_call' } }) {
  const [expanded, setExpanded] = useState(false)

  return (
    <div className="rounded-md border border-border/50 bg-muted/20 text-sm">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-muted/30 transition-colors"
      >
        {seg.done ? (
          seg.error ? (
            <XCircle className="h-4 w-4 shrink-0 text-destructive" />
          ) : (
            <CheckCircle2 className="h-4 w-4 shrink-0 text-green-500" />
          )
        ) : (
          <Loader2 className="h-4 w-4 shrink-0 animate-spin text-primary" />
        )}
        <span className="font-medium text-foreground">{seg.name}</span>
        <span className="ml-auto flex items-center gap-2 text-xs text-muted-foreground">
          {seg.done && seg.duration_ms > 0 && (
            <span className="flex items-center gap-1">
              <Clock className="h-3 w-3" />
              {(seg.duration_ms / 1000).toFixed(1)}s
            </span>
          )}
          {expanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        </span>
      </button>
      {expanded && (
        <div className="space-y-2 border-t border-border/50 px-3 py-2">
          {seg.args && (
            <div>
              <div className="mb-1 text-xs font-medium text-muted-foreground">Arguments</div>
              <pre className="whitespace-pre-wrap break-all rounded bg-muted/50 p-2 text-xs leading-relaxed">
                {seg.args}
              </pre>
            </div>
          )}
          {seg.done && seg.result && (
            <div>
              <div className="mb-1 text-xs font-medium text-muted-foreground">Result</div>
              <pre className="whitespace-pre-wrap break-all rounded bg-muted/50 p-2 text-xs leading-relaxed max-h-48 overflow-y-auto">
                {seg.result}
              </pre>
            </div>
          )}
          {seg.done && seg.error && (
            <div>
              <div className="mb-1 text-xs font-medium text-destructive">Error</div>
              <pre className="whitespace-pre-wrap break-all rounded bg-destructive/10 p-2 text-xs text-destructive">
                {seg.error}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function ThinkingBlock({ text, initiallyOpen }: { text: string; initiallyOpen?: boolean }) {
  const [open, setOpen] = useState(initiallyOpen ?? true)
  return (
    <div className="rounded-md border border-border/40">
      <button
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs font-medium text-muted-foreground hover:bg-muted/30 transition-colors"
      >
        {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        Thinking
        <Badge variant="outline" className="ml-auto text-[10px]">
          {text.length} chars
        </Badge>
      </button>
      {open && (
        <div className="border-t border-border/40 px-3 py-2">
          <pre className="whitespace-pre-wrap font-mono text-xs leading-relaxed text-muted-foreground">
            {text}
          </pre>
        </div>
      )}
    </div>
  )
}

function ContentBlock({ text }: { text: string }) {
  return (
    <div className="rounded-md border border-border/40 p-3">
      <MarkdownPreview content={text} />
    </div>
  )
}

interface AgentStreamViewProps {
  state: AgentStreamState
}

export function AgentStreamView({ state }: AgentStreamViewProps) {
  const bottomRef = useRef<HTMLDivElement>(null)
  const hasError = !!state.error

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [state.segments.length])

  if (hasError) {
    return (
      <div className="rounded-md border border-destructive/50 bg-destructive/5 p-4">
        <p className="text-sm font-medium text-destructive">Error</p>
        <p className="mt-1 text-sm text-destructive/80">{state.error}</p>
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {/* Status indicator */}
      {state.processing && (
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          <span>Processing{state.iteration > 0 ? ` (iteration ${state.iteration})` : ''}...</span>
        </div>
      )}

      {/* Segments in chronological order */}
      {state.segments.map((seg, i) => {
        switch (seg.type) {
          case 'thinking':
            return <ThinkingBlock key={i} text={seg.text} />
          case 'content':
            return <ContentBlock key={i} text={seg.text} />
          case 'tool_call':
            return <ToolCallCard key={seg.call_id || i} seg={seg} />
        }
      })}

      {/* Empty state */}
      {state.segments.length === 0 && !state.processing && (
        <p className="py-8 text-center text-sm text-muted-foreground">Agent idle, no output</p>
      )}

      <div ref={bottomRef} />
    </div>
  )
}
