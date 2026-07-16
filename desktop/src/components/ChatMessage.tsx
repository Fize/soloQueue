import type { ChatMessage } from '@/types'
import { User, Sparkles, Copy, Check, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from '@/lib/i18n'
import { useState, useMemo, memo } from 'react'
import { toast } from 'sonner'
import { getFileUrl } from '@/lib/api'
import { getModelColorVar } from '@/lib/utils'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useChatStore } from '@/stores/chatStore'
import { SegmentView, LoadingIndicator } from './chat/SegmentView'
import { WorkedSegment } from './chat/WorkedSegment'
import { MessageImageGallery } from './chat/ToolCallSegment'

export interface ChatMessageProps {
  message: ChatMessage
  agentName?: string
  isStreaming?: boolean
  onUserInteraction?: () => void
  modelName?: string
  sessionId?: string
}

function ChatMessageViewInner({ message, agentName = 'Assistant', isStreaming = false, onUserInteraction, modelName, sessionId }: ChatMessageProps) {
  const { t } = useTranslation()
  const isUser = message.role === 'user'
  const isEmpty = message.segments.length === 0
  const isDesignMode = useRuntimeStore((s) => s.isDesignMode)
  const activeSessionId = useChatStore((s) => s.activeSessionId)
  const rewindSession = useChatStore((s) => s.rewindSession)
  const deleteSessionMessages = useChatStore((s) => s.deleteSessionMessages)
  const currentSessionId = sessionId || activeSessionId
  const compact = isDesignMode
  const modelColorVar = useMemo(() => getModelColorVar(isUser ? undefined : modelName), [isUser, modelName])
  // Memoize the grouping so that re-renders caused by stable references
  // (e.g. parent re-render that didn't change segments) skip the work, and
  // the resulting `grouped` array reference is stable across renders that
  // do not change the structural shape of the segments.
  const grouped = useMemo(() => groupSegments(message.segments), [message.segments])

  const handleRewind = async () => {
    if (!currentSessionId || !message.timestamp) return
    const text = extractFullContent(message)
    const confirmMsg = t('chat.confirmRewind') || 'Are you sure you want to rewind the session to this point? The text will be moved to the input box for editing, but history after this point will be truncated.'
    if (!window.confirm(confirmMsg)) return

    try {
      await rewindSession(currentSessionId, message.timestamp)
      window.dispatchEvent(new CustomEvent('fill-chat-input', { detail: text }))
      toast.success(t('chat.rewindSuccess') || 'Session rewound successfully')
    } catch (e: any) {
      toast.error(e.message || 'Failed to rewind session')
    }
  }

  const handleDelete = async () => {
    if (!currentSessionId || !message.timestamp) return
    const confirmMsg = t('chat.confirmDelete') || 'Are you sure you want to delete this message? The AI response pair will also be deleted.'
    if (!window.confirm(confirmMsg)) return

    try {
      await deleteSessionMessages(currentSessionId, [message.timestamp])
      toast.success(t('chat.deleteSuccess') || 'Message deleted successfully')
    } catch (e: any) {
      toast.error(e.message || 'Failed to delete message')
    }
  }

  if (!isUser && isEmpty) {
    return null
  }

  return (
    <div className={`group/message ${compact ? 'px-2 py-2' : 'px-4 py-3'} ${isUser ? 'flex justify-end' : ''}`}>
      <div
        className={`flex ${compact ? 'gap-2' : 'gap-3'} w-full ${isUser ? (compact ? 'max-w-[92%] flex-row-reverse' : 'max-w-[80%] sm:max-w-[70%] lg:max-w-[60%] flex-row-reverse') : ''}`}
      >
        {/* Avatar */}
        <div className="shrink-0 self-start">
          {isUser ? (
            <div className={`${compact ? 'h-5 w-5' : 'h-7 w-7'} rounded-full bg-primary/15 flex items-center justify-center`}>
              <User className={`${compact ? 'h-3 w-3' : 'h-3.5 w-3.5'} text-primary/70`} />
            </div>
          ) : (
            <div
              className={`${compact ? 'h-5 w-5' : 'h-7 w-7'} rounded-full flex items-center justify-center`}
              style={{
                background: `linear-gradient(135deg, color-mix(in srgb, ${modelColorVar} 20%, transparent), color-mix(in srgb, ${modelColorVar} 30%, transparent))`,
              }}
            >
              <Sparkles
                className={`${compact ? 'h-3 w-3' : 'h-3.5 w-3.5'}`}
                style={{ color: modelColorVar }}
              />
            </div>
          )}
        </div>

        {/* Bubble */}
        <div className={`min-w-0 max-w-full ${isUser ? 'w-fit' : 'w-full'}`}>
          {/* Role label */}
          <div className={`flex items-center gap-2 mb-1 ${isUser ? 'justify-end flex-row-reverse' : ''}`}>
            <span
              className={`text-[11px] font-medium text-muted-foreground/80`}
            >
              {isUser ? 'You' : agentName}
            </span>
            {message.timestamp && (
              <span className="text-[10px] text-muted-foreground/40 font-normal select-none">
                {formatTimestamp(message.timestamp)}
              </span>
            )}
          </div>

          {/* Bubble content */}
          <div
            className={
              isUser
                ? `${compact ? 'rounded-xl px-2.5 py-2' : 'rounded-2xl px-4 py-2.5'} bg-muted/60 text-foreground rounded-br-md shadow-sm border border-border/40`
                : 'w-full text-foreground'
            }
          >
            {/* Message body */}
            {isEmpty ? (
              <LoadingIndicator />
            ) : (
              <div className="space-y-2">
                {grouped.map((item) => {
                  if (item.type === 'worked') {
                    return (
                      <WorkedSegment
                        key={item.id}
                        group={item}
                        isUser={isUser}
                        onUserInteraction={onUserInteraction}
                      />
                    )
                  } else {
                    return (
                      <SegmentView
                        key={item.index}
                        segment={item.segment}
                        isUser={isUser}
                        segmentIndex={item.index}
                        segments={message.segments}
                        isStreaming={isStreaming}
                        onUserInteraction={onUserInteraction}
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

                {/* Surface-level image gallery for agent-generated images */}
                <MessageImageGallery segments={message.segments} isUser={isUser} />
              </div>
            )}
          </div>

          {/* Actions bar */}
          {!isEmpty && (
            <div className="flex items-center gap-1 mt-1.5 opacity-0 group-hover/message:opacity-100 transition-opacity">
              {isUser && message.timestamp && (
                <>
                  <button
                    type="button"
                    onClick={handleRewind}
                    className="p-1 rounded text-muted-foreground/40 hover:text-primary hover:bg-primary/10 transition-colors cursor-pointer"
                    title={t('chat.rewindAndEdit') || 'Rewind & Edit'}
                  >
                    <RotateCcw className="h-3 w-3" />
                  </button>
                  <button
                    type="button"
                    onClick={handleDelete}
                    className="p-1 rounded text-muted-foreground/40 hover:text-destructive hover:bg-destructive/10 transition-colors cursor-pointer"
                    title={t('chat.deleteMessage') || 'Delete'}
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                </>
              )}
              <CopyButton text={extractFullContent(message)} label={isUser ? 'Copy message' : 'Copy response'} />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

/**
 * ChatMessageView is wrapped in React.memo so that re-renders triggered by
 * per-token chat_chunk updates skip previously-rendered messages whose
 * props (message / agentName / onUserInteraction) have not changed. The
 * chatStore's appendToLastAssistantContent deliberately keeps the
 * reference of all but the last message stable (only the last message
 * is cloned), so this memoization hits on every token.
 */
export const ChatMessageView = memo(
  ChatMessageViewInner,
  (prev, next) =>
    prev.message === next.message &&
    prev.agentName === next.agentName &&
    prev.isStreaming === next.isStreaming &&
    prev.onUserInteraction === next.onUserInteraction,
)

export interface GroupedWorkedSegment {
  segment: ChatMessage['segments'][number]
  originalIndex: number
}

export interface GroupedWorked {
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
      // Key the group by the ORIGINAL index of its first segment plus its
      // length. This stays stable across streaming updates (the same group
      // keeps the same id) and lets React preserve the DOM subtree even
      // when groupSegments re-runs because a content/thinking segment was
      // appended in place. The previous key included `grouped.length` and
      // a type-stringify of every segment, both of which change on every
      // streaming tick and caused React to unmount/remount the entire
      // WorkedSegment subtree (which also tore down MarkdownPreview and
      // forced a full markdown re-parse).
      const firstOriginalIndex = currentGroup[0].originalIndex
      grouped.push({
        type: 'worked',
        id: `worked-${firstOriginalIndex}-${currentGroup.length}`,
        segments: [...currentGroup],
        hasToolCalls,
        isLast: false,
      })
      currentGroup = []
    }
  }

  for (let i = 0; i < segments.length; i++) {
    const seg = segments[i]

    // Skip empty thinking segments unless it's the last segment in the entire message
    if (seg.type === 'thinking' && !seg.text.trim()) {
      if (i !== segments.length - 1) {
        continue
      }
    }

    // Skip tool_confirm segments entirely for rendering, they are handled by the StickyToolConfirmPanel
    if (seg.type === 'tool_confirm') {
      continue
    }

    // Delegation segments (active subagent sessions) and delegate_* tool calls
    // are rendered as standalone cards — keep them outside the worked group.
    const isStandalone =
      seg.type === 'delegation' || (seg.type === 'tool_call' && seg.name.startsWith('delegate_'))
    if (!isStandalone && (seg.type === 'thinking' || seg.type === 'tool_call' || seg.type === 'compact')) {
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

function CopyButton({ text, label = 'Copy' }: { text: string; label?: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      toast.success(label === 'Copy' ? t('common.copiedToClipboard') : t('common.copiedSuccess'))
      setTimeout(() => setCopied(false), 2000)
    } catch {
      toast.error(t('common.failedToCopy'))
    }
  }
  return (
    <button
      onClick={handleCopy}
      className="p-1 rounded text-muted-foreground/40 hover:text-foreground/75 hover:bg-muted/50 transition-colors cursor-pointer"
      title={label}
    >
      {copied ? <Check className="h-3 w-3 text-success" /> : <Copy className="h-3 w-3" />}
    </button>
  )
}

function extractFullContent(msg: ChatMessage): string {
  return msg.segments
    .filter((s: any) => s.type === 'content')
    .map((s: any) => s.text)
    .join('')
}

function formatTimestamp(tsStr: string): string {
  if (!tsStr) return ''
  try {
    const date = new Date(tsStr)
    if (isNaN(date.getTime())) return ''
    
    const now = new Date()
    const isToday = date.toDateString() === now.toDateString()
    const isSameYear = date.getFullYear() === now.getFullYear()
    
    const pad = (n: number) => n.toString().padStart(2, '0')
    const hours = pad(date.getHours())
    const minutes = pad(date.getMinutes())
    
    if (isToday) {
      return `${hours}:${minutes}`
    }
    
    const month = pad(date.getMonth() + 1)
    const day = pad(date.getDate())
    if (isSameYear) {
      return `${month}-${day} ${hours}:${minutes}`
    }
    
    return `${date.getFullYear()}-${month}-${day} ${hours}:${minutes}`
  } catch {
    return ''
  }
}
