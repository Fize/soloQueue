import { AlertCircle, Check, ChevronDown, Copy, Loader2 } from 'lucide-react'
import { StreamdownPreview } from '@/components/ui/streamdown-preview'
import { LongMarkdown } from '@/components/ui/long-markdown'
import { DelegationCard } from '@/components/DelegationCard'
import { ThinkingSegment } from './ThinkingSegment'
import { ToolCallSegment } from './ToolCallSegment'
import { SubagentCard } from './WorkedSegment'
import { useTranslation } from '@/lib/i18n'
import { memo, useCallback, useEffect, useId, useRef, useState } from 'react'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import type { ChatMessage } from '@/types'

export function LoadingIndicator() {
  const { t } = useTranslation()
  return (
    <div className="flex items-center gap-2 py-1">
      <span className="inline-flex gap-0.5">
        <span
          className={`h-1.5 w-1.5 rounded-full animate-bounce [animation-delay:0ms] bg-signal`}
        />
        <span
          className={`h-1.5 w-1.5 rounded-full animate-bounce [animation-delay:150ms] bg-signal`}
        />
        <span
          className={`h-1.5 w-1.5 rounded-full animate-bounce [animation-delay:300ms] bg-signal`}
        />
      </span>
      <span
        className={`text-sm text-muted-foreground/60`}
      >
        {t('chat.thinking')}
      </span>
    </div>
  )
}

type CompactStatus = 'running' | 'completed' | 'failed' | 'interrupted'

// A committed compact event row only exists once a summary does, so the
// terminal state is `completed`; failures surface as compaction notices.
const COMPACT_ICON: Record<CompactStatus, { className: string; size: string }> = {
  running: { className: 'text-signal', size: 'h-3 w-3' },
  completed: { className: 'text-success', size: 'h-3.5 w-3.5' },
  failed: { className: 'text-destructive', size: 'h-3.5 w-3.5' },
  interrupted: { className: 'text-warning', size: 'h-3.5 w-3.5' },
}

function CompactSegment({ text, status = 'completed' }: { text: string; status?: CompactStatus }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const copyTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const summary = text.trim()
  const expandable = summary.length > 0
  const bodyId = `compact-body-${useId()}`
  const icon = COMPACT_ICON[status]

  useEffect(() => () => clearTimeout(copyTimer.current), [])

  const copy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      clearTimeout(copyTimer.current)
      copyTimer.current = setTimeout(() => setCopied(false), 1200)
    } catch {
      toast.error(t('common.failedToCopy'))
    }
  }, [text, t])

  return (
    <div className="py-0.5">
      {expandable ? (
        <button
          type="button"
          aria-expanded={open}
          aria-controls={bodyId}
          aria-label={open ? t('chat.compactEventCollapse') : t('chat.compactEventExpand')}
          onClick={() => setOpen((prev) => !prev)}
          className="flex h-7 w-full items-center gap-1.5 rounded-lg bg-transparent px-2 text-left transition-colors hover:bg-white/[0.04] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#8AB4FF] cursor-pointer"
        >
          {status === 'running' ? (
            <Loader2 aria-hidden="true" className={cn(icon.size, icon.className, 'animate-spin')} />
          ) : (
            <Check aria-hidden="true" className={cn(icon.size, icon.className)} strokeWidth={2} />
          )}
          <span className="truncate text-[12px] font-medium leading-4 text-[#9CA3AF]">
            {t('common.contextCompacted')}
          </span>
          <ChevronDown
            aria-hidden="true"
            className={cn(
              'ml-auto h-3 w-3 shrink-0 text-[#9CA3AF] transition-transform duration-150',
              open && 'rotate-180',
            )}
          />
        </button>
      ) : (
        <div className="flex h-7 items-center gap-1.5 px-2">
          <Check aria-hidden="true" className={cn(icon.size, icon.className)} strokeWidth={2} />
          <span className="truncate text-[12px] font-medium leading-4 text-[#9CA3AF]">
            {t('common.contextCompacted')}
          </span>
        </div>
      )}

      {expandable && open && (
        <div id={bodyId} className="mt-1.5 rounded-lg bg-white/[0.04] px-3 pb-3 pt-2">
          <div className="flex h-6 items-center justify-between">
            <span className="text-[11px] leading-4 text-[#9CA3AF]">{t('chat.contextCompaction')}</span>
            <button
              type="button"
              onClick={copy}
              aria-label={t('chat.compactEventCopy')}
              title={t('chat.compactEventCopy')}
              className="flex h-6 w-6 items-center justify-center rounded-md text-[#9CA3AF] transition-colors hover:bg-white/[0.06] hover:text-[#EDEDF0] cursor-pointer focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#8AB4FF]"
            >
              {copied ? (
                <Check aria-hidden="true" className="h-3.5 w-3.5 text-success" />
              ) : (
                <Copy aria-hidden="true" className="h-3.5 w-3.5" />
              )}
            </button>
          </div>
          <div className="max-h-[320px] overflow-y-auto overscroll-contain">
            <StreamdownPreview content={text} className="text-[12px] font-normal leading-[18px] text-[#9CA3AF]" />
          </div>
          <p aria-live="polite" className="sr-only">
            {copied ? t('common.copiedSuccess') : ''}
          </p>
        </div>
      )}
    </div>
  )
}

function SegmentViewInner({
  segment,
  isUser,
  segmentIndex,
  segments,
  isStreaming,
  onUserInteraction,
  requestId,
}: {
  segment: ChatMessage['segments'][number]
  isUser?: boolean
  segmentIndex?: number
  segments?: ChatMessage['segments']
  /** True if the parent message is in an actively-streaming session. */
  isStreaming?: boolean
  onUserInteraction?: () => void
  requestId?: string
}) {
  const isLastSegment =
    segmentIndex != null && segments != null && segmentIndex === segments.length - 1
  switch (segment.type) {
    case 'content':
      // LongMarkdown freezes completed blocks and streams only the tail, so a
      // long answer is parsed once instead of once per token.
      return (
        <LongMarkdown
          content={segment.text}
          isAnimating={!!isStreaming}
        />
      )
    case 'compact':
      return (
        <CompactSegment
          text={segment.text}
        />
      )
    case 'thinking':
      return (
        <ThinkingSegment
          segment={segment}
          isLastSegment={isLastSegment}
          onUserInteraction={onUserInteraction}
        />
      )
    case 'tool_call':
      if (segment.name === 'delegate' || segment.name.startsWith('delegate_')) {
        let teamName = 'agent'
        if (segment.name.startsWith('delegate_')) {
          teamName = segment.name.substring(9).replace(/_/g, ' ')
        } else {
          try {
            const parsed = JSON.parse(segment.args)
            if (parsed.target) teamName = parsed.target
          } catch {
            const match = segment.args.match(/"target"\s*:\s*"([^"]+)"/)
            if (match && match[1]) teamName = match[1]
          }
        }
        return (
          <DelegationCard
            name={teamName}
            args={segment.args}
            callId={segment.callId}
            done={segment.done}
            result={segment.result}
            error={segment.error}
            durationMs={segment.durationMs}
            agentInstanceId={segment.agentInstanceId}
            dispatchId={segment.dispatchId}
            requestId={requestId}
          />
        )
      }
      if (segment.name === 'request_team_help') {
        let teamName = 'peer team'
        try {
          const args = JSON.parse(segment.args)
          if (args.team_name) teamName = args.team_name
        } catch {
          // ignore parse errors
        }
        return (
          <DelegationCard
            name={teamName}
            args={segment.args}
            callId={segment.callId}
            done={segment.done}
            result={segment.result}
            error={segment.error}
            durationMs={segment.durationMs}
            agentInstanceId={segment.agentInstanceId}
            dispatchId={segment.dispatchId}
            requestId={requestId}
          />
        )
      }
      return (
        <ToolCallSegment
          segment={segment}
          isUser={isUser}
          onUserInteraction={onUserInteraction}
        />
      )
    case 'delegation':
      return <SubagentCard segment={segment} requestId={requestId} />
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

/**
 * SegmentView is wrapped in React.memo so that a parent re-render caused
 * by per-token chat_chunk updates skips segments whose props have not
 * changed. The `segment` reference is stable for already-finalized
 * segments; only the actively-streaming content/thinking segment's
 * reference changes (and it is the only one that needs to re-render).
 */
export const SegmentView = memo(
  SegmentViewInner,
  (prev, next) =>
    prev.segment === next.segment &&
    prev.segments === next.segments &&
    prev.segmentIndex === next.segmentIndex &&
    prev.isUser === next.isUser &&
    prev.isStreaming === next.isStreaming &&
    prev.onUserInteraction === next.onUserInteraction &&
    prev.requestId === next.requestId,
)
