import type { ChatMessage, RewindFillDetail } from '@/types'
import { User, Sparkles, Copy, Check, RotateCcw, Trash2, CornerDownRight } from 'lucide-react'
import { useTranslation } from '@/lib/i18n'
import { useState, useMemo, memo } from 'react'
import { toast } from 'sonner'
import { getModelColorVar } from '@/lib/utils'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useTranscriptStore } from '@/stores/transcriptStore'
import { useChatStore } from '@/stores/chatStore'
import { SegmentView, LoadingIndicator } from './chat/SegmentView'
import { WorkedSegment } from './chat/WorkedSegment'
import { groupSegments } from './chat/groupSegments'
import { workedStateKey } from './chat/workedStateKey'
export type { GroupedWorked, GroupedDelegation } from './chat/groupSegments'
import { MessageAttachment } from './chat/MessageAttachment'
import { DelegationGroupView } from './chat/DelegationGroupView'
import { MessageImageGallery } from './chat/ToolCallSegment'
import { ExecutionDetails } from './observability/ExecutionDetails'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'

export interface ChatMessageProps {
  message: ChatMessage
  agentName?: string
  isStreaming?: boolean
  onUserInteraction?: () => void
  modelName?: string
  sessionId?: string
  sessionAgentId?: string | null
}

function ChatMessageViewInner({
  message,
  agentName = 'Assistant',
  isStreaming = false,
  onUserInteraction,
  modelName,
  sessionId,
}: ChatMessageProps) {
  const { t } = useTranslation()
  const isUser = message.role === 'user'
  const isEmpty = message.segments.length === 0
  const isDesignMode = useRuntimeStore((s) => s.isDesignMode)
  const activeSessionId = useChatStore((s) => s.activeSessionId)
  const rewindSession = useChatStore((s) => s.rewindSession)
  const deleteSessionMessages = useChatStore((s) => s.deleteSessionMessages)
  const currentSessionId = sessionId || activeSessionId
  // Transcript bubbles carry their request and live state explicitly.
  const effectiveRequestId = isUser ? undefined : message.requestId
  const messageIsRunning = !isUser && !!message.running
  const renderedMessage = message
  const compact = isDesignMode
  const [confirmAction, setConfirmAction] = useState<'rewind' | 'delete' | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const modelColorVar = useMemo(
    () => getModelColorVar(isUser ? undefined : modelName),
    [isUser, modelName]
  )
  // Memoize the grouping so that re-renders caused by stable references
  // (e.g. parent re-render that didn't change segments) skip the work, and
  // the resulting `grouped` array reference is stable across renders that
  // do not change the structural shape of the segments.
  const grouped = useMemo(() => groupSegments(displaySegments(renderedMessage)), [renderedMessage])

  const handleConfirmAction = async () => {
    if (!confirmAction || actionLoading || !currentSessionId || !message.timestamp) return
    setActionLoading(true)
    try {
      if (confirmAction === 'rewind') {
        await rewindSession(currentSessionId, message.timestamp)
        const detail: RewindFillDetail = {
          text: extractFullContent(message),
          files: message.files ?? [],
        }
        window.dispatchEvent(new CustomEvent('fill-chat-input', { detail }))
        toast.success(t('chat.rewindSuccess') || 'Session rewound successfully')
      } else {
        await deleteSessionMessages(
          currentSessionId,
          useTranscriptStore.getState().deleteTargets(currentSessionId, [message.timestamp])
        )
        toast.success(t('chat.deleteSuccess') || 'Message deleted successfully')
      }
      setConfirmAction(null)
    } catch (error) {
      const fallback =
        confirmAction === 'rewind' ? 'Failed to rewind session' : 'Failed to delete message'
      toast.error(error instanceof Error ? error.message : fallback)
    } finally {
      setActionLoading(false)
    }
  }

  if (!isUser && isEmpty && !messageIsRunning) {
    return null
  }

  // User meta lives in the hover overlay so it never inflates the bubble width.
  const userMeta = isUser && !compact

  // Compaction results are system events, not replies: render them in the flow
  // without an avatar, role label or bubble.
  const compactOnly =
    !isUser &&
    renderedMessage.segments.length > 0 &&
    renderedMessage.segments.every((segment) => segment.type === 'compact')

  if (compactOnly) {
    return (
      <div className={`group/message w-full ${compact ? 'px-2 py-1' : 'px-4 py-1'}`}>
        <div className="w-full space-y-1">
          {renderedMessage.segments.map((segment, index) => (
            <SegmentView
              key={index}
              segment={segment}
              segmentIndex={index}
              segments={renderedMessage.segments}
              isStreaming={isStreaming}
              requestId={effectiveRequestId}
            />
          ))}
        </div>
      </div>
    )
  }

  return (
    <div
      className={`group/message ${compact ? 'px-2 py-2' : 'px-4 py-3'} ${isUser ? 'flex justify-end' : ''}`}
    >
      <div
        className={`flex ${compact ? 'gap-2' : 'gap-3'} w-full ${isUser ? (compact ? 'max-w-[92%] flex-row-reverse' : 'max-w-[80%] sm:max-w-[70%] lg:max-w-[70%] flex-row-reverse') : ''}`}
      >
        {/* Avatar */}
        <div className="shrink-0 self-start">
          {isUser ? (
            <div
              className={`${compact ? 'h-5 w-5' : 'h-7 w-7'} rounded-full bg-primary/15 flex items-center justify-center`}
            >
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
        <div
          className={`min-w-0 max-w-full ${isUser ? (userMeta ? 'relative flex flex-col items-end' : 'w-fit') : 'w-full'}`}
        >
          {/* Role label */}
          {!userMeta && (
            <div
              className={`flex items-center gap-2 mb-1 ${isUser ? 'justify-end flex-row-reverse' : ''}`}
            >
              <span className={`text-[11px] font-medium text-muted-foreground/80`}>
                {isUser ? 'You' : agentName}
              </span>
              {!isUser && message.continuation && (
                <span className="inline-flex items-center gap-1 text-[10px] text-muted-foreground/60 select-none">
                  <CornerDownRight className="h-3 w-3" aria-hidden="true" />
                  {t('chat.continuedReply')}
                </span>
              )}
              {message.timestamp && (
                <span className="text-[10px] text-muted-foreground/40 font-normal select-none">
                  {formatTimestamp(message.timestamp)}
                </span>
              )}
            </div>
          )}

          {/* Bubble content */}
          <div
            className={
              isUser
                ? `${compact ? 'rounded-xl px-2.5 py-2' : 'rounded-2xl px-4 py-2.5'} max-w-full bg-muted/60 text-foreground rounded-br-md shadow-sm border border-border/40`
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
                        isStreaming={messageIsRunning}
                        stateKey={JSON.stringify([
                          currentSessionId,
                          workedStateKey(message, item.id),
                        ])}
                        isUser={isUser}
                        onUserInteraction={onUserInteraction}
                        requestId={effectiveRequestId}
                      />
                    )
                  } else if (item.type === 'delegation_group') {
                    return (
                      <DelegationGroupView
                        key={item.id}
                        group={item}
                        isUser={isUser}
                        segments={renderedMessage.segments}
                        isStreaming={isStreaming}
                        onUserInteraction={onUserInteraction}
                        requestId={effectiveRequestId}
                      />
                    )
                  } else {
                    return (
                      <SegmentView
                        key={item.index}
                        segment={item.segment}
                        isUser={isUser}
                        segmentIndex={item.index}
                        segments={renderedMessage.segments}
                        isStreaming={isStreaming}
                        onUserInteraction={onUserInteraction}
                        requestId={effectiveRequestId}
                      />
                    )
                  }
                })}
                {isUser && message.queuedImageCount && message.queuedImageCount > 0 && <span className="text-xs text-muted-foreground">{t('observability.images')} ({message.queuedImageCount})</span>}
                {isUser && message.queued && <span className="text-xs text-muted-foreground">{t('observability.queued')}</span>}
                {isUser && message.queueCancelled && <span className="text-xs text-muted-foreground">{t('observability.cancelled')}</span>}
                {isUser && message.queueError && <span className="text-xs text-destructive">{t('observability.callError')}</span>}
                {/* Render uploaded files/images here */}
                {message.files && message.files.length > 0 && (
                  <div className="flex flex-wrap gap-2 mt-2 pt-1.5 border-t border-dashed border-border/10">
                    {message.files.map((file, idx) => (
                      <MessageAttachment key={`${file.path}-${idx}`} file={file} />
                    ))}
                  </div>
                )}

                {/* Surface-level image gallery for agent-generated images */}
                <MessageImageGallery segments={message.segments} isUser={isUser} />
              </div>
            )}
          </div>

          {!isUser && currentSessionId && effectiveRequestId && (
            <ExecutionDetails sessionId={currentSessionId} requestId={effectiveRequestId} running={messageIsRunning} />
          )}

          {/* Actions bar */}
          {!isEmpty && (
            <div
              className={`flex items-center gap-1 opacity-0 group-hover/message:opacity-100 transition-opacity ${userMeta ? 'absolute top-full right-0 pt-1' : 'mt-1.5'}`}
            >
              {userMeta && (
                <span className="mr-1 text-[10px] text-muted-foreground/40 font-normal select-none">
                  {['You', message.timestamp && formatTimestamp(message.timestamp)]
                    .filter(Boolean)
                    .join(' ')}
                </span>
              )}
              {isUser && message.timestamp && (
                <>
                  <button
                    type="button"
                    onClick={() => setConfirmAction('rewind')}
                    className="p-1 rounded text-muted-foreground/40 hover:text-primary hover:bg-primary/10 transition-colors cursor-pointer"
                    title={t('chat.rewindAndEdit') || 'Rewind & Edit'}
                  >
                    <RotateCcw className="h-3 w-3" />
                  </button>
                  <button
                    type="button"
                    onClick={() => setConfirmAction('delete')}
                    className="p-1 rounded text-muted-foreground/40 hover:text-destructive hover:bg-destructive/10 transition-colors cursor-pointer"
                    title={t('chat.deleteMessage') || 'Delete'}
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                </>
              )}
              <CopyButton
                text={extractFullContent(message)}
                label={isUser ? 'Copy message' : 'Copy response'}
              />
            </div>
          )}
        </div>
      </div>
      <ConfirmDialog
        open={confirmAction !== null}
        onOpenChange={(open) => {
          if (!open && !actionLoading) setConfirmAction(null)
        }}
        title={confirmAction === 'rewind' ? t('chat.rewindAndEdit') : t('chat.deleteMessage')}
        message={confirmAction === 'rewind' ? t('chat.confirmRewind') : t('chat.confirmDelete')}
        confirmLabel={confirmAction === 'rewind' ? t('common.confirm') : t('chat.deleteMessage')}
        loading={actionLoading}
        onConfirm={handleConfirmAction}
      />
    </div>
  )
}

/**
 * ChatMessageView is wrapped in React.memo so that re-renders triggered by
 * per-token transcript frames skip previously-rendered messages whose props
 * have not changed. The transcript presentation keeps the identity of every
 * bubble whose items did not change, so this memoization hits on every token.
 */
export const ChatMessageView = memo(
  ChatMessageViewInner,
  (prev, next) =>
    prev.message === next.message &&
    prev.modelName === next.modelName &&
    prev.agentName === next.agentName &&
    prev.isStreaming === next.isStreaming &&
    prev.onUserInteraction === next.onUserInteraction &&
    prev.sessionId === next.sessionId &&
    prev.sessionAgentId === next.sessionAgentId
)

function CopyButton({ text, label = 'Copy' }: { text: string; label?: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    try {
      try {
        if (!navigator.clipboard?.writeText) throw new Error('Clipboard API unavailable')
        await navigator.clipboard.writeText(text)
      } catch {
        copyTextWithDom(text)
      }
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

function copyTextWithDom(text: string) {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.readOnly = true
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()

  try {
    if (!document.execCommand?.('copy')) throw new Error('DOM copy failed')
  } finally {
    textarea.remove()
  }
}

function displaySegments(message: ChatMessage): ChatMessage['segments'] {
  if (message.role !== 'user') return message.segments
  return message.segments.map((segment) => {
    if (segment.type !== 'content') return segment
    // Channel prompts append these annotations for the model, not the transcript.
    const text = segment.text
      .replace(/(?:^|\r?\n)\[User uploaded images, processed by visual recognition\]\s*$/g, '')
      .replace(
        /(?:^|\r?\n)\[(?:User uploaded files, saved locally:|User uploaded attachments:|Uploaded files:|User has uploaded a file, saved locally at:)[\s\S]*?\]\s*$/g,
        ''
      )
      .replace(/(?:^|\r?\n)\[System: The user included [\s\S]*?\]\s*$/g, '')
    return text === segment.text ? segment : { ...segment, text: text.trimEnd() }
  })
}

function extractFullContent(msg: ChatMessage): string {
  return displaySegments(msg)
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
