import type { ChatMessage } from '@/types'
import {
  User,
  Sparkles,
  ChevronDown,
  ChevronRight,
  Loader2,
  AlertCircle,
  Copy,
  Check,
  Bot,
  X,
  ShieldAlert,
  CheckCircle2,
  ExternalLink,
} from 'lucide-react'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { useState, useRef, useEffect } from 'react'
import { toast } from 'sonner'
import { confirmSessionTool, getFileUrl } from '@/lib/api'
import { useChatStore } from '@/stores/chatStore'
import { useAgentStore } from '@/stores/agentStore'
import { useAgentStream } from '@/hooks/useAgentStream'
import { AgentStreamView } from '@/components/AgentStreamView'
import { formatToolCallHeader, cn } from '@/lib/utils'
import { DelegationCard } from '@/components/DelegationCard'

export interface ChatMessageProps {
  message: ChatMessage
  agentName?: string
}

export function ChatMessageView({ message, agentName = 'Assistant' }: ChatMessageProps) {
  const isUser = message.role === 'user'
  const isEmpty = message.segments.length === 0

  return (
    <div className={`group/message px-4 py-3 ${isUser ? 'flex justify-end' : ''}`}>
      <div
        className={`flex gap-3 ${isUser ? 'max-w-[80%] sm:max-w-[70%] lg:max-w-[60%] flex-row-reverse' : 'max-w-[90%] sm:max-w-[80%] lg:max-w-[70%]'}`}
      >
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
        <div className={`min-w-0 max-w-full ${isUser ? 'w-fit' : 'w-full'}`}>
          {/* Role label */}
          <div className={`flex items-center gap-2 mb-1 ${isUser ? 'justify-end' : ''}`}>
            <span
              className={`text-[11px] font-medium ${isUser ? 'text-primary/60' : 'text-violet-500/60'}`}
            >
              {isUser ? 'You' : agentName}
            </span>
          </div>

          {/* Bubble content */}
          <div
            className={`rounded-2xl px-4 py-2.5 ${
              isUser
                ? 'bg-primary text-primary-foreground rounded-br-md'
                : 'bg-muted/60 text-foreground rounded-bl-md'
            }`}
          >
            {/* Message body */}
            {isEmpty ? (
              <LoadingIndicator isUser={isUser} />
            ) : (
              <div className="space-y-2">
                {groupSegments(message.segments).map((item) => {
                  if (item.type === 'worked') {
                    return <WorkedSegment key={item.id} group={item} isUser={isUser} />
                  } else {
                    return (
                      <SegmentView
                        key={item.index}
                        segment={item.segment}
                        isUser={isUser}
                        segmentIndex={item.index}
                        segments={message.segments}
                      />
                    )
                  }
                })}
                {/* Render uploaded files/images here */}
                {message.files && message.files.length > 0 && (
                  <div className="flex flex-wrap gap-2 mt-2 pt-1.5 border-t border-dashed border-border/10">
                    {message.files.map((file, idx) => {
                      const isImage = /\.(jpg|jpeg|png|webp|gif|svg)$/i.test(file.name)
                      if (isImage) {
                        return (
                          <div
                            key={idx}
                            className="relative group/img max-w-[240px] rounded-lg overflow-hidden border border-border/40"
                          >
                            <img
                              src={getFileUrl(file.path)}
                              alt={file.name}
                              className="max-h-[160px] object-contain cursor-pointer hover:opacity-90 transition-opacity"
                              onClick={() => window.open(getFileUrl(file.path), '_blank')}
                            />
                          </div>
                        )
                      }
                      return (
                        <a
                          key={idx}
                          href={getFileUrl(file.path)}
                          target="_blank"
                          rel="noreferrer"
                          className={`flex items-center gap-2 p-2 rounded-lg border text-xs max-w-xs transition-colors ${
                            isUser
                              ? 'bg-white/10 border-white/20 hover:bg-white/20 text-primary-foreground'
                              : 'bg-card/50 border-border hover:bg-card text-foreground'
                          }`}
                        >
                          <svg
                            className="h-4 w-4 shrink-0 opacity-70"
                            fill="none"
                            viewBox="0 0 24 24"
                            stroke="currentColor"
                          >
                            <path
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              strokeWidth={2}
                              d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
                            />
                          </svg>
                          <span className="truncate flex-1 font-medium">{file.name}</span>
                        </a>
                      )
                    })}
                  </div>
                )}
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
        <span
          className={`h-1.5 w-1.5 rounded-full animate-bounce [animation-delay:0ms] ${isUser ? 'bg-primary-foreground/60' : 'bg-violet-400'}`}
        />
        <span
          className={`h-1.5 w-1.5 rounded-full animate-bounce [animation-delay:150ms] ${isUser ? 'bg-primary-foreground/60' : 'bg-violet-400'}`}
        />
        <span
          className={`h-1.5 w-1.5 rounded-full animate-bounce [animation-delay:300ms] ${isUser ? 'bg-primary-foreground/60' : 'bg-violet-400'}`}
        />
      </span>
      <span
        className={`text-sm ${isUser ? 'text-primary-foreground/60' : 'text-muted-foreground/60'}`}
      >
        Thinking...
      </span>
    </div>
  )
}
interface GroupedWorkedSegment {
  segment: ChatMessage['segments'][number]
  originalIndex: number
}

interface GroupedWorked {
  type: 'worked'
  id: string
  segments: GroupedWorkedSegment[]
  hasToolCalls: boolean
  isLast: boolean
}

interface GroupedOther {
  type: 'other'
  segment: ChatMessage['segments'][number]
  index: number
}

type GroupedItem = GroupedWorked | GroupedOther

function groupSegments(segments: ChatMessage['segments']): GroupedItem[] {
  const grouped: GroupedItem[] = []
  let currentGroup: GroupedWorkedSegment[] = []

  const flush = () => {
    if (currentGroup.length > 0) {
      const hasToolCalls = currentGroup.some((s) => s.segment.type === 'tool_call')
      grouped.push({
        type: 'worked',
        id: `worked-${grouped.length}-${currentGroup.map((s) => s.segment.type).join('-')}`,
        segments: [...currentGroup],
        hasToolCalls,
        isLast: false,
      })
      currentGroup = []
    }
  }

  for (let i = 0; i < segments.length; i++) {
    const seg = segments[i]
    // Delegation segments (active subagent sessions) and delegate_* tool calls
    // are rendered as standalone cards — keep them outside the worked group.
    const isStandalone =
      seg.type === 'delegation' || (seg.type === 'tool_call' && seg.name.startsWith('delegate_'))
    if (!isStandalone && (seg.type === 'thinking' || seg.type === 'tool_call')) {
      currentGroup.push({ segment: seg, originalIndex: i })
    } else {
      flush()
      grouped.push({
        type: 'other',
        segment: seg,
        index: i,
      })
    }
  }
  flush()

  for (let i = grouped.length - 1; i >= 0; i--) {
    if (grouped[i].type === 'worked') {
      ;(grouped[i] as GroupedWorked).isLast = i === grouped.length - 1
      break
    }
  }

  return grouped
}

function WorkedSegment({ group, isUser }: { group: GroupedWorked; isUser?: boolean }) {
  const streaming = useChatStore((s) => s.streaming)
  const [doneKey, setDoneKey] = useState(0)
  const prevStreaming = useRef(streaming)

  const isDone = !group.isLast || (group.isLast && !streaming)

  useEffect(() => {
    if (prevStreaming.current && !streaming) {
      setDoneKey((k) => k + 1)
    }
    prevStreaming.current = streaming
  }, [streaming])

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
    <details className="group/worked" open={!isDone} key={doneKey}>
      <summary
        className={`flex items-center gap-1.5 text-xs cursor-pointer transition-colors py-1 ${isUser ? 'text-primary-foreground/60 hover:text-primary-foreground/80' : 'text-muted-foreground hover:text-foreground/70'}`}
      >
        {!isDone ? (
          <span className="relative flex h-2 w-2 shrink-0">
            <span
              className={`absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping ${isUser ? 'bg-primary-foreground/40' : 'bg-violet-400'}`}
            />
            <span
              className={`relative inline-flex h-2 w-2 rounded-full ${isUser ? 'bg-primary-foreground/60' : 'bg-violet-500'}`}
            />
          </span>
        ) : (
          <div className="h-2 w-2 rounded-full bg-emerald-500/30 shrink-0 flex items-center justify-center">
            <div className="h-1 w-1 rounded-full bg-emerald-500" />
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
        className={`mt-1.5 ml-2.5 pl-3.5 border-l-2 space-y-3 ${isUser ? 'border-primary-foreground/15' : 'border-muted-foreground/20'}`}
      >
        {group.segments.map(({ segment }, idx) => {
          if (segment.type === 'thinking') {
            return (
              <div
                key={idx}
                className={`text-xs whitespace-pre-wrap leading-relaxed ${isUser ? 'text-primary-foreground/65' : 'text-muted-foreground/75'}`}
              >
                {segment.text}
              </div>
            )
          } else if (segment.type === 'tool_call') {
            return (
              <div key={idx} className="my-1.5">
                <ToolCallSegment segment={segment} isUser={isUser} />
              </div>
            )
          }
          return null
        })}
      </div>
    </details>
  )
}

function SegmentView({
  segment,
  isUser,
  segmentIndex,
  segments,
}: {
  segment: ChatMessage['segments'][number]
  isUser?: boolean
  segmentIndex?: number
  segments?: ChatMessage['segments']
}) {
  const isLastSegment =
    segmentIndex != null && segments != null && segmentIndex === segments.length - 1
  switch (segment.type) {
    case 'content':
      return (
        <MarkdownPreview
          content={segment.text}
          className={
            isUser
              ? 'text-primary-foreground [&_a]:text-primary-foreground/80 [&_code]:bg-primary-foreground/20 [&_pre]:bg-primary-foreground/10'
              : ''
          }
        />
      )
    case 'thinking':
      return <ThinkingSegment segment={segment} isUser={isUser} isLastSegment={isLastSegment} />
    case 'tool_call':
      if (segment.name.startsWith('delegate_')) {
        return (
          <DelegationCard
            name={segment.name}
            args={segment.args}
            callId={segment.callId}
            done={segment.done}
            result={segment.result}
            error={segment.error}
            durationMs={segment.durationMs}
          />
        )
      }
      return <ToolCallSegment segment={segment} isUser={isUser} />
    case 'tool_confirm':
      return <ToolConfirmSegment segment={segment} isUser={isUser} />
    case 'delegation':
      return <SubagentCard segment={segment} />
    case 'error':
      return (
        <div
          className={`flex items-start gap-2 p-3 rounded-lg text-sm ${isUser ? 'bg-destructive/20 text-destructive-foreground' : 'bg-destructive/5 border border-destructive/20 text-destructive/90'}`}
        >
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
            ? 'cursor-pointer hover:shadow-md hover:shadow-violet-500/5 border-violet-500/30 bg-gradient-to-r from-violet-500/8 via-violet-500/4 to-transparent'
            : 'cursor-default border-border/50 bg-card/20'
        )}
      >
        {/* Accent bar */}
        <div
          className={cn(
            'h-0.5 w-full',
            running
              ? 'bg-gradient-to-r from-violet-500 to-purple-400'
              : failed
                ? 'bg-destructive/60'
                : 'bg-emerald-500/40'
          )}
        />

        <div className="flex items-center gap-2.5 px-3 py-2.5">
          {/* Status icon */}
          <div
            className={cn(
              'h-7 w-7 rounded-lg flex items-center justify-center shrink-0',
              running ? 'bg-violet-500/15' : failed ? 'bg-destructive/10' : 'bg-emerald-500/10'
            )}
          >
            {running ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin text-violet-500" />
            ) : failed ? (
              <AlertCircle className="h-3.5 w-3.5 text-destructive" />
            ) : (
              <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />
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
                    ? 'bg-violet-500/15 text-violet-600'
                    : failed
                      ? 'bg-destructive/10 text-destructive'
                      : 'bg-emerald-500/10 text-emerald-600'
                )}
              >
                {running ? 'Running' : failed ? 'Failed' : 'Done'}
              </span>
              {isClickable && <ExternalLink className="h-2.5 w-2.5 text-violet-500/40 shrink-0" />}
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
            className="bg-card border border-border/60 rounded-2xl shadow-2xl w-[90vw] max-w-4xl h-[80vh] flex flex-col overflow-hidden animate-in zoom-in-95 duration-200"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="shrink-0 flex items-center justify-between px-5 py-4 border-b border-border/50 bg-card/50">
              <div className="flex items-center gap-2.5">
                <div className="h-7 w-7 rounded-lg bg-violet-500/10 flex items-center justify-center">
                  <Bot className="h-4 w-4 text-violet-500" />
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
                <pre className="whitespace-pre-wrap text-xs leading-relaxed text-foreground/80 font-mono">
                  {segment.resultContent}
                </pre>
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

function ThinkingSegment({
  segment,
  isUser,
  isLastSegment = true,
}: {
  segment: Extract<ChatMessage['segments'][number], { type: 'thinking' }>
  isUser?: boolean
  isLastSegment?: boolean
}) {
  const streaming = useChatStore((s) => s.streaming)
  const [doneKey, setDoneKey] = useState(0)
  const prevStreaming = useRef(streaming)

  // A thinking segment is done when:
  //   a) there are subsequent segments (LLM moved on to content/tool_call), OR
  //   b) it's the last segment but the global stream has ended
  const isDone = !isLastSegment || (isLastSegment && !streaming)

  // When streaming transitions from true → false, remount details as closed
  useEffect(() => {
    if (prevStreaming.current && !streaming) {
      setDoneKey((k) => k + 1)
    }
    prevStreaming.current = streaming
  }, [streaming])

  return (
    <details className="group/thinking" open={!isDone} key={doneKey}>
      <summary
        className={`flex items-center gap-1.5 text-xs cursor-pointer transition-colors py-1 ${isUser ? 'text-primary-foreground/60 hover:text-primary-foreground/80' : 'text-muted-foreground hover:text-foreground/70'}`}
      >
        {!isDone ? (
          <span className="relative flex h-2 w-2 shrink-0">
            <span
              className={`absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping ${isUser ? 'bg-primary-foreground/40' : 'bg-violet-400'}`}
            />
            <span
              className={`relative inline-flex h-2 w-2 rounded-full ${isUser ? 'bg-primary-foreground/60' : 'bg-violet-500'}`}
            />
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
      <div
        className={`mt-1 ml-5 pl-3 border-l-2 text-xs whitespace-pre-wrap leading-relaxed ${isUser ? 'border-primary-foreground/15 text-primary-foreground/65' : 'border-muted-foreground/20 text-muted-foreground/75'}`}
      >
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
    <div
      className={`text-xs border rounded-xl overflow-hidden w-full ${isUser ? 'border-primary-foreground/15 bg-primary-foreground/5' : 'border-border/60 bg-muted/20'}`}
    >
      <button
        onClick={() => setExpanded(!expanded)}
        className={`flex items-center gap-2 w-full px-3 py-2 transition-colors ${isUser ? 'text-primary-foreground/70 hover:text-primary-foreground' : 'text-muted-foreground hover:text-foreground'}`}
      >
        {running ? (
          <Loader2
            className={`h-3.5 w-3.5 animate-spin ${isUser ? 'text-primary-foreground' : 'text-violet-500'}`}
          />
        ) : segment.error ? (
          <AlertCircle className="h-3.5 w-3.5 text-destructive" />
        ) : (
          <div className="h-3.5 w-3.5 rounded-full bg-emerald-500/20 flex items-center justify-center">
            <div className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
          </div>
        )}
        <span className="font-mono text-[11px] text-left truncate flex-1 min-w-0 whitespace-nowrap">
          {formatToolCallHeader(segment.name, segment.args).replace(/\r?\n/g, ' ')}
        </span>
        <div className="flex items-center gap-2 text-xs shrink-0 select-none">
          {segment.durationMs != null && (
            <span
              className={`tabular-nums text-[10px] ${isUser ? 'text-primary-foreground/50' : 'text-muted-foreground/50'}`}
            >
              {(segment.durationMs / 1000).toFixed(1)}s
            </span>
          )}
          <span
            className={`text-[10px] uppercase tracking-wider ${isUser ? 'text-primary-foreground/40' : 'text-muted-foreground/40'}`}
          >
            {running ? 'Running' : segment.error ? 'Failed' : 'Done'}
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
          className={`px-3 pb-2 space-y-2 border-t pt-2 ${isUser ? 'border-primary-foreground/10' : 'border-border/30'}`}
        >
          {segment.args && (
            <div>
              <div
                className={`text-[10px] font-semibold uppercase tracking-wider mb-1 ${isUser ? 'text-primary-foreground/40' : 'text-muted-foreground/50'}`}
              >
                Arguments
              </div>
              <pre
                className={`text-[11px] leading-relaxed whitespace-pre-wrap overflow-x-auto rounded-lg p-2 max-h-[150px] overflow-y-auto font-mono ${isUser ? 'bg-primary-foreground/10' : 'bg-muted/40'}`}
              >
                {tryPrettify(segment.args)}
              </pre>
            </div>
          )}
          {(segment.result || segment.error) && (
            <div>
              <div
                className={`text-[10px] font-semibold uppercase tracking-wider mb-1 ${isUser ? 'text-primary-foreground/40' : 'text-muted-foreground/50'}`}
              >
                {segment.error ? 'Error' : 'Result'}
              </div>
              <pre
                className={`text-[11px] leading-relaxed whitespace-pre-wrap overflow-x-auto rounded-lg p-2 max-h-[250px] overflow-y-auto font-mono ${
                  segment.error
                    ? 'bg-destructive/5 text-destructive/90'
                    : isUser
                      ? 'bg-primary-foreground/10'
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

function ToolConfirmSegment({
  segment,
  isUser,
}: {
  segment: Extract<ChatMessage['segments'][number], { type: 'tool_confirm' }>
  isUser?: boolean
}) {
  const activeSessionId = useChatStore((s) => s.activeSessionId)
  const resolveToolConfirm = useChatStore((s) => s.resolveToolConfirm)
  const [allowAlways, setAllowAlways] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const handleConfirm = async (approved: boolean) => {
    if (!activeSessionId) return
    setSubmitting(true)
    const choice = approved ? (allowAlways ? 'allow-in-session' : 'yes') : ''
    try {
      await confirmSessionTool(activeSessionId, segment.callId, choice)
      resolveToolConfirm(segment.callId, choice)
    } catch (err) {
      console.error('Failed to confirm tool:', err)
      toast.error('Failed to confirm tool')
    } finally {
      setSubmitting(false)
    }
  }

  const resolved = segment.resolved
  const choice = segment.choice

  return (
    <div
      className={`p-4 rounded-xl border flex flex-col gap-3 text-xs leading-relaxed max-w-md my-2 ${
        isUser
          ? 'border-primary-foreground/15 bg-primary-foreground/5 text-primary-foreground'
          : 'border-amber-500/25 bg-amber-500/5 text-foreground'
      }`}
    >
      {/* Title */}
      <div className="flex items-center gap-2">
        <ShieldAlert
          className={`h-4 w-4 shrink-0 ${isUser ? 'text-primary-foreground' : 'text-amber-500'}`}
        />
        <span className="font-semibold uppercase tracking-wider text-[10px]">
          Execution Permission Required ({segment.name})
        </span>
      </div>

      {/* Prompt / Message */}
      <div
        className={`p-3 rounded-lg font-mono text-[11px] whitespace-pre-wrap ${
          isUser ? 'bg-primary-foreground/10' : 'bg-muted/40'
        }`}
      >
        {segment.prompt}
      </div>

      {resolved ? (
        <div className="flex items-center gap-2 mt-1">
          {choice === '' ? (
            <>
              <div className="h-2 w-2 rounded-full bg-destructive" />
              <span className="font-medium text-destructive">Denied by user</span>
            </>
          ) : (
            <>
              <div className="h-2 w-2 rounded-full bg-emerald-500" />
              <span className="font-medium text-emerald-500">
                Approved {choice === 'allow-in-session' ? '(Always allow in this session)' : ''}
              </span>
            </>
          )}
        </div>
      ) : (
        <div className="flex flex-col gap-3 mt-1">
          {/* Allow in Session Checkbox */}
          {segment.allowInSession && (
            <label className="flex items-center gap-2 cursor-pointer select-none">
              <input
                type="checkbox"
                checked={allowAlways}
                onChange={(e) => setAllowAlways(e.target.checked)}
                disabled={submitting}
                className="rounded border-gray-300 text-violet-600 focus:ring-violet-500 h-3.5 w-3.5"
              />
              <span className={isUser ? 'text-primary-foreground/70' : 'text-muted-foreground'}>
                Don't ask again for this tool in the current session
              </span>
            </label>
          )}

          {/* Action buttons */}
          <div className="flex items-center gap-2">
            <button
              onClick={() => handleConfirm(true)}
              disabled={submitting}
              className="px-3 py-1.5 rounded-lg font-medium bg-emerald-600 text-white hover:bg-emerald-500 disabled:opacity-50 transition-colors flex items-center gap-1 cursor-pointer"
            >
              {submitting ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <Check className="h-3.5 w-3.5" />
              )}
              Approve
            </button>
            <button
              onClick={() => handleConfirm(false)}
              disabled={submitting}
              className={`px-3 py-1.5 rounded-lg font-medium border disabled:opacity-50 transition-colors flex items-center gap-1 cursor-pointer ${
                isUser
                  ? 'border-primary-foreground/25 hover:bg-primary-foreground/10 text-primary-foreground'
                  : 'border-border hover:bg-muted text-foreground'
              }`}
            >
              <X className="h-3.5 w-3.5" />
              Deny
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
