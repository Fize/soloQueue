import { useState, useEffect, useRef } from 'react';
import type { AgentStreamState, ToolCallState } from '@/types';
import { MarkdownPreview } from '@/components/ui/markdown-preview';
import { Badge } from '@/components/ui/badge';
import { ChevronDown, ChevronRight, Loader2, CheckCircle2, XCircle, Clock } from 'lucide-react';

function ToolCallCard({ tc }: { tc: ToolCallState }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="rounded-md border border-border/50 bg-muted/20 text-sm">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-muted/30 transition-colors"
      >
        {tc.done ? (
          tc.error ? (
            <XCircle className="h-4 w-4 shrink-0 text-destructive" />
          ) : (
            <CheckCircle2 className="h-4 w-4 shrink-0 text-green-500" />
          )
        ) : (
          <Loader2 className="h-4 w-4 shrink-0 animate-spin text-primary" />
        )}
        <span className="font-medium text-foreground">{tc.name}</span>
        <span className="ml-auto flex items-center gap-2 text-xs text-muted-foreground">
          {tc.done && tc.duration_ms > 0 && (
            <span className="flex items-center gap-1">
              <Clock className="h-3 w-3" />
              {(tc.duration_ms / 1000).toFixed(1)}s
            </span>
          )}
          {expanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        </span>
      </button>
      {expanded && (
        <div className="space-y-2 border-t border-border/50 px-3 py-2">
          {tc.args && (
            <div>
              <div className="mb-1 text-xs font-medium text-muted-foreground">Arguments</div>
              <pre className="whitespace-pre-wrap break-all rounded bg-muted/50 p-2 text-xs leading-relaxed">{tc.args}</pre>
            </div>
          )}
          {tc.done && tc.result && (
            <div>
              <div className="mb-1 text-xs font-medium text-muted-foreground">Result</div>
              <pre className="whitespace-pre-wrap break-all rounded bg-muted/50 p-2 text-xs leading-relaxed max-h-48 overflow-y-auto">{tc.result}</pre>
            </div>
          )}
          {tc.done && tc.error && (
            <div>
              <div className="mb-1 text-xs font-medium text-destructive">Error</div>
              <pre className="whitespace-pre-wrap break-all rounded bg-destructive/10 p-2 text-xs text-destructive">{tc.error}</pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

interface AgentStreamViewProps {
  state: AgentStreamState;
}

export function AgentStreamView({ state }: AgentStreamViewProps) {
  const [thinkingOpen, setThinkingOpen] = useState(true);
  const bottomRef = useRef<HTMLDivElement>(null);

  const hasThinking = state.thinking.length > 0;
  const hasContent = state.content.length > 0;

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [state.content, state.thinking, (state.tool_calls ?? []).length]);

  if (state.error) {
    return (
      <div className="rounded-md border border-destructive/50 bg-destructive/5 p-4">
        <p className="text-sm font-medium text-destructive">Error</p>
        <p className="mt-1 text-sm text-destructive/80">{state.error}</p>
      </div>
    );
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

      {/* Thinking section */}
      {hasThinking && (
        <div className="rounded-md border border-border/40">
          <button
            onClick={() => setThinkingOpen(!thinkingOpen)}
            className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs font-medium text-muted-foreground hover:bg-muted/30 transition-colors"
          >
            {thinkingOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
            Thinking
            <Badge variant="outline" className="ml-auto text-[10px]">
              {state.thinking.length} chars
            </Badge>
          </button>
          {thinkingOpen && (
            <div className="border-t border-border/40 px-3 py-2">
              <pre className="whitespace-pre-wrap font-mono text-xs leading-relaxed text-muted-foreground">
                {state.thinking}
              </pre>
            </div>
          )}
        </div>
      )}

      {/* Content section */}
      {hasContent && (
        <div className="rounded-md border border-border/40 p-3">
          <MarkdownPreview content={state.content} />
        </div>
      )}

      {/* Tool calls section */}
      {state.tool_calls.length > 0 && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground">
            Tool Calls
            <Badge variant="outline" className="ml-2 text-[10px]">
              {state.tool_calls.length}
            </Badge>
          </div>
          <div className="space-y-1.5">
            {state.tool_calls.map((tc) => (
              <ToolCallCard key={tc.call_id} tc={tc} />
            ))}
          </div>
        </div>
      )}

      {/* Empty state */}
      {!hasContent && !hasThinking && state.tool_calls.length === 0 && !state.processing && (
        <p className="py-8 text-center text-sm text-muted-foreground">Agent idle, no output</p>
      )}

      <div ref={bottomRef} />
    </div>
  );
}
