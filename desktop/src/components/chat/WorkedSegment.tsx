import { useState, useEffect, useRef } from 'react'
import {
  ChevronRight,
  ChevronDown,
  Loader2,
  AlertCircle,
  CheckCircle2,
  ExternalLink,
  Bot,
  X,
} from 'lucide-react'
import { useChatStore } from '@/stores/chatStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useAgentStore } from '@/stores/agentStore'
import { useAgentStream } from '@/hooks/useAgentStream'
import { AgentStreamView } from '@/components/AgentStreamView'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { cn } from '@/lib/utils'
import type { ChatMessage } from '@/types'
import type { GroupedWorked } from '../ChatMessage'
import { ToolCallSegment } from './ToolCallSegment'

export function WorkedSegment({
  group,
  isUser,
  onUserInteraction,
}: {
  group: GroupedWorked
  isUser?: boolean
  onUserInteraction?: () => void
}) {
  const streamingSessions = useChatStore((s) => s.streamingSessions)
  const activeSessionId = useChatStore((s) => s.activeSessionId)
  const streaming = activeSessionId ? !!streamingSessions[activeSessionId] : false
  const isDesignMode = useRuntimeStore((s) => s.isDesignMode)
  const compact = isDesignMode

  const isDone = !group.isLast || (group.isLast && !streaming)

  const [isOpen, setIsOpen] = useState(!isDone)
  const hasManuallyToggled = useRef(false)

  // Sync isOpen with isDone only if not manually toggled
  useEffect(() => {
    if (!hasManuallyToggled.current) {
      setIsOpen(!isDone)
    }
  }, [isDone])

  // Reset manual toggle when active session changes
  useEffect(() => {
    hasManuallyToggled.current = false
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setIsOpen(!isDone)
  }, [activeSessionId, isDone])

  const label = 'worked'

  const toolCalls = group.segments.filter((s) => s.segment.type === 'tool_call')
  const completedToolCalls = toolCalls.filter((s) => {
    const seg = s.segment
    return seg.type === 'tool_call' && seg.done
  })
  const statsText = group.hasToolCalls
    ? `(${toolCalls.length} step${toolCalls.length > 1 ? 's' : ''}${!isDone ? `: ${completedToolCalls.length} done` : ''})`
    : ''

  return (
    <details className="group/worked" open={isOpen}>
      <summary
        onClick={(e) => {
          e.preventDefault()
          hasManuallyToggled.current = true
          setIsOpen(!isOpen)
          onUserInteraction?.()
        }}
        className={`flex items-center gap-1.5 text-xs cursor-pointer transition-colors ${compact ? 'py-0.5' : 'py-1'} text-muted-foreground hover:text-foreground/70`}
      >
        {!isDone ? (
          <span className="relative flex h-2 w-2 shrink-0">
            <span
              className={`absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping bg-primary`}
            />
            <span
              className={`relative inline-flex h-2 w-2 rounded-full bg-primary`}
            />
          </span>
        ) : (
          <div className="h-2 w-2 rounded-full bg-success/30 shrink-0 flex items-center justify-center">
            <div className="h-1 w-1 rounded-full bg-success" />
          </div>
        )}
        <span className="font-medium inline-flex items-center gap-1.5">
          <span>{label}</span>
          {statsText && <span className="opacity-60 font-normal">{statsText}</span>}
        </span>
        <ChevronRight className="h-3 w-3 ml-auto group-open/worked:hidden" />
        <ChevronDown className="h-3 w-3 ml-auto hidden group-open/worked:block" />
      </summary>
      <div
        className={`${compact ? 'mt-1 ml-1.5 pl-2 space-y-2' : 'mt-1.5 ml-2.5 pl-3.5 space-y-3'} border-l-2 border-muted-foreground/20`}
      >
        {group.segments.map(({ segment }, idx) => {
          if (segment.type === 'thinking') {
            return (
              <div
                key={idx}
                className={`text-xs whitespace-pre-wrap leading-relaxed text-muted-foreground/75`}
              >
                {segment.text}
              </div>
            )
          } else if (segment.type === 'tool_call') {
            return (
              <div key={idx} className="my-1.5">
                <ToolCallSegment
                  segment={segment}
                  isUser={isUser}
                  onUserInteraction={onUserInteraction}
                />
              </div>
            )
          }
          return null
        })}
      </div>
    </details>
  )
}

export function SubagentCard({
  segment,
}: {
  segment: Extract<ChatMessage['segments'][number], { type: 'delegation' }>
}) {
  const [modalOpen, setModalOpen] = useState(false)
  const running = segment.status === 'running'
  const failed = segment.status === 'failed'
  const completed = segment.status === 'completed'
  const hasResult = !!segment.resultContent

  // Resolve the live agent stream by matching the agent name.
  const agentsData = useAgentStore((state) => state.agents)
  const namePart = segment.agentName.toLowerCase().replace(/[\s_]/g, '')
  const matchedAgent = agentsData?.agents.find(
    (a) => a.name.toLowerCase().replace(/[\s_]/g, '') === namePart
  )
  const instanceId = matchedAgent?.instance_id || null
  const agentStream = useAgentStream(instanceId)

  // Clickable whenever the agent is running (to watch live stream) or has
  // finished output (to review results).
  const isClickable = running || completed || failed

  return (
    <>
      <button
        onClick={() => {
          if (isClickable) setModalOpen(true)
        }}
        disabled={!isClickable}
        className={cn(
          'w-full text-left rounded-xl border overflow-hidden transition-all',
          isClickable
            ? 'cursor-pointer hover:shadow-md hover:shadow-primary/5 border-primary/30 bg-gradient-to-r from-primary/8 via-primary/4 to-transparent'
            : 'cursor-default border-border/50 bg-card/20'
        )}
      >
        {/* Accent bar */}
        <div
          className={cn(
            'h-0.5 w-full',
            running
              ? 'bg-gradient-to-r from-primary to-purple-400'
              : failed
                ? 'bg-destructive/60'
                : 'bg-success/40'
          )}
        />

        <div className="flex items-center gap-2.5 px-3 py-2.5">
          {/* Status icon */}
          <div
            className={cn(
              'h-7 w-7 rounded-lg flex items-center justify-center shrink-0',
              running ? 'bg-primary/15' : failed ? 'bg-destructive/10' : 'bg-success/10'
            )}
          >
            {running ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" />
            ) : failed ? (
              <AlertCircle className="h-3.5 w-3.5 text-destructive" />
            ) : (
              <CheckCircle2 className="h-3.5 w-3.5 text-success" />
            )}
          </div>

          {/* Content */}
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <span className="font-semibold text-xs text-foreground/90 truncate">
                {segment.agentName}
              </span>
              <span
                className={cn(
                  'text-[9px] uppercase font-bold tracking-wider px-1.5 py-0.5 rounded-md',
                  running
                    ? 'bg-primary/15 text-primary'
                    : failed
                      ? 'bg-destructive/10 text-destructive'
                      : 'bg-success/10 text-success'
                )}
              >
                {running ? 'Running' : failed ? 'Failed' : 'Done'}
              </span>
              {isClickable && <ExternalLink className="h-2.5 w-2.5 text-primary/40 shrink-0" />}
            </div>
            {segment.task && (
              <p className="text-[11px] text-muted-foreground/60 truncate mt-0.5">{segment.task}</p>
            )}
          </div>

          {/* Duration */}
          {segment.durationMs != null && (
            <span className="text-[9px] text-muted-foreground/30 font-mono shrink-0">
              {(segment.durationMs / 1000).toFixed(1)}s
            </span>
          )}
        </div>
      </button>

      {/* Modal for subagent live stream / result */}
      {modalOpen && isClickable && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm animate-in fade-in duration-200"
          onClick={() => setModalOpen(false)}
        >
          <div
            className="bg-background border border-border/60 rounded-2xl shadow-2xl w-[90vw] max-w-4xl h-[80vh] flex flex-col overflow-hidden animate-in zoom-in-95 duration-200"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="shrink-0 flex items-center justify-between px-5 py-4 border-b border-border/50 bg-card/50">
              <div className="flex items-center gap-2.5">
                <div className="h-7 w-7 rounded-lg bg-primary/10 flex items-center justify-center">
                  <Bot className="h-4 w-4 text-primary" />
                </div>
                <div>
                  <h3 className="text-sm font-semibold text-foreground">{segment.agentName}</h3>
                  {instanceId && (
                    <p className="text-[10px] text-muted-foreground/60 font-mono">
                      Instance: {instanceId}
                    </p>
                  )}
                </div>
              </div>
              <button
                onClick={() => setModalOpen(false)}
                className="text-muted-foreground hover:text-foreground p-1.5 rounded-lg hover:bg-muted/50 transition-colors cursor-pointer"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="flex-1 overflow-y-auto p-6 bg-card/20">
              {running && agentStream ? (
                <AgentStreamView state={agentStream} />
              ) : hasResult ? (
                <div className="text-xs leading-relaxed text-foreground/80">
                  <MarkdownPreview content={segment.resultContent || ''} />
                </div>
              ) : agentStream ? (
                <AgentStreamView state={agentStream} />
              ) : (
                <div className="flex flex-col items-center justify-center h-full gap-3 text-muted-foreground/40">
                  <Bot className="h-8 w-8" />
                  <p className="text-xs">Waiting for agent stream...</p>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  )
}
